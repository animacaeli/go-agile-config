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

func TestTransport_ListServices(t *testing.T) {
	port := 8080
	services := []ServiceInfo{
		{
			ServiceID:   "orders",
			ServiceName: "orders-api",
			IP:          "10.0.0.1",
			Port:        &port,
			MetaData:    []string{"version=1"},
			Status:      ServiceStatusHealthy,
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/RegisterCenter/services" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}

		auth := r.Header.Get("Authorization")
		expected := "Basic " + base64.StdEncoding.EncodeToString([]byte("app1:secret1"))
		if auth != expected {
			t.Errorf("unexpected auth header: %s", auth)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(services)
	}))
	defer srv.Close()

	tp := newTestTransport(srv.URL, "app1", "secret1", 5*time.Second)
	result, err := tp.listServices(context.Background(), ServiceQueryStatusAll)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 service, got %d", len(result))
	}
	if result[0].ServiceID != "orders" || result[0].Status != ServiceStatusHealthy {
		t.Fatalf("unexpected service: %+v", result[0])
	}
	if result[0].Port == nil || *result[0].Port != port {
		t.Fatalf("unexpected port: %+v", result[0].Port)
	}
}

func TestTransport_ListServices_StatusPaths(t *testing.T) {
	tests := []struct {
		name   string
		status ServiceQueryStatus
		path   string
	}{
		{name: "online", status: ServiceQueryStatusOnline, path: "/api/RegisterCenter/services/online"},
		{name: "offline", status: ServiceQueryStatusOffline, path: "/api/RegisterCenter/services/offline"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tt.path {
					t.Errorf("unexpected path: %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}

				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode([]ServiceInfo{})
			}))
			defer srv.Close()

			tp := newTestTransport(srv.URL, "app1", "secret1", 5*time.Second)
			if _, err := tp.listServices(context.Background(), tt.status); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestTransport_RegisterService(t *testing.T) {
	port := 8080

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/RegisterCenter" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
			t.Errorf("unexpected content type: %s", contentType)
		}

		var req RegisterService
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.ServiceID != "orders" || req.Port == nil || *req.Port != port {
			t.Fatalf("unexpected request: %+v", req)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(RegisterResult{UniqueID: "service-unique-id"})
	}))
	defer srv.Close()

	tp := newTestTransport(srv.URL, "app1", "secret1", 5*time.Second)
	result, err := tp.registerService(context.Background(), RegisterService{
		ServiceID:     "orders",
		ServiceName:   "orders-api",
		IP:            "10.0.0.1",
		Port:          &port,
		MetaData:      []string{"version=1"},
		HeartbeatMode: HeartbeatModeClient,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.UniqueID != "service-unique-id" {
		t.Fatalf("unexpected unique id: %s", result.UniqueID)
	}
}

func TestTransport_UnregisterService_EscapesUniqueID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/api/RegisterCenter/service%2Fone" {
			t.Errorf("unexpected path: %s", r.URL.EscapedPath())
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodDelete {
			t.Errorf("unexpected method: %s", r.Method)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(RegisterResult{UniqueID: "service/one"})
	}))
	defer srv.Close()

	tp := newTestTransport(srv.URL, "app1", "secret1", 5*time.Second)
	result, err := tp.unregisterService(context.Background(), "service/one")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.UniqueID != "service/one" {
		t.Fatalf("unexpected unique id: %s", result.UniqueID)
	}
}

func TestTransport_Heartbeat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/RegisterCenter/heartbeat" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}

		var req heartbeatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.UniqueID != "service-unique-id" {
			t.Fatalf("unexpected unique id: %s", req.UniqueID)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(HeartbeatResult{
			Module: "RegisterCenter",
			Action: "ping",
			Data:   "services-md5",
		})
	}))
	defer srv.Close()

	tp := newTestTransport(srv.URL, "app1", "secret1", 5*time.Second)
	result, err := tp.heartbeat(context.Background(), "service-unique-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Action != "ping" || result.Data != "services-md5" {
		t.Fatalf("unexpected heartbeat result: %+v", result)
	}
}
