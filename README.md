# Netclode

<p align="center">
  <img src="clients/ios/Netclode/Resources/Assets.xcassets/AppIcon.appiconset/AppIcon.png" alt="netclode" width="120" height="120">
</p>

Self-hosted coding agent with microVM sandboxes and a native iOS app.

<p align="center">
  <img src="docs/images/ios-netclode.png" alt="Netclode iOS App" height="500">
</p>

## Why I built this

I wanted a self-hosted Claude Code environment I can use from my phone, with the UX I actually want. The existing cloud coding agents were a bit underwhelming when I tried them, so I built my own!

I wrote a blog post about how it works: [Building a self-hosted cloud coding agent](https://stanislas.blog/2026/02/netclode-self-hosted-cloud-coding-agent/).

## What makes it nice

- **Full yolo mode** - Docker, root access, install anything. The microVM handles isolation
- **Local inference with Ollama** - Run models on your own GPU, nothing leaves your machine
- **Tailnet integration** - Preview URLs, port forwarding, access to my infra through Tailscale
- **JuiceFS for storage** - Storage offloaded to S3. Paused sessions cost nothing but storage
- **Live terminal access** - Drop into the sandbox shell from the app
- **Session history** - Auto-snapshots after each turn. Roll back workspace and chat to any previous point
- **Multiple SDKs** - Claude Code, OpenCode, Copilot, Codex. Swap anytime

## How it works

```mermaid
flowchart LR
    subgraph CLIENT["Client"]
        APP["iOS / macOS<br/><sub>SwiftUI</sub>"]
    end

    subgraph VPS["VPS - k3s"]
        TS["Tailscale Ingress<br/><sub>TLS - HTTP/2</sub>"]
        CP["Control Plane<br/><sub>Go</sub>"]
        REDIS[("Redis<br/><sub>Sessions</sub>")]
        POOL["agent-sandbox<br/><sub>Warm Pool</sub>"]
        JFS[("JuiceFS")]

        subgraph SANDBOX["Sandbox - Kata VM<br/><sub>Cloud Hypervisor</sub>"]
            AGENT["Agent<br/><sub>Claude / OpenCode / Copilot / Codex SDK</sub>"]
            DOCKER["Docker"]
        end
    end

    S3[("S3")]
    LLM["LLM APIs"]

    APP <-->|"Connect RPC<br/>HTTPS/H2"| TS
    TS <-->|"Connect RPC<br/>h2c"| CP
    CP <-->|"Redis Streams"| REDIS
    CP <-->|"Connect RPC<br/>gRPC/h2c"| AGENT
    POOL -.->|"allocate"| SANDBOX
    JFS <--> SANDBOX
    JFS <-->|"POSIX on S3"| S3
    AGENT --> LLM
```

The control plane grabs a pre-booted Kata VM from the warm pool (so it's instant), forwards prompts to the agent SDK inside, and streams responses back. Redis persists events so clients can reconnect without losing anything.

When pausing, the VM is deleted but JuiceFS keeps everything in S3: workspace, installed tools, Docker images, SDK session. Resume mounts the same storage and the conversation continues as if nothing happened. Dozens of paused sessions cost practically nothing.

## Stack

| Layer             | Technology                         | Purpose                                      |
| ----------------- | ---------------------------------- | -------------------------------------------- |
| **Host**          | Linux VPS + Ansible                | Provisioned via playbooks                    |
| **Orchestration** | k3s                                | Lightweight Kubernetes, nice for single-node |
| **Isolation**     | Kata Containers + Cloud Hypervisor | MicroVM per agent session                    |
| **Storage**       | JuiceFS → S3                       | POSIX filesystem on object storage           |
| **State**         | Redis (Streams)                    | Real-time, streaming session state           |
| **Network**       | Tailscale Operator                 | VPN to host, ingress, sandbox previews       |
| **API**           | Protobuf + Connect RPC             | Type-safe, gRPC-like, streams                |
| **Control Plane** | Go                                 | Session and sandbox orchestration            |
| **Agent**         | TypeScript/Node.js                 | SDK runner inside sandbox                    |
| **Local LLM**     | Ollama                             | Optional, local models on GPU                |
| **Client**        | SwiftUI (iOS 26)                   | Native iOS/macOS app                         |
| **CLI**           | Go                                 | Debug client for development                 |

## Project structure

```
netclode/
├── clients/
│   ├── ios/              # iOS/Mac app (SwiftUI)
│   └── cli/              # Debug CLI (Go)
├── services/
│   ├── control-plane/    # Session orchestration (Go)
│   └── agent/            # SDK runner (Node.js)
├── proto/                # Protobuf definitions
├── infra/
│   ├── ansible/          # Server provisioning
│   └── k8s/              # Kubernetes manifests
└── docs/
```

## Getting started

See [docs/deployment.md](docs/deployment.md) for full setup. I tried to make it as easy as possible: ideally a single playbook run.

Quick version:

1. Provision a VPS with nested virtualization support
2. Run Ansible playbooks to provision the server
3. Configure secrets (API keys, S3 credentials, Tailscale OAuth)
4. Deploy k8s manifests
5. Connect via Tailscale and you're good to go

## Docs

- [Deployment](docs/deployment.md) - Full setup
- [Operations](docs/operations.md) - Day-to-day management
- [Sandbox Architecture](docs/sandbox-architecture.md) - Kata VMs, JuiceFS, warm pool
- [Session Lifecycle](docs/session-lifecycle.md) - How sessions work
- [Session History](docs/session-history.md) - Snapshots and rollback
- [GitHub Integration](docs/github-integration.md) - Clone repos and push commits
- [Network Access](docs/network-access.md) - Internet and Tailnet access control
- [Web Previews](docs/web-previews.md) - Port exposure via Tailscale
- [Terminal](docs/terminal.md) - Live shell access
- [SDK Support](docs/sdk-support.md) - Claude, OpenCode, Copilot, Codex
- [Agent Events](docs/agent-events.md) - Event types and streaming
- [iOS App](clients/ios/README.md)
- [CLI](clients/cli/README.md) - Debug CLI
- [Control Plane](services/control-plane/README.md)
- [Agent](services/agent/README.md)
- [Infrastructure](infra/k8s/README.md)

## License

MIT
