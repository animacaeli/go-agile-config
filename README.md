# go-agile-config

[中文文档](README_zh.md)

A Go client SDK for [AgileConfig](https://github.com/dotnetcore/AgileConfig) — a lightweight configuration center. This library enables Go applications to fetch configurations from AgileConfig server and receive real-time updates via WebSocket.

## Features

- Fetch published configurations via HTTP with `appId:secret` Basic Auth
- Real-time config updates via WebSocket with automatic reconnection
- Thread-safe in-memory config store with change detection
- Functional options for flexible configuration
- Zero external dependencies beyond [gorilla/websocket](https://github.com/gorilla/websocket)

## Installation

```bash
go get github.com/animacaeli/go-agile-config
```

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"

    agileconfig "github.com/animacaeli/go-agile-config"
)

func main() {
    client := agileconfig.NewClient(
        "http://localhost:5000",
        "my-app-id",
        "my-app-secret",
        agileconfig.WithEnv("DEV"),
    )

    // Load configs and establish WebSocket connection
    if err := client.Start(context.Background()); err != nil {
        log.Fatal(err)
    }
    defer client.Stop()

    // Read config values
    host := client.GetString("db.host", "localhost")
    fmt.Println("DB Host:", host)
}
```

## API Reference

### Creating a Client

```go
client := agileconfig.NewClient(serverURL, appID, secret, opts...)
```

### Options

| Option | Default | Description |
|--------|---------|-------------|
| `WithEnv(env)` | `""` | Environment name (e.g. `"DEV"`, `"PROD"`) |
| `WithHTTPTimeout(d)` | `10s` | HTTP request timeout |
| `WithWSRetryMaxInterval(d)` | `30s` | Max WebSocket reconnection backoff interval |
| `WithOnChange(fn)` | `nil` | Callback when config values change |

### Lifecycle

```go
// Start: fetch configs + establish WebSocket connection
err := client.Start(ctx)

// Stop: close WebSocket, release resources
client.Stop()
```

### Reading Configs

AgileConfig organizes configs by `group` and `key`. When a config has a group, it is stored as `group:key`. When there is no group, it is stored as just `key`.

```go
// Get returns (value, exists)
val, ok := client.Get("db:host")

// GetString returns value or default
host := client.GetString("db.host", "localhost")

// GetByGroup: query by group and key separately
val, ok := client.GetByGroup("database", "host")

// GetAll: returns a copy of all configs
all := client.GetAll()
```

### Listening for Changes

```go
client := agileconfig.NewClient(serverURL, appID, secret,
    agileconfig.WithOnChange(func(changedKeys []string) {
        for _, key := range changedKeys {
            log.Printf("config changed: %s", key)
        }
    }),
)
```

The `OnChange` callback is triggered when the server pushes a `reload` action via WebSocket. The client automatically re-fetches all configs and reports which keys changed (added, removed, or modified).

## How It Works

```
Start()
  │
  ├─ 1. HTTP GET /api/Config/app/{appId}?env={env}
  │     Auth: Basic base64(appId:secret)
  │     → Load configs into memory
  │
  ├─ 2. WebSocket ws://server/ws
  │     Auth: Basic base64(appId:secret)
  │     Background goroutine
  │
  └─ 3. Message loop
        ├─ "reload"  → HTTP re-fetch → diff → OnChange callback
        ├─ "offline" → reconnect with exponential backoff
        └─ "ping"    → keep-alive
```

## Requirements

- Go 1.21+
- Running [AgileConfig](https://github.com/dotnetcore/AgileConfig) server

## License

[MIT](LICENSE)
