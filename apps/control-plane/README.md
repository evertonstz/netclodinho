# Control Plane

Go service that orchestrates agent sessions and Kubernetes sandboxes.

## Overview

The control plane is the backend for Netclode. It:

- Manages session lifecycle (create, pause, resume, delete)
- Creates and monitors Kubernetes Sandbox CRDs via informers
- Proxies prompts/responses between web clients and agents
- Persists session state and message history to Redis

## Architecture

```
apps/control-plane/
├── cmd/control-plane/     # Entry point
└── internal/
    ├── api/               # HTTP/WebSocket server
    ├── session/           # Session manager and state
    ├── k8s/               # Kubernetes client (Sandbox CRDs)
    ├── storage/           # Redis persistence
    ├── protocol/          # Message types
    └── config/            # Configuration
```

## API

### WebSocket (`/ws`)

Clients connect via WebSocket and send JSON messages:

| Message Type | Description |
|--------------|-------------|
| `session.create` | Create new session |
| `session.list` | List all sessions |
| `session.open` | Open session with history |
| `session.resume` | Resume paused session (creates sandbox) |
| `session.pause` | Pause session (deletes sandbox, keeps data) |
| `session.delete` | Delete session and all resources |
| `prompt` | Send prompt to agent |
| `prompt.interrupt` | Interrupt running prompt |
| `sync` | Get all sessions with metadata |

### HTTP

| Endpoint | Description |
|----------|-------------|
| `GET /health` | Health check |
| `GET /ready` | Readiness check |

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `3000` | Server port |
| `K8S_NAMESPACE` | `netclode` | Kubernetes namespace |
| `AGENT_IMAGE` | `ghcr.io/angristan/netclode-agent:latest` | Agent container image |
| `SANDBOX_TEMPLATE` | `netclode-agent` | SandboxTemplate name (warm pool) |
| `REDIS_URL` | `redis://redis-sessions...` | Redis connection URL |
| `WARM_POOL_ENABLED` | `false` | Use SandboxClaim for warm pool |
| `MAX_MESSAGES_PER_SESSION` | `1000` | Message history limit |
| `MAX_EVENTS_PER_SESSION` | `50` | Event history limit |

## Redis Storage

Redis is used for persistence, not real-time messaging. WebSocket clients receive updates via in-memory channels.

### Data Model

| Key Pattern | Type | Description |
|-------------|------|-------------|
| `sessions:all` | Set | Index of all session IDs |
| `session:{id}` | Hash | Session metadata (name, status, timestamps) |
| `session:{id}:messages` | List | Conversation history (auto-trimmed to `MAX_MESSAGES_PER_SESSION`) |
| `session:{id}:events:stream` | Stream | Tool events (auto-trimmed to `MAX_EVENTS_PER_SESSION`) |

### Why Redis?

- **Persistence**: Sessions survive control-plane restarts
- **Atomic operations**: Pipelined writes for consistency
- **Efficient trimming**: Lists and Streams auto-trim old entries
- **Simple queries**: No need for a full database

### Data Flow

```
┌─────────┐     WebSocket      ┌───────────────┐      HTTP/SSE      ┌─────────┐
│  Web    │◄──────────────────►│ Control Plane │◄──────────────────►│  Agent  │
│ Client  │                    │               │                    │   Pod   │
└─────────┘                    └───────┬───────┘                    └─────────┘
                                       │
                                       │ Persist
                                       ▼
                               ┌───────────────┐
                               │     Redis     │
                               │  (sessions,   │
                               │   messages,   │
                               │    events)    │
                               └───────────────┘
```

1. Client sends prompt via WebSocket
2. Control plane persists user message to Redis
3. Control plane forwards prompt to agent via HTTP
4. Agent streams SSE events back
5. Control plane persists assistant messages/events to Redis
6. Control plane broadcasts to all connected WebSocket clients (in-memory)

## Session Lifecycle

```
┌─────────────────────────────────────────────────────────────┐
│                                                             │
│   create ──► creating ──► running ◄──► paused               │
│                  │            │           │                 │
│                  └────────────┴───────────┼──► deleted      │
│                       error ◄─────────────┘                 │
│                                                             │
└─────────────────────────────────────────────────────────────┘

- creating: Sandbox being provisioned
- running: Sandbox ready, agent accepting prompts
- paused: Sandbox deleted, PVC retained (data persists)
- error: Sandbox creation failed
```

## Development

```bash
# Run locally (requires kubeconfig and Redis)
go run ./cmd/control-plane

# Run tests
go test ./...

# Build
go build -o control-plane ./cmd/control-plane
```

## Docker

```bash
docker build -t control-plane -f Dockerfile .
```
