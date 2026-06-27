package agileconfig

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// testServer creates a mock AgileConfig server for integration tests.
func testServer(configs []apiConfig) *httptest.Server {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	mux := http.NewServeMux()

	mux.HandleFunc("/api/Config/app/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("publish-time-line-id", "test-timeline-1")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(configs)
	})

	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if string(msg) == "c:ping" {
				action := websocketAction{Module: "c", Action: "ping", Data: "test-timeline-1"}
				data, _ := json.Marshal(action)
				conn.WriteMessage(websocket.TextMessage, data)
			}
		}
	})

	return httptest.NewServer(mux)
}

func newTestClient(serverURL, appID, secret string, opts ...Option) *Client {
	opts = append([]Option{WithInsecureHTTP()}, opts...)
	return NewClient(serverURL, appID, secret, opts...)
}

func TestClient_StartAndGet(t *testing.T) {
	configs := []apiConfig{
		{AppId: "app1", Group: "db", Key: "host", Value: "localhost"},
		{AppId: "app1", Group: "db", Key: "port", Value: "3306"},
		{AppId: "app1", Group: "", Key: "app.name", Value: "myapp"},
	}

	srv := testServer(configs)
	defer srv.Close()

	client := newTestClient(srv.URL, "app1", "secret1",
		WithEnv("DEV"),
	)

	err := client.Start(context.Background())
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer client.Stop()

	host, ok := client.Get("db:host")
	if !ok || host != "localhost" {
		t.Fatalf("expected db:host=localhost, got %q, ok=%v", host, ok)
	}

	port, ok := client.Get("db:port")
	if !ok || port != "3306" {
		t.Fatalf("expected db:port=3306, got %q, ok=%v", port, ok)
	}

	name, ok := client.Get("app.name")
	if !ok || name != "myapp" {
		t.Fatalf("expected app.name=myapp, got %q, ok=%v", name, ok)
	}
}

func TestClient_GetString(t *testing.T) {
	configs := []apiConfig{
		{AppId: "app1", Key: "db.host", Value: "localhost"},
	}

	srv := testServer(configs)
	defer srv.Close()

	client := newTestClient(srv.URL, "app1", "secret1")
	err := client.Start(context.Background())
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer client.Stop()

	val := client.GetString("db.host", "fallback")
	if val != "localhost" {
		t.Fatalf("expected localhost, got %s", val)
	}

	val = client.GetString("missing.key", "fallback")
	if val != "fallback" {
		t.Fatalf("expected fallback, got %s", val)
	}
}

func TestClient_GetByGroup(t *testing.T) {
	configs := []apiConfig{
		{AppId: "app1", Group: "database", Key: "host", Value: "127.0.0.1"},
	}

	srv := testServer(configs)
	defer srv.Close()

	client := newTestClient(srv.URL, "app1", "secret1")
	err := client.Start(context.Background())
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer client.Stop()

	val, ok := client.GetByGroup("database", "host")
	if !ok || val != "127.0.0.1" {
		t.Fatalf("expected 127.0.0.1, got %q, ok=%v", val, ok)
	}

	_, ok = client.GetByGroup("cache", "host")
	if ok {
		t.Fatal("expected key to not exist")
	}
}

func TestClient_GetAll(t *testing.T) {
	configs := []apiConfig{
		{AppId: "app1", Key: "a", Value: "1"},
		{AppId: "app1", Key: "b", Value: "2"},
	}

	srv := testServer(configs)
	defer srv.Close()

	client := newTestClient(srv.URL, "app1", "secret1")
	err := client.Start(context.Background())
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer client.Stop()

	all := client.GetAll()
	if len(all) != 2 {
		t.Fatalf("expected 2 configs, got %d", len(all))
	}
	if all["a"] != "1" || all["b"] != "2" {
		t.Fatalf("unexpected values: %v", all)
	}
}

func TestClient_Start_ServerDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := newTestClient(srv.URL, "app1", "secret1")
	err := client.Start(context.Background())
	if err == nil {
		t.Fatal("expected error when server returns 500")
		client.Stop()
	}
}

func TestClient_OnChange(t *testing.T) {
	configs := []apiConfig{
		{AppId: "app1", Key: "a", Value: "1"},
	}

	srv := testServer(configs)
	defer srv.Close()

	var changeMu sync.Mutex
	var changedKeys []string

	client := newTestClient(srv.URL, "app1", "secret1",
		WithOnChange(func(keys []string) {
			changeMu.Lock()
			changedKeys = keys
			changeMu.Unlock()
		}),
	)

	err := client.Start(context.Background())
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer client.Stop()

	// Reset the captured keys after initial Start, which triggers onChange for first load.
	changeMu.Lock()
	changedKeys = nil
	changeMu.Unlock()

	client.loadConfigs(context.Background())

	changeMu.Lock()
	keys := changedKeys
	changeMu.Unlock()

	if len(keys) != 0 {
		t.Fatalf("expected no changed keys for identical data, got %v", keys)
	}
}

func TestClient_OnChangePanicDoesNotPropagate(t *testing.T) {
	configs := []apiConfig{
		{AppId: "app1", Key: "a", Value: "1"},
	}

	srv := testServer(configs)
	defer srv.Close()

	client := newTestClient(srv.URL, "app1", "secret1",
		WithOnChange(func(keys []string) {
			panic("boom")
		}),
	)

	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer client.Stop()

	if value, ok := client.Get("a"); !ok || value != "1" {
		t.Fatalf("expected loaded config after panic, got %q, ok=%v", value, ok)
	}
}

func TestClient_PingWithNewTimeline_ReloadsConfigs(t *testing.T) {
	var fetches int32
	reloaded := make(chan struct{}, 1)

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	mux := http.NewServeMux()

	mux.HandleFunc("/api/Config/app/", func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&fetches, 1)
		w.Header().Set("publish-time-line-id", "timeline-1")
		if n > 1 {
			w.Header().Set("publish-time-line-id", "timeline-2")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]apiConfig{
			{AppId: "app1", Key: "a", Value: string(rune('0' + n))},
		})
	})

	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if string(msg) == "c:ping" {
				action := websocketAction{Module: "c", Action: "ping", Data: "timeline-2"}
				data, _ := json.Marshal(action)
				conn.WriteMessage(websocket.TextMessage, data)
				return
			}
		}
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := newTestClient(srv.URL, "app1", "secret1",
		WithWSPingInterval(10*time.Millisecond),
		WithOnChange(func(keys []string) {
			if atomic.LoadInt32(&fetches) > 1 {
				select {
				case reloaded <- struct{}{}:
				default:
				}
			}
		}),
	)

	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer client.Stop()

	select {
	case <-reloaded:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for reload after timeline change")
	}

	value, ok := client.Get("a")
	if !ok || value != "2" {
		t.Fatalf("expected reloaded a=2, got %q, ok=%v", value, ok)
	}
}

func TestClient_Stop(t *testing.T) {
	configs := []apiConfig{
		{AppId: "app1", Key: "a", Value: "1"},
	}

	srv := testServer(configs)
	defer srv.Close()

	client := newTestClient(srv.URL, "app1", "secret1")
	err := client.Start(context.Background())
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	client.Stop()
	client.Stop()
}

func TestClient_Start_WhenAlreadyStarted_ReturnsError(t *testing.T) {
	configs := []apiConfig{
		{AppId: "app1", Key: "a", Value: "1"},
	}

	srv := testServer(configs)
	defer srv.Close()

	client := newTestClient(srv.URL, "app1", "secret1")
	err := client.Start(context.Background())
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer client.Stop()

	err = client.Start(context.Background())
	if err == nil {
		t.Fatal("expected error when starting an already started client")
	}
}

func TestClient_WebSocketUnexpectedClose_Reconnects(t *testing.T) {
	configs := []apiConfig{
		{AppId: "app1", Key: "a", Value: "1"},
	}

	var wsConnections int32
	firstClosed := make(chan struct{})
	reconnected := make(chan struct{})
	closeFirstOnce := sync.Once{}
	reconnectedOnce := sync.Once{}

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	mux := http.NewServeMux()

	mux.HandleFunc("/api/Config/app/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(configs)
	})

	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		n := atomic.AddInt32(&wsConnections, 1)
		if n == 1 {
			conn.Close()
			closeFirstOnce.Do(func() {
				close(firstClosed)
			})
			return
		}

		reconnectedOnce.Do(func() {
			close(reconnected)
		})
		defer conn.Close()
		<-r.Context().Done()
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := newTestClient(srv.URL, "app1", "secret1",
		WithWSRetryMaxInterval(10*time.Millisecond),
	)
	err := client.Start(context.Background())
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer client.Stop()

	select {
	case <-firstClosed:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for first websocket connection to close")
	}

	start := time.Now()
	select {
	case <-reconnected:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for websocket reconnect")
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("expected reconnect to honor max interval, took %s", elapsed)
	}
}

func TestClient_StartAfterStop_IgnoresStaleConnectFailure(t *testing.T) {
	configs := []apiConfig{
		{AppId: "app1", Key: "a", Value: "1"},
	}

	var wsRequests int32
	allowFirstWS := make(chan struct{})
	firstWSStarted := make(chan struct{})
	firstWSReleased := make(chan struct{})

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	mux := http.NewServeMux()

	mux.HandleFunc("/api/Config/app/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(configs)
	})

	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&wsRequests, 1)
		if n == 1 {
			close(firstWSStarted)
			<-allowFirstWS
			close(firstWSReleased)
			http.Error(w, "late failure", http.StatusServiceUnavailable)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		<-r.Context().Done()
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := newTestClient(srv.URL, "app1", "secret1",
		WithWSRetryMaxInterval(10*time.Millisecond),
	)
	err := client.Start(context.Background())
	if err != nil {
		t.Fatalf("first Start failed: %v", err)
	}

	select {
	case <-firstWSStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for first websocket request")
	}

	client.Stop()

	err = client.Start(context.Background())
	if err != nil {
		t.Fatalf("second Start failed: %v", err)
	}
	defer client.Stop()

	close(allowFirstWS)

	select {
	case <-firstWSReleased:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for stale websocket request to finish")
	}

	time.Sleep(100 * time.Millisecond)

	if got := atomic.LoadInt32(&wsRequests); got != 2 {
		t.Fatalf("expected stale connect failure to be ignored, got %d websocket requests", got)
	}
}

func TestClient_StartAfterStop_IgnoresStaleReconnectTimer(t *testing.T) {
	configs := []apiConfig{
		{AppId: "app1", Key: "a", Value: "1"},
	}

	var wsRequests int32
	firstClosed := make(chan struct{})
	secondConnected := make(chan struct{})
	reconnectedOnce := sync.Once{}
	secondOnce := sync.Once{}

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	mux := http.NewServeMux()

	mux.HandleFunc("/api/Config/app/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(configs)
	})

	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		n := atomic.AddInt32(&wsRequests, 1)
		if n == 1 {
			conn.Close()
			reconnectedOnce.Do(func() {
				close(firstClosed)
			})
			return
		}

		secondOnce.Do(func() {
			close(secondConnected)
		})
		defer conn.Close()
		<-r.Context().Done()
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := newTestClient(srv.URL, "app1", "secret1",
		WithWSRetryMaxInterval(10*time.Millisecond),
	)
	err := client.Start(context.Background())
	if err != nil {
		t.Fatalf("first Start failed: %v", err)
	}

	select {
	case <-firstClosed:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for first websocket close")
	}

	client.Stop()

	err = client.Start(context.Background())
	if err != nil {
		t.Fatalf("second Start failed: %v", err)
	}
	defer client.Stop()

	select {
	case <-secondConnected:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for second websocket connection")
	}

	time.Sleep(1200 * time.Millisecond)

	if got := atomic.LoadInt32(&wsRequests); got != 2 {
		t.Fatalf("expected stale reconnect timer to be ignored, got %d websocket requests", got)
	}
}
