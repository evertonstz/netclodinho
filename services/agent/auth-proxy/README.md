# Auth Proxy

Tiny HTTP proxy that adds Kubernetes ServiceAccount token authentication to outbound requests. Runs inside sandbox microVMs.

For architecture overview, see [docs/secret-proxy.md](../../../docs/secret-proxy.md).

## Purpose

Sits between SDK HTTP clients and the external secret-proxy:

```
SDK → auth-proxy (localhost:8080) → secret-proxy (external)
      adds SA token                  injects secrets
```

This proxy has **NO secrets** - it only adds authentication.

## Building

```bash
go build -o auth-proxy .
```

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `LISTEN_ADDR` | `:8080` | Address to listen on |
| `TOKEN_PATH` | `/var/run/secrets/proxy-auth/token` | Path to SA token file |
| `UPSTREAM_PROXY` | `http://secret-proxy.netclode.svc.cluster.local:8080` | External proxy URL |

## How It Works

1. Reads ServiceAccount token from mounted volume (refreshed every 5 minutes)
2. For each request, adds `Proxy-Authorization: Bearer <token>` header
3. Forwards to upstream secret-proxy

Supports both HTTP requests and HTTPS CONNECT tunneling.
