# Control Plane

Go service that orchestrates agent sessions and Kubernetes sandboxes.

## Overview

The control plane is the backend for Netclode. It:

- Manages session lifecycle (create, pause, resume, delete)
- Creates and monitors Kubernetes Sandbox CRDs via informers
- Proxies prompts/responses between web clients and agents
- Persists session state and message history to Redis
- Provides real-time updates via Redis Streams

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
| `session.open` | Open session with history (supports `lastNotificationId` for reconnection) |
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

Redis is used for both persistence and real-time messaging via Redis Streams.

### Data Model

| Key Pattern | Type | Description |
|-------------|------|-------------|
| `sessions:all` | Set | Index of all session IDs |
| `session:{id}` | Hash | Session metadata (name, status, timestamps) |
| `session:{id}:messages` | List | Conversation history (auto-trimmed to `MAX_MESSAGES_PER_SESSION`) |
| `session:{id}:events:stream` | Stream | Tool events (auto-trimmed to `MAX_EVENTS_PER_SESSION`) |
| `session:{id}:notifications` | Stream | Real-time notifications for all session activity |

### Notification Types

The notifications stream contains all real-time updates:

| Type | Description |
|------|-------------|
| `event` | Tool use events (start, input, end) |
| `message` | Agent messages (partial and complete) |
| `session_update` | Session status changes |
| `user_message` | User prompts (for cross-client sync) |
| `agent_done` | Agent finished processing |
| `agent_error` | Agent encountered an error |

### Why Redis Streams?

- **No race conditions**: Cursor-based reading guarantees no missed events between history fetch and subscription
- **Horizontal scaling**: Multiple control-plane replicas share Redis as the message bus
- **Reconnection resilience**: Clients can resume from their last position after disconnect
- **Complete replay**: Can replay entire session from any point in the stream

### Data Flow

```
┌─────────┐     WebSocket      ┌───────────────┐      HTTP/SSE      ┌─────────┐
│  Web    │◄──────────────────►│ Control Plane │◄──────────────────►│  Agent  │
│ Client  │                    │               │                    │   Pod   │
└─────────┘                    └───────┬───────┘                    └─────────┘
     ▲                                 │
     │                                 │ Publish + Persist
     │                                 ▼
     │                         ┌───────────────┐
     │     XREAD BLOCK         │     Redis     │
     └─────────────────────────│  (sessions,   │
                               │   messages,   │
                               │ notifications)│
                               └───────────────┘
```

1. Client sends prompt via WebSocket
2. Control plane persists user message to Redis
3. Control plane publishes user message to notifications stream
4. Control plane forwards prompt to agent via HTTP
5. Agent streams SSE events back
6. Control plane persists messages/events to Redis
7. Control plane publishes notifications to Redis Stream
8. All subscribed WebSocket connections read from Redis Stream via XREAD BLOCK

### Reconnection Handling

Clients can reconnect without losing events:

1. On `session.open`, server returns `lastNotificationId` (Redis Stream ID)
2. Client stores this ID locally
3. On reconnect, client sends `lastNotificationId` in `session.open` message
4. Server subscribes starting from that cursor
5. Any events that occurred during disconnect are delivered

```json
// Client reconnects with cursor
{ "type": "session.open", "id": "abc123", "lastNotificationId": "1234567890-0" }

// Server responds with current state and resumes from cursor
{ "type": "session.state", "session": {...}, "lastNotificationId": "1234567891-0" }
```

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
