# Agent

Claude Code agent that runs inside sandboxed VMs. Uses the Claude Agent SDK to execute coding tasks.

## Structure

```
apps/agent/
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

Environment variables (injected by control plane):

| Variable | Description |
|----------|-------------|
| `ANTHROPIC_API_KEY` | Anthropic API key |
| `SESSION_ID` | Current session ID |
| `WORKSPACE` | Workspace path (default: `/workspace`) |

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

### GET /health

Health check.

**Response:**
```
ok
```

## Claude Agent SDK Integration

The agent uses the Claude Agent SDK for tool execution:

```typescript
import { createAgent } from "@anthropic-ai/claude-agent-sdk";

const agent = createAgent({
  apiKey: process.env.ANTHROPIC_API_KEY,
  allowedTools: ["Read", "Write", "Edit", "Bash", "Glob", "Grep"],
  permissionMode: "bypassPermissions", // VM handles isolation
});

await agent.run("Fix the bug in auth.ts");
```

### Available Tools

The SDK provides built-in tools:

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

### Hooks

The agent uses hooks for event streaming:

```typescript
const hooks = {
  PostToolUse: [{
    matcher: ".*",
    hooks: [(input, toolUseId, context) => {
      emitEvent({
        type: "tool_call",
        tool: input.tool_name,
        ...
      });
      return {};
    }]
  }]
};
```

## VM Environment

When running inside a Kata Container VM:

- `/workspace` - Mounted from JuiceFS (session's workspace)
- `/opt/agent` - Agent code
- Docker available for container workloads
- Nix available for dynamic dependencies
- Internet access (no internal network access)

### Installing Dependencies

The agent can install dependencies dynamically:

```bash
# Via nix-shell
nix-shell -p python311 --run "python script.py"

# Via Docker
docker run -v /workspace:/app node:20 npm install
```

## Development

```bash
# Install dependencies
bun install

# Run locally (limited without VM environment)
bun run dev

# Type check
bun run typecheck
```

## Building the Agent VM

The agent is packaged into a NixOS-based OCI image:

```bash
# Build image
cd infra/nixos
nix build .#agent-image

# The result is a Docker/containerd-compatible image
```

The image includes:

- NixOS minimal system
- Bun runtime
- Docker daemon
- Git, gh CLI
- Common dev tools

## Event Types

Events emitted during execution:

```typescript
// Tool call started
{ type: "tool_call", tool: "Read", path: "auth.ts" }

// Tool result
{ type: "tool_result", tool: "Read", content: "..." }

// Assistant message
{ type: "assistant", content: "I found the issue..." }

// Error
{ type: "error", message: "Failed to read file" }
```

## Debugging

Inside the VM:

```bash
# View agent logs
journalctl -u agent -f

# Check agent status
systemctl status agent

# Manually run agent
cd /opt/agent
bun run src/index.ts
```

From the host:

```bash
# Exec into VM
nerdctl exec -it sess-abc123 /bin/bash

# View VM logs
nerdctl logs sess-abc123
```
