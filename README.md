# Netclode

Self-hosted Claude Code Cloud - persistent sandboxed AI coding agents accessible from iOS/web, with full shell/Docker/network access, running on a single VPS with microVM isolation.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│  VPS (NixOS + k3s)                                                  │
├─────────────────────────────────────────────────────────────────────┤
│  k3s Cluster                                                        │
│  ├── control-plane (Deployment)                                     │
│  │   └── WebSocket API, Session Manager                             │
│  ├── web (Deployment)                                               │
│  │   └── React SPA + nginx proxy                                    │
│  ├── Agent Sandboxes (Kata VMs via RuntimeClass)                    │
│  │   └── /agent → JuiceFS PVC (home + workspace + docker)           │
│  └── Tailscale Operator                                             │
│       └── Exposes services to your tailnet                          │
│                                                                     │
│  JuiceFS CSI ────────────────────────────────► S3 (R2/B2)          │
│                                                                     │
│  Tailscale ──────────────────────────────────► Your devices         │
└─────────────────────────────────────────────────────────────────────┘
```

## Stack

| Component | Technology |
|-----------|------------|
| **Host OS** | NixOS (fully declarative) |
| **Orchestration** | k3s (lightweight Kubernetes) |
| **VM Runtime** | Kata Containers (Cloud Hypervisor) via RuntimeClass |
| **Agent VMs** | NixOS-based OCI images |
| **Storage** | JuiceFS CSI (S3-backed PVCs) |
| **Networking** | Tailscale Operator + Flannel |
| **Control Plane** | Bun + TypeScript |

## Project Structure

```
netclode/
├── apps/
│   ├── control-plane/    # Session management, WebSocket API
│   ├── agent/            # Runs inside VM, Claude Agent SDK
│   └── web/              # React web client + nginx
├── packages/
│   └── protocol/         # Shared TypeScript types
├── infra/
│   ├── nixos/            # NixOS configuration (host + agent VM)
│   └── k8s/              # Kubernetes manifests
├── .github/
│   └── workflows/        # CI/CD for container images
└── scripts/              # Deployment scripts
```

## Quick Start

### Prerequisites

- [Nix](https://nixos.org/download.html) with flakes enabled
- A VPS with KVM support (DigitalOcean, Hetzner, etc.)
- S3-compatible storage (Cloudflare R2, Backblaze B2)
- Tailscale account with OAuth client configured

### Local Development

```bash
# Enter development shell
cd infra/nixos
nix develop

# Install dependencies
cd ../..
bun install

# Run control plane locally
bun run --cwd apps/control-plane dev
```

### Deploy to Server

1. **Create the droplet** (DigitalOcean example):

```bash
doctl compute droplet create netclode \
  --size s-2vcpu-8gb-amd \
  --image debian-13-x64 \
  --region fra1 \
  --ssh-keys <your-key-id>
```

2. **Install NixOS** using nixos-anywhere:

```bash
cd infra/nixos
nix run github:nix-community/nixos-anywhere -- \
  --flake .#netclode \
  root@<droplet-ip>
```

3. **Configure secrets** (create `.env` file locally):

```bash
cat > .env << 'EOF'
ANTHROPIC_API_KEY=sk-ant-xxx
JUICEFS_BUCKET=https://your-bucket.r2.cloudflarestorage.com
AWS_ACCESS_KEY_ID=xxx
AWS_SECRET_ACCESS_KEY=xxx
TS_OAUTH_CLIENT_ID=xxx
TS_OAUTH_CLIENT_SECRET=xxx
EOF
```

4. **Deploy secrets and manifests**:

```bash
./scripts/deploy-secrets.sh <server-ip>
./scripts/deploy-k8s.sh <server-ip>
```

## Configuration

### Environment Variables

**Control Plane** (via k8s Secret `netclode-secrets`):

| Variable | Description | Default |
|----------|-------------|---------|
| `ANTHROPIC_API_KEY` | Anthropic API key | Required |
| `PORT` | HTTP server port | `3000` |
| `K8S_NAMESPACE` | Kubernetes namespace | `netclode` |

### Tailscale Setup

1. Add ACL tags in Tailscale admin console:
   ```json
   {
     "tagOwners": {
       "tag:k8s-operator": ["autogroup:admin"],
       "tag:k8s": ["tag:k8s-operator"]
     }
   }
   ```

2. Create OAuth client with `tag:k8s-operator` permission

3. Enable MagicDNS in Tailscale settings

## Usage

### Access Services

After deployment, access via Tailscale:

- **Web App**: `http://netclode-web.<your-tailnet>.ts.net`
- **Control Plane API**: `http://netclode.<your-tailnet>.ts.net`

### WebSocket API

Connect to `ws://netclode.<your-tailnet>.ts.net/ws`

**Create Session:**
```json
{ "type": "session.create", "name": "my-project", "repo": "https://github.com/user/repo" }
```

**List Sessions:**
```json
{ "type": "session.list" }
```

**Send Prompt:**
```json
{ "type": "prompt", "sessionId": "abc123", "text": "Fix the bug in auth.ts" }
```

**Pause Session:**
```json
{ "type": "session.pause", "id": "abc123" }
```

See `packages/protocol/src/messages.ts` for full API.

## Operations

### kubectl Access

Use the `netclode` kubectl context (configured via Tailscale):

```bash
kubectl --context netclode -n netclode get pods
kubectl --context netclode -n netclode logs -l app=control-plane -f
kubectl --context netclode apply -f infra/k8s/control-plane.yaml
```

Or SSH to the server:

```bash
ssh root@netclode
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
kubectl -n netclode get pods
```

### View Logs

```bash
# Control plane logs
kubectl --context netclode -n netclode logs -l app=control-plane -f

# Web app logs
kubectl --context netclode -n netclode logs -l app=web -f

# k3s/kubelet logs (SSH required)
ssh root@netclode journalctl -u k3s -f
```

### Manage Pods

```bash
# List all pods
kubectl --context netclode get pods -A

# Describe a pod
kubectl --context netclode describe pod -n netclode <pod-name>

# Exec into control plane
kubectl --context netclode exec -it -n netclode deploy/control-plane -- sh

# Restart a deployment
kubectl --context netclode rollout restart deployment -n netclode control-plane
```

### Update Images

Images are built automatically via GitHub Actions on push to `master`.

To manually trigger a rebuild:
```bash
gh workflow run "Control Plane Image"
gh workflow run "Web App Image"
gh workflow run "Agent Image"
```

Then restart deployments to pull new images:
```bash
kubectl rollout restart deployment -n netclode control-plane web
```

### Rollback NixOS

```bash
# List generations
nixos-rebuild list-generations

# Rollback
nixos-rebuild switch --rollback
```

## Security

- **VM Isolation**: Each agent session runs in a separate Kata Container (Cloud Hypervisor microVM)
- **Network Isolation**: Kubernetes NetworkPolicy blocks agent access to internal networks
- **Storage Isolation**: Each agent gets its own PVC via JuiceFS CSI
- **Access Control**: Tailscale restricts access to your devices only

## License

MIT
