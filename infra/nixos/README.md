# NixOS Infrastructure

Fully declarative NixOS configuration for the Netclode host and agent VMs.

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
├── modules/
│   ├── containerd.nix        # containerd + Kata Containers
│   ├── juicefs.nix           # JuiceFS mount service
│   ├── tailscale.nix         # Tailscale + serve config
│   ├── nix-serve.nix         # Binary cache for VMs
│   └── control-plane.nix     # Netclode service
│
└── agent/
    ├── default.nix           # Agent VM NixOS config
    └── oci.nix               # OCI image builder
```

## Outputs

| Output | Description |
|--------|-------------|
| `nixosConfigurations.netclode` | Host system configuration |
| `nixosConfigurations.agent` | Agent VM configuration |
| `packages.x86_64-linux.agent-image` | Agent OCI image |
| `devShells.x86_64-linux.default` | Development shell |

## Usage

### Deploy Host

```bash
# From local machine with Nix
nixos-rebuild switch --flake .#netclode --target-host root@your-server
```

### Build Agent Image

```bash
# Build OCI image
nix build .#agent-image

# Load into containerd on server
ssh root@server 'nerdctl load' < result
```

### Development Shell

```bash
nix develop
# Provides: bun, nodejs, nerdctl, jq, nixos-rebuild
```

## Host Modules

### containerd.nix

Configures containerd with Kata Containers (Cloud Hypervisor):

- Default runtime: `kata-clh` (Kata + Cloud Hypervisor)
- virtio-fs for fast file sharing
- CNI networking with bridge + portmap
- Downloads Kata assets on first boot

### juicefs.nix

JuiceFS filesystem mount:

- Mounts at `/juicefs`
- Auto-formats on first boot
- Local cache at `/var/cache/juicefs`
- Requires `/var/secrets/juicefs.env` with S3 credentials

### tailscale.nix

Tailscale networking:

- Auto-connects using authkey at `/var/secrets/tailscale-authkey`
- Exposes control plane via `tailscale serve`
- Trusts `tailscale0` interface in firewall

### nix-serve.nix

Binary cache for agent VMs:

- Listens on `10.88.0.1:5000` (VM network only)
- Auto-generates signing keys
- VMs fetch packages from host instead of cache.nixos.org

### control-plane.nix

Netclode control plane service:

- Runs from `/opt/netclode`
- Depends on containerd and JuiceFS
- Reads secrets from `/var/secrets/netclode.env`

## Agent VM

The agent VM is a minimal NixOS system with:

- Bun runtime
- Docker daemon
- Git, gh CLI
- Nix (for dynamic deps via `nix-shell`)

It's built as an OCI image and runs inside Kata Containers.

### Customizing Agent

Edit `agent/default.nix` to add packages:

```nix
environment.systemPackages = with pkgs; [
  # Add your packages here
  python311
  rustc
  go
];
```

Then rebuild:

```bash
nix build .#agent-image
```

## Network Topology

```
┌─────────────────────────────────────────────────┐
│  Host                                           │
│  eth0: public IP                                │
│  tailscale0: 100.x.x.x                          │
│  cni0: 10.88.0.1/16 (bridge to VMs)            │
│                                                 │
│  ┌─────────────┐  ┌─────────────┐              │
│  │ VM 1        │  │ VM 2        │              │
│  │ 10.88.0.x   │  │ 10.88.0.y   │              │
│  └─────────────┘  └─────────────┘              │
│                                                 │
│  nftables:                                      │
│  - VMs can reach internet                       │
│  - VMs cannot reach 10.x, 172.x, 192.168.x     │
│  - VMs can reach host:5000 (nix-serve)         │
└─────────────────────────────────────────────────┘
```

## Secrets

Required files in `/var/secrets/`:

| File | Format | Purpose |
|------|--------|---------|
| `tailscale-authkey` | Plain text | Tailscale auth key (one-time) |
| `juicefs.env` | Shell env | `JUICEFS_BUCKET`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` |
| `netclode.env` | Shell env | `ANTHROPIC_API_KEY`, etc. |

Auto-generated:

| File | Purpose |
|------|---------|
| `nix-serve-private-key` | Signing key for binary cache |
| `nix-serve-public-key` | Public key (add to VMs' trusted keys) |

## Troubleshooting

### containerd fails to start

Check Kata assets are downloaded:

```bash
ls -la /var/lib/kata/
# Should have: vmlinux, kata-containers.img
```

Re-download:

```bash
systemctl restart kata-assets
```

### VMs can't reach internet

Check NAT is enabled:

```bash
iptables -t nat -L POSTROUTING
# Should show MASQUERADE for cni0
```

Check nftables:

```bash
nft list ruleset
```

### JuiceFS mount fails

Check credentials:

```bash
cat /var/secrets/juicefs.env
# Verify bucket URL and credentials
```

Test manually:

```bash
source /var/secrets/juicefs.env
juicefs status sqlite3:///var/lib/juicefs/meta.db
```
