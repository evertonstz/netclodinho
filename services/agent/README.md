# Agent

Claude Code agent that runs inside Kata Container VMs. Uses the Claude Agent SDK to execute coding tasks.

## What it does

- Executes prompts via the SDK's `query()` async iterator
- Full access to Docker, root, any tools - VM handles isolation
- Persistent workspace survives pause/resume
- Terminal access via bidirectional Connect streaming

## Structure

```
services/agent/
├── src/
│   ├── index.ts           # Entry point
│   ├── connect-server.ts  # Connect protocol server
│   ├── grpc-service.ts    # Service implementation
│   └── git.ts             # Git operations
├── gen/                   # Generated protobuf types
├── package.json
└── tsconfig.json
```

## Configuration

| Variable | Description |
|----------|-------------|
| `ANTHROPIC_API_KEY` | Anthropic API key |
| `AGENT_PORT` | Agent Connect server port (default 3002) |

## API

Connect protocol (gRPC-compatible) on port `3002`. See `proto/netclode/v1/agent.proto` for full definitions.

### ExecutePrompt (streaming)

Execute a prompt, stream results back via server streaming.

```protobuf
rpc ExecutePrompt(ExecutePromptRequest) returns (stream AgentStreamResponse);
```

Request:
```
session_id: "sess-abc123"
text: "Fix the bug in auth.ts"
config: { repo: "github.com/user/repo", github_token: "..." }
```

Response stream includes:
- `text_delta` - Text content (partial or complete)
- `event` - Tool events, thinking, file changes, etc.
- `system_message` - SDK system messages
- `result` - Final result with token counts
- `error` - Error messages

### Interrupt

Stop current execution.

```protobuf
rpc Interrupt(InterruptRequest) returns (InterruptResponse);
```

### GenerateTitle

Generate session title from first prompt.

```protobuf
rpc GenerateTitle(GenerateTitleRequest) returns (GenerateTitleResponse);
```

### GetGitStatus / GetGitDiff

Query git state of the workspace.

```protobuf
rpc GetGitStatus(GetGitStatusRequest) returns (GetGitStatusResponse);
rpc GetGitDiff(GetGitDiffRequest) returns (GetGitDiffResponse);
```

### Terminal (bidirectional streaming)

Interactive terminal via bidirectional streaming.

```protobuf
rpc Terminal(stream TerminalInput) returns (stream TerminalOutput);
```

Client sends:
- `data` - Terminal input (keystrokes)
- `resize` - Terminal dimensions (cols, rows)

Server streams:
- `data` - Terminal output

The PTY is managed by [node-pty](https://github.com/microsoft/node-pty). It's spawned lazily on first input to avoid idle shell processes. The shell runs as root in `/agent/workspace`.

```
iOS/Web ──► Control Plane ──► Agent ──► node-pty ──► bash
            (Connect)        (Connect)    (PTY)
```

The control plane maintains a Connect stream to the agent and bridges messages. Multiple clients can share the same terminal session.

### Health

```protobuf
rpc Health(HealthRequest) returns (HealthResponse);
```

Also available at `GET /health` for k8s probes.

## Claude Agent SDK

```typescript
import { query } from "@anthropic-ai/claude-agent-sdk";

const q = query({
  prompt: text,
  options: {
    cwd: workspaceDir,
    permissionMode: "bypassPermissions",
    model: "claude-opus-4-5-20251101",
    persistSession: true,
    systemPrompt: { type: "preset", preset: "claude_code", append: "..." },
    ...(sdkSessionId && { resume: sdkSessionId }),
  },
});

for await (const message of q) {
  // system, assistant (text, tool_use, thinking), user, result, stream_event
}
```

Available tools (all enabled via `bypassPermissions`): Read, Write, Edit, Bash, Glob, Grep, WebSearch, WebFetch.

## VM environment

```
/agent/                     # Home (JuiceFS PVC, persistent)
├── workspace/              # User's code (Claude's cwd)
├── docker/                 # Docker data
├── .local/share/mise/      # Installed tools
├── .cache/                 # Package caches
├── .claude/                # SDK session data
└── .session-mapping.json   # Session ID mapping

/opt/agent/                 # Agent code (read-only)
```

### Session ID mapping

The control plane assigns session IDs (`sess-abc123`). The Claude Agent SDK has its own session IDs for conversation persistence. These are different.

When you pause and resume a session, you get a new VM, but the JuiceFS PVC is the same. The agent needs to know which SDK session to resume.

`.session-mapping.json` maps control-plane session IDs to SDK session IDs:

```json
{
  "sess-abc123": "sdk-session-xyz789"
}
```

On first prompt, the agent stores the SDK session ID. On resume, it reads the mapping and passes `resume: sdkSessionId` to the SDK's `query()` call. Conversations survive pause/resume.

Tools persist via mise:

```bash
mise use node@22
mise use python@3.12
mise use go@latest
```

Docker is available:

```bash
docker run -v /agent/workspace:/app node:20 npm install
```

### Network isolation

Agents have internet access but are blocked from reaching cluster internals via NetworkPolicy:

- Can reach: internet (any external IP)
- Blocked: pod network (10.42.0.0/16), service network (10.43.0.0/16), node IPs

This prevents a compromised agent from attacking other pods, the k8s API, or Redis. The only allowed internal traffic is to the control plane (for session config and health checks).

### Port exposure (previews)

When a client sends `port.expose`, the control plane creates a Tailscale Service for the sandbox pod, giving it a MagicDNS hostname like `sandbox-abc123.tailnet-name.ts.net`.

The preview URL is then `http://sandbox-abc123.tailnet-name.ts.net:3000`. Accessible from any device on your tailnet.

## Development

```bash
npm install
npm run dev
npm run typecheck
npm run build
```

## Docker image

```bash
docker build -t ghcr.io/angristan/netclode-agent:latest -f services/agent/Dockerfile .
```

Includes Debian bookworm-slim, Node.js via mise, Docker, Git, curl, build-essential, Claude CLI.

## Agent Events

Events streamed during prompt execution:

| Event Kind | Description | Fields |
|------------|-------------|--------|
| `tool_start` | Tool invocation started | `tool`, `toolUseId`, `input` |
| `tool_input` | Streaming tool input | `toolUseId`, `inputDelta` |
| `tool_input_complete` | Tool input finished | `toolUseId`, `input` |
| `tool_end` | Tool completed | `tool`, `toolUseId`, `result?`, `error?` |
| `thinking` | Extended thinking content | `thinkingId`, `content`, `partial` |
| `file_change` | File created/edited/deleted | `path`, `action`, `linesAdded?`, `linesRemoved?` |
| `command_start` | Shell command started | `command`, `cwd` |
| `command_end` | Shell command completed | `command`, `exitCode`, `output` |
| `repo_clone` | Repository clone progress | `repo`, `stage`, `message` |

All events include a `timestamp` field.

### Thinking Events

When using models that support extended thinking (e.g., `claude-opus-4-5-20251101`), the agent streams thinking content in real-time:

- `partial: true` - Streaming delta (accumulate by `thinkingId`)
- `partial: false` - Complete thinking block

Clients should accumulate content for events with the same `thinkingId` to build up the full thinking output.

## Debugging

```bash
# List pods
kubectl get pods -n netclode -l sandbox=true

# Logs
kubectl logs -n netclode <agent-pod> -f

# Exec
kubectl exec -it -n netclode <agent-pod> -- /bin/bash
```

Inside the VM:

```bash
ps aux | grep node
ls -la /agent/workspace
curl http://control-plane.netclode.svc.cluster.local:80/health
```
