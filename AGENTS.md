# Repository Guidelines

## Project Overview

Netclode is a **self-hosted coding agent** that lets you run AI-powered development sessions inside isolated microVMs, controlled from a native iOS/macOS app or CLI. This fork uses **Docker Compose + BoxLite microVMs** instead of the upstream Kubernetes+Kata deployment.

The monorepo spans three languages: **Go** (control plane, bots, CLI), **TypeScript** (agent SDK runner), and **SwiftUI** (iOS/macOS client). All inter-service communication uses **Protocol Buffers + Connect RPC** over HTTP/2 bidirectional streams.

## Architecture & Data Flow

```
Client (iOS/CLI)  ←→  Control Plane (Go)  ←→  Agent (Node.js inside BoxLite VM)
                          ↕ Redis
                      GitHub Bot (Go)
```

- **Clients** connect to the control plane via `ClientService.Connect` — a single bidirectional Connect RPC stream carrying all requests (create/list/open sessions, send prompts, terminal I/O, snapshots, git operations).
- **Control plane** manages session lifecycle, orchestrates BoxLite/K8s sandboxes, and forwards prompts to agents via `AgentService.Connect` (agents connect *to* the control plane, not the other way around).
- **Agent** is a Node.js process inside a BoxLite microVM. It wraps multiple SDKs (Claude, OpenCode, Copilot, Codex) behind a unified interface and streams prompt events back to the control plane.
- **Redis** (with Streams) persists all session state and event history. Clients can disconnect/reconnect without losing events.
- **Secret Proxy** sits outside the sandbox and injects real API keys into requests via MITM, so secrets never enter the VM.
- **GitHub Bot** listens for webhooks, creates sessions on @mentions, and auto-reviews dependency update PRs.

### Session Lifecycle

1. Client sends `CreateSessionRequest` (SDK type, repos, model, resources).
2. Control plane allocates a BoxLite VM (or picks from warm pool in K8s mode). In Docker mode, warm pool is disabled.
3. Agent boots, connects to control plane, receives `SessionConfig` (API keys as placeholders, repo list, SDK config).
4. Agent clones repos, initializes SDK session, streams events. Secret proxy transparently substitutes real keys for placeholders on outbound LLM API calls.
5. Client streams messages/events in real time. Terminal access available via attached PTY.
6. On pause, VM is stopped but persistent storage remains. Resume remounts and continues.

## Key Directories

| Path | Purpose |
|------|---------|
| `services/control-plane/` | Go — session orchestration, BoxLite/K8s runtime, Connect API server |
| `services/control-plane/internal/config/` | Configuration from env vars |
| `services/control-plane/internal/api/` | HTTP/Connect server, client + agent handlers |
| `services/control-plane/internal/session/` | Session manager (the core — 127KB), state, agent handlers |
| `services/control-plane/internal/storage/` | Redis-backed session persistence |
| `services/control-plane/internal/boxlite/` | BoxLite runtime (Docker mode) |
| `services/control-plane/internal/k8s/` | Kubernetes runtime (upstream mode) |
| `services/control-plane/internal/github/` | GitHub App client for repo access |
| `services/agent/src/` | TypeScript — SDK runner entry point and bidirectional transport |
| `services/agent/src/sdk/` | SDK abstraction layer: types, factory, runtime, adapters |
| `services/agent/src/sdk/claude/` | Claude Code SDK adapter |
| `services/agent/src/sdk/opencode/` | OpenCode adapter |
| `services/agent/src/sdk/copilot/` | GitHub Copilot adapter |
| `services/agent/src/sdk/codex/` | OpenAI Codex adapter |
| `services/agent/src/services/` | Session mapping, terminal PTY, prompt title generation |
| `services/agent/auth-proxy/` | Go — K8s SA token authentication proxy (upstream only) |
| `services/secret-proxy/` | Go — MITM proxy injecting API keys outside sandbox |
| `services/github-bot/` | Go — webhook handler, @mention bot, dependency review |
| `clients/cli/` | Go — debug CLI (Cobra-based) |
| `clients/ios/` | SwiftUI — native iOS/macOS app |
| `proto/` | Protobuf definitions, buf config |
| `third_party/boxlite-go-sdk/` | Vendored BoxLite Go SDK (CGo) |
| `docs/` | Architecture, deployment, operations docs |
| `infra/docker/` | Docker deployment docs |

## Development Commands

### Proto

```bash
make proto         # Generate code from proto files (Go, TS, Swift)
make proto-lint    # Lint proto files
make proto-breaking # Check for breaking changes against main
```

Generated code destinations:
- Go: `services/control-plane/gen/`
- TypeScript: `services/agent/gen/`
- Swift: `clients/ios/Netclode/Generated/`

### Agent (TypeScript)

```bash
cd services/agent
npm run dev        # Watch mode with tsx
npm run build      # Bundle with esbuild → dist/agent.js
npm run typecheck  # tsc --noEmit
npm test           # vitest run
npm run test:watch # vitest watch mode
```

### Control Plane / Go Services

All Go services share the `go.work` workspace at root (Go 1.25.5). Build/test from service directories:

```bash
cd services/control-plane
go build ./cmd/control-plane
go test ./...

# Other services:
cd services/secret-proxy && go test ./...
cd services/github-bot    && go test ./...
```

### CLI

```bash
cd clients/cli
make build         # Build binary
make install       # Install to $GOPATH/bin
make build-all     # Cross-compile for 4 platforms
```

Run the CLI directly after build:
```bash
./netclode sessions list
NETCLODE_URL=https://your-server ./netclode sessions list
```

### iOS/macOS

```bash
make test-ios      # Run iOS unit tests (XCTest on macOS)
make run-macos     # Build and run macOS Catalyst app
make run-ios       # Build and run iOS simulator
make run-device    # Build and run on connected iPhone
```

### Docker Compose

```bash
docker compose up -d                          # Start control-plane + redis + tailscale
docker compose --profile github-bot up -d     # Include GitHub bot
```

## Code Conventions & Common Patterns

### Go

- **Module path**: `github.com/angristan/netclode/services/<name>` for services, `github.com/angristan/netclode/clients/cli` for CLI.
- **Go workspace** (`go.work`) at root ties together `clients/cli`, `services/control-plane`, `services/github-bot`, `services/secret-proxy`, and `services/agent/auth-proxy`.
- **`internal/` packages** enforce visibility — all implementation lives under `internal/`, only `cmd/` and `gen/` are importable externally.
- **Configuration**: Each service has a `config.Load()` that reads env vars with sensible defaults. Pattern: `getEnv("KEY", "default")`, `getEnvInt("KEY", 0)`, `getEnvBool("KEY", false)`.
- **Logging**: `log/slog` structured JSON logging with Datadog trace correlation. Use `slog.Info("message", "key", value)`.
- **Metrics**: DogStatsD via `github.com/DataDog/datadog-go/v5`. Init with `metrics.Init()`, close with `metrics.Close()`.
- **Error handling**: Standard `fmt.Errorf("context: %w", err)` wrapping. Functions return `error` as last value.
- **Dependency injection**: Interfaces defined at use-site (e.g., `storage.Storage`, `k8s.Runtime`), concrete implementations injected in `main()`.
- **Context propagation**: `context.Context` as first parameter, used for cancellation and tracing.
- **Graceful shutdown**: SIGTERM/SIGINT → cancel context → drain connections → close resources.

### TypeScript

- **Module system**: ESM (`"type": "module"`). Use `.js` extensions in imports.
- **TypeScript config**: Strict mode, ES2022 target, `verbatimModuleSyntax`, `isolatedModules`. Base config in `tsconfig.base.json`.
- **SDK adapter pattern**: Each AI provider (Claude, OpenCode, Copilot, Codex) implements a common `NetclodePromptBackend` interface. Factory (`createNetclodeAgent`) composes backends with runtime services.
- **Async generators**: Prompt backends return `AsyncGenerator<PromptEvent>` — events are yielded as they stream from the LLM.
- **Session state**: In-memory `Map<string, string>` persisted to disk (`/agent/.session-mapping.json`) to survive agent restarts.
- **Node modules**: `node-pty` for terminal PTY, `undici` for HTTP, `@bufbuild/protobuf` + `@connectrpc/connect` for RPC.
- **Bundle**: `esbuild` bundles to a single ESM file for the runtime image.

### Protobuf

- **Package**: `netclode.v1`
- **Services**: `ClientService` (client ↔ control plane), `AgentService` (agent ↔ control plane)
- **Code generation**: `buf generate` with remote plugins for Go (protobuf, gRPC, Connect), TypeScript (Protobuf-ES v2), Swift (SwiftProtobuf + Connect-Swift).
- **Go import path**: `github.com/angristan/netclode/services/control-plane/gen/netclode/v1`
- **Streaming**: Both services use bidirectional streams — `rpc Connect(stream Message) returns (stream Message)`.

### Swift/iOS

- **Architecture**: MVVM with `@Observable` store classes (SwiftUI Observation framework).
- **Key services**: `ConnectService` (Connect RPC client — 61KB), `MessageRouter` (message dispatch), `AppStateCoordinator`, `SpeechService`.
- **Stores**: `ChatStore`, `EventStore`, `SessionStore`, `GitHubStore`, `TerminalStore`, `SnapshotStore`.
- **Code generation**: Proto → Swift types in `Netclode/Generated/`.
- **Testing**: `XCTest` with `@testable import Netclode`.

## Important Files

| File | Role |
|------|------|
| `docker-compose.yml` | Single-server deployment with control-plane, redis, tailscale |
| `go.work` | Go workspace tying all modules together |
| `Makefile` | Root build orchestration (proto, rollout, iOS) |
| `proto/buf.gen.yaml` | Code generation plugins and output paths |
| `proto/buf.yaml` | Buf config (lint: STANDARD, breaking: FILE) |
| `services/control-plane/cmd/control-plane/main.go` | Control plane entry point |
| `services/control-plane/internal/config/config.go` | All env-var configuration |
| `services/control-plane/internal/session/manager.go` | Core session lifecycle (127KB) |
| `services/control-plane/internal/api/connect_agent.go` | Agent bidirectional stream handler |
| `services/control-plane/internal/api/connect_client.go` | Client bidirectional stream handler |
| `services/control-plane/internal/storage/redis.go` | Redis Streams persistence |
| `services/control-plane/internal/boxlite/runtime.go` | BoxLite VM lifecycle |
| `services/agent/src/index.ts` | Agent entry point |
| `services/agent/src/connect-client.ts` | Agent ↔ control plane transport (23KB) |
| `services/agent/src/sdk/types.ts` | Core agent types and interfaces |
| `services/agent/src/sdk/factory.ts` | Agent runtime composition |
| `services/agent/src/sdk/runtime.ts` | `ComposedNetclodeAgent` — orchestrates backends |
| `services/agent/entrypoint.sh` | VM boot script (git config, env, startup log shipping) |
| `services/agent/Dockerfile` | Multi-stage image (Node + CLI tools + agent bundle) |
| `services/control-plane/Dockerfile` | BoxLite runtime + Go binary |
| `clients/cli/cmd/root.go` | CLI root command (Cobra) |
| `clients/ios/Netclode/App/NetclodeApp.swift` | iOS app entry |
| `tsconfig.base.json` | Shared TypeScript strict config |

## Runtime/Tooling Preferences

- **Go**: 1.25.5 (workspace mode). Build with `CGO_ENABLED=1` for BoxLite CGo bindings.
- **Node.js**: 24 (managed via mise in Docker image). Production bundle uses `esbuild`.
- **TypeScript**: 5.7.x, strict mode, `moduleResolution: "bundler"`.
- **Package manager**: npm (no lockfile preference — `package-lock.json` present).
- **Container base**: `debian:trixie-slim` (agent), `debian:bookworm-slim` (control plane), `golang:1.25-bookworm` (build stage).
- **Orchestration**: Docker Compose (this fork). Upstream uses k3s/Kubernetes.
- **Observability**: Datadog (tracing, profiling, metrics via DogStatsD). Slog JSON logging with trace correlation.
- **Proxy**: The secret-proxy is a standalone Go service using `elazarl/goproxy` for MITM certificate injection.
- **iOS**: SwiftUI targeting iOS 26+, macOS Catalyst. Signing via `DEVELOPMENT_TEAM` env var.
- **Protobuf**: `buf` CLI (install via `brew install bufbuild/buf/buf`). All generation through remote plugins.

## Testing & QA

### Go Tests

- Standard library `testing` package.
- Test files: `*_test.go` alongside source.
- **Mock pattern**: Interfaces enable test doubles (e.g., `storage.Storage` mocked with `miniredis` in control plane tests).
- **Table-driven tests**: Used extensively, especially in config and bot detection tests.
- Run: `go test ./...` from service directory or `go test ./internal/...` for specific packages.

### TypeScript Tests

- Framework: **Vitest** (`vitest run` or `vitest` for watch).
- Test files: `*.test.ts` alongside source.
- Key test areas: SDK factory composition (`factory.test.ts`), secret materialization (`secret-materialization.test.ts`), git operations (`git.test.ts`), connect client (`connect-client.test.ts`), terminal PTY (`terminal.test.ts`), prompt generation (`prompt.test.ts`).
- Run: `npm test` or `npm run test:watch` in `services/agent/`.

### Swift Tests

- Framework: **XCTest** (`NetclodeTests` target).
- Key test files: `EventStoreTests.swift` (19KB), `MessageRouterTests.swift` (7KB).
- Run: `make test-ios` or `xcodebuild test -scheme NetclodeTests -destination 'platform=macOS' -quiet`.

### Integration Tests

- `third_party/boxlite-go-sdk/network_secrets_integration_test.go` — BoxLite network + secrets integration.
- Use build tags or env guards for tests requiring live infrastructure.
- CLI `shell_test.go` — integration-style tests for the shell command.
