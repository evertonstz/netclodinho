# Sandbox Architecture

Netclode sandboxes provide isolated, persistent environments for AI agents to execute code. This document covers the architecture for operators deploying Netclode.

## Overview

Each session runs in a sandbox that provides:

- **VM-level isolation** via Kata Containers (microVMs)
- **Persistent storage** via JuiceFS (copy-on-write, snapshots)
- **Fast startup** via warm pool (pre-booted VMs)
- **Network isolation** via Kubernetes NetworkPolicy

```
┌─────────────────────────────────────────────────────────────┐
│                     Kubernetes Node                          │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────┐  │
│  │   Sandbox Pod   │  │   Sandbox Pod   │  │  Warm Pool  │  │
│  │  (Kata microVM) │  │  (Kata microVM) │  │    Pods     │  │
│  │                 │  │                 │  │             │  │
│  │  ┌───────────┐  │  │  ┌───────────┐  │  │  (ready to  │  │
│  │  │   Agent   │  │  │  │   Agent   │  │  │   assign)   │  │
│  │  └───────────┘  │  │  └───────────┘  │  │             │  │
│  │       │         │  │       │         │  │             │  │
│  │  JuiceFS PVC    │  │  JuiceFS PVC    │  │  JuiceFS    │  │
│  └─────────────────┘  └─────────────────┘  └─────────────┘  │
│           │                   │                   │          │
│           └───────────────────┴───────────────────┘          │
│                              │                               │
│                      JuiceFS CSI Driver                      │
│                              │                               │
└──────────────────────────────┼───────────────────────────────┘
                               │
                        ┌──────┴──────┐
                        │  S3 Bucket  │
                        │  (data)     │
                        └──────┬──────┘
                               │
                        ┌──────┴──────┐
                        │   Redis     │
                        │  (metadata) │
                        └─────────────┘
```

## Kata Containers

Sandboxes use [Kata Containers](https://katacontainers.io/) to run each pod in a lightweight VM (microVM).

### Why Kata?

- **Strong isolation**: Each sandbox is a separate VM, not just a container
- **Untrusted code**: Agents can run arbitrary code safely
- **Root access**: Agents have sudo without risking the host
- **Docker-in-Docker**: Full Docker support inside the VM

### Requirements

The Kubernetes node must support nested virtualization:
- DigitalOcean droplets
- Vultr VMs
- Most cloud providers except Hetzner Cloud

### Runtime Class

Sandboxes use the `kata-clh` RuntimeClass:

```yaml
apiVersion: node.k8s.io/v1
kind: RuntimeClass
metadata:
  name: kata-clh
handler: kata-clh
```

The handler uses Cloud Hypervisor (CLH) as the VMM.

### Resource Limits

VM resources are configured in Kata settings:

| Setting | Default | Description |
|---------|---------|-------------|
| `KATA_VM_CPUS` | 4 | vCPUs per VM |
| `KATA_VM_MEMORY_MB` | 4096 | RAM per VM |

## JuiceFS Storage

[JuiceFS](https://juicefs.com/) provides POSIX-compliant persistent storage with:

- **Copy-on-write**: Efficient snapshots
- **S3 backend**: Data stored in object storage
- **Redis metadata**: Fast file operations

### Architecture

```
Agent Pod ──► virtiofs ──► JuiceFS mount ──► S3 + Redis
```

The JuiceFS CSI driver mounts volumes into pods. For Kata Containers, virtiofs passes the mount into the VM with caching enabled.

### Workspace Layout

```
/agent/                     # PVC mount point (persistent)
├── workspace/              # User's code
├── docker/                 # Docker data
├── .local/share/mise/      # Installed tools
├── .cache/                 # Package caches
├── .claude/                # SDK session data
└── .session-mapping.json   # Session ID mapping
```

### Performance Tuning

JuiceFS with S3 backend has high latency for small file operations. Caching is essential:

| Configuration | IOPS |
|---------------|------|
| No caching | ~30 |
| + JuiceFS writeback | ~400 |
| + virtiofs cache | ~650 |

Configuration in `infra/k8s/juicefs-config.yaml`:

```yaml
mountOptions:
  - writeback           # Async writes
  - cache-dir=/var/jfsCache
  - cache-size=102400   # 100GB cache
```

### Maintenance

See [JuiceFS Maintenance Guide](../infra/docs/juicefs-maintenance.md) for:
- Garbage collection
- Trash cleanup
- Monitoring Redis memory

## Warm Pool

The warm pool keeps pre-booted VMs ready for instant session allocation.

### How It Works

1. SandboxWarmPool maintains N ready pods
2. Session creation claims a pod from the pool
3. Agent calls control-plane API to get session config
4. Pool replenishes automatically

### Configuration

```yaml
apiVersion: extensions.agents.x-k8s.io/v1alpha1
kind: SandboxWarmPool
metadata:
  name: netclode-pool
spec:
  replicas: 2                    # Number of warm pods
  templateRef:
    name: netclode-agent         # SandboxTemplate to use
```

Enable in control-plane:

```yaml
env:
  - name: WARM_POOL_ENABLED
    value: "true"
```

### Startup Time Comparison

| Mode | Startup Time |
|------|--------------|
| Cold start (no warm pool) | ~30s |
| Warm pool | ~1s |

### Session Config API

Since warm pool pods start before session assignment, they can't receive per-session env vars at boot. Instead, agents poll for config:

```
GET /internal/session-config?pod=<podName>
```

Returns session ID, API keys, repo URL, etc.

## Custom Resources

### Sandbox

Represents a running sandbox pod:

```yaml
apiVersion: agents.x-k8s.io/v1alpha1
kind: Sandbox
metadata:
  name: sandbox-sess-abc123
spec:
  runtimeClassName: kata-clh
  template:
    spec:
      containers:
        - name: agent
          image: ghcr.io/angristan/netclode-agent:latest
```

### SandboxClaim

Claims a pod from the warm pool:

```yaml
apiVersion: extensions.agents.x-k8s.io/v1alpha1
kind: SandboxClaim
metadata:
  name: claim-sess-abc123
spec:
  poolRef:
    name: netclode-pool
```

### SandboxTemplate

Defines the pod template for warm pool:

```yaml
apiVersion: extensions.agents.x-k8s.io/v1alpha1
kind: SandboxTemplate
metadata:
  name: netclode-agent
spec:
  template:
    spec:
      runtimeClassName: kata-clh
      containers:
        - name: agent
          # ...
  volumeClaimTemplates:
    - metadata:
        name: workspace
      spec:
        storageClassName: juicefs-sc
        accessModes: ["ReadWriteOnce"]
        resources:
          requests:
            storage: 50Gi
```

## Network Isolation

Sandboxes are network-isolated via Kubernetes NetworkPolicy.

### Default Policy

Sandboxes can:
- Access the internet (external IPs)
- Reach the control-plane (for config, health)

Sandboxes cannot:
- Reach other pods (10.42.0.0/16)
- Reach services (10.43.0.0/16)
- Reach private networks (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16)
- Reach Tailnet (100.64.0.0/10) by default

### Tailnet Access

Enable with `--tailnet` flag when creating a session:

```bash
netclode sessions create --repo owner/repo --tailnet
```

This allows the sandbox to reach other devices on your Tailscale network.

### Port Exposure

Exposed ports allow inbound traffic from the Tailnet:

```yaml
ingress:
  - ports:
    - port: 3000
    from:
      - ipBlock:
          cidr: 100.64.0.0/10  # Tailscale range
```

## Session Lifecycle

```
create ──► creating ──► ready ◄──► running
               │          │           │
               │          ▼           │
               │       paused ◄───────┘
               │          │
               └──────────┴──────► deleted
```

### Creating

1. Control-plane creates Sandbox (or SandboxClaim for warm pool)
2. Kata boots a microVM
3. JuiceFS PVC is mounted
4. Agent starts and registers with control-plane

### Paused

1. Control-plane deletes Sandbox (VM stops)
2. Session anchor ConfigMap preserves PVC
3. PVC retains workspace data

### Resumed

1. Control-plane creates new Sandbox with same PVC
2. New VM boots with preserved workspace
3. Agent registers and resumes SDK session

### Deleted

1. Session anchor ConfigMap deleted
2. PVC garbage collected
3. JuiceFS data eventually cleaned up

## PVC Preservation (Session Anchors)

When a session is paused, the Sandbox CR is deleted. Without protection, the PVC would be garbage collected.

**Solution**: A ConfigMap "anchor" acts as a second owner:

1. Session created → ConfigMap `session-anchor-<id>` created
2. PVC gets two `ownerReferences`: Sandbox + ConfigMap
3. Pause → Sandbox deleted, ConfigMap keeps PVC alive
4. Resume → New Sandbox uses existing PVC
5. Delete → ConfigMap deleted, PVC garbage collected

## Troubleshooting

### Pods stuck in Pending

Check available resources:
```bash
kubectl describe node | grep -A5 "Allocated resources"
```

Check warm pool status:
```bash
kubectl --context netclode -n netclode get sandboxwarmpool
kubectl --context netclode -n netclode get pods -l agents.x-k8s.io/pool
```

### JuiceFS mount failures

Check CSI driver logs:
```bash
kubectl --context netclode -n kube-system logs -l app=juicefs-csi-driver
```

Verify secret exists:
```bash
kubectl --context netclode -n netclode get secret juicefs-secret
```

### Kata not starting

Check containerd config:
```bash
ssh root@netclode cat /var/lib/rancher/k3s/agent/etc/containerd/config.toml.tmpl | grep kata
```

Verify Kata runtime:
```bash
ssh root@netclode /opt/kata/bin/kata-runtime kata-env
```

### Session anchor issues

List anchors:
```bash
kubectl --context netclode -n netclode get configmap -l netclode.dev/component=session-anchor
```

Check PVC ownership:
```bash
kubectl --context netclode -n netclode get pvc <pvc-name> -o jsonpath='{.metadata.ownerReferences}' | jq
```

## Resource Sizing

### Single Node (2 CPU, 8GB)

- Max concurrent sessions: 1-2
- Warm pool replicas: 1
- Scale down CoreDNS: 2 replicas

### Production (4+ CPU, 16GB+)

- Max concurrent sessions: 3-5
- Warm pool replicas: 2-3
- Consider separate Redis for JuiceFS metadata
