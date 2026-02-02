# Terminal

Full interactive shell access to the sandbox.

## Overview

Each session has a PTY running inside the sandbox:

- Runs as `agent` user with passwordless sudo
- Starts in `/agent/workspace`
- Full color support (xterm-256color)
- Persists across app backgrounding

## Usage

**iOS App:** Tap terminal icon in bottom nav. Supports touch keyboard, copy/paste, pinch-to-zoom.

Terminal I/O flows through Connect streams: iOS App → Control Plane → Agent → node-pty → bash

## Environment

- `HOME=/agent`
- `SHELL=/bin/bash`
- `TERM=xterm-256color`
- `PATH` includes mise shims

```
/agent/                     # Home (persistent)
├── workspace/              # Your code
├── .local/share/mise/      # Installed tools
├── .cache/                 # Package caches
└── .claude/                # SDK session data
```

### Tools

```bash
mise use node@22            # Install runtimes via mise
docker compose up -d        # Docker available
sudo apt install htop       # Passwordless sudo
```

Tools persist across pause/resume (stored on JuiceFS).

## PTY Lifecycle

PTY spawns lazily on first terminal interaction. Survives app backgrounding and reconnection.

Destroyed on session pause/delete or shell exit. After resume, new PTY created on first interaction (shell history may be available from `.bash_history`).

## Troubleshooting

**Terminal not connecting** - check session is ready/running, check control-plane logs:
```bash
kubectl --context netclode -n netclode logs -l app=control-plane | grep terminal
```

**No output after connecting** - try sending a keystroke (triggers PTY creation).

**Commands hang** - check network policy and DNS (`nslookup google.com`).
