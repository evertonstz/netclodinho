# Sandbox Architecture

Each session runs in an isolated sandbox with:

- **VM isolation** via Kata Containers (microVMs)
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

Each sandbox runs in a [Kata Container](https://katacontainers.io/) - a lightweight VM using Cloud Hypervisor.

**Why Kata?** Strong isolation (separate VM, not just namespaces), agents can run arbitrary code safely, sudo without risking host, full Docker-in-Docker support.

**Requirements:** Node must support nested virtualization (DigitalOcean, Vultr work; Hetzner Cloud doesn't).

**Resources:** Default 4 vCPUs, 4GB RAM per VM (configurable via `KATA_VM_CPUS`, `KATA_VM_MEMORY_MB`).

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

### Session Assignment

Since warm pool pods start before session assignment, they can't receive per-session env vars at boot. Instead, agents connect via gRPC and receive config when a session is assigned:

1. Agent reads Kubernetes ServiceAccount token from `/var/run/secrets/kubernetes.io/serviceaccount/token`
2. Agent connects to control-plane via gRPC with the token
3. Control-plane validates token via Kubernetes TokenReview API (prevents impersonation)
4. When a SandboxClaim binds to this pod, control-plane pushes `SessionAssigned` message with config

This provides mutual authentication - the control-plane cryptographically verifies the agent's pod identity.

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
- Reach the control-plane (for config, events)
- Reach the secret-proxy (for API requests)
- Resolve DNS

Sandboxes cannot:
- Access the internet directly (must go through secret-proxy)
- Reach other pods (10.42.0.0/16)
- Reach services (10.43.0.0/16) except control-plane and secret-proxy
- Reach private networks (10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16)
- Reach Tailnet (100.64.0.0/10) by default

### Tailnet Access

Enable with `--tailnet` flag when creating a session:

```bash
netclode sessions create --repo owner/repo --repo owner/other --tailnet
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

## Secret Protection

API keys (Anthropic, OpenAI, etc.) are protected using a two-tier proxy architecture. **Real secrets never enter the sandbox microVM.**

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           KATA MICROVM (Sandbox)                            │
│                                                                             │
│  ┌─────────┐    HTTP_PROXY     ┌─────────────┐                              │
│  │   SDK   │ ───────────────── │ auth-proxy  │                              │
│  │ (Claude)│   localhost:8080  │             │                              │
│  └─────────┘                   └──────┬──────┘                              │
│       │                               │                                     │
│       │ ANTHROPIC_API_KEY=            │ Adds: Proxy-Authorization           │
│       │ NETCLODE_PLACEHOLDER_xxx      │       Bearer <SA token>             │
│       │                               │                                     │
│       │ (NO real secrets)             │ (NO real secrets)                   │
└───────┼───────────────────────────────┼─────────────────────────────────────┘
        │                               │
        │                               ▼
        │               ┌───────────────────────────────┐
        │               │     secret-proxy Service      │
        │               │   (OUTSIDE the microVM)       │
        │               │                               │
        │               │  1. Validate token with       │
        │               │     control-plane             │
        │               │  2. Check SDK type → hosts    │
        │               │  3. Replace placeholder       │
        │               │     with real secret          │
        │               │                               │
        │               │  (HAS real secrets)           │
        │               └───────────────┬───────────────┘
        │                               │
        │                               ▼
        │                       ┌───────────────┐
        │                       │   Internet    │
        │                       └───────────────┘
```

### How It Works

1. **Placeholder injection**: Agent sees `ANTHROPIC_API_KEY=NETCLODE_PLACEHOLDER_anthropic`
2. **Local proxy**: `HTTP_PROXY=localhost:8080` routes traffic through auth-proxy
3. **Token auth**: auth-proxy reads mounted ServiceAccount token, adds to request
4. **Validation**: secret-proxy validates token with control-plane (token → pod → session → SDK type)
5. **Secret injection**: If target host is allowed for SDK type, placeholder is replaced with real secret

### SDK to Host Mapping

| SDK Type | Allowed API Hosts |
|----------|-------------------|
| Claude | `api.anthropic.com` |
| OpenCode | `api.anthropic.com`, `api.openai.com`, `api.mistral.ai`, `openrouter.ai` |
| Copilot | `api.github.com`, `copilot-proxy.githubusercontent.com`, `api.anthropic.com` |
| Codex | `api.openai.com` |

### Security Properties

- **No secret exfiltration**: Even with RCE, attacker only sees placeholder values
- **Host restriction**: Secrets only sent to allowlisted API endpoints
- **Per-session authorization**: Claude session can't use OpenAI key
- **Cryptographic identity**: Token-based auth via K8s TokenReview API

For detailed documentation, see [Secret Proxy Architecture](secret-proxy.md).

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

When paused, the Sandbox CR is deleted but we need to keep the PVC. A ConfigMap "anchor" acts as a second owner:

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
