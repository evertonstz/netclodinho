# Deployment

Here's how to get Netclode running on your own server.

## Prerequisites

- VPS with nested virtualization (2 vCPU, 8GB RAM minimum)
- S3-compatible storage
- Tailscale account with OAuth client
- Nix with flakes enabled
- Anthropic API key

> [!WARNING]
> Hetzner Cloud doesn't support nested virtualization, so Kata microVMs won't work. Use DigitalOcean, Vultr, or similar.

## 1. Create your VPS

DigitalOcean:

```bash
doctl compute droplet create netclode \
  --size s-2vcpu-8gb-amd \
  --image debian-13-x64 \
  --region fra1 \
  --ssh-keys <your-key-id>
```

## 2. Install NixOS

```bash
cd infra/nixos

nix run github:nix-community/nixos-anywhere -- \
  --flake .#netclode \
  root@<server-ip>
```

This installs NixOS with k3s, Kata Containers, Tailscale, and JuiceFS CSI. The server reboots after.

## 3. Configure Tailscale

Add ACL tags in [Tailscale admin](https://login.tailscale.com/admin/acls):

```json
{
  "tagOwners": {
    "tag:k8s-operator": ["autogroup:admin"],
    "tag:k8s": ["tag:k8s-operator"]
  }
}
```

Create an OAuth client at [Settings → OAuth clients](https://login.tailscale.com/admin/settings/oauth) with `tag:k8s-operator` permission. Enable MagicDNS in [DNS settings](https://login.tailscale.com/admin/dns).

## 4. Configure secrets

Create `.env` at the repo root:

```bash
# Anthropic
ANTHROPIC_API_KEY=sk-ant-xxx

# JuiceFS / S3
JUICEFS_BUCKET=https://your-bucket.r2.cloudflarestorage.com
AWS_ACCESS_KEY_ID=xxx
AWS_SECRET_ACCESS_KEY=xxx

# Tailscale
TS_OAUTH_CLIENT_ID=xxx
TS_OAUTH_CLIENT_SECRET=xxx

# Deployment
DEPLOY_HOST=<server-ip-or-hostname>
```

Create a bucket named `netclode-juicefs` with read/write credentials.

## 5. Deploy

```bash
./scripts/deploy-secrets.sh
./scripts/deploy-k8s.sh
```

## 6. Verify

```bash
kubectl --context netclode -n netclode get pods
```

You should see `control-plane` and `redis-sessions` running. Test access from your tailnet:

```bash
curl http://netclode/health
```

## 7. Connect clients

**iOS/Mac**: Open the app → Settings → enter `netclode.your-tailnet.ts.net` → Connect

## Configuration

### Control plane

| Variable | Default | Description |
|----------|---------|-------------|
| `ANTHROPIC_API_KEY` | Required | Anthropic API key |
| `PORT` | `3000` | Server port |
| `K8S_NAMESPACE` | `netclode` | Kubernetes namespace |
| `REDIS_URL` | `redis://redis-sessions...` | Redis URL |
| `WARM_POOL_ENABLED` | `false` | Use warm pool |
| `MAX_ACTIVE_SESSIONS` | `2` | Max concurrent sessions |

### Agent

| Variable | Description |
|----------|-------------|
| `ANTHROPIC_API_KEY` | Anthropic API key |
| `SESSION_ID` | Session identifier |
| `GIT_REPOS` | Optional JSON array of repos to clone (URL or `owner/repo`) |

## Troubleshooting

**Pods stuck in Pending** - check warm pool:
```bash
kubectl --context netclode -n netclode get sandboxclaim
kubectl --context netclode -n netclode get sandbox
```

**JuiceFS mount failures** - check CSI driver:
```bash
kubectl --context netclode -n kube-system logs -l app=juicefs-csi-driver
```

**Tailscale services not getting IPs** - check operator:
```bash
kubectl --context netclode -n tailscale logs -l app=operator
```

## Updating

After pushing changes:

```bash
gh run watch
./scripts/rollout.sh
```

NixOS updates:

```bash
cd infra/nixos
nixos-rebuild switch --flake .#netclode --target-host root@netclode
```

## Rollback

K8s:

```bash
kubectl --context netclode -n netclode rollout undo deployment/control-plane
```

NixOS:

```bash
ssh root@netclode
nixos-rebuild switch --rollback
```
