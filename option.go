package agileconfig

import "time"

const (
	defaultHTTPTimeout        = 10 * time.Second
	defaultWSRetryMaxInterval = 30 * time.Second
	defaultWSPingInterval     = 30 * time.Second
	defaultMaxResponseBody    = 10 * 1024 * 1024
	defaultMaxWSMessageSize   = 64 * 1024
)

// Option configures a Client.
type Option func(*options)

type options struct {
	env                string
	httpTimeout        time.Duration
	wsRetryMaxInterval time.Duration
	wsPingInterval     time.Duration
	maxResponseBody    int64
	maxWSMessageSize   int64
	allowInsecureHTTP  bool
	onChange           func(changedKeys []string)
}

func defaultOptions() options {
	return options{
		httpTimeout:        defaultHTTPTimeout,
		wsRetryMaxInterval: defaultWSRetryMaxInterval,
		wsPingInterval:     defaultWSPingInterval,
		maxResponseBody:    defaultMaxResponseBody,
		maxWSMessageSize:   defaultMaxWSMessageSize,
	}
}

// WithEnv sets the environment name for config, WebSocket, and service discovery requests.
func WithEnv(env string) Option {
	return func(o *options) {
		o.env = env
	}
}

// WithHTTPTimeout sets the HTTP request timeout. Default is 10 seconds.
func WithHTTPTimeout(d time.Duration) Option {
	return func(o *options) {
		if d <= 0 {
			o.httpTimeout = defaultHTTPTimeout
			return
		}
		o.httpTimeout = d
	}
}

// WithWSRetryMaxInterval sets the maximum interval for WebSocket reconnection backoff.
// Default is 30 seconds.
func WithWSRetryMaxInterval(d time.Duration) Option {
	return func(o *options) {
		if d <= 0 {
			o.wsRetryMaxInterval = defaultWSRetryMaxInterval
			return
		}
		o.wsRetryMaxInterval = d
	}
}

// WithWSPingInterval sets the interval for checking the server publish timeline.
// Default is 30 seconds.
func WithWSPingInterval(d time.Duration) Option {
	return func(o *options) {
		if d <= 0 {
			o.wsPingInterval = defaultWSPingInterval
			return
		}
		o.wsPingInterval = d
	}
}

// WithMaxResponseBody sets the maximum HTTP config response size in bytes.
// Default is 10 MiB.
func WithMaxResponseBody(n int64) Option {
	return func(o *options) {
		if n <= 0 {
			o.maxResponseBody = defaultMaxResponseBody
			return
		}
		o.maxResponseBody = n
	}
}

// WithMaxWSMessageSize sets the maximum WebSocket message size in bytes.
// Default is 64 KiB.
func WithMaxWSMessageSize(n int64) Option {
	return func(o *options) {
		if n <= 0 {
			o.maxWSMessageSize = defaultMaxWSMessageSize
			return
		}
		o.maxWSMessageSize = n
	}
}

// WithInsecureHTTP allows http:// and ws:// connections.
// It should only be used for trusted local development or private networks.
func WithInsecureHTTP() Option {
	return func(o *options) {
		o.allowInsecureHTTP = true
	}
}

// WithOnChange registers a callback invoked when config values change.
// The callback receives the list of keys that changed.
func WithOnChange(fn func(changedKeys []string)) Option {
	return func(o *options) {
		o.onChange = fn
	}
}
