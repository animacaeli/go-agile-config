package agileconfig

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

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

func TestClient_StartAndGet(t *testing.T) {
	configs := []apiConfig{
		{AppId: "app1", Group: "db", Key: "host", Value: "localhost"},
		{AppId: "app1", Group: "db", Key: "port", Value: "3306"},
		{AppId: "app1", Group: "", Key: "app.name", Value: "myapp"},
	}

	srv := testServer(configs)
	defer srv.Close()

	client := NewClient(srv.URL, "app1", "secret1",
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

	client := NewClient(srv.URL, "app1", "secret1")
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

	client := NewClient(srv.URL, "app1", "secret1")
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

	client := NewClient(srv.URL, "app1", "secret1")
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

	client := NewClient(srv.URL, "app1", "secret1")
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

	client := NewClient(srv.URL, "app1", "secret1",
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

func TestClient_Stop(t *testing.T) {
	configs := []apiConfig{
		{AppId: "app1", Key: "a", Value: "1"},
	}

	srv := testServer(configs)
	defer srv.Close()

	client := NewClient(srv.URL, "app1", "secret1")
	err := client.Start(context.Background())
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	client.Stop()
	client.Stop()
}
