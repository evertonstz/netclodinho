# Netclode

Self-hosted Claude Code Cloud - persistent sandboxed AI coding agents accessible from iOS/web, with full shell/Docker/network access, running on a single VPS with microVM isolation.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│  VPS (NixOS)                                                        │
├─────────────────────────────────────────────────────────────────────┤
│  Control Plane (Bun)          containerd + kata-clh                 │
│  ├── WebSocket API     ───►   ├── Agent VM 1 (NixOS)               │
│  ├── Session Manager          │   └── /workspace → JuiceFS         │
│  └── JuiceFS storage          ├── Agent VM 2 (NixOS)               │
│                               │   └── /workspace → JuiceFS         │
│                               └── ...                               │
│                                                                     │
│  JuiceFS (/juicefs) ──────────────────────────────► S3 (R2/B2)     │
│                                                                     │
│  Tailscale ───────────────────────────────────────► Your devices   │
└─────────────────────────────────────────────────────────────────────┘
```

## Stack

| Component | Technology |
|-----------|------------|
| **Host OS** | NixOS (fully declarative) |
| **VM Runtime** | containerd + Kata Containers (Cloud Hypervisor) |
| **Agent VMs** | NixOS-based OCI images |
| **Storage** | JuiceFS (S3-backed, virtio-fs into VMs) |
| **Networking** | Tailscale + nftables |
| **Control Plane** | Bun + TypeScript |

## Project Structure

```
netclode/
├── apps/
│   ├── control-plane/    # Session management, WebSocket API
│   ├── agent/            # Runs inside VM, Claude Agent SDK
│   └── web/              # React web client
├── packages/
│   └── protocol/         # Shared TypeScript types
├── infra/
│   └── nixos/            # NixOS configuration (host + agent VM)
└── scripts/              # Deployment scripts
```

## Quick Start

### Prerequisites

- [Nix](https://nixos.org/download.html) with flakes enabled
- A VPS with KVM support (DigitalOcean, Hetzner, etc.)
- S3-compatible storage (Cloudflare R2, Backblaze B2)
- Tailscale account

### Local Development

```bash
# Enter development shell
cd infra/nixos
nix develop

# Install dependencies
cd ../..
bun install

# Run control plane locally (won't work without containerd)
bun run --cwd apps/control-plane dev
```

### Deploy to Server

1. **Prepare secrets** on the server:

```bash
ssh root@your-server

mkdir -p /var/secrets

# Tailscale auth key (get from admin console)
echo "tskey-auth-xxx" > /var/secrets/tailscale-authkey

# JuiceFS S3 credentials
cat > /var/secrets/juicefs.env << 'EOF'
JUICEFS_BUCKET=https://your-bucket.r2.cloudflarestorage.com
AWS_ACCESS_KEY_ID=xxx
AWS_SECRET_ACCESS_KEY=xxx
EOF

# Control plane secrets
cat > /var/secrets/netclode.env << 'EOF'
ANTHROPIC_API_KEY=sk-ant-xxx
EOF

chmod 600 /var/secrets/*
```

2. **Deploy NixOS configuration**:

```bash
# From your local machine
cd infra/nixos
nixos-rebuild switch --flake .#netclode --target-host root@your-server
```

3. **Deploy application code**:

```bash
./scripts/deploy.sh your-server
```

## Configuration

### Environment Variables

**Control Plane** (`/var/secrets/netclode.env`):

| Variable | Description | Default |
|----------|-------------|---------|
| `ANTHROPIC_API_KEY` | Anthropic API key | Required |
| `PORT` | HTTP server port | `3000` |
| `JUICEFS_ROOT` | JuiceFS mount point | `/juicefs` |
| `AGENT_IMAGE` | Agent OCI image | `ghcr.io/stanislas/netclode-agent:latest` |
| `DEFAULT_CPUS` | Default VM CPUs | `2` |
| `DEFAULT_MEMORY_MB` | Default VM memory | `2048` |

### Secrets

| File | Purpose |
|------|---------|
| `/var/secrets/tailscale-authkey` | Tailscale auth (consumed on first boot) |
| `/var/secrets/juicefs.env` | JuiceFS S3 credentials |
| `/var/secrets/netclode.env` | Control plane environment |
| `/var/secrets/nix-serve-*` | Auto-generated signing keys |

## Usage

### WebSocket API

Connect to `wss://your-server/ws` (via Tailscale).

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

### View Logs

```bash
# Control plane
journalctl -u netclode -f

# containerd
journalctl -u containerd -f

# JuiceFS
journalctl -u juicefs -f
```

### Manage VMs

```bash
# List running VMs
nerdctl ps --filter label=netclode.session

# View VM logs
nerdctl logs sess-abc123

# Exec into VM
nerdctl exec -it sess-abc123 /bin/bash

# Stop VM
nerdctl stop sess-abc123
```

### Update Agent Image

```bash
# Build new image
cd infra/nixos
nix build .#agent-image

# Load into containerd
nerdctl load < result

# Tag and push (optional)
nerdctl tag netclode-agent:latest ghcr.io/you/netclode-agent:latest
nerdctl push ghcr.io/you/netclode-agent:latest
```

### Rollback NixOS

```bash
# List generations
nixos-rebuild list-generations

# Rollback
nixos-rebuild switch --rollback
```

## Security

- **VM Isolation**: Each session runs in a separate Kata Container (Cloud Hypervisor VM)
- **Network Isolation**: nftables blocks VM access to internal networks (10.x, 172.x, 192.168.x, Tailscale)
- **Storage Isolation**: Each VM only sees its own workspace via virtio-fs mount
- **Access Control**: Tailscale ACLs restrict access to your devices only

## License

MIT
