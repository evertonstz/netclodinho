# Control Plane

Go service that orchestrates Netclode. Manages sessions, proxies communication between clients and agents, persists state to Redis.

## What it does

- Session lifecycle (create, pause, resume, delete)
- Creates Sandbox CRDs, monitors readiness via k8s informers
- Bridges Connect clients to Connect agents
- Stores sessions, messages, and events in Redis
- Real-time sync across clients via Redis Streams

## Architecture

```
services/control-plane/
├── cmd/control-plane/     # Entry point
└── internal/
    ├── api/               # Connect protocol server
    ├── session/           # Session manager
    ├── k8s/               # Kubernetes client (Sandbox CRDs)
    ├── storage/           # Redis persistence
    ├── protocol/          # Message types
    └── config/            # Configuration
```

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `3000` | HTTP port (health checks) |
| `CONNECT_PORT` | `3001` | Connect protocol port |
| `AGENT_PORT` | `3002` | Agent Connect port |
| `K8S_NAMESPACE` | `netclode` | Kubernetes namespace |
| `AGENT_IMAGE` | `ghcr.io/angristan/netclode-agent:latest` | Agent image |
| `SANDBOX_TEMPLATE` | `netclode-agent` | SandboxTemplate name |
| `REDIS_URL` | `redis://redis-sessions...` | Redis URL |
| `WARM_POOL_ENABLED` | `false` | Use warm pool |
| `MAX_ACTIVE_SESSIONS` | `2` | Max concurrent sessions |
| `MAX_MESSAGES_PER_SESSION` | `1000` | Message history limit |
| `MAX_EVENTS_PER_SESSION` | `50` | Event history limit |

## Connect API

Connect to port `3001` using Connect protocol (gRPC-compatible). See `proto/netclode/v1/client.proto` for full definitions.

### Bidirectional streaming

The main client API is a single bidirectional stream:

```protobuf
rpc Connect(stream ClientMessage) returns (stream ServerMessage);
```

### Client → Server messages

| Message Type | Fields | Description |
|--------------|--------|-------------|
| `create_session` | `name`, `repo?`, `repo_access?` | Create session |
| `list_sessions` | | List sessions |
| `open_session` | `session_id`, `last_notification_id?` | Open with history |
| `resume_session` | `session_id` | Resume paused |
| `pause_session` | `session_id` | Pause |
| `delete_session` | `session_id` | Delete |
| `delete_all_sessions` | | Delete all |
| `send_prompt` | `session_id`, `text` | Send prompt |
| `interrupt_prompt` | `session_id` | Interrupt |
| `expose_port` | `session_id`, `port` | Expose port |
| `terminal_input` | `session_id`, `data` | Terminal input |
| `terminal_resize` | `session_id`, `cols`, `rows` | Resize terminal |
| `sync` | | Get all sessions with metadata |
| `git_status` | `session_id` | Get git status |
| `git_diff` | `session_id`, `file?` | Get git diff |
| `list_github_repos` | | List available GitHub repos |
| `list_snapshots` | `session_id` | List session snapshots |
| `restore_snapshot` | `session_id`, `snapshot_id` | Restore to snapshot |

### Server → Client messages

| Message Type | Description |
|--------------|-------------|
| `session_created` | Session created |
| `session_updated` | Status changed |
| `session_deleted` | Deleted |
| `sessions_deleted_all` | All deleted |
| `session_list` | List of sessions |
| `session_state` | Session with history |
| `session_error` | Operation failed |
| `sync_response` | All sessions with metadata |
| `agent_message` | Text from agent (`partial` for streaming) |
| `agent_event` | Tool event |
| `agent_done` | Finished |
| `agent_error` | Error |
| `user_message` | User prompt (cross-client sync) |
| `port_exposed` | Port exposed with `preview_url` |
| `port_error` | Port exposure failed |
| `terminal_output` | Terminal output |
| `github_repos` | Available GitHub repos |
| `git_status` | Git status |
| `git_diff` | Git diff |
| `git_error` | Git operation failed |
| `snapshot_created` | Auto-snapshot created after turn |
| `snapshot_list` | List of session snapshots |
| `snapshot_restored` | Snapshot restored with message count |
| `error` | Generic error |

### Agent events

Delivered via `agent_event`:

| Kind | Description |
|------|-------------|
| `tool_start` | Tool started (includes `input` if available) |
| `tool_input` | Input delta (streaming) |
| `tool_input_complete` | Tool input finished (full `input` object) |
| `tool_end` | Tool completed |
| `file_change` | File created/edited/deleted |
| `command_start` | Shell command started |
| `command_end` | Shell command completed |
| `thinking` | Agent reasoning |
| `port_exposed` | Port exposed |
| `repo_clone` | Repository clone progress |

## HTTP

| Endpoint | Description |
|----------|-------------|
| `GET /health` | Health check |

## Redis

| Key | Type | Description |
|-----|------|-------------|
| `sessions:all` | Set | All session IDs |
| `session:{id}` | Hash | Session metadata |
| `session:{id}:messages` | List | Conversation history |
| `session:{id}:events:stream` | Stream | Tool events |
| `session:{id}:notifications` | Stream | Real-time notifications |

### Why Redis Streams

The classic approach for real-time updates is: fetch history, then subscribe to pub/sub. Problem is there's a race condition. Events that happen between the history fetch and the subscription are lost.

Redis Streams solve this with cursor-based reading. Each entry in a stream has an ID (e.g., `1234567890123-0`). When a client opens a session:

1. Server returns current state + `last_notification_id` (the latest stream ID)
2. Client stores this cursor
3. Server starts a blocking read with `XREAD BLOCK 0 STREAMS session:{id}:notifications {cursor}`
4. New events get pushed to the client as they arrive

On reconnect, the client sends its stored `last_notification_id`. The server resumes from that position. Events that happened while disconnected are delivered immediately.

```
Client A connects
    │
    ▼
Server: XREAD BLOCK ... $  ($ = only new events)
    │
    ├──── Event 1 arrives ──► Client A receives
    ├──── Event 2 arrives ──► Client A receives
    │
Client A disconnects (cursor = "1234567890123-1")
    │
    ├──── Event 3 arrives ──► stored in stream
    ├──── Event 4 arrives ──► stored in stream
    │
Client A reconnects with cursor "1234567890123-1"
    │
    ▼
Server: XREAD BLOCK ... 1234567890123-1
    │
    ├──── Event 3 delivered immediately
    ├──── Event 4 delivered immediately
    └──── Resume blocking for new events
```

Multi-client sync works the same way. iOS app and web client on the same session both get events through separate XREAD consumers on the same stream.

The streams are trimmed to keep memory bounded (`MAXLEN ~50` for events, configurable via `MAX_EVENTS_PER_SESSION`).

## Data flow

```
┌─────────┐      Connect       ┌───────────────┐      Connect       ┌─────────┐
│ Client  │◄──────────────────►│ Control Plane │◄──────────────────►│  Agent  │
└─────────┘                    └───────┬───────┘                    └─────────┘
                                       │
                                       ▼
                               ┌───────────────┐
                               │     Redis     │
                               └───────────────┘
```

1. Client sends prompt via Connect stream
2. Control plane persists to Redis, publishes to notifications stream
3. Control plane forwards to agent via Connect
4. Agent streams responses back
5. Control plane persists and publishes to Redis Stream
6. All clients read via XREAD BLOCK

## Terminal proxy

The control plane proxies terminal I/O between clients and the agent's PTY:

```
Client                Control Plane              Agent
  │                        │                       │
  │ terminal_input ───────►│                       │
  │                        │ Connect stream ──────►│
  │                        │                       │ node-pty
  │                        │◄─── Connect stream ───│
  │◄─── terminal_output ───│                       │
```

The control plane maintains one Connect bidirectional stream per active session to the agent's Terminal RPC. Multiple clients can connect to the same session and share the same PTY.

Terminal data is ephemeral (not persisted to Redis).

## Preview URLs

When a client sends `expose_port`, the control plane:

1. Creates a Tailscale Service for the sandbox pod (if not already created)
2. Waits for Tailscale to assign a MagicDNS hostname
3. Returns `port_exposed` with the preview URL

```
sandbox-{sessionId}.tailnet-name.ts.net:{port}
```

The sandbox pod gets its own Tailscale identity, so preview URLs are accessible from any device on your tailnet. Each sandbox can expose multiple ports on the same hostname.

## Session lifecycle

```
create → creating → running ↔ paused
              │         │        │
              └─────────┴────────┼──→ deleted
                   error ←───────┘
```

### Auto-pause

When `MAX_ACTIVE_SESSIONS` is reached and a new session needs to run, the control plane automatically pauses the oldest inactive session. "Inactive" means no prompt currently running.

Many paused sessions (cheap, just S3 storage), limited concurrent VMs (expensive, memory/CPU).

### Warm pool

When `WARM_POOL_ENABLED=true`, the control plane creates `SandboxClaim` resources instead of `Sandbox` resources directly. The agent-sandbox-controller assigns a pre-booted VM from the warm pool.

Pre-booted VMs are already running and have their JuiceFS PVC mounted, so session start is nearly instant (~1s vs ~30s cold start).

Since warm pool pods are created before we know which session they'll serve, they can't receive per-session env vars at boot. Instead, the agent calls `GET /internal/session-config?pod=<podName>` to fetch its configuration (session ID, API key, git repo) after startup.

## Development

```bash
go run ./cmd/control-plane
go test ./...
go build -o control-plane ./cmd/control-plane
```

## Docker

```bash
docker build -t control-plane -f Dockerfile .
```
