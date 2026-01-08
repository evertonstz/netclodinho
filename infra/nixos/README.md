# NixOS Infrastructure

Fully declarative NixOS configuration for the Netclode host with k3s and Kata Containers.

## Structure

```
infra/nixos/
├── flake.nix                 # Main flake definition
├── flake.lock                # Locked dependencies
│
├── hosts/
│   └── netclode-do/          # DigitalOcean host configuration
│       ├── default.nix       # Main host config
│       ├── hardware.nix      # Hardware/cloud-init config
│       └── disk-config.nix   # Disk partitioning (disko)
│
└── modules/
    ├── k3s.nix               # k3s + Kata Containers runtime
    ├── juicefs.nix           # JuiceFS mount service
    └── tailscale.nix         # Tailscale daemon
```

## Outputs

| Output | Description |
|--------|-------------|
| `nixosConfigurations.netclode` | Host system configuration |
| `devShells.x86_64-linux.default` | Development shell |

## Usage

### Deploy Host

Using nixos-anywhere for fresh install:

```bash
nix run github:nix-community/nixos-anywhere -- \
  --flake .#netclode \
  root@<server-ip>
```

For updates after initial install:

```bash
# Sync config and rebuild
rsync -avz --delete ./ root@<server-ip>:/etc/nixos/
ssh root@<server-ip> "cd /etc/nixos && nixos-rebuild switch --flake .#netclode"
```

### Development Shell

```bash
nix develop
# Provides: nodejs, kubectl, jq, nixos-rebuild
```

## Host Modules

### k3s.nix

Configures k3s with Kata Containers (Cloud Hypervisor):

- k3s single-node server with Flannel networking
- Kata runtime registered as `kata-clh` RuntimeClass
- containerd config template with CNI paths
- Downloads Kata assets (kernel + rootfs) on first boot
- Device access for KVM, vhost-net, vhost-vsock

Key configuration:
```nix
services.k3s = {
  enable = true;
  role = "server";
  extraFlags = [
    "--disable=traefik"
    "--disable=servicelb"
    "--flannel-backend=host-gw"
  ];
};
```

#### Kata Runtime Options

Kata Containers supports multiple VMM (Virtual Machine Monitor) and shared filesystem combinations. This project uses **Cloud Hypervisor + virtiofs** for better performance and compatibility.

**VMM Comparison:**

| VMM | Memory Overhead | Features | Notes |
|-----|-----------------|----------|-------|
| **Cloud Hypervisor** (`kata-clh`) | ~20-50 MB/VM | GPU passthrough, hot-plug, virtiofs | Good balance of features and performance |
| Firecracker (`kata-fc`) | ~5-10 MB/VM | Minimal | AWS Lambda/Fargate VMM, no virtiofs support |
| QEMU (`kata-qemu`) | ~50-100 MB/VM | Full featured | Most compatible, heaviest |

**Shared Filesystem Comparison:**

| Filesystem | Memory Model | Performance | Notes |
|------------|--------------|-------------|-------|
| **virtio-9p** | Kernel page cache (reclaimable) | Slower | Memory pressure can reclaim cache |
| virtiofs | Shared memory (pinned RAM) | Faster | Uses `virtiofsd` daemon with DAX window; shows as "used" not "cache" in htop |

**Why Cloud Hypervisor + virtiofs:**

- **Performance**: virtiofs provides better I/O performance than virtio-9p, especially for Docker image pulls and builds
- **Features**: Cloud Hypervisor supports GPU passthrough, device hot-plug, and other advanced features
- **Compatibility**: Better tested and more stable with the Kata static release

**Memory note**: virtiofsd processes allocate shared memory for the DAX cache. This appears as "used" memory (not cache) in htop. To minimize this, the configuration sets `virtio_fs_cache_size = 0`.

**To switch to Firecracker + virtio-9p (experimental on NixOS):**

1. Change `kata-clh` → `kata-fc` in `k3s.nix`, `runtime-class.yaml`, `sandbox-template.yaml`, `sandbox.go`
2. Change `configuration-clh.toml` → `configuration-fc.toml` in `k3s.nix`
3. Note: Firecracker requires the jailer for proper operation, which has path compatibility issues on NixOS

### juicefs.nix

JuiceFS filesystem mount (for host-level access):

- Mounts at `/juicefs`
- Auto-formats on first boot
- Local cache at `/var/cache/juicefs`
- Requires `/var/secrets/juicefs.env` with S3 credentials

Note: Agent pods use JuiceFS CSI driver for PVC-based storage instead.

### tailscale.nix

Tailscale daemon for host access:

- Auto-connects using authkey
- Trusts `tailscale0` interface in firewall
- k3s API exposed on tailscale0:6443

Note: Service exposure to Tailscale is handled by the Tailscale Operator in k8s.

## Network Topology

```
┌─────────────────────────────────────────────────────────────────┐
│  Host                                                           │
│  eth0: public IP                                                │
│  tailscale0: 100.x.x.x                                          │
│  cni0: 10.42.0.1 (k3s Flannel bridge)                          │
│                                                                 │
│  k3s Cluster                                                    │
│  ├── Pod Network: 10.42.0.0/16                                 │
│  ├── Service Network: 10.43.0.0/16                             │
│  │                                                              │
│  │  ┌─────────────────┐  ┌─────────────────┐                   │
│  │  │ control-plane   │  │ web             │                   │
│  │  │ 10.42.0.x       │  │ 10.42.0.y       │                   │
│  │  └─────────────────┘  └─────────────────┘                   │
│  │                                                              │
│  │  ┌─────────────────┐  ┌─────────────────┐                   │
│  │  │ Agent VM (Kata) │  │ Agent VM (Kata) │                   │
│  │  │ 10.42.0.z       │  │ 10.42.0.w       │                   │
│  │  └─────────────────┘  └─────────────────┘                   │
│  │                                                              │
│  └── Tailscale Operator → exposes services to tailnet          │
│                                                                 │
│  nftables:                                                      │
│  - Pods can reach internet                                      │
│  - Pods can reach k3s service network                          │
│  - cni0 is trusted interface                                   │
└─────────────────────────────────────────────────────────────────┘
```

## Kubernetes Manifests

The k8s manifests in `infra/k8s/` are applied separately:

| Manifest | Purpose |
|----------|---------|
| `namespace.yaml` | netclode namespace + RBAC |
| `runtime-class.yaml` | kata-clh RuntimeClass |
| `control-plane.yaml` | Control plane Deployment + Service |
| `web.yaml` | Web app Deployment + Service |
| `sandbox-template.yaml` | Agent SandboxTemplate |
| `juicefs-*.yaml` | JuiceFS CSI driver |
| `tailscale-operator.yaml` | Tailscale Operator |

## Secrets

Required in `.env` file (deployed via `scripts/deploy-secrets.sh`):

| Variable | Purpose |
|----------|---------|
| `ANTHROPIC_API_KEY` | Anthropic API key for agents |
| `JUICEFS_BUCKET` | S3 bucket URL for JuiceFS |
| `AWS_ACCESS_KEY_ID` | S3 credentials |
| `AWS_SECRET_ACCESS_KEY` | S3 credentials |
| `TS_OAUTH_CLIENT_ID` | Tailscale OAuth client |
| `TS_OAUTH_CLIENT_SECRET` | Tailscale OAuth secret |

## Troubleshooting

### k3s fails to start

Check kubelet logs:
```bash
journalctl -u k3s -f
```

Common issues:
- `/dev/kmsg` permission denied → check `ProtectKernelLogs = false` in k3s service
- CNI not initialized → check containerd config template has CNI paths

### Pods can't reach API server

Check firewall:
```bash
nft list ruleset
```

Ensure `cni0` is in trusted interfaces:
```nix
networking.firewall.trustedInterfaces = ["cni0"];
```

### Kata assets missing

Re-download:
```bash
systemctl restart kata-assets
ls -la /var/lib/kata/
# Should have: vmlinux, kata-containers.img
```

### JuiceFS mount fails

Check credentials:
```bash
cat /var/secrets/juicefs.env
```

Test manually:
```bash
source /var/secrets/juicefs.env
juicefs status sqlite3:///var/lib/juicefs/meta.db
```

### Tailscale operator crash

Check ACL tags are configured:
```bash
kubectl logs -n tailscale -l app=operator
```

Error "tag:k8s-operator not permitted" means:
1. Add tag to Tailscale ACLs
2. Ensure OAuth client has the tag permission
