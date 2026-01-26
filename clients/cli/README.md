# Netclode CLI

A command-line interface for debugging and inspecting Netclode sessions.

## Installation

### From source

```bash
cd clients/cli
make build

# Or install to $GOPATH/bin
make install
```

### Binary

The built binary will be at `./netclode`.

## Configuration

The CLI connects to the Netclode control-plane via the Connect protocol.

### Server URL

Configure the control-plane URL using one of these methods (in order of precedence):

1. **Flag**: `--url https://your-server.example.com`
2. **Environment variable**: `NETCLODE_URL=https://your-server.example.com`
3. **Default**: `http://localhost:3000`

For production, set `NETCLODE_URL` to your Tailscale ingress URL:
```bash
export NETCLODE_URL=https://netclode-control-plane-ingress.<your-tailnet>.ts.net
```

## Usage

### List sessions

```bash
# Table format
netclode sessions list

# JSON format
netclode sessions list --json
```

Output:
```
ID            NAME                            STATUS  REPO                  MSGS  CREATED  ACTIVE
9f7c8e64-c84  Haptic Feedback App Impleme...  ready   angristan/netclode    2     10h ago  10h ago
05965814-225  Protocol Review Request         ready   angristan/netclode    2     10h ago  10h ago
```

### Get session details

```bash
netclode sessions get <session-id>
```

Output:
```
Session Details
----------------------------------------
ID:             9f7c8e64-c84
Name:           Haptic Feedback App Implementation Analysis
Status:         ready
Repo:           angristan/netclode
Created:        23:28:43 (10h ago)
Last Active:    23:28:43 (10h ago)

Statistics
----------------------------------------
Messages:       2
Events:         107
```

### Create a session

```bash
# Basic creation
netclode sessions create --repo owner/repo --name "My Session"

# With SDK type (claude, opencode, copilot)
netclode sessions create --repo owner/repo --sdk opencode --model anthropic/claude-sonnet-4-0
```

### Pause a session

Pauses a session, stopping the agent container but preserving the workspace data.

```bash
netclode sessions pause <session-id>
```

### Resume a session

Resumes a paused session, restoring the agent with the preserved workspace.

```bash
netclode sessions resume <session-id>
```

### Delete a session

```bash
netclode sessions delete <session-id>
```

### View messages (chat history)

```bash
# All messages
netclode messages <session-id>

# Last N messages
netclode messages <session-id> -n 10

# Filter by role
netclode messages <session-id> --role user
netclode messages <session-id> --role assistant
```

Output:
```
[user] 23:28:46
  How is haptic feedback used in the app?

[assistant] 23:29:15
  The workspace directory is currently empty...
```

### View events

```bash
# Recent events (default: 50)
netclode events <session-id>

# Last N events
netclode events <session-id> -n 100

# Filter by event kind
netclode events <session-id> --kind tool_start
netclode events <session-id> --kind file_change
```

Output:
```
TIME      KIND        TOOL/PATH  DETAILS
23:28:56  tool_start  Task       
23:29:01  tool_start  Bash       command=find /agent/workspace -type f...
23:29:04  tool_start  Bash       command=ls -la /agent/workspace
```

Available event kinds:
- `tool_start`, `tool_end`, `tool_input`, `tool_input_complete`
- `file_change`
- `command_start`, `command_end`
- `thinking`
- `port_exposed`
- `repo_clone`

### Stream events in real-time

```bash
netclode events tail <session-id>
```

This keeps the connection open and prints events as they occur. Press `Ctrl+C` to stop.

Output:
```
Tailing events for session 9f7c8e64-c84 (Ctrl+C to stop)...

[14:32:05] tool_start      Read                 filePath=/src/main.go
[14:32:05] tool_end        Read                 success
[14:32:06] thinking        Analyzing the code structure...
```

## Global Flags

| Flag | Description |
|------|-------------|
| `--url` | Control-plane URL (overrides `NETCLODE_URL` env var) |
| `--json` | Output in JSON format |
| `-h, --help` | Show help |

## JSON Output

All commands support `--json` for machine-readable output:

```bash
netclode sessions list --json
netclode sessions get <id> --json
netclode messages <id> --json
netclode events <id> --json
netclode events tail <id> --json  # One JSON object per line
```

## Development

### Prerequisites

- Go 1.25+
- Access to a running Netclode control-plane

### Building

```bash
make build      # Build for current platform
make build-all  # Build for multiple platforms
make clean      # Remove built binaries
```

### Testing

```bash
go test ./...
```

### Project Structure

```
clients/cli/
├── main.go                    # Entry point
├── go.mod                     # Module definition
├── Makefile                   # Build targets
├── cmd/
│   ├── root.go               # Root command, global flags
│   ├── sessions.go           # sessions list/get/delete
│   ├── messages.go           # messages command
│   └── events.go             # events + events tail
└── internal/
    ├── client/
    │   ├── client.go         # Connect protocol client
    │   └── client_test.go    # Client tests
    └── output/
        ├── format.go         # Output formatting
        └── format_test.go    # Formatting tests
```

## Architecture

The CLI uses the [Connect protocol](https://connectrpc.com/) to communicate with the Netclode control-plane. It establishes a bidirectional stream and sends/receives protobuf messages.

The client wrapper (`internal/client/`) provides a simple Go API:
- `CreateSession(opts)` - Create a new session
- `ListSessions()` - List all sessions
- `SyncSessions()` - List sessions with metadata
- `GetSession(id)` - Get session with messages and events
- `PauseSession(id)` - Pause a session (preserves workspace)
- `ResumeSession(id)` - Resume a paused session
- `DeleteSession(id)` - Delete a session
- `TailEvents(id, handler)` - Stream events in real-time
