# Terminal Access

Netclode provides full interactive terminal access to the sandbox environment, allowing you to run commands, inspect files, and interact with the workspace directly.

## Overview

Each session includes a pseudo-terminal (PTY) that runs inside the agent's sandbox. The terminal:

- Runs as the `agent` user with passwordless sudo
- Starts in `/agent/workspace` (your code directory)
- Uses `xterm-256color` for full color and escape sequence support
- Persists across app backgrounding (the PTY stays alive in the sandbox)

## Usage

### iOS App

1. Open a session
2. Tap the terminal icon in the bottom navigation
3. The terminal connects automatically when you focus it

The terminal supports:
- Touch keyboard input
- Copy/paste from the terminal
- Pinch-to-zoom (font size)
- Swipe gestures for common keys

### Technical Flow

```
iOS App ──► Control Plane ──► Agent ──► node-pty ──► bash
         (Connect stream)   (Connect stream)   (PTY)
```

Terminal I/O flows through bidirectional Connect streams. The control plane proxies between clients and the agent's PTY, allowing multiple clients to share the same terminal session.

## Shell Environment

The terminal runs bash with the following environment:

| Variable | Value | Description |
|----------|-------|-------------|
| `HOME` | `/agent` | Agent home directory |
| `SHELL` | `/bin/bash` | Default shell |
| `TERM` | `xterm-256color` | Terminal type |
| `PATH` | Includes mise shims | Tools installed via mise |

### Workspace Layout

```
/agent/                     # Home directory (persistent)
├── workspace/              # Your code (agent's cwd)
├── .local/share/mise/      # Installed tools
├── .cache/                 # Package caches
└── .claude/                # SDK session data

/opt/agent/                 # Agent code (read-only)
```

### Installing Tools

Use [mise](https://mise.jdx.dev/) to install language runtimes and tools:

```bash
mise use node@22
mise use python@3.12
mise use go@latest
```

Tools persist across pause/resume since they're stored in the JuiceFS volume.

### Using Docker

Docker is available in the sandbox:

```bash
docker run -v /agent/workspace:/app node:20 npm install
docker compose up -d
```

### Sudo Access

The agent user has passwordless sudo:

```bash
sudo apt update
sudo apt install -y htop
```

## PTY Lifecycle

### Spawning

The PTY is spawned lazily on first terminal interaction. This avoids idle shell processes when the terminal isn't used.

Specifically, the PTY is created when:
- The client sends a resize event (terminal dimensions)
- The client sends the first input character

### Persistence

The PTY survives:
- App backgrounding
- Client reconnection
- Multiple clients connecting to the same session

The PTY is destroyed when:
- The sandbox pod is deleted (session pause/delete)
- The shell process exits (`exit` command)

### After Pause/Resume

When you pause and resume a session:
1. The old PTY is destroyed (sandbox pod deleted)
2. A new PTY is created on first terminal interaction
3. Shell history may be available if `.bash_history` was written

## Protocol Details

Terminal communication uses the Connect protocol with these message types:

### Client to Server

| Message | Fields | Description |
|---------|--------|-------------|
| `terminal_input` | `session_id`, `data` | Send keystrokes to PTY |
| `terminal_resize` | `session_id`, `cols`, `rows` | Resize the PTY |

### Server to Client

| Message | Fields | Description |
|---------|--------|-------------|
| `terminal_output` | `session_id`, `data` | Output from PTY |

Terminal data is ephemeral and not persisted to Redis (unlike messages and events).

## Troubleshooting

### Terminal not connecting

1. Check the session is in `ready` or `running` status
2. Check control-plane logs for terminal proxy errors:
   ```bash
   kubectl --context netclode -n netclode logs -l app=control-plane | grep terminal
   ```

### No output after connecting

The PTY may not have spawned yet. Try:
1. Resize the terminal (triggers PTY creation)
2. Send a keystroke (Enter key)

### Commands hang or timeout

The sandbox may have network restrictions. Check:
1. Network policy allows the destination
2. DNS resolution works: `nslookup google.com`

### Shell exits immediately

Check if a previous command crashed the shell:
```bash
# From control-plane pod
kubectl --context netclode -n netclode exec -it <agent-pod> -- /bin/bash
```

## Keyboard Reference

Common escape sequences sent by the iOS keyboard:

| Action | Escape Sequence |
|--------|-----------------|
| Up arrow | `\x1b[A` |
| Down arrow | `\x1b[B` |
| Left arrow | `\x1b[D` |
| Right arrow | `\x1b[C` |
| Ctrl+C | `\x03` |
| Ctrl+D | `\x04` |
| Ctrl+Z | `\x1a` |
| Tab | `\x09` |
