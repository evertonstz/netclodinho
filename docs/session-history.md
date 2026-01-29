# Session History

Netclode automatically creates snapshots after each agent turn, allowing you to roll back both the workspace and chat history to any previous point.

## Overview

When the agent completes a turn:
1. The control plane creates a Kubernetes VolumeSnapshot of the agent's PVC
2. The current message count and event stream ID are recorded with the snapshot
3. Up to 10 snapshots are retained per session (oldest auto-deleted)

When you restore a snapshot:
1. The agent pod is deleted
2. A new PVC is created from the VolumeSnapshot
3. Chat messages and events are truncated to match the snapshot point
4. The session becomes "ready" for the sandbox to be recreated

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

**Note**: When restoring, the warm pool is bypassed. The restore follows a specific order to prevent race conditions:

1. **Create standalone PVC first**: A PVC is created with `dataSource` pointing to the VolumeSnapshot
2. **Wait for restore job**: JuiceFS CSI creates a restore job that copies snapshot data to the new PVC. We wait for this job to complete before proceeding.
3. **Create sandbox with existing PVC**: The sandbox is created using `volumes` (referencing the existing PVC) instead of `volumeClaimTemplates`. This ensures the pod doesn't mount the volume until the restore is complete.

This ordering is critical because if the pod mounts the volume while the restore job is running, the restore fails with "directory not empty".

After the restore job completes successfully, the old PVC is deleted to avoid resource leaks.

### Kubernetes VolumeSnapshots

Snapshots use the Kubernetes VolumeSnapshot API with the JuiceFS CSI driver:

- **Fast**: VolumeSnapshots are typically metadata-only operations
- **Space-efficient**: Storage only grows when files diverge (copy-on-write)
- **Full state**: Captures the entire `/agent` directory including:
  - `/agent/workspace` - the code/repo
  - `/agent/.claude` - Claude SDK session data
  - `/agent/.session-mapping.json` - SDK session mapping
  - `/agent/.local` - mise/tool data

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

The `streamId` is the Redis Stream ID of the last entry at snapshot time. On restore, all entries after this ID are deleted to match the snapshot state.

## Retention

- **Maximum snapshots**: 10 per session
- **Auto-cleanup**: After creating a new snapshot, oldest snapshots beyond the limit are deleted
- **Session delete**: All snapshots (both Redis metadata and K8s VolumeSnapshots) are deleted when a session is deleted

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

## Infrastructure Requirements

The snapshot feature requires the following Kubernetes components (deployed via `infra/ansible/roles/juicefs-csi/`):

| Component | Description |
|-----------|-------------|
| **VolumeSnapshot CRDs** | Custom resource definitions from [external-snapshotter](https://github.com/kubernetes-csi/external-snapshotter) that define VolumeSnapshot, VolumeSnapshotContent, and VolumeSnapshotClass resources |
| **Snapshot Controller** | Watches VolumeSnapshot objects and triggers the CSI driver to create/delete snapshots |
| **csi-snapshotter sidecar** | Sidecar container in the JuiceFS CSI controller that handles snapshot operations. The upstream JuiceFS CSI driver doesn't include this by default, so Ansible patches the StatefulSet to add it. |
| **VolumeSnapshotClass** | Defines the CSI driver (`csi.juicefs.com`) and deletion policy for snapshots |
| **RBAC** | Control-plane ServiceAccount needs: (1) permissions to create/delete VolumeSnapshots in netclode namespace, (2) ClusterRole to read Jobs in kube-system (to wait for JuiceFS restore jobs) |

## Limitations

- **Destructive restore**: Restoring to a snapshot deletes all data after that point - workspace files, messages, events, and newer snapshots. This is similar to `git reset --hard`.
- **Running sessions**: Cannot restore while the agent is processing a prompt (session must be in "ready" state)
- **Pod restart**: Restore requires deleting and recreating the agent pod, which resets terminal state and running processes
- **Bypasses warm pool**: Restored sessions use a dedicated sandbox (not from the warm pool) to ensure the PVC is created from the snapshot
- **No manual snapshots**: Snapshots are only created automatically after turns

## Troubleshooting

### Snapshot creation fails

Check control-plane logs:

```bash
kubectl --context netclode -n netclode logs -l app=control-plane | grep -i snapshot
```

Common causes:
- VolumeSnapshotClass not configured
- JuiceFS CSI driver not supporting snapshots
- Quota exceeded

### Restore fails

- Ensure session is not running (pause first if needed)
- Check that the VolumeSnapshot still exists:
  ```bash
  kubectl --context netclode -n netclode get volumesnapshots -l netclode.io/session={session-id}
  ```
- Check control-plane logs for restore errors

### Snapshots not appearing in UI

- Check control-plane logs for `snapshot.created` events
- Verify Redis connectivity
- Try reopening the session to refresh
