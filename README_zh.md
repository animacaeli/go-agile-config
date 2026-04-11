# go-agile-config

[English](README.md)

一个面向 [AgileConfig](https://github.com/dotnetcore/AgileConfig) 的 Go 客户端 SDK —— 轻量级配置中心。该库使 Go 应用能够从 AgileConfig 服务端获取配置，并通过 WebSocket 接收实时更新。

## 特性

- 通过 HTTP 以 `appId:secret` Basic Auth 方式拉取已发布的配置
- 通过 WebSocket 实时接收配置变更，支持自动重连
- 线程安全的内存配置存储，支持变更检测
- 函数式选项，灵活配置
- 除 [gorilla/websocket](https://github.com/gorilla/websocket) 外零外部依赖

## 安装

```bash
go get github.com/animacaeli/go-agile-config
```

## 快速开始

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

    // 加载配置并建立 WebSocket 连接
    if err := client.Start(context.Background()); err != nil {
        log.Fatal(err)
    }
    defer client.Stop()

    // 读取配置值
    host := client.GetString("db.host", "localhost")
    fmt.Println("DB Host:", host)
}
```

## API 参考

### 创建客户端

```go
client := agileconfig.NewClient(serverURL, appID, secret, opts...)
```

### 选项

| 选项 | 默认值 | 说明 |
|------|--------|------|
| `WithEnv(env)` | `""` | 环境名称（如 `"DEV"`、`"PROD"`） |
| `WithHTTPTimeout(d)` | `10s` | HTTP 请求超时时间 |
| `WithWSRetryMaxInterval(d)` | `30s` | WebSocket 重连最大退避间隔 |
| `WithOnChange(fn)` | `nil` | 配置变更时的回调函数 |

### 生命周期

```go
// Start: 拉取配置 + 建立 WebSocket 连接
err := client.Start(ctx)

// Stop: 关闭 WebSocket，释放资源
client.Stop()
```

### 读取配置

AgileConfig 按 `group` 和 `key` 组织配置。当配置存在分组时，以 `group:key` 格式存储；无分组时，直接以 `key` 存储。

```go
// Get 返回 (value, exists)
val, ok := client.Get("db:host")

// GetString 返回值或默认值
host := client.GetString("db.host", "localhost")

// GetByGroup: 按分组和键分别查询
val, ok := client.GetByGroup("database", "host")

// GetAll: 返回所有配置的副本
all := client.GetAll()
```

### 监听配置变更

```go
client := agileconfig.NewClient(serverURL, appID, secret,
    agileconfig.WithOnChange(func(changedKeys []string) {
        for _, key := range changedKeys {
            log.Printf("配置变更: %s", key)
        }
    }),
)
```

当服务端通过 WebSocket 推送 `reload` 动作时，`OnChange` 回调将被触发。客户端会自动重新拉取所有配置，并报告哪些键发生了变化（新增、删除或修改）。

## 工作原理

```
Start()
  │
  ├─ 1. HTTP GET /api/Config/app/{appId}?env={env}
  │     认证: Basic base64(appId:secret)
  │     → 加载配置到内存
  │
  ├─ 2. WebSocket ws://server/ws
  │     认证: Basic base64(appId:secret)
  │     后台 goroutine
  │
  └─ 3. 消息循环
        ├─ "reload"  → HTTP 重新拉取 → 差异比对 → OnChange 回调
        ├─ "offline" → 指数退避重连
        └─ "ping"    → 保活心跳
```

## 环境要求

- Go 1.21+
- 运行中的 [AgileConfig](https://github.com/dotnetcore/AgileConfig) 服务端

## 许可证

[MIT](LICENSE)
