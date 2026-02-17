# Session Lifecycle

How sessions move through different states.

## Statuses

| Status | Meaning |
|--------|---------|
| `CREATING` | New session, sandbox being provisioned |
| `RESUMING` | Paused session being resumed, sandbox starting |
| `READY` | Sandbox running, waiting for user prompt |
| `RUNNING` | Agent actively processing a prompt |
| `PAUSED` | Sandbox deleted, session data preserved |
| `INTERRUPTED` | Agent disconnected mid-task, needs user action |
| `ERROR` | Something went wrong |

## Lifecycle Diagrams

### New Session Creation

```
CreateSession()
      |
      v
 ┌─────────┐
 │CREATING │  <-- Initial status
 └────┬────┘
      |
      v  createSandbox() [warm pool or direct]
      |
      v  sandbox becomes ready
 ┌─────────┐
 │ READY   │  <-- No pending prompt (waiting for user)
 └─────────┘
      OR
 ┌─────────┐
 │ RUNNING │  <-- Pending prompt exists (auto-processing)
 └─────────┘
```

### Normal Operation

```
 ┌─────────┐  user sends prompt   ┌─────────┐
 │ READY   │ -------------------> │ RUNNING │
 └─────────┘                      └────┬────┘
      ^                                |
      |    agent completes response    |
      +--------------------------------+
```

### Pause / Resume

```
 ┌─────────┐  sandbox deleted     ┌─────────┐
 │ READY   │ -------------------> │ PAUSED  │
 └─────────┘  (manual/auto)       └────┬────┘
                                       |
                                       | Resume() called
                                       v
                                 ┌──────────┐
                                 │ RESUMING │  <-- Creating new sandbox
                                 └────┬─────┘
                                      |
                                      v sandbox ready
                                 ┌─────────┐
                                 │ READY   │
                                 └─────────┘
```

A session can be paused manually or automatically by two triggers:

- **Capacity limit** (`MAX_ACTIVE_SESSIONS`): When the limit is reached and a new session needs to run, the oldest session (by `LastActiveAt`) is paused to make room.
- **Idle timeout** (`IDLE_TIMEOUT_MINUTES`): When set (default `0` = disabled), a background reaper checks every minute and pauses any active session idle longer than the configured duration.

### Agent Disconnect (while running)

```
 ┌─────────┐  agent disconnects   ┌─────────────┐
 │ RUNNING │ -------------------> │ INTERRUPTED │
 └─────────┘  (crash/timeout)     └─────────────┘
```

User must acknowledge and decide: retry or continue.

### Snapshot Restore

```
 ┌─────────┐  RestoreSnapshot()   ┌─────────┐
 │ READY   │ -------------------> │ PAUSED  │  <-- Cleanup in progress
 └─────────┘                      └────┬────┘
                                       |
                                       | Resume() with snapshotID
                                       v
                                 ┌──────────┐
                                 │ RESUMING │  <-- Restoring PVC + creating sandbox
                                 └────┬─────┘
                                      |
                                      v sandbox ready
                                 ┌─────────┐
                                 │ READY   │
                                 └─────────┘
```

## Sandbox Creation Paths

| Path | When | Speed |
|------|------|-------|
| `createSandboxViaClaim` | New sessions (warm pool) | Fast (~seconds) |
| `createSandboxDirect` | No warm pool or snapshot restore | Slower (~30-60s) |

Snapshot restore uses `createSandboxDirect` because it needs to create a PVC from the VolumeSnapshot first.

## Code

- `services/control-plane/internal/session/state.go` - status definitions
- `services/control-plane/internal/session/manager.go` - CreateSession, Resume, RestoreSnapshot
- `proto/netclode/v1/common.proto` - SessionStatus enum
