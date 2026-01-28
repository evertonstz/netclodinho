# Netclode Ansible Infrastructure

Ansible playbooks for deploying Netclode infrastructure on Ubuntu/Debian.

> **Note:** This replaces the previous NixOS-based infrastructure. See [/docs/nix-exploration-postmortem.md](/docs/nix-exploration-postmortem.md) for background.

## Architecture

The host runs:
- **k3s** - Single-node Kubernetes with Kata Containers support
- **Cilium** - CNI with NetworkPolicy enforcement
- **Tailscale** - Secure network access
- **nftables** - Firewall

Everything else runs in Kubernetes:
- Redis (JuiceFS metadata)
- JuiceFS CSI driver
- Control plane, web, agent sandboxes

## Requirements

- Ansible 2.15+
- Python 3.10+
- SSH access to target host

## Setup

```bash
# Install Ansible collections
ansible-galaxy collection install -r requirements.yaml
```

## Configuration

### Inventory

Edit `inventory/hosts.yaml` to set your target host:

```yaml
all:
  hosts:
    netclode-do:
      ansible_host: your-server-ip
      ansible_user: root
```

Or use the `DEPLOY_HOST` environment variable:

```bash
export DEPLOY_HOST=your-server-ip
```

### Secrets

All secrets are read from the `.env` file at the repo root. Required entries:

```bash
# .env file

# Core
ANTHROPIC_API_KEY=sk-ant-api03-xxx

# SSH
SSH_AUTHORIZED_KEYS=ssh-ed25519 AAAA... user@host

# Tailscale
TS_OAUTH_CLIENT_ID=your-oauth-client-id
TS_OAUTH_CLIENT_SECRET=your-oauth-client-secret
TAILSCALE_AUTHKEY=tskey-auth-xxx  # Optional, for auto-connect

# JuiceFS / S3 storage
DO_SPACES_ACCESS_KEY=your-spaces-access-key
DO_SPACES_SECRET_KEY=your-spaces-secret-key
JUICEFS_BUCKET=https://fra1.digitaloceanspaces.com/your-bucket
JUICEFS_META_URL=redis://redis-juicefs.netclode.svc.cluster.local:6379/0

# GitHub App (optional - for repo picker)
GITHUB_APP_ID=123456
GITHUB_APP_PRIVATE_KEY_B64=base64-encoded-pem-private-key
GITHUB_INSTALLATION_ID=12345678

# Mistral (optional - for Mistral models via OpenCode)
MISTRAL_API_KEY=your-mistral-api-key

# Kata VM Resources (optional - defaults shown)
KATA_VM_CPUS=4
KATA_VM_MEMORY_MB=4096
```

Deploy secrets:

```bash
ENV_FILE=/path/to/.env DEPLOY_HOST=your-server-ip ansible-playbook playbooks/secrets.yaml
```

This creates:

**Host files** (in `/var/secrets/`):
- `ssh-authorized-keys` - SSH public key for root
- `ts-oauth-client-id` - Tailscale OAuth client ID
- `ts-oauth-client-secret` - Tailscale OAuth client secret
- `tailscale-authkey` - Tailscale auth key (optional)

**Kubernetes secrets** (in `netclode` namespace):
- `netclode-secrets` - Contains `anthropic-api-key`, and optionally `github-app-id`, `github-app-private-key`, `github-installation-id`
- `juicefs-secret` - Contains S3 credentials and JuiceFS metadata URL

## Usage

### Full Deployment

```bash
# Deploy everything (with secrets from .env)
ENV_FILE=/path/to/.env ansible-playbook playbooks/site.yaml

# Deploy without secrets (if already deployed)
ansible-playbook playbooks/site.yaml

# Deploy with verbose output
ENV_FILE=/path/to/.env ansible-playbook playbooks/site.yaml -v

# Dry run (check mode)
ansible-playbook playbooks/site.yaml --check
```

### Partial Deployment

```bash
# Deploy only base system (common + firewall)
ansible-playbook playbooks/site.yaml --tags base

# Deploy only k8s components
ansible-playbook playbooks/site.yaml --tags k8s

# Skip k8s manifest deployment
ansible-playbook playbooks/site.yaml --skip-tags k8s-manifests

# Deploy only k8s manifests (fast updates)
ansible-playbook playbooks/k8s-only.yaml
```

### Local kubectl Access

After deployment, fetch the kubeconfig to use kubectl locally:

```bash
ansible-playbook playbooks/fetch-kubeconfig.yaml
```

This creates `~/.kube/netclode.yaml` configured to connect via Tailscale. Use it with:

```bash
export KUBECONFIG=~/.kube/netclode.yaml
kubectl get nodes

# Or merge with existing config
export KUBECONFIG=~/.kube/config:~/.kube/netclode.yaml
kubectl config view --flatten > ~/.kube/config.merged
mv ~/.kube/config.merged ~/.kube/config
kubectl config use-context netclode
```

### Available Tags

| Tag | Description |
|-----|-------------|
| `common` | Base packages, SSH, directories |
| `nftables` | Firewall configuration |
| `secrets` | Deploy secrets (host + k8s) |
| `tailscale` | Tailscale daemon |
| `kata` | Kata Containers runtime (use with `secrets` tag to read .env) |
| `k3s` | k3s Kubernetes server |
| `cilium` | Cilium CNI (NetworkPolicy support) |
| `juicefs-csi` | JuiceFS CSI driver |
| `tailscale-operator` | Tailscale K8s Operator |
| `k8s-manifests` | Deploy k8s workloads |
| `base` | common + nftables + secrets |
| `k8s` | kata + k3s |
| `cni` | cilium |
| `addons` | juicefs-csi + tailscale-operator |
| `workloads` | k8s-manifests |
| `k8s-secrets` | Deploy only k8s secrets |

## Roles

| Role | Purpose |
|------|---------|
| `common` | Base system setup (packages, SSH, kernel modules) |
| `nftables` | Firewall with persistence |
| `deploy-secrets` | Deploy secrets from .env to host and k8s |
| `tailscale` | Tailscale daemon + auto-connect |
| `kata` | Kata Containers static release |
| `k3s` | k3s single-node server with Kata support |
| `cilium` | Cilium CNI for NetworkPolicy enforcement |
| `juicefs-csi` | JuiceFS CSI driver with VolumeSnapshot support |
| `tailscale-operator` | Tailscale K8s Operator via Helm |
| `k8s-manifests` | Deploy all k8s manifests from infra/k8s/ |

## Supported Distributions

- Debian 13 (Trixie)
- Debian 12 (Bookworm)
- Ubuntu 24.04 LTS
- Ubuntu 22.04 LTS

## Troubleshooting

### Tailscale not connected

If no auth key was provided, authenticate manually:
```bash
ssh root@your-host tailscale up --ssh
```

### k3s not starting

Check logs:
```bash
ssh root@your-host journalctl -u k3s -f
```

### Kata pods not starting

Verify Kata installation:
```bash
ssh root@your-host /opt/kata/bin/kata-runtime kata-env
```

Check containerd config:
```bash
ssh root@your-host cat /var/lib/rancher/k3s/agent/etc/containerd/config.toml.tmpl
```

### Cilium not working / NetworkPolicy not enforced

Check Cilium status:
```bash
kubectl -n kube-system get pods -l app.kubernetes.io/part-of=cilium
kubectl -n kube-system exec ds/cilium -- cilium status
```

Check if NetworkPolicies are being enforced:
```bash
kubectl -n kube-system exec ds/cilium -- cilium policy get
```

### JuiceFS CSI not working

Check JuiceFS secret exists in k8s:
```bash
kubectl -n netclode get secret juicefs-secret
```

Check CSI controller logs:
```bash
kubectl -n kube-system logs -l app=juicefs-csi-controller
```

### JuiceFS slow performance

JuiceFS uses S3-compatible object storage as backend (DigitalOcean Spaces), which has high latency for small file operations. Multiple layers of caching are configured to improve IOPS.

**Configurations:**
- `infra/k8s/juicefs-config.yaml` - JuiceFS CSI mount options
- `infra/ansible/roles/kata/tasks/main.yaml` - Kata virtiofs settings

**JuiceFS settings:**
- `--writeback`: Async writes to local disk, synced to S3 in background
- `cacheDirs` with HostPath: Persists cache across pod restarts

**Kata virtiofs settings:**
- `virtio_fs_cache = "always"`: Caches metadata, data, and pathname lookup in guest

**Performance:**
| Configuration | IOPS |
|---------------|------|
| No caching | ~30 |
| + JuiceFS writeback | ~400 |
| + virtiofs cache="always" | **~650** |

To verify JuiceFS caching:
```bash
kubectl -n kube-system exec $(kubectl -n kube-system get pods -l app.kubernetes.io/name=juicefs-mount -o name | head -1) -- ps aux | grep juicefs
# Should show: -o writeback,cache-dir=/var/jfsCache,...
```

**Warning:** Writeback caching means writes are acknowledged before being synced to S3. Data could be lost if the node crashes before sync completes. This is acceptable for agent workloads since sessions can be replayed.
