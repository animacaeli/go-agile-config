package agileconfig

import (
	"testing"
	"time"
)

func TestWithHTTPTimeout_NonPositiveDuration_UsesDefault(t *testing.T) {
	opts := defaultOptions()

	WithHTTPTimeout(0)(&opts)
	if opts.httpTimeout != defaultHTTPTimeout {
		t.Fatalf("expected default HTTP timeout, got %s", opts.httpTimeout)
	}

	WithHTTPTimeout(-1 * time.Second)(&opts)
	if opts.httpTimeout != defaultHTTPTimeout {
		t.Fatalf("expected default HTTP timeout for negative duration, got %s", opts.httpTimeout)
	}
}

func TestWithWSRetryMaxInterval_NonPositiveDuration_UsesDefault(t *testing.T) {
	opts := defaultOptions()

	WithWSRetryMaxInterval(0)(&opts)
	if opts.wsRetryMaxInterval != defaultWSRetryMaxInterval {
		t.Fatalf("expected default retry interval, got %s", opts.wsRetryMaxInterval)
	}

	WithWSRetryMaxInterval(-1 * time.Second)(&opts)
	if opts.wsRetryMaxInterval != defaultWSRetryMaxInterval {
		t.Fatalf("expected default retry interval for negative duration, got %s", opts.wsRetryMaxInterval)
	}
}
