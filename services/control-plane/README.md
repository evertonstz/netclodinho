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
services/control-plane/
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

Clients connect via WebSocket and exchange JSON messages.

#### Client → Server

| Message Type | Fields | Description |
|--------------|--------|-------------|
| `session.create` | `name`, `repo?` | Create new session |
| `session.list` | | List all sessions |
| `session.open` | `id`, `lastNotificationId?` | Open session with history |
| `session.resume` | `id` | Resume paused session |
| `session.pause` | `id` | Pause session |
| `session.delete` | `id` | Delete session |
| `prompt` | `sessionId`, `text` | Send prompt to agent |
| `prompt.interrupt` | `sessionId` | Interrupt running prompt |
| `port.expose` | `sessionId`, `port` | Expose port for preview |
| `terminal.input` | `sessionId`, `data` | Send terminal input |
| `terminal.resize` | `sessionId`, `cols`, `rows` | Resize terminal |
| `sync` | | Get all sessions with metadata |

#### Server → Client

| Message Type | Description |
|--------------|-------------|
| `session.created` | Session created |
| `session.updated` | Session status changed |
| `session.deleted` | Session deleted |
| `session.list` | List of sessions |
| `session.state` | Session with message/event history |
| `session.error` | Session operation failed |
| `sync.response` | All sessions with metadata |
| `agent.message` | Text from agent (`partial` for streaming) |
| `agent.event` | Tool/command event (see Agent Events below) |
| `agent.done` | Agent finished processing |
| `agent.error` | Agent error |
| `user.message` | User prompt (for cross-client sync) |
| `port.exposed` | Port exposed with `previewUrl` |
| `port.error` | Port expose failed |
| `error` | Generic error |

#### Agent Events

Events emitted during agent execution, delivered via `agent.event`:

| Kind | Description | Fields |
|------|-------------|--------|
| `tool_start` | Tool invocation started | `tool`, `toolUseId`, `input` |
| `tool_input` | Streaming tool input | `toolUseId`, `inputDelta` |
| `tool_end` | Tool completed | `tool`, `toolUseId`, `result?`, `error?` |
| `file_change` | File created/edited/deleted | `path`, `action`, `linesAdded?`, `linesRemoved?` |
| `command_start` | Shell command started | `command`, `cwd?` |
| `command_end` | Shell command completed | `command`, `exitCode`, `output?` |
| `thinking` | Agent reasoning | `content` |
| `port_exposed` | Port exposed for preview | `port`, `process?`, `previewUrl?` |

All events include `kind` and `timestamp` (ISO 8601).

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
| `MAX_ACTIVE_SESSIONS` | `2` | Max concurrent active sessions (0 = unlimited). Oldest inactive session is auto-paused when limit reached. |
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
