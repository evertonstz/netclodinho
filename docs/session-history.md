# Session History

Netclode automatically creates snapshots after each agent turn, allowing you to roll back both the workspace and chat history to any previous point.

## Overview

When the agent completes a turn:
1. A snapshot of `/agent/workspace` is created using JuiceFS copy-on-write clones
2. The current message count is recorded with the snapshot
3. Up to 10 snapshots are retained per session (oldest auto-deleted)

When you restore a snapshot:
1. The workspace is restored from the snapshot
2. Chat messages are truncated to match the snapshot point
3. You can continue the conversation from that point

## How It Works

### Snapshot Creation Flow

```
┌─────────────┐     ┌──────────────────┐     ┌─────────────────┐
│   Client    │     │  Control Plane   │     │     Agent       │
└─────────────┘     └────────┬─────────┘     └────────┬────────┘
                             │                        │
      Agent turn completes   │                        │
      ◄──────────────────────┤                        │
                             │                        │
                             │  CreateSnapshotCommand │
                             │───────────────────────►│
                             │                        │
                             │                        │ juicefs clone
                             │                        │ /agent/workspace
                             │                        │ /agent/.snapshots/{id}
                             │                        │
                             │   AgentSnapshotResult  │
                             │◄───────────────────────│
                             │                        │
      Save snapshot metadata │                        │
      (Redis)                │                        │
                             │                        │
      snapshot.created       │                        │
      ◄──────────────────────│                        │
```

### Restore Flow

```
┌─────────────┐     ┌──────────────────┐     ┌─────────────────┐
│   Client    │     │  Control Plane   │     │     Agent       │
└──────┬──────┘     └────────┬─────────┘     └────────┬────────┘
       │                     │                        │
       │ RestoreSnapshot     │                        │
       │────────────────────►│                        │
       │                     │                        │
       │                     │ RestoreSnapshotCommand │
       │                     │───────────────────────►│
       │                     │                        │
       │                     │                        │ mv workspace → backup
       │                     │                        │ juicefs clone
       │                     │                        │ snapshot → workspace
       │                     │                        │ rm backup
       │                     │                        │
       │                     │   AgentSnapshotResult  │
       │                     │◄───────────────────────│
       │                     │                        │
       │                     │ Truncate messages      │
       │                     │ Truncate events        │
       │                     │ (Redis)                │
       │                     │                        │
       │ snapshot.restored   │                        │
       │◄────────────────────│                        │
       │                     │                        │
       │ Reload chat history │                        │
       │                     │                        │
```

### JuiceFS Copy-on-Write

Snapshots use `juicefs clone`, which is a metadata-only operation:

- **Instant**: No data is copied, only metadata pointers
- **Space-efficient**: Storage only grows when files diverge
- **Atomic**: Clone completes in milliseconds regardless of workspace size

```bash
# What the agent runs
juicefs clone /agent/workspace /agent/.snapshots/{snapshot-id}
```

If JuiceFS isn't available (local development), the agent falls back to `cp -a`.

## Storage

### Agent Filesystem

```
/agent/
├── workspace/              # Current working directory
└── .snapshots/
    ├── {snapshot-id}/      # Cloned workspace state
    ├── {snapshot-id}.meta.json
    └── ...
```

Each snapshot has a metadata file:

```json
{
  "id": "abc123def456",
  "name": "Turn 3: Fix the authentication bug",
  "createdAt": "2026-01-25T10:30:00Z"
}
```

### Redis

Snapshot metadata is stored in Redis:

| Key | Type | Description |
|-----|------|-------------|
| `session:{id}:snapshots` | Sorted Set | Snapshot IDs scored by timestamp |
| `session:{id}:snapshot:{snapshotId}` | Hash | Snapshot metadata (name, size, turn, message count, event stream ID) |

## API

### Client Messages

| Message | Fields | Description |
|---------|--------|-------------|
| `list_snapshots` | `session_id` | List all snapshots for a session |
| `restore_snapshot` | `session_id`, `snapshot_id` | Restore to a snapshot |

### Server Messages

| Message | Fields | Description |
|---------|--------|-------------|
| `snapshot_created` | `session_id`, `snapshot` | Auto-snapshot created after turn |
| `snapshot_list` | `session_id`, `snapshots[]` | Response to list request |
| `snapshot_restored` | `session_id`, `snapshot_id`, `message_count` | Restore completed |

### Snapshot Object

```json
{
  "id": "abc123def456",
  "sessionId": "sess-xyz789",
  "name": "Turn 3: Fix the authentication bug",
  "createdAt": "2026-01-25T10:30:00Z",
  "sizeBytes": 1048576,
  "turnNumber": 3,
  "messageCount": 7,
  "eventStreamId": "1706180400000-0"
}
```

The `eventStreamId` is the Redis Stream ID of the last event at snapshot time. On restore, all events after this ID are deleted to match the snapshot state.

## Retention

- **Maximum snapshots**: 10 per session
- **Auto-cleanup**: After creating a new snapshot, oldest snapshots beyond the limit are deleted
- **Session delete**: All snapshots are deleted when a session is deleted

## iOS App

Access session history from the workspace menu:

1. Open a session
2. Tap the menu (top right)
3. Tap "History"
4. Select a snapshot to restore
5. Confirm the restore action

The UI shows:
- Snapshot name (turn number + prompt summary)
- Relative timestamp
- Message count at that point

## Limitations

- **Running sessions**: Cannot restore while the agent is processing a prompt (session must be in "ready" state)
- **Workspace only**: Docker containers/images are not snapshot (they persist in `/agent/docker` which is outside workspace)
- **No manual snapshots**: Snapshots are only created automatically after turns

## Troubleshooting

### Snapshot creation fails

Check agent logs:

```bash
kubectl --context netclode -n netclode logs <agent-pod> | grep snapshot
```

Common causes:
- Disk full (check JuiceFS quota)
- Permissions issue on `.snapshots` directory

### Restore fails

- Ensure session is not running (pause first if needed)
- Check that the snapshot still exists (may have been auto-cleaned)

### Snapshots not appearing in UI

- Check control-plane logs for `snapshot.created` events
- Verify Redis connectivity
- Try reopening the session to refresh
