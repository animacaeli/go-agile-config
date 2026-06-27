# Gin Demo

This demo starts a Gin HTTP server, reads configs from AgileConfig through this SDK, prints all config parameters at startup, and prints them again whenever the SDK receives a real-time reload event.

## Run

```bash
cd examples/gin
go run . \
  -server https://localhost:5000 \
  -app-id your-app-id \
  -secret your-app-secret \
  -env DEV \
  -listen :8080
```

The same settings can be provided with environment variables:

```bash
AGILE_CONFIG_SERVER=https://localhost:5000 \
AGILE_CONFIG_APP_ID=your-app-id \
AGILE_CONFIG_SECRET=your-app-secret \
AGILE_CONFIG_ENV=DEV \
GIN_LISTEN=:8080 \
go run .
```

The SDK requires HTTPS/WSS by default because credentials are sent with Basic Auth. For a trusted local AgileConfig server that only exposes `http://`, opt in explicitly:

```bash
go run . \
  -server http://localhost:5000 \
  -insecure-http \
  -app-id your-app-id \
  -secret your-app-secret
```

Or with environment variables:

```bash
AGILE_CONFIG_SERVER=http://localhost:5000 \
AGILE_CONFIG_INSECURE_HTTP=true \
AGILE_CONFIG_APP_ID=your-app-id \
AGILE_CONFIG_SECRET=your-app-secret \
go run .
```

## Endpoints

- `GET /healthz`
- `GET /configs`
- `GET /configs/:key`
- `GET /groups/:group/configs/:key`

Grouped config keys are stored by the SDK as `group:key`, so `/configs/database:host` and `/groups/database/configs/host` read the same value.
