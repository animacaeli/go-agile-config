package agileconfig

import (
	"context"
	"fmt"
	"sync"
)

// MultiClientApp describes one AgileConfig app managed by a MultiClient.
type MultiClientApp struct {
	AppID  string
	Secret string
}

// MultiOption configures a MultiClient.
type MultiOption func(*multiOptions)

type multiOptions struct {
	clientOptions []Option
	onChange      func(appID string, changedKeys []string)
}

// MultiClient manages multiple AgileConfig apps behind one SDK entry point.
type MultiClient struct {
	clients map[string]*Client
	order   []string

	lifecycleMu sync.Mutex
	started     bool
}

// NewMultiClient creates a client that manages multiple AgileConfig app IDs.
func NewMultiClient(serverURL string, apps []MultiClientApp, opts ...MultiOption) (*MultiClient, error) {
	if len(apps) == 0 {
		return nil, fmt.Errorf("at least one app is required")
	}

	var o multiOptions
	for _, opt := range opts {
		opt(&o)
	}

	clients := make(map[string]*Client, len(apps))
	order := make([]string, 0, len(apps))
	for _, app := range apps {
		if app.AppID == "" {
			return nil, fmt.Errorf("app ID is required")
		}
		if _, exists := clients[app.AppID]; exists {
			return nil, fmt.Errorf("duplicate app ID %q", app.AppID)
		}

		clientOpts := append([]Option(nil), o.clientOptions...)
		if o.onChange != nil {
			appID := app.AppID
			clientOpts = append(clientOpts, WithOnChange(func(changedKeys []string) {
				o.onChange(appID, changedKeys)
			}))
		}

		clients[app.AppID] = NewClient(serverURL, app.AppID, app.Secret, clientOpts...)
		order = append(order, app.AppID)
	}

	return &MultiClient{
		clients: clients,
		order:   order,
	}, nil
}

// WithMultiClientOptions applies options to every underlying Client.
func WithMultiClientOptions(opts ...Option) MultiOption {
	return func(o *multiOptions) {
		o.clientOptions = append(o.clientOptions, opts...)
	}
}

// WithMultiOnChange registers a callback invoked when an app's config values change.
func WithMultiOnChange(fn func(appID string, changedKeys []string)) MultiOption {
	return func(o *multiOptions) {
		o.onChange = fn
	}
}

// Start starts every managed app client. If any app fails, already-started clients are stopped.
func (m *MultiClient) Start(ctx context.Context) error {
	m.lifecycleMu.Lock()
	if m.started {
		m.lifecycleMu.Unlock()
		return fmt.Errorf("multi client already started")
	}
	m.started = true
	m.lifecycleMu.Unlock()

	started := make([]*Client, 0, len(m.order))
	for _, appID := range m.order {
		client := m.clients[appID]
		if err := client.Start(ctx); err != nil {
			for i := len(started) - 1; i >= 0; i-- {
				started[i].Stop()
			}
			m.lifecycleMu.Lock()
			m.started = false
			m.lifecycleMu.Unlock()
			return fmt.Errorf("start app %q: %w", appID, err)
		}
		started = append(started, client)
	}

	return nil
}

// Stop stops every managed app client.
func (m *MultiClient) Stop() {
	m.lifecycleMu.Lock()
	if !m.started {
		m.lifecycleMu.Unlock()
		return
	}
	m.started = false
	m.lifecycleMu.Unlock()

	for i := len(m.order) - 1; i >= 0; i-- {
		m.clients[m.order[i]].Stop()
	}
}

// Client returns the underlying Client for an app ID.
func (m *MultiClient) Client(appID string) (*Client, bool) {
	client, ok := m.clients[appID]
	return client, ok
}

// Get returns the config value for the given app ID and key.
func (m *MultiClient) Get(appID, key string) (string, bool) {
	client, ok := m.clients[appID]
	if !ok {
		return "", false
	}
	return client.Get(key)
}

// GetString returns the config value for the given app ID and key, or the default value if not found.
func (m *MultiClient) GetString(appID, key, defaultVal string) string {
	if val, ok := m.Get(appID, key); ok {
		return val
	}
	return defaultVal
}

// GetByGroup returns the config value for the given app ID, group, and key.
func (m *MultiClient) GetByGroup(appID, group, key string) (string, bool) {
	client, ok := m.clients[appID]
	if !ok {
		return "", false
	}
	return client.GetByGroup(group, key)
}

// GetAll returns all configs grouped by app ID.
func (m *MultiClient) GetAll() map[string]map[string]string {
	all := make(map[string]map[string]string, len(m.clients))
	for _, appID := range m.order {
		all[appID] = m.clients[appID].GetAll()
	}
	return all
}
