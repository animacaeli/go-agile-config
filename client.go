package agileconfig

import (
	"context"
	"fmt"
	"log"
	"math"
	"sync"
	"time"
)

// Client is the main entry point for interacting with AgileConfig.
// It fetches configurations via HTTP and listens for real-time changes via WebSocket.
type Client struct {
	tp    *transport
	store *configStore
	opts  options

	lifecycleMu sync.Mutex
	started     bool
	generation  uint64
	ctx         context.Context
	cancel      context.CancelFunc

	timelineMu sync.RWMutex
	timelineID string

	ws           *wsClient
	wsMu         sync.Mutex
	reconnMu     sync.Mutex
	reconnecting bool
}

// NewClient creates a new AgileConfig client.
// Call Start() to load configs and establish WebSocket connection.
func NewClient(serverURL, appID, secret string, opts ...Option) *Client {
	o := defaultOptions()
	for _, opt := range opts {
		opt(&o)
	}

	tp := newTransport(serverURL, appID, secret, o)

	return &Client{
		tp:    tp,
		store: newConfigStore(),
		opts:  o,
	}
}

// Start fetches configs from the server via HTTP and starts a WebSocket connection
// for real-time updates. Returns an error if the initial config fetch fails.
func (c *Client) Start(ctx context.Context) error {
	c.lifecycleMu.Lock()
	if c.started {
		c.lifecycleMu.Unlock()
		return fmt.Errorf("client already started")
	}
	c.generation++
	generation := c.generation
	c.ctx, c.cancel = context.WithCancel(ctx)
	c.started = true
	c.lifecycleMu.Unlock()

	if err := c.loadConfigs(c.ctx); err != nil {
		c.cancel()
		c.lifecycleMu.Lock()
		c.started = false
		c.lifecycleMu.Unlock()
		return fmt.Errorf("initial config load: %w", err)
	}

	c.startWebSocket(generation)

	return nil
}

// Stop gracefully shuts down the client, closing the WebSocket connection.
func (c *Client) Stop() {
	c.lifecycleMu.Lock()
	if !c.started {
		c.lifecycleMu.Unlock()
		return
	}
	c.started = false
	if c.cancel != nil {
		c.cancel()
	}
	c.lifecycleMu.Unlock()

	// Stop new reconnect attempts and close the current WebSocket if one exists.
	c.reconnMu.Lock()
	c.wsMu.Lock()
	if c.ws != nil {
		c.ws.close()
	}
	c.wsMu.Unlock()
	c.reconnMu.Unlock()
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
	configs, timelineID, err := c.tp.fetchConfigs(ctx, c.opts.env)
	if err != nil {
		return err
	}
	c.setTimelineID(timelineID)

	data := make(map[string]string, len(configs))
	for _, cfg := range configs {
		key := storeKey(cfg.Group, cfg.Key)
		data[key] = cfg.Value
	}

	changed := c.store.reload(data)
	if len(changed) > 0 {
		c.notifyChange(changed)
	}

	return nil
}

// wsActionHandler returns a shared WebSocket action callback.
func (c *Client) wsActionHandler(generation uint64) func(websocketAction) {
	return func(action websocketAction) {
		switch action.Action {
		case "reload":
			ctx, ok := c.activeContext(generation)
			if !ok {
				return
			}
			if err := c.loadConfigs(ctx); err != nil {
				log.Printf("agileconfig: reload failed: %v", err)
			}
		case "offline":
			go c.tryReconnect(generation)
		case "ping":
			ctx, ok := c.activeContext(generation)
			if !ok {
				return
			}
			c.reloadIfTimelineChanged(ctx, action.Data)
		}
	}
}

// startWebSocket establishes a WebSocket connection in a background goroutine.
func (c *Client) startWebSocket(generation uint64) {
	url, err := buildWSURL(c.tp.getServerURL())
	if err != nil {
		log.Printf("agileconfig: %v", err)
		return
	}

	ws := newWSClient(url, c.tp.getAppID(), c.tp.getSecret(), c.opts.env, c.opts.httpTimeout,
		c.opts.maxWSMessageSize,
		c.wsActionHandler(generation),
		func(ws *wsClient) {
			if c.shouldReconnect(generation, ws) {
				go c.tryReconnect(generation)
			}
		},
	)
	c.setWS(ws)

	go func() {
		ctx, ok := c.activeContext(generation)
		if !ok {
			return
		}
		if err := ws.connect(ctx); err != nil {
			log.Printf("agileconfig: websocket connect failed: %v", err)
			if c.shouldReconnect(generation, ws) {
				go c.tryReconnect(generation)
			}
			return
		}
		c.startPing(generation, ws)
	}()
}

// startPing sends periodic heartbeat pings via WebSocket.
func (c *Client) startPing(generation uint64, ws *wsClient) {
	interval := c.opts.wsPingInterval
	if interval <= 0 {
		interval = defaultWSPingInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		ctx, ok := c.activeContext(generation)
		if !ok {
			return
		}

		select {
		case <-ticker.C:
			if err := ws.send("c:ping"); err != nil {
				if c.shouldReconnect(generation, ws) {
					go c.tryReconnect(generation)
				}
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func (c *Client) reloadIfTimelineChanged(ctx context.Context, timelineID string) {
	if timelineID == "" || timelineID == c.getTimelineID() {
		return
	}
	if err := c.loadConfigs(ctx); err != nil {
		log.Printf("agileconfig: reload failed: %v", err)
	}
}

func (c *Client) setTimelineID(timelineID string) {
	if timelineID == "" {
		return
	}
	c.timelineMu.Lock()
	c.timelineID = timelineID
	c.timelineMu.Unlock()
}

func (c *Client) getTimelineID() string {
	c.timelineMu.RLock()
	defer c.timelineMu.RUnlock()
	return c.timelineID
}

func (c *Client) setWS(ws *wsClient) {
	c.wsMu.Lock()
	c.ws = ws
	c.wsMu.Unlock()
}

func (c *Client) closeCurrentWS(generation uint64) bool {
	c.lifecycleMu.Lock()
	if !c.started || c.ctx == nil || c.generation != generation {
		c.lifecycleMu.Unlock()
		return false
	}
	c.wsMu.Lock()
	ws := c.ws
	c.wsMu.Unlock()
	c.lifecycleMu.Unlock()

	if ws != nil {
		ws.close()
	}
	return true
}

func (c *Client) replaceWS(generation uint64, ws *wsClient) bool {
	c.lifecycleMu.Lock()
	if !c.started || c.ctx == nil || c.generation != generation {
		c.lifecycleMu.Unlock()
		return false
	}
	c.wsMu.Lock()
	c.ws = ws
	c.wsMu.Unlock()
	c.lifecycleMu.Unlock()
	return true
}

func (c *Client) getWS() *wsClient {
	c.wsMu.Lock()
	defer c.wsMu.Unlock()
	return c.ws
}

func (c *Client) activeContext(generation uint64) (context.Context, bool) {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	return c.ctx, c.started && c.ctx != nil && c.generation == generation
}

func (c *Client) shouldReconnect(generation uint64, ws *wsClient) bool {
	if _, ok := c.activeContext(generation); !ok {
		return false
	}

	c.wsMu.Lock()
	defer c.wsMu.Unlock()
	return c.ws == ws
}

// tryReconnect guards reconnect with a mutex to prevent concurrent reconnection attempts.
func (c *Client) tryReconnect(generation uint64) {
	if _, ok := c.activeContext(generation); !ok {
		return
	}

	c.reconnMu.Lock()
	if c.reconnecting {
		c.reconnMu.Unlock()
		return
	}
	c.reconnecting = true
	c.reconnMu.Unlock()

	defer func() {
		c.reconnMu.Lock()
		c.reconnecting = false
		c.reconnMu.Unlock()
	}()

	c.reconnect(generation)
}

// reconnect attempts to re-establish the WebSocket connection with exponential backoff.
func (c *Client) reconnect(generation uint64) {
	backoff := 1 * time.Second
	maxInterval := c.opts.wsRetryMaxInterval
	if maxInterval <= 0 {
		maxInterval = defaultWSRetryMaxInterval
	}
	if backoff > maxInterval {
		backoff = maxInterval
	}

	for {
		ctx, ok := c.activeContext(generation)
		if !ok {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		if ok := c.closeCurrentWS(generation); !ok {
			return
		}

		url, err := buildWSURL(c.tp.getServerURL())
		if err != nil {
			log.Printf("agileconfig: %v", err)
			return
		}
		ws := newWSClient(url, c.tp.getAppID(), c.tp.getSecret(), c.opts.env, c.opts.httpTimeout,
			c.opts.maxWSMessageSize,
			c.wsActionHandler(generation),
			func(ws *wsClient) {
				if c.shouldReconnect(generation, ws) {
					go c.tryReconnect(generation)
				}
			},
		)

		if ok := c.replaceWS(generation, ws); !ok {
			ws.close()
			return
		}

		if err := ws.connect(ctx); err != nil {
			log.Printf("agileconfig: reconnect failed: %v", err)
			backoff = time.Duration(math.Min(float64(backoff*2), float64(maxInterval)))
			continue
		}

		if err := c.loadConfigs(ctx); err != nil {
			log.Printf("agileconfig: post-reconnect load failed: %v", err)
		}

		go c.startPing(generation, ws)
		return
	}
}

func (c *Client) notifyChange(changed []string) {
	if c.opts.onChange == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Printf("agileconfig: onChange panic: %v", r)
		}
	}()
	c.opts.onChange(append([]string(nil), changed...))
}
