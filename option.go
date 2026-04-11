package agileconfig

import "time"

const (
	defaultHTTPTimeout        = 10 * time.Second
	defaultWSRetryMaxInterval = 30 * time.Second
)

// Option configures a Client.
type Option func(*options)

type options struct {
	env                string
	httpTimeout        time.Duration
	wsRetryMaxInterval time.Duration
	onChange           func(changedKeys []string)
}

func defaultOptions() options {
	return options{
		httpTimeout:        defaultHTTPTimeout,
		wsRetryMaxInterval: defaultWSRetryMaxInterval,
	}
}

// WithEnv sets the environment name for config requests.
func WithEnv(env string) Option {
	return func(o *options) {
		o.env = env
	}
}

// WithHTTPTimeout sets the HTTP request timeout. Default is 10 seconds.
func WithHTTPTimeout(d time.Duration) Option {
	return func(o *options) {
		o.httpTimeout = d
	}
}

// WithWSRetryMaxInterval sets the maximum interval for WebSocket reconnection backoff.
// Default is 30 seconds.
func WithWSRetryMaxInterval(d time.Duration) Option {
	return func(o *options) {
		o.wsRetryMaxInterval = d
	}
}

// WithOnChange registers a callback invoked when config values change.
// The callback receives the list of keys that changed.
func WithOnChange(fn func(changedKeys []string)) Option {
	return func(o *options) {
		o.onChange = fn
	}
}
