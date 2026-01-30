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
- Control plane, agent sandboxes

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
| `nvidia` | NVIDIA driver, container toolkit, device plugin (optional) |
| `k3s` | k3s single-node server with Kata support |
| `cilium` | Cilium CNI for NetworkPolicy enforcement |
| `juicefs-csi` | JuiceFS CSI driver with VolumeSnapshot support |
| `tailscale-operator` | Tailscale K8s Operator via Helm |
| `k8s-manifests` | Deploy all k8s manifests from infra/k8s/ |

## GPU Support (Optional)

For local model inference with Ollama, enable NVIDIA GPU support.

### Prerequisites

- NVIDIA GPU (RTX 30/40/50 series recommended)
- GPU must be detected by `lspci | grep -i nvidia`

### Configuration

Add to `.env`:

```bash
# Enable NVIDIA driver and container toolkit installation
NVIDIA_ENABLED=true

# Enable Ollama deployment with GPU access
OLLAMA_ENABLED=true

# Ollama URL for control-plane (auto-configured if OLLAMA_ENABLED=true)
OLLAMA_URL=http://ollama.netclode.svc.cluster.local:11434

# MOK enrollment password for Secure Boot (required if Secure Boot is enabled)
# This is entered at the blue MOK screen during reboot - choose something simple
MOK_PASSWORD=mypassword
```

### What gets installed

1. **NVIDIA Driver** (590+ from NVIDIA CUDA repo, supports RTX 50 series Blackwell)
2. **NVIDIA Container Toolkit** - Enables GPU access from containers
3. **NVIDIA Device Plugin** - K8s scheduler sees `nvidia.com/gpu` resources
4. **nvtop** - GPU monitoring TUI
5. **Ollama** - Local LLM inference server with GPU acceleration

### Secure Boot Support (Two-Step Installation)

If your system has **Secure Boot enabled**, the NVIDIA driver installation requires two steps because kernel modules must be signed with a trusted key.

#### Step 1: Install Driver (Remote)

Run the playbook normally:

```bash
DEPLOY_HOST=your-server NVIDIA_ENABLED=true ansible-playbook playbooks/site.yaml --tags nvidia
```

If Secure Boot is enabled and the MOK (Machine Owner Key) isn't enrolled yet, the playbook will:
1. Install the NVIDIA driver via DKMS (creates signed kernel modules)
2. Queue the MOK key for enrollment
3. Display instructions and exit gracefully

**Example output:**
```
╔══════════════════════════════════════════════════════════════════════════╗
║                     SECURE BOOT: MOK ENROLLMENT REQUIRED                  ║
╠══════════════════════════════════════════════════════════════════════════╣
║  STEP 1: Reboot the machine                                              ║
║          ssh root@your-server reboot                                     ║
║                                                                          ║
║  STEP 2: At the blue "MOK Management" screen:                            ║
║          → Select "Enroll MOK"                                           ║
║          → Select "Continue"                                             ║
║          → Enter password: nvidia                                        ║
║          → Select "Reboot"                                               ║
║                                                                          ║
║  STEP 3: Re-run this playbook to complete GPU setup                      ║
╚══════════════════════════════════════════════════════════════════════════╝
```

#### Step 2: Enroll MOK (Physical Access Required)

1. **Reboot the server:**
   ```bash
   ssh root@your-server reboot
   ```

2. **At the blue MOK Management screen** (requires physical access or remote KVM):
   - Select **"Enroll MOK"**
   - Select **"Continue"**
   - Enter the password: `nvidia` (or your custom `MOK_PASSWORD`)
   - Select **"Reboot"**

   > **Note:** This is a UEFI pre-boot screen, not Linux. It cannot be accessed via SSH.
   > If you have IPMI/iDRAC/iLO/PiKVM, use that for remote access.

#### Step 3: Complete Setup (Remote)

Re-run the playbook:

```bash
DEPLOY_HOST=your-server NVIDIA_ENABLED=true ansible-playbook playbooks/site.yaml --tags nvidia
```

This time the driver will load successfully, and the playbook will complete the full setup (container toolkit, device plugin, etc.).

#### Checking Secure Boot Status

```bash
# Check if Secure Boot is enabled
ssh root@your-server mokutil --sb-state

# Check if MOK is enrolled
ssh root@your-server mokutil --list-enrolled

# Check if nvidia module loads
ssh root@your-server modprobe nvidia && echo "OK" || echo "FAILED"
```

#### Without Secure Boot

If Secure Boot is **disabled**, the installation completes in a single run - no reboot or physical access needed.

### GPU Monitoring

```bash
# Quick status
ssh root@netclode-host nvidia-smi

# Live monitoring
ssh root@netclode-host nvidia-smi -l 1

# Pretty TUI
ssh root@netclode-host nvtop

# From inside Ollama pod
kubectl --context netclode -n netclode exec -it deploy/ollama -- nvidia-smi
```

### Ollama Model Management

```bash
# Pull a model
kubectl --context netclode -n netclode exec -it deploy/ollama -- ollama pull qwen2.5-coder:32b

# List downloaded models
kubectl --context netclode -n netclode exec -it deploy/ollama -- ollama list

# Remove a model
kubectl --context netclode -n netclode exec -it deploy/ollama -- ollama rm qwen2.5-coder:32b
```

### Recommended Models for 16GB VRAM (RTX 5080)

| Model | Size | Use Case |
|-------|------|----------|
| `qwen2.5-coder:32b-instruct-q4_K_M` | ~19GB | Best coding performance |
| `deepseek-coder-v2:16b` | ~9GB | Fast coding |
| `mistral:7b-instruct` | ~4GB | Fast general purpose |

### Limitations

**Tool calling with local models is unreliable.** OpenCode + Ollama may generate tool call JSON but commands don't execute. This is a model capability issue, not infrastructure. Use cloud APIs (Anthropic, OpenAI) for production agentic workloads.

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
