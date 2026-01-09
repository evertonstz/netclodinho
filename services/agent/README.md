# Agent

Claude Code agent that runs inside sandboxed Kata Container VMs. Uses the Claude Agent SDK to execute coding tasks.

## Structure

```
services/agent/
├── src/
│   ├── index.ts        # HTTP server entry point
│   ├── config.ts       # Configuration
│   ├── sdk/
│   │   ├── agent.ts    # Claude Agent SDK wrapper
│   │   └── tools.ts    # Tool configuration
│   ├── events/
│   │   └── emitter.ts  # Event streaming
│   └── ipc/
│       └── handler.ts  # IPC message handling
├── package.json
└── tsconfig.json
```

## Configuration

Environment variables (injected via k8s Secret):

| Variable | Description |
|----------|-------------|
| `ANTHROPIC_API_KEY` | Anthropic API key |
| `SESSION_ID` | Current session ID |
| `GIT_REPO` | (Optional) Git repo URL to clone into workspace |

## API

The agent exposes an HTTP API on port 3002:

### POST /prompt

Execute a prompt and stream results via SSE.

**Request:**
```json
{
  "sessionId": "abc123",
  "text": "Fix the bug in auth.ts"
}
```

**Response:** Server-Sent Events stream

```
data: {"type":"tool_call","tool":"Read","path":"auth.ts"}

data: {"type":"tool_result","content":"..."}

data: {"type":"assistant","content":"I found the issue..."}
```

### POST /interrupt

Interrupt the current operation.

**Response:**
```json
{ "ok": true }
```

### POST /generate-title

Generate a session title using Claude Haiku based on the user's prompt.

**Request:**
```json
{
  "prompt": "Build a REST API with user authentication"
}
```

**Response:**
```json
{
  "title": "REST API Authentication"
}
```

This endpoint is called by the control plane after the first agent response to generate a meaningful session title.

### GET /health

Health check.

**Response:**
```
ok
```

## Claude Agent SDK Integration

The agent uses the `query()` async iterator from the Claude Agent SDK to stream events in real-time:

```typescript
import { query } from "@anthropic-ai/claude-agent-sdk";

const q = query({
  prompt: text,
  options: {
    cwd: workspaceDir,
    permissionMode: "bypassPermissions", // VM handles isolation
    model: "claude-opus-4-5-20251101",
    persistSession: true,
    systemPrompt: { type: "preset", preset: "claude_code", append: "..." },
    ...(sdkSessionId && { resume: sdkSessionId }),
  },
});

for await (const message of q) {
  switch (message.type) {
    case "system":      // Init, session ID
    case "assistant":   // Text blocks, tool_use blocks
    case "user":        // Tool results
    case "result":      // Final result with cost/turns
    case "stream_event": // Content deltas for real-time streaming
  }
}
```

### Message Types

| Type | Description |
|------|-------------|
| `system` | Init message with `session_id` for resuming conversations |
| `assistant` | Claude's response with `text` and `tool_use` content blocks |
| `user` | Tool results (`tool_result` blocks with `tool_use_id`) |
| `result` | Final result with `num_turns`, `total_cost_usd` |
| `stream_event` | Real-time deltas: `content_block_start`, `content_block_delta`, `content_block_stop` |

### Available Tools

The SDK provides built-in tools (all enabled via `bypassPermissions`):

| Tool | Description |
|------|-------------|
| `Read` | Read file contents |
| `Write` | Write file contents |
| `Edit` | Edit file with string replacement |
| `Bash` | Execute shell commands |
| `Glob` | Find files by pattern |
| `Grep` | Search file contents |
| `WebSearch` | Search the web |
| `WebFetch` | Fetch URL content |

## VM Environment

When running inside a Kata Container VM:

- `/agent` - Home directory (JuiceFS PVC, persistent)
  - `/agent/workspace` - User's code/projects (cwd for Claude)
  - `/agent/docker` - Docker data
  - `/agent/.local/share/mise` - Installed tools via mise
  - `/agent/.cache` - Package manager caches
- `/opt/agent` - Agent code (read-only)
- Docker available for container workloads
- mise for installing language runtimes (Node, Python, Go, Rust, etc.)
- Internet access (no internal network access)

### Installing Dependencies

The agent can install tools via mise (persistent across sessions):

```bash
# Install and activate Node.js
mise use node@22

# Install Python
mise use python@3.12

# Install Go
mise use go@latest
```

Docker is also available:

```bash
docker run -v /agent/workspace:/app node:20 npm install
```

## Development

```bash
# Install dependencies
npm install

# Run locally (limited without VM environment)
npm run dev

# Type check
npm run typecheck
```

## Building the Agent Image

The agent is packaged into a Debian-slim + mise OCI image:

```bash
# Build image locally
docker build -t ghcr.io/angristan/netclode-agent:latest -f services/agent/Dockerfile .
```

The image includes:

- Debian bookworm-slim base
- Node.js via mise (on-demand tooling)
- Docker daemon
- Git, curl, build-essential
- Claude CLI

## SSE Event Types

Events streamed via Server-Sent Events during prompt execution:

| Type | Description |
|------|-------------|
| `start` | Prompt execution started |
| `agent.system` | SDK system message (init, session ID) |
| `agent.message` | Text content from Claude (`partial: true` for streaming) |
| `agent.event` | Tool/command events (see below) |
| `agent.result` | Final result with `numTurns` and `costUsd` |
| `done` | Prompt execution completed |
| `error` | Error occurred |

### Agent Events (`agent.event`)

| Event Kind | Description | Fields |
|------------|-------------|--------|
| `tool_start` | Tool invocation started | `tool`, `toolUseId`, `input` |
| `tool_input` | Streaming tool input | `toolUseId`, `inputDelta` |
| `tool_end` | Tool completed | `tool`, `toolUseId`, `result?`, `error?` |

All events include a `timestamp` field (ISO 8601).

## Debugging

From the k8s cluster (when an agent pod is running):

```bash
# List agent pods
kubectl get pods -n netclode -l sandbox=true

# View agent logs
kubectl logs -n netclode <agent-pod-name> -f

# Exec into agent pod
kubectl exec -it -n netclode <agent-pod-name> -- /bin/bash
```

Inside the VM:

```bash
# Check agent process
ps aux | grep node

# View workspace
ls -la /agent/workspace

# Test connectivity
curl http://control-plane.netclode.svc.cluster.local:80/health
```
