# Agent Events

Real-time visibility into what the agent is doing. Events stream to clients and persist in Redis for replay.

## Event types

- **Tool events** - when the agent uses tools (Read, Edit, Bash, etc.)
- **File changes** - files created, modified, or deleted
- **Commands** - shell command execution with output
- **Thinking** - extended reasoning content (for supported models)
- **Connection events** - agent connect/disconnect status

## Tool Events

Tool events track the lifecycle of tool invocations.

### TOOL_START

Emitted when the agent begins using a tool.

| Field | Type | Description |
|-------|------|-------------|
| `tool` | string | Tool name (e.g., "Read", "Edit", "Bash") |
| `tool_use_id` | string | Unique ID for this invocation |
| `parent_tool_use_id` | string? | Set when tool runs inside a Task/subagent |
| `input` | object? | Full tool input (if available at start) |

### TOOL_INPUT

Emitted as tool input is streamed (for large inputs).

| Field | Type | Description |
|-------|------|-------------|
| `tool_use_id` | string | Correlates to TOOL_START |
| `input_delta` | string | Partial input chunk |

### TOOL_INPUT_COMPLETE

Emitted when tool input streaming finishes.

| Field | Type | Description |
|-------|------|-------------|
| `tool_use_id` | string | Correlates to TOOL_START |
| `input` | object | Complete tool input as JSON |

### TOOL_END

Emitted when tool execution completes.

| Field | Type | Description |
|-------|------|-------------|
| `tool` | string | Tool name |
| `tool_use_id` | string | Correlates to TOOL_START |
| `result` | string? | Tool output (on success) |
| `error` | string? | Error message (on failure) |
| `duration_ms` | int64? | Execution time in milliseconds |

### Example

```
TOOL_START     tool=Bash, id=abc123
TOOL_INPUT     id=abc123, delta="npm "
TOOL_INPUT     id=abc123, delta="install"
TOOL_INPUT_COMPLETE id=abc123, input={"command": "npm install"}
... (command runs) ...
TOOL_END       id=abc123, result="added 150 packages", duration_ms=4523
```

## File Change Events

Emitted when the agent modifies files.

| Field | Type | Description |
|-------|------|-------------|
| `path` | string | File path relative to workspace |
| `action` | enum | `CREATE`, `EDIT`, or `DELETE` |
| `lines_added` | int32? | Lines added (for EDIT) |
| `lines_removed` | int32? | Lines removed (for EDIT) |

## Command Events

### COMMAND_START

| Field | Type | Description |
|-------|------|-------------|
| `command` | string | The command being executed |
| `cwd` | string? | Working directory |

### COMMAND_END

| Field | Type | Description |
|-------|------|-------------|
| `command` | string | The command that ran |
| `exit_code` | int32? | Exit code (0 = success) |
| `output` | string? | Command output (may be truncated) |

## Thinking Events

Emitted when the model uses extended thinking (Claude Opus, etc.).

| Field | Type | Description |
|-------|------|-------------|
| `thinking_id` | string | Unique ID for this thinking block |
| `content` | string | Thinking content (delta if partial) |
| `partial` | bool | `true` for streaming deltas, `false` for final |

Thinking content streams in chunks. Accumulate content for the same `thinking_id` until `partial=false`.

## Other Events

| Kind | Description |
|------|-------------|
| `REPO_CLONE` | Repository clone progress (STARTING/CLONING/DONE/ERROR) |
| `PORT_EXPOSED` | Port exposed for preview (port, preview_url) |
| `PORT_UNEXPOSED` | Port exposure removed (port) |
| `AGENT_DISCONNECTED` | Agent lost connection |
| `AGENT_RECONNECTED` | Agent reconnected |

## Nested Tool Calls

When the agent uses the Task tool to spawn a subagent, events include `parent_tool_use_id`:

```
TOOL_START     tool=Task, id=task_1
  TOOL_START   tool=Read, id=read_1, parent=task_1
  TOOL_END     tool=Read, id=read_1, parent=task_1
  TOOL_START   tool=Edit, id=edit_1, parent=task_1
  TOOL_END     tool=Edit, id=edit_1, parent=task_1
TOOL_END       tool=Task, id=task_1
```

## Persistence

Events stored in Redis Stream: `session:{id}:stream`. Cursor-based reading handles reconnection without missing events.

## CLI

```bash
netclode events <session-id>                    # List events
netclode events <session-id> --kind tool_start  # Filter by kind
netclode events tail <session-id>               # Stream in real-time
netclode events <session-id> --json             # JSON output
```

## iOS App

Events appear in the activity feed with expandable tool input/output, file diffs, and collapsible thinking blocks.
