# Agent Events

Agent events provide real-time visibility into what the AI agent is doing during prompt execution. Events are streamed to clients and persisted in Redis for replay.

## Overview

When the agent executes a prompt, it emits a stream of events:

- **Tool events**: When the agent uses tools (Read, Edit, Bash, etc.)
- **File changes**: When files are created, modified, or deleted
- **Commands**: Shell command execution with output
- **Thinking**: Extended reasoning content (for supported models)
- **Connection events**: Agent connect/disconnect status

## Event Types

### Tool Events

Tool events track the lifecycle of tool invocations.

#### TOOL_START

Emitted when the agent begins using a tool.

| Field | Type | Description |
|-------|------|-------------|
| `tool` | string | Tool name (e.g., "Read", "Edit", "Bash") |
| `tool_use_id` | string | Unique ID for this invocation |
| `parent_tool_use_id` | string? | Set when tool runs inside a Task/subagent |
| `input` | object? | Full tool input (if available at start) |

#### TOOL_INPUT

Emitted as tool input is streamed (for large inputs).

| Field | Type | Description |
|-------|------|-------------|
| `tool_use_id` | string | Correlates to TOOL_START |
| `input_delta` | string | Partial input chunk |
| `parent_tool_use_id` | string? | Parent tool if nested |

#### TOOL_INPUT_COMPLETE

Emitted when tool input streaming finishes.

| Field | Type | Description |
|-------|------|-------------|
| `tool_use_id` | string | Correlates to TOOL_START |
| `input` | object | Complete tool input as JSON |
| `parent_tool_use_id` | string? | Parent tool if nested |

#### TOOL_END

Emitted when tool execution completes.

| Field | Type | Description |
|-------|------|-------------|
| `tool` | string | Tool name |
| `tool_use_id` | string | Correlates to TOOL_START |
| `result` | string? | Tool output (on success) |
| `error` | string? | Error message (on failure) |
| `duration_ms` | int64? | Execution time in milliseconds |
| `parent_tool_use_id` | string? | Parent tool if nested |

### Tool Lifecycle Example

```
TOOL_START     tool=Bash, id=abc123
TOOL_INPUT     id=abc123, delta="npm "
TOOL_INPUT     id=abc123, delta="install"
TOOL_INPUT_COMPLETE id=abc123, input={"command": "npm install"}
... (command runs) ...
TOOL_END       id=abc123, result="added 150 packages", duration_ms=4523
```

### File Change Events

Emitted when the agent modifies files in the workspace.

| Field | Type | Description |
|-------|------|-------------|
| `path` | string | File path relative to workspace |
| `action` | enum | `CREATE`, `EDIT`, or `DELETE` |
| `lines_added` | int32? | Lines added (for EDIT) |
| `lines_removed` | int32? | Lines removed (for EDIT) |

File change events are often emitted alongside tool events (e.g., after an Edit tool completes).

### Command Events

Emitted for shell command execution (from Bash tool).

#### COMMAND_START

| Field | Type | Description |
|-------|------|-------------|
| `command` | string | The command being executed |
| `cwd` | string? | Working directory |

#### COMMAND_END

| Field | Type | Description |
|-------|------|-------------|
| `command` | string | The command that ran |
| `exit_code` | int32? | Exit code (0 = success) |
| `output` | string? | Command output (may be truncated) |

### Thinking Events

Emitted when the model uses extended thinking (Claude Opus, etc.).

| Field | Type | Description |
|-------|------|-------------|
| `thinking_id` | string | Unique ID for this thinking block |
| `content` | string | Thinking content (delta if partial) |
| `partial` | bool | `true` for streaming deltas, `false` for final |

#### Streaming Aggregation

Thinking content is streamed in chunks:

```
THINKING  id=think_1, content="Let me analyze...", partial=true
THINKING  id=think_1, content=" this code structure", partial=true
THINKING  id=think_1, content="", partial=false  # End marker
```

Clients should accumulate content for the same `thinking_id` until `partial=false`.

### Repository Clone Events

Emitted during initial repository cloning.

| Field | Type | Description |
|-------|------|-------------|
| `repo` | string | Repository URL being cloned |
| `stage` | enum | `STARTING`, `CLONING`, `DONE`, `ERROR` |
| `message` | string | Human-readable progress message |

### Port Exposed Events

Emitted when a port is exposed for preview.

| Field | Type | Description |
|-------|------|-------------|
| `port` | int32 | Port number exposed |
| `process` | string? | Process name listening on the port |
| `preview_url` | string? | URL to access the port |

### Connection Events

Emitted when the agent connection state changes.

#### AGENT_DISCONNECTED

The agent unexpectedly disconnected (sandbox crash, network issue, etc.).

#### AGENT_RECONNECTED

The agent reconnected after a disconnect.

## Event Payload Structure

All events share a common wrapper:

```protobuf
message AgentEvent {
  AgentEventKind kind = 1;           // Event type enum
  google.protobuf.Timestamp timestamp = 2;
  
  oneof payload {
    ToolEventPayload tool = 3;
    FileChangePayload file_change = 4;
    CommandPayload command = 5;
    ThinkingPayload thinking = 6;
    PortExposedPayload port_exposed = 7;
    RepoClonePayload repo_clone = 8;
  }
}
```

## Nested Tool Calls (Task Tool)

When the agent uses the Task tool to spawn a subagent, events from the subagent include `parent_tool_use_id`:

```
TOOL_START     tool=Task, id=task_1
  TOOL_START   tool=Read, id=read_1, parent=task_1
  TOOL_END     tool=Read, id=read_1, parent=task_1
  TOOL_START   tool=Edit, id=edit_1, parent=task_1
  TOOL_END     tool=Edit, id=edit_1, parent=task_1
TOOL_END       tool=Task, id=task_1
```

This allows clients to display nested tool execution hierarchically.

## Persistence

Events are stored in a unified Redis Stream per session:

```
session:{id}:stream  # All session data (events, messages, status changes)
```

The stream uses cursor-based reading to handle reconnection without missing events (see [control-plane README](../services/control-plane/README.md#unified-stream-model)).

## Filtering Events

### By Kind

Use the `--kind` flag with the CLI:

```bash
netclode events <session-id> --kind tool_start
netclode events <session-id> --kind file_change
netclode events <session-id> --kind thinking
```

### Multiple Kinds

Filter in your client code:

```swift
events.filter { $0.kind == .toolStart || $0.kind == .toolEnd }
```

## Event Timing

Events include timestamps for:
- **Debugging**: Understanding execution order
- **Performance**: Measuring tool duration
- **Replay**: Recreating the execution timeline

The `duration_ms` field on TOOL_END is calculated from TOOL_START to TOOL_END timestamps.

## Client Integration

### iOS App

Events appear in the activity feed, showing:
- Tool invocations with expandable input/output
- File changes with diff previews
- Thinking content in collapsible blocks
- Command execution with output

### CLI

Stream events in real-time:

```bash
netclode events tail <session-id>
```

Output:
```
[14:32:05] tool_start      Read                 filePath=/src/main.go
[14:32:05] tool_end        Read                 success (45ms)
[14:32:06] thinking        Analyzing the code structure...
[14:32:07] tool_start      Edit                 filePath=/src/main.go
[14:32:08] file_change     /src/main.go         edit (+5, -2)
[14:32:08] tool_end        Edit                 success (1200ms)
```

### JSON Output

For programmatic access:

```bash
netclode events <session-id> --json
netclode events tail <session-id> --json  # One JSON object per line
```

## Available Event Kinds

| Kind | Description |
|------|-------------|
| `TOOL_START` | Tool invocation started |
| `TOOL_INPUT` | Streaming tool input delta |
| `TOOL_INPUT_COMPLETE` | Tool input finished |
| `TOOL_END` | Tool completed |
| `FILE_CHANGE` | File created/edited/deleted |
| `COMMAND_START` | Shell command started |
| `COMMAND_END` | Shell command completed |
| `THINKING` | Agent reasoning content |
| `PORT_EXPOSED` | Port exposed for preview |
| `REPO_CLONE` | Repository clone progress |
| `AGENT_DISCONNECTED` | Agent lost connection |
| `AGENT_RECONNECTED` | Agent reconnected |
