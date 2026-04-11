package agileconfig

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestWSClient_ConnectAndReceiveReload(t *testing.T) {
	reloadReceived := make(chan struct{}, 1)
	var connectedHeaders http.Header

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connectedHeaders = r.Header.Clone()

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade error: %v", err)
			return
		}
		defer conn.Close()

		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if string(msg) == "c:ping" {
				action := websocketAction{Module: "c", Action: "reload", Data: ""}
				data, _ := json.Marshal(action)
				conn.WriteMessage(websocket.TextMessage, data)
				return
			}
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	ws := newWSClient(wsURL, "app1", "secret1", "DEV", 5*time.Second,
		func(action websocketAction) {
			if action.Action == "reload" {
				select {
				case reloadReceived <- struct{}{}:
				default:
				}
			}
		},
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := ws.connect(ctx)
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	auth := connectedHeaders.Get("Authorization")
	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte("app1:secret1"))
	if auth != expected {
		t.Errorf("expected auth %s, got %s", expected, auth)
	}

	if connectedHeaders.Get("env") != "DEV" {
		t.Errorf("expected env=DEV, got %s", connectedHeaders.Get("env"))
	}

	ws.send("c:ping")

	select {
	case <-reloadReceived:
		// success
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for reload action")
	}

	ws.close()
}

func TestWSClient_OfflineAction(t *testing.T) {
	offlineReceived := make(chan struct{}, 1)

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
				action := websocketAction{Module: "c", Action: "offline", Data: ""}
				data, _ := json.Marshal(action)
				conn.WriteMessage(websocket.TextMessage, data)
				return
			}
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	ws := newWSClient(wsURL, "app1", "secret1", "DEV", 5*time.Second,
		func(action websocketAction) {
			if action.Action == "offline" {
				select {
				case offlineReceived <- struct{}{}:
				default:
				}
			}
		},
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := ws.connect(ctx)
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	ws.send("c:ping")

	select {
	case <-offlineReceived:
		// success
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for offline action")
	}

	ws.close()
}

func TestWSClient_PingPong(t *testing.T) {
	pingReceived := make(chan string, 1)

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
				action := websocketAction{Module: "c", Action: "ping", Data: "timeline-abc"}
				data, _ := json.Marshal(action)
				conn.WriteMessage(websocket.TextMessage, data)
				return
			}
		}
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	ws := newWSClient(wsURL, "app1", "secret1", "DEV", 5*time.Second,
		func(action websocketAction) {
			if action.Action == "ping" {
				select {
				case pingReceived <- action.Data:
				default:
				}
			}
		},
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := ws.connect(ctx)
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	ws.send("c:ping")

	select {
	case data := <-pingReceived:
		if data != "timeline-abc" {
			t.Fatalf("expected timeline-abc, got %s", data)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for ping response")
	}

	ws.close()
}

func TestWSURL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"http://localhost:5000", "ws://localhost:5000/ws"},
		{"http://localhost:5000/", "ws://localhost:5000/ws"},
		{"https://example.com", "wss://example.com/ws"},
		{"https://example.com/", "wss://example.com/ws"},
	}
	for _, tt := range tests {
		got, err := buildWSURL(tt.input)
		if err != nil {
			t.Errorf("buildWSURL(%q) unexpected error: %v", tt.input, err)
			continue
		}
		if got != tt.expected {
			t.Errorf("buildWSURL(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestWSURL_InvalidInput(t *testing.T) {
	_, err := buildWSURL("localhost:5000")
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}
