package agileconfig

import (
	"context"
	"fmt"
	"log"
	"math"
	"time"
)

// Client is the main entry point for interacting with AgileConfig.
// It fetches configurations via HTTP and listens for real-time changes via WebSocket.
type Client struct {
	tp    *transport
	store *configStore
	ws    *wsClient
	opts  options

	ctx    context.Context
	cancel context.CancelFunc

	pingTicker *time.Ticker
}

// NewClient creates a new AgileConfig client.
// Call Start() to load configs and establish WebSocket connection.
func NewClient(serverURL, appID, secret string, opts ...Option) *Client {
	o := defaultOptions()
	for _, opt := range opts {
		opt(&o)
	}

	tp := newTransport(serverURL, appID, secret, o.httpTimeout)

	return &Client{
		tp:    tp,
		store: newConfigStore(),
		opts:  o,
	}
}

// Start fetches configs from the server via HTTP and starts a WebSocket connection
// for real-time updates. Returns an error if the initial config fetch fails.
func (c *Client) Start(ctx context.Context) error {
	c.ctx, c.cancel = context.WithCancel(ctx)

	if err := c.loadConfigs(c.ctx); err != nil {
		return fmt.Errorf("initial config load: %w", err)
	}

	c.startWebSocket()

	return nil
}

// Stop gracefully shuts down the client, closing the WebSocket connection.
func (c *Client) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
	if c.ws != nil {
		c.ws.close()
	}
	if c.pingTicker != nil {
		c.pingTicker.Stop()
	}
}

// Get returns the config value for the given key and whether it exists.
// Keys are stored as "group:key" or just "key" (no group).
func (c *Client) Get(key string) (string, bool) {
	return c.store.get(key)
}

// GetString returns the config value for the given key, or the default value if not found.
func (c *Client) GetString(key, defaultVal string) string {
	if val, ok := c.store.get(key); ok {
		return val
	}
	return defaultVal
}

// GetByGroup returns the config value for the given group and key.
func (c *Client) GetByGroup(group, key string) (string, bool) {
	return c.store.getByGroup(group, key)
}

// GetAll returns a copy of all current config key-value pairs.
func (c *Client) GetAll() map[string]string {
	return c.store.getAll()
}

// loadConfigs fetches all published configs from the server and updates the store.
func (c *Client) loadConfigs(ctx context.Context) error {
	configs, _, err := c.tp.fetchConfigs(ctx, c.opts.env)
	if err != nil {
		return err
	}

	data := make(map[string]string, len(configs))
	for _, cfg := range configs {
		key := storeKey(cfg.Group, cfg.Key)
		data[key] = cfg.Value
	}

	changed := c.store.reload(data)
	if len(changed) > 0 && c.opts.onChange != nil {
		c.opts.onChange(changed)
	}

	return nil
}

// startWebSocket establishes a WebSocket connection in a background goroutine.
func (c *Client) startWebSocket() {
	url := buildWSURL(c.tp.serverURL)

	c.ws = newWSClient(url, c.tp.appID, c.tp.secret, c.opts.env, c.opts.httpTimeout,
		func(action websocketAction) {
			switch action.Action {
			case "reload":
				if err := c.loadConfigs(c.ctx); err != nil {
					log.Printf("agileconfig: reload failed: %v", err)
				}
			case "offline":
				go c.reconnect()
			case "ping":
				// Heartbeat response, no action needed
			}
		},
	)

	go func() {
		if err := c.ws.connect(c.ctx); err != nil {
			log.Printf("agileconfig: websocket connect failed: %v", err)
			go c.reconnect()
			return
		}
		c.startPing()
	}()
}

// startPing sends periodic heartbeat pings via WebSocket.
func (c *Client) startPing() {
	c.pingTicker = time.NewTicker(30 * time.Second)
	defer c.pingTicker.Stop()

	for {
		select {
		case <-c.pingTicker.C:
			if err := c.ws.send("c:ping"); err != nil {
				return
			}
		case <-c.ctx.Done():
			return
		}
	}
}

// reconnect attempts to re-establish the WebSocket connection with exponential backoff.
func (c *Client) reconnect() {
	backoff := 1 * time.Second
	maxInterval := c.opts.wsRetryMaxInterval

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-time.After(backoff):
		}

		if c.ws != nil {
			c.ws.close()
		}

		url := buildWSURL(c.tp.serverURL)
		c.ws = newWSClient(url, c.tp.appID, c.tp.secret, c.opts.env, c.opts.httpTimeout,
			func(action websocketAction) {
				switch action.Action {
				case "reload":
					if err := c.loadConfigs(c.ctx); err != nil {
						log.Printf("agileconfig: reload failed: %v", err)
					}
				case "offline":
					go c.reconnect()
				}
			},
		)

		if err := c.ws.connect(c.ctx); err != nil {
			log.Printf("agileconfig: reconnect failed: %v", err)
			backoff = time.Duration(math.Min(float64(backoff*2), float64(maxInterval)))
			continue
		}

		if err := c.loadConfigs(c.ctx); err != nil {
			log.Printf("agileconfig: post-reconnect load failed: %v", err)
		}

		go c.startPing()
		return
	}
}
