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

func multiTestServer(configs map[string][]apiConfig) *httptest.Server {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/Config/app/", func(w http.ResponseWriter, r *http.Request) {
		appID := r.URL.Path[len("/api/Config/app/"):]
		w.Header().Set("publish-time-line-id", appID+"-timeline")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(configs[appID])
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		<-r.Context().Done()
	})

	return httptest.NewServer(mux)
}

func TestMultiClient_StartAndGet(t *testing.T) {
	srv := multiTestServer(map[string][]apiConfig{
		"mysql": {
			{AppId: "mysql", Group: "db", Key: "host", Value: "127.0.0.1"},
			{AppId: "mysql", Group: "db", Key: "port", Value: "3306"},
		},
		"redis": {
			{AppId: "redis", Key: "addr", Value: "127.0.0.1:6379"},
		},
	})
	defer srv.Close()

	client, err := NewMultiClient(srv.URL, []MultiClientApp{
		{AppID: "mysql", Secret: "mysql-secret"},
		{AppID: "redis", Secret: "redis-secret"},
	}, WithMultiClientOptions(WithEnv("DEV"), WithInsecureHTTP()))
	if err != nil {
		t.Fatalf("NewMultiClient failed: %v", err)
	}

	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer client.Stop()

	host, ok := client.GetByGroup("mysql", "db", "host")
	if !ok || host != "127.0.0.1" {
		t.Fatalf("expected mysql db:host=127.0.0.1, got %q, ok=%v", host, ok)
	}

	addr := client.GetString("redis", "addr", "")
	if addr != "127.0.0.1:6379" {
		t.Fatalf("expected redis addr, got %q", addr)
	}

	all := client.GetAll()
	if all["mysql"]["db:port"] != "3306" {
		t.Fatalf("expected mysql db:port=3306, got %q", all["mysql"]["db:port"])
	}
	if all["redis"]["addr"] != "127.0.0.1:6379" {
		t.Fatalf("expected redis addr, got %q", all["redis"]["addr"])
	}
}

func TestMultiClient_OnChangeIncludesAppID(t *testing.T) {
	srv := multiTestServer(map[string][]apiConfig{
		"mysql": {
			{AppId: "mysql", Key: "host", Value: "127.0.0.1"},
		},
	})
	defer srv.Close()

	var mu sync.Mutex
	changes := make(map[string][]string)
	client, err := NewMultiClient(srv.URL, []MultiClientApp{
		{AppID: "mysql", Secret: "mysql-secret"},
	}, WithMultiClientOptions(WithInsecureHTTP()), WithMultiOnChange(func(appID string, changedKeys []string) {
		mu.Lock()
		changes[appID] = append([]string(nil), changedKeys...)
		mu.Unlock()
	}))
	if err != nil {
		t.Fatalf("NewMultiClient failed: %v", err)
	}

	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer client.Stop()

	mu.Lock()
	keys := changes["mysql"]
	mu.Unlock()
	if len(keys) != 1 || keys[0] != "host" {
		t.Fatalf("expected mysql host change, got %v", keys)
	}
}

func TestMultiClient_DuplicateAppID(t *testing.T) {
	_, err := NewMultiClient("http://localhost:5000", []MultiClientApp{
		{AppID: "mysql", Secret: "a"},
		{AppID: "mysql", Secret: "b"},
	}, WithMultiClientOptions(WithInsecureHTTP()))
	if err == nil {
		t.Fatal("expected duplicate app ID error")
	}
}

func TestMultiClient_StartRollbackOnFailure(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/Config/app/", func(w http.ResponseWriter, r *http.Request) {
		appID := r.URL.Path[len("/api/Config/app/"):]
		if appID == "bad" {
			http.Error(w, "bad app", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]apiConfig{{AppId: appID, Key: "ok", Value: "1"}})
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		<-r.Context().Done()
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client, err := NewMultiClient(srv.URL, []MultiClientApp{
		{AppID: "good", Secret: "good-secret"},
		{AppID: "bad", Secret: "bad-secret"},
	}, WithMultiClientOptions(WithInsecureHTTP()))
	if err != nil {
		t.Fatalf("NewMultiClient failed: %v", err)
	}

	if err := client.Start(context.Background()); err == nil {
		t.Fatal("expected start failure")
	}

	if err := client.Start(context.Background()); err == nil {
		t.Fatal("expected restart to retry and fail, not already-started error")
	}
}
