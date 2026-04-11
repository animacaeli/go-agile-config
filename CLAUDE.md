# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Test Commands

```bash
go build ./...                          # compile all packages
go test -v -race ./...                  # run all tests with race detector
go test -v -race -run TestClient_Start ./...  # run a single test by name
go vet ./...                            # static analysis
```

## Architecture

Single-package Go library (`package agileconfig`) — no sub-packages. All internal types are unexported; only `Client`, `NewClient`, `Option`, and `With*` functions are the public API.

**Component graph:**
```
Client (client.go) ── public entry point, orchestrates lifecycle
  ├── configStore (config.go) ── thread-safe map with change detection
  ├── options (option.go) ── functional options pattern
  ├── transport (transport.go) ── HTTP GET with Basic Auth
  └── wsClient (websocket.go) ── WebSocket with reconnect + heartbeat
```

**Key flows:**
- `Start()` → HTTP fetch configs → populate store → WebSocket connect in background goroutine
- WebSocket receives `reload` action → re-fetch via HTTP → `store.reload()` diffs and fires `OnChange`
- WebSocket receives `offline` → exponential backoff reconnect (1s→30s max), guarded by `reconnMu`
- 30s heartbeat ticker sends `c:ping` via WebSocket

**Config key format:** `group:key` when group is non-empty, otherwise just `key`.

**Concurrency:** `configStore` uses `sync.RWMutex`. `Client` uses `wsMu` for ws field and `reconnMu` to prevent concurrent reconnection. `wsClient` uses `sync.Mutex` for conn state.

## Testing Conventions

- Standard `testing` package only — no testify or other frameworks
- All tests in same package (access to unexported symbols)
- Mock servers via `httptest.NewServer` with `gorilla/websocket.Upgrader`
- `client_test.go` contains `testServer()` helper for integration tests
- Channel-based sync for async WebSocket test assertions (`chan struct{}` with `select`/`time.After`)
- Naming: `TestComponent_Scenario` (e.g., `TestConfigStore_Reload_DetectsChanges`)

## Project Details

- **Module:** `github.com/animacaeli/go-agile-config`
- **Go version:** 1.21+ (set in go.mod)
- **Single external dependency:** `github.com/gorilla/websocket`
- **AgileConfig API compatibility:** PascalCase JSON tags match server's Newtonsoft.Json serialization
- **Dual README:** `README.md` (English) and `README_zh.md` (Chinese)
