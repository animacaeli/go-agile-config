# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Test Commands

```bash
go build ./...                              # compile the root SDK module
go test -v -race ./...                      # run root SDK tests with race detector
go test -v -race -run TestClient_Start ./... # run a single root SDK test by name
go vet ./...                                # static analysis for the root SDK module

(cd examples/gin && go build ./...)         # compile the Gin example module
(cd examples/gin && go test ./...)          # test the Gin example module
```

## Architecture

Single-package Go SDK (`package agileconfig`) with one independent example module under `examples/gin`.
The root package is the public API surface. Internal implementation types remain unexported in the
same package until the implementation grows enough to justify an `internal/` split.

Public API includes:
- Config client: `Client`, `NewClient`, `Option`, and `With*` functions
- Multi-app client: `MultiClient`, `NewMultiClient`, `MultiClientApp`, `MultiOption`, and `WithMulti*` functions
- Service discovery: `ServiceInfo`, `RegisterService`, `RegisterResult`, `HeartbeatResult`,
  `ServiceStatus`, `HeartbeatMode`, and `Client` service registry methods

**Component graph:**
```
Client (client.go) ── public entry point, orchestrates lifecycle
  ├── configStore (config.go) ── thread-safe map with change detection
  ├── options (option.go) ── functional options pattern
  ├── transport (transport.go) ── JSON HTTP API client with Basic Auth
  ├── service discovery (discovery.go) ── registry types and Client methods
  └── wsClient (websocket.go) ── WebSocket with reconnect + heartbeat
MultiClient (multiclient.go) ── manages multiple Client instances
examples/gin ── separate demo module using replace github.com/animacaeli/go-agile-config => ../..
```

**Key flows:**
- `Start()` → HTTP fetch configs → populate store → WebSocket connect in background goroutine
- WebSocket receives `reload` action → re-fetch via HTTP → `store.reload()` diffs and fires `OnChange`
- WebSocket receives `offline` → exponential backoff reconnect (1s→30s max), guarded by `reconnMu`
- Config timeline `ping` can trigger reload when the server publish timeline changes
- 30s heartbeat ticker sends `c:ping` via WebSocket by default
- Service registry methods use the same transport limits, Basic Auth, and HTTPS-by-default policy
- Transport defaults to HTTPS/WSS; use `WithInsecureHTTP()` only for trusted local development

**Config key format:** `group:key` when group is non-empty, otherwise just `key`.

**Concurrency:** `configStore` uses `sync.RWMutex`. `Client` uses `wsMu` for ws field and `reconnMu` to prevent concurrent reconnection. `wsClient` uses `sync.Mutex` for conn state.

**Structure policy:**
- Keep the SDK as a single public package while files remain small and cohesive.
- Do not introduce `pkg/`; the module root is already the importable package.
- Do not split into `internal/transport` or `internal/ws` unless those implementations gain
  enough independent complexity that the package boundary improves readability.
- Keep examples as independent modules with local `replace` directives. Do not commit `go.work`;
  developers can create a local workspace when they want editor support across modules.

## Testing Conventions

- Standard `testing` package only — no testify or other frameworks
- All tests in same package (access to unexported symbols)
- Mock servers via `httptest.NewServer` with `gorilla/websocket.Upgrader`
- `client_test.go` contains `testServer()` helper for integration tests
- Channel-based sync for async WebSocket test assertions (`chan struct{}` with `select`/`time.After`)
- Naming: `TestComponent_Scenario` (e.g., `TestConfigStore_Reload_DetectsChanges`)
- Run root SDK tests and example-module tests separately because the repository does not commit `go.work`.

## Project Details

- **Module:** `github.com/animacaeli/go-agile-config`
- **Example module:** `github.com/animacaeli/go-agile-config/examples/gin`
- **Go version:** 1.25+ with `toolchain go1.25.11` (set in go.mod)
- **Single external dependency:** `github.com/gorilla/websocket`
- **AgileConfig API compatibility:** PascalCase JSON tags match server's Newtonsoft.Json serialization
- **Dual README:** `README.md` (English) and `README_zh.md` (Chinese)
