# Session History

Auto-snapshots after each agent turn let you roll back workspace and chat to any previous point.

## Overview

After each turn, control-plane creates a VolumeSnapshot of the agent's PVC. Up to 10 snapshots retained per session.

When you restore:
1. Agent pod deleted
2. New PVC created from snapshot
3. Messages and events truncated to match
4. Session becomes ready to recreate sandbox

## How It Works

### Snapshot Creation Flow

```
┌─────────────┐     ┌──────────────────┐     ┌─────────────────┐
│   Client    │     │  Control Plane   │     │   Kubernetes    │
└─────────────┘     └────────┬─────────┘     └────────┬────────┘
                             │                        │
      Agent turn completes   │                        │
      ◄──────────────────────┤                        │
                             │                        │
                             │ Create VolumeSnapshot  │
                             │───────────────────────►│
                             │                        │
                             │ Wait for ready         │
                             │◄──────────────────────►│
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
│   Client    │     │  Control Plane   │     │   Kubernetes    │
└──────┬──────┘     └────────┬─────────┘     └────────┬────────┘
       │                     │                        │
       │ RestoreSnapshot     │                        │
       │────────────────────►│                        │
       │                     │                        │
       │                     │ Orphan old PVC         │
       │                     │ (remove ownerRefs)     │
       │                     │───────────────────────►│
       │                     │                        │
       │                     │ Delete sandbox/claim   │
       │                     │───────────────────────►│
       │                     │                        │
       │                     │ Store restore snapshot │
       │                     │ ID in session state    │
       │                     │                        │
       │                     │ Truncate messages      │
       │                     │ Truncate events        │
       │                     │ (Redis)                │
       │                     │                        │
       │ snapshot.restored   │                        │
       │◄────────────────────│                        │
       │                     │                        │
       │ Resume session      │                        │
       │────────────────────►│                        │
       │                     │                        │
       │                     │ 1. Create PVC with     │
       │                     │    dataSource pointing │
       │                     │    to VolumeSnapshot   │
       │                     │───────────────────────►│
       │                     │                        │
       │                     │ 2. Wait for JuiceFS    │
       │                     │    restore job to      │
       │                     │    complete            │
       │                     │◄──────────────────────►│
       │                     │                        │
       │                     │ 3. Create Sandbox with │
       │                     │    existing PVC (via   │
       │                     │    volumes, not        │
       │                     │    volumeClaimTemplate)│
       │                     │───────────────────────►│
```

Restore bypasses the warm pool. Order matters: create PVC from snapshot → wait for JuiceFS restore job → create sandbox with existing PVC. If the pod mounts before restore completes, it fails with "directory not empty".

### VolumeSnapshots

Uses Kubernetes VolumeSnapshot API with JuiceFS CSI:

- **Fast** - metadata-only operation
- **Space-efficient** - copy-on-write, storage only grows when files diverge
- **Full state** - captures entire `/agent` directory (workspace, SDK session, tools)

```yaml
apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshot
metadata:
  name: sess-{session-id}-snap-{snapshot-id}
  namespace: netclode
spec:
  volumeSnapshotClassName: juicefs-snapclass
  source:
    persistentVolumeClaimName: workspace-sess-{session-id}
```

## Storage

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
  "sizeBytes": 0,
  "turnNumber": 3,
  "messageCount": 7,
  "streamId": "1706180400000-0"
}
```

The `streamId` is the Redis Stream ID of the last entry at snapshot time. On restore, entries after this ID are deleted.

## Retention

- Max 10 snapshots per session
- Oldest auto-deleted when limit reached
- All snapshots deleted with session

## iOS App

Session menu → History → select snapshot to restore.

## Infrastructure

Deployed via `infra/ansible/roles/juicefs-csi/`:

- VolumeSnapshot CRDs from external-snapshotter
- Snapshot controller
- csi-snapshotter sidecar (patched into JuiceFS CSI)
- VolumeSnapshotClass for juicefs
- RBAC for control-plane to manage snapshots

## Limitations

- **Destructive** - restoring deletes everything after that point (like `git reset --hard`)
- **Session must be ready** - can't restore while agent is processing
- **Pod restart** - terminal state and running processes reset
- **No manual snapshots** - auto-created after turns only

## Troubleshooting

**Snapshot creation fails** - check control-plane logs:
```bash
kubectl --context netclode -n netclode logs -l app=control-plane | grep -i snapshot
```

**Restore fails** - ensure session is paused, check VolumeSnapshot exists:
```bash
kubectl --context netclode -n netclode get volumesnapshots -l netclode.io/session={session-id}
```

**Snapshots not appearing** - check control-plane logs for `snapshot.created` events, try reopening session.
