# go-agile-config

[中文文档](README_zh.md)

A Go client SDK for [AgileConfig](https://github.com/dotnetcore/AgileConfig) — a lightweight configuration center. This library enables Go applications to fetch configurations from AgileConfig server and receive real-time updates via WebSocket.

## Features

- Fetch published configurations via HTTP with `appId:secret` Basic Auth
- Real-time config updates via WebSocket with automatic reconnection
- Thread-safe in-memory config store with change detection
- HTTPS/WSS by default, with explicit opt-in for local insecure HTTP
- Bounded HTTP response and WebSocket message sizes
- Service discovery APIs for listing, registering, unregistering, and heartbeating services
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
        "https://config.example.com",
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
| `WithWSPingInterval(d)` | `30s` | Interval for checking the server publish timeline |
| `WithMaxResponseBody(n)` | `10 MiB` | Maximum HTTP config response size in bytes |
| `WithMaxWSMessageSize(n)` | `64 KiB` | Maximum WebSocket message size in bytes |
| `WithInsecureHTTP()` | disabled | Allow `http://` and `ws://` connections for trusted local development |
| `WithOnChange(fn)` | `nil` | Callback when config values change |

### Transport Security

The client requires HTTPS/WSS by default because AgileConfig credentials are sent with Basic Auth. Plain HTTP URLs and HTTPS-to-HTTP redirects are rejected unless `WithInsecureHTTP()` is explicitly set.

Use `WithInsecureHTTP()` only for trusted local development or private test networks:

```go
client := agileconfig.NewClient(
    "http://localhost:5000",
    "my-app-id",
    "my-app-secret",
    agileconfig.WithInsecureHTTP(),
)
```

When `WithInsecureHTTP()` is not set, redirects are limited to HTTPS targets. Redirect loops are stopped after 10 redirects.

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

### Multiple App IDs

Use `MultiClient` when one process depends on several AgileConfig app IDs. Each app keeps its own HTTP fetch and WebSocket connection.

```go
client, err := agileconfig.NewMultiClient(serverURL, []agileconfig.MultiClientApp{
    {AppID: "mysql", Secret: "mysql-secret"},
    {AppID: "redis", Secret: "redis-secret"},
},
    agileconfig.WithMultiClientOptions(agileconfig.WithEnv("DEV")),
    agileconfig.WithMultiOnChange(func(appID string, changedKeys []string) {
        log.Printf("%s changed: %v", appID, changedKeys)
    }),
)
if err != nil {
    log.Fatal(err)
}

if err := client.Start(context.Background()); err != nil {
    log.Fatal(err)
}
defer client.Stop()

mysqlHost, _ := client.GetByGroup("mysql", "db", "host")
redisAddr := client.GetString("redis", "addr", "localhost:6379")
all := client.GetAll() // map[appID]map[key]value
```

### Service Discovery

AgileConfig can also work as a simple service registry. Use these APIs to read registered service instances or manage your own registration:

```go
services, err := client.ListServices(context.Background())
online, err := client.ListOnlineServices(context.Background())
offline, err := client.ListOfflineServices(context.Background())
```

To register a service instance and keep it alive with client-side heartbeat:

```go
port := 8080
result, err := client.RegisterService(context.Background(), agileconfig.RegisterService{
    ServiceID:     "order-service",
    ServiceName:   "Order Service",
    IP:            "10.0.0.8",
    Port:          &port,
    MetaData:      []string{"version=1.0.0"},
    HeartbeatMode: agileconfig.HeartbeatModeClient,
})
if err != nil {
    log.Fatal(err)
}

_, err = client.Heartbeat(context.Background(), result.UniqueID)
_, err = client.UnregisterService(context.Background(), result.UniqueID)
```

## How It Works

```
Start()
  │
  ├─ 1. HTTPS GET /api/Config/app/{appId}?env={env}
  │     Auth: Basic base64(appId:secret)
  │     → Load configs into memory
  │
  ├─ 2. WebSocket wss://server/ws
  │     Auth: Basic base64(appId:secret)
  │     Background goroutine
  │
  └─ 3. Message loop
        ├─ "reload"  → HTTP re-fetch → diff → OnChange callback
        ├─ "offline" → reconnect with exponential backoff
        └─ "ping"    → keep-alive
```

## Requirements

- Go 1.25+ (`toolchain go1.25.11` is recommended for security fixes)
- Running [AgileConfig](https://github.com/dotnetcore/AgileConfig) server

## License

[MIT](LICENSE)
