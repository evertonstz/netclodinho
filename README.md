# Netclode

Self-hosted coding agent. Persistent sandboxed sessions accessible from iOS, with full shell/Docker/network access, running on a single VPS with microVM isolation.

> [!NOTE]
> This is experimental and not ready for self-hosting. I'm building it for myself and iterating quickly.

## Why

I wanted a self-hosted Claude Code environment with the UX I actually want:

- **Full YOLO mode** - Docker, root access, install anything. The VM handles isolation.
- **Tailnet integration** - Preview URLs, port forwarding, access to my infra (like my home k8s cluster) through Tailscale.
- **JuiceFS for storage** - Storage offloaded to S3. Paused sessions cost nothing but storage.
- **Live terminal access** - Drop into the sandbox shell from the app. Debug, install tools, run commands.
- **Single-tenant by design** - Optimized for personal use. Architecture scales to multi-node or multi-tenant if needed.

## How it works

```
┌─────────────────────────────────────────────────────────────────────┐
│  VPS (NixOS + k3s)                                                  │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│   ┌─────────────────┐      ┌─────────────────────────────────────┐  │
│   │  Control Plane  │◄────►│  Agent Sandbox (Kata Container VM)  │  │
│   │  Go + Redis     │      │  Claude Agent SDK + mise + Docker   │  │
│   └────────┬────────┘      └─────────────────────────────────────┘  │
│            │                         ▲                              │
│            │                         │ Warm pool pre-boots VMs      │
│   ┌────────┴────────┐                │                              │
│   │      Redis      │      ┌─────────┴─────────┐                    │
│   │ sessions/events │      │   JuiceFS CSI     │───► S3             │
│   └────────┬────────┘      └───────────────────┘                    │
│            │                                                        │
│   ┌────────▼────────┐                                               │
│   │   Tailscale     │                                               │
│   │   Operator      │                                               │
│   └────────┬────────┘                                               │
│            │                                                        │
└────────────┼────────────────────────────────────────────────────────┘
             │
             ▼
    ┌────────────────┐
    │  iOS/Mac app   │  ◄── Main interface
    └────────────────┘
```

1. iOS app connects to control plane via WebSocket over Tailscale
2. Control plane grabs a pre-booted VM from the warm pool (or creates one)
3. Prompts go to the Claude Agent SDK running inside the VM
4. Responses stream back in real-time via Redis Streams (no missed events on reconnect)
5. Pause deletes the VM, but JuiceFS PVC keeps the data (workspace, mise tools, Docker)
6. Resume mounts the same storage in a new VM, conversation continues

### Why JuiceFS

JuiceFS is a POSIX filesystem backed by S3:

- **Pause**: VM deleted, PVC retained. Data lives in S3, costs ~$0.01/GB/month.
- **Resume**: New VM mounts the same PVC. Workspace, installed tools, Docker images, SDK session all still there.

Dozens of paused sessions on a small VPS. Only running sessions consume compute.

### Kata Containers

Each agent runs in a Kata Container, a lightweight VM using Cloud Hypervisor. Separate kernel, memory, filesystem per agent.

Why Cloud Hypervisor over Firecracker? Firecracker doesn't support virtiofs, which means you'd need devmapper snapshotter and a more complex storage setup. Cloud Hypervisor + virtiofs is simpler and performs well enough.

### Sandbox CRDs

Sandboxes are managed via custom k8s resources:

- `Sandbox` - A running agent VM
- `SandboxClaim` - Request for a sandbox (can be satisfied from warm pool)
- `SandboxTemplate` - Pod spec + PVC templates for sandboxes
- `SandboxWarmPool` - Maintains N pre-booted VMs ready for instant allocation

The [agent-sandbox-controller](https://github.com/angristan/agent-sandbox) reconciles these. It's a fork of [kubernetes-sigs/agent-sandbox](https://github.com/kubernetes-sigs/agent-sandbox) with additions:

- `volumeClaimTemplates` in SandboxTemplate (upstream only supports ephemeral storage)
- PVC adoption when SandboxClaim binds to a warm pool pod

## Stack

| Component | Technology |
|-----------|------------|
| Host OS | NixOS (fully declarative) |
| Orchestration | k3s |
| VM Runtime | Kata Containers (Cloud Hypervisor) |
| Storage | JuiceFS CSI → S3, Redis |
| Networking | Tailscale Operator |
| Control Plane | Go + Redis |
| Agent | Node.js + Claude Agent SDK |
| iOS/Mac | SwiftUI (iOS 26 Liquid Glass) |

## Client

Main interface is the **iOS/Mac app** (SwiftUI, iOS 26 Liquid Glass).

## Project structure

```
netclode/
├── clients/
│   └── ios/              # iOS/Mac app (SwiftUI)
├── services/
│   ├── control-plane/    # Session orchestration (Go)
│   └── agent/            # Claude Agent SDK runner (Node.js)
├── infra/
│   ├── nixos/            # NixOS configuration
│   └── k8s/              # Kubernetes manifests
└── docs/                 # Setup guides
```

## Getting started

See [docs/deployment.md](docs/deployment.md) for full setup.

Quick version:

1. Provision a VPS with nested virtualization support (DigitalOcean, Vultr)
2. Install NixOS via nixos-anywhere
3. Configure secrets (Anthropic API key, S3 credentials, Tailscale OAuth)
4. Deploy k8s manifests
5. Connect via Tailscale

## Docs

- [Deployment](docs/deployment.md) - Full setup
- [Operations](docs/operations.md) - Day-to-day management
- [GitHub Integration](docs/github-integration.md) - Clone repos and push commits
- [iOS App](clients/ios/README.md)
- [Control Plane](services/control-plane/README.md)
- [Agent](services/agent/README.md)
- [Infrastructure](infra/k8s/README.md)

## Future

- OpenCode server/SDK support
- Code diff viewer
- Notifications (iOS push, etc.)
- Plan mode
- Custom environment support

## License

MIT
