package agileconfig

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestTransport(serverURL, appID, secret string, timeout time.Duration, opts ...Option) *transport {
	o := defaultOptions()
	o.httpTimeout = timeout
	WithInsecureHTTP()(&o)
	for _, opt := range opts {
		opt(&o)
	}
	return newTransport(serverURL, appID, secret, o)
}

func TestTransport_FetchConfigs(t *testing.T) {
	configs := []apiConfig{
		{AppId: "app1", Group: "db", Key: "host", Value: "localhost"},
		{AppId: "app1", Group: "", Key: "port", Value: "3306"},
	}
	body, _ := json.Marshal(configs)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/Config/app/app1" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("env") != "DEV" {
			t.Errorf("unexpected env: %s", r.URL.Query().Get("env"))
		}
		auth := r.Header.Get("Authorization")
		expected := "Basic " + base64.StdEncoding.EncodeToString([]byte("app1:secret1"))
		if auth != expected {
			t.Errorf("unexpected auth header: %s", auth)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("publish-time-line-id", "timeline-123")
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	tp := newTestTransport(srv.URL, "app1", "secret1", 5*time.Second)
	result, timelineID, err := tp.fetchConfigs(context.Background(), "DEV")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if timelineID != "timeline-123" {
		t.Fatalf("expected timeline-123, got %s", timelineID)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 configs, got %d", len(result))
	}
	if result[0].Key != "host" || result[0].Group != "db" {
		t.Fatalf("unexpected first config: %+v", result[0])
	}
	if result[1].Key != "port" || result[1].Group != "" {
		t.Fatalf("unexpected second config: %+v", result[1])
	}
}

func TestTransport_FetchConfigs_RejectsInsecureHTTPByDefault(t *testing.T) {
	tp := newTransport("http://example.com", "app1", "secret1", defaultOptions())

	_, _, err := tp.fetchConfigs(context.Background(), "")
	if err == nil {
		t.Fatal("expected insecure HTTP error")
	}
	if !strings.Contains(err.Error(), "WithInsecureHTTP") {
		t.Fatalf("expected WithInsecureHTTP guidance, got %v", err)
	}
}

func TestTransport_FetchConfigs_RejectsHTTPSDowngradeRedirect(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("unexpected insecure redirect target request")
	}))
	defer target.Close()

	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/api/Config/app/app1", http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	o := defaultOptions()
	tp := newTransport(source.URL, "app1", "secret1", o)
	tp.client.Transport = source.Client().Transport

	_, _, err := tp.fetchConfigs(context.Background(), "")
	if err == nil {
		t.Fatal("expected insecure redirect error")
	}
	if !strings.Contains(err.Error(), "refusing redirect to insecure URL") {
		t.Fatalf("expected insecure redirect error, got %v", err)
	}
}

func TestTransport_FetchConfigs_AllowsHTTPRedirectWhenInsecureEnabled(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]apiConfig{{AppId: "app1", Key: "a", Value: "1"}})
	}))
	defer target.Close()

	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/api/Config/app/app1", http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	o := defaultOptions()
	WithInsecureHTTP()(&o)
	tp := newTransport(source.URL, "app1", "secret1", o)
	tp.client.Transport = source.Client().Transport

	configs, _, err := tp.fetchConfigs(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(configs) != 1 || configs[0].Key != "a" {
		t.Fatalf("unexpected configs: %+v", configs)
	}
}

func TestTransport_FetchConfigs_NormalizesTrailingSlash(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/Config/app/app1" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]apiConfig{{AppId: "app1", Key: "a", Value: "1"}})
	}))
	defer srv.Close()

	tp := newTestTransport(srv.URL+"/", "app1", "secret1", 5*time.Second)
	if _, _, err := tp.fetchConfigs(context.Background(), ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTransport_FetchConfigs_LimitsSuccessResponseBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"AppId":"app1","Key":"a","Value":"`))
		w.Write([]byte(strings.Repeat("x", 256)))
		w.Write([]byte(`"}]`))
	}))
	defer srv.Close()

	tp := newTestTransport(srv.URL, "app1", "secret1", 5*time.Second, WithMaxResponseBody(32))
	_, _, err := tp.fetchConfigs(context.Background(), "")
	if err == nil {
		t.Fatal("expected response size error")
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) && !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected response size or EOF error, got %v", err)
	}
}

func TestTransport_FetchConfigs_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	tp := newTestTransport(srv.URL, "app1", "secret1", 5*time.Second)
	_, _, err := tp.fetchConfigs(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestTransport_FetchConfigs_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	tp := newTestTransport(srv.URL, "app1", "wrong-secret", 5*time.Second)
	_, _, err := tp.fetchConfigs(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
}

func TestTransport_FetchConfigs_ServerErrorLimitsResponseBody(t *testing.T) {
	largeBody := strings.Repeat("x", maxErrorResponseBody+1024)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, largeBody, http.StatusInternalServerError)
	}))
	defer srv.Close()

	tp := newTestTransport(srv.URL, "app1", "secret1", 5*time.Second)
	_, _, err := tp.fetchConfigs(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}

	if len(err.Error()) > maxErrorResponseBody+128 {
		t.Fatalf("expected limited error body, got error length %d", len(err.Error()))
	}
}
