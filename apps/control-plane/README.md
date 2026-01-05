# Control Plane

Session management server for Netclode. Manages agent VMs via containerd/nerdctl and provides WebSocket API for clients.

## Structure

```
apps/control-plane/
├── src/
│   ├── index.ts          # Entry point, HTTP/WebSocket server
│   ├── config.ts         # Configuration from environment
│   ├── api/
│   │   └── ws-server.ts  # WebSocket message handling
│   ├── sessions/
│   │   └── manager.ts    # Session lifecycle management
│   ├── runtime/
│   │   └── nerdctl.ts    # containerd/nerdctl integration
│   └── storage/
│       └── juicefs.ts    # JuiceFS workspace operations
├── package.json
└── tsconfig.json
```

## Configuration

Environment variables:

| Variable | Description | Default |
|----------|-------------|---------|
| `PORT` | HTTP server port | `3000` |
| `ANTHROPIC_API_KEY` | Anthropic API key | Required |
| `JUICEFS_ROOT` | JuiceFS mount path | `/juicefs` |
| `AGENT_IMAGE` | Agent container image | `ghcr.io/stanislas/netclode-agent:latest` |
| `DEFAULT_CPUS` | Default VM CPU count | `2` |
| `DEFAULT_MEMORY_MB` | Default VM memory (MB) | `2048` |

## Development

```bash
# Install dependencies
bun install

# Run in development
bun run dev

# Type check
bun run typecheck
```

Note: Full functionality requires containerd and JuiceFS, which are only available on the NixOS server.

## API

### HTTP Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/health` | GET | Health check |
| `/ws` | GET | WebSocket upgrade |

### WebSocket Messages

Connect to `/ws` for real-time session management.

#### Client → Server

**Create Session**
```json
{
  "type": "session.create",
  "name": "my-project",
  "repo": "https://github.com/user/repo"
}
```

**List Sessions**
```json
{ "type": "session.list" }
```

**Resume Session**
```json
{ "type": "session.resume", "id": "abc123" }
```

**Pause Session**
```json
{ "type": "session.pause", "id": "abc123" }
```

**Delete Session**
```json
{ "type": "session.delete", "id": "abc123" }
```

**Send Prompt**
```json
{
  "type": "prompt",
  "sessionId": "abc123",
  "text": "Fix the bug in auth.ts"
}
```

**Interrupt Prompt**
```json
{ "type": "prompt.interrupt", "sessionId": "abc123" }
```

#### Server → Client

**Session Created**
```json
{
  "type": "session.created",
  "session": { "id": "abc123", "name": "my-project", "status": "ready", ... }
}
```

**Session List**
```json
{
  "type": "session.list",
  "sessions": [...]
}
```

**Agent Event**
```json
{
  "type": "agent.event",
  "sessionId": "abc123",
  "event": { "type": "tool_call", "tool": "Read", ... }
}
```

**Agent Done**
```json
{ "type": "agent.done", "sessionId": "abc123" }
```

**Error**
```json
{ "type": "error", "message": "Something went wrong" }
```

## Session Lifecycle

```
┌──────────┐     create      ┌──────────┐
│          │ ───────────────►│ creating │
│  (none)  │                 └────┬─────┘
│          │                      │
└──────────┘                      ▼
                            ┌──────────┐
       resume               │  ready   │◄────────┐
    ┌──────────────────────►└────┬─────┘         │
    │                            │               │
    │                            │ resume        │
    │                            ▼               │
    │                       ┌──────────┐         │
    │                       │ running  │─────────┘
    │                       └────┬─────┘  agent done
    │                            │
    │         pause              │
    │   ┌────────────────────────┘
    │   │
    │   ▼
┌───┴──────┐     delete     ┌──────────┐
│  paused  │ ──────────────►│ (deleted)│
└──────────┘                └──────────┘
```

## Runtime Integration

The control plane manages VMs via nerdctl:

```typescript
// Create VM
await runtime.createVM({
  sessionId: "abc123",
  cpus: 2,
  memoryMB: 2048,
});

// Execute command in VM
const result = await runtime.execInVM(sessionId, ["ls", "-la"]);

// Stop and remove VM
await runtime.stopVM(sessionId);
await runtime.removeVM(sessionId);
```

## Storage Integration

Workspaces are stored on JuiceFS:

```typescript
// Create workspace
await storage.createWorkspace(sessionId);

// Clone repo
await storage.cloneRepo(sessionId, "https://github.com/user/repo");

// Snapshot
await storage.createSnapshot(sessionId, "turn-5");
await storage.restoreSnapshot(sessionId, "turn-5");
```

## Deployment

The control plane runs as a systemd service on the NixOS host:

```bash
# View logs
journalctl -u netclode -f

# Restart
systemctl restart netclode

# Status
systemctl status netclode
```

Code is deployed to `/opt/netclode` via rsync.
