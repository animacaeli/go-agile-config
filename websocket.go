package agileconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// websocketAction mirrors the AgileConfig WebsocketAction protocol message.
// Fields use PascalCase JSON tags to match the server's Newtonsoft.Json serialization.
type websocketAction struct {
	Module string `json:"Module"`
	Action string `json:"Action"`
	Data   string `json:"Data"`
}

// wsClient manages a WebSocket connection to the AgileConfig server.
type wsClient struct {
	url       string
	appID     string
	secret    string
	env       string
	timeout   time.Duration
	readLimit int64
	onAction  func(action websocketAction)
	onClose   func(ws *wsClient)

	mu     sync.Mutex
	conn   *websocket.Conn
	closed bool
}

func newWSClient(
	url, appID, secret, env string,
	timeout time.Duration,
	readLimit int64,
	onAction func(websocketAction),
	onClose func(*wsClient),
) *wsClient {
	return &wsClient{
		url:       url,
		appID:     appID,
		secret:    secret,
		env:       env,
		timeout:   timeout,
		readLimit: readLimit,
		onAction:  onAction,
		onClose:   onClose,
	}
}

func (w *wsClient) connect(ctx context.Context) error {
	header := http.Header{}
	header.Set("Authorization", "Basic "+basicAuth(w.appID, w.secret))
	header.Set("appid", w.appID)
	header.Set("env", w.env)
	header.Set("client-v", "1.8.0")

	dialer := websocket.Dialer{HandshakeTimeout: w.timeout}
	conn, _, err := dialer.DialContext(ctx, w.url, header)
	if err != nil {
		return fmt.Errorf("websocket dial: %w", err)
	}
	if w.readLimit > 0 {
		conn.SetReadLimit(w.readLimit)
	}

	w.mu.Lock()
	w.conn = conn
	w.closed = false
	w.mu.Unlock()

	go w.readLoop()

	return nil
}

func (w *wsClient) readLoop() {
	defer func() {
		closed := w.clearConn()
		if !closed && w.onClose != nil {
			w.onClose(w)
		}
	}()

	w.mu.Lock()
	conn := w.conn
	w.mu.Unlock()

	if conn == nil {
		return
	}

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var action websocketAction
		if err := json.Unmarshal(message, &action); err != nil {
			continue
		}

		if w.onAction != nil {
			w.onAction(action)
		}
	}
}

func (w *wsClient) send(msg string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.conn == nil || w.closed {
		return fmt.Errorf("websocket not connected")
	}
	return w.conn.WriteMessage(websocket.TextMessage, []byte(msg))
}

func (w *wsClient) close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	if w.conn != nil {
		w.conn.Close()
		w.conn = nil
	}
}

func (w *wsClient) clearConn() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	closed := w.closed
	if w.conn != nil {
		w.conn.Close()
		w.conn = nil
	}
	return closed
}

// buildWSURL converts an HTTP(S) server URL to a WS(S) URL for the AgileConfig WebSocket endpoint.
func buildWSURL(serverURL string) (string, error) {
	u := normalizeServerURL(serverURL)
	if strings.HasPrefix(u, "https://") {
		return "wss" + strings.TrimPrefix(u, "https") + "/ws", nil
	}
	if strings.HasPrefix(u, "http://") {
		return "ws" + strings.TrimPrefix(u, "http") + "/ws", nil
	}
	return "", fmt.Errorf("invalid server URL %q: must start with http:// or https://", serverURL)
}
