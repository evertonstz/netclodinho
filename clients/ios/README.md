# Netclode iOS

Native iOS 26 app for Netclode. Built with SwiftUI and the Liquid Glass API.

## Features

- Session management (create, pause, resume, delete)
- Real-time chat with streaming responses
- Session history with rollback (restore workspace and chat to any previous turn)
- Terminal emulator via [SwiftTerm](https://github.com/migueldeicaza/SwiftTerm)
- Git changes view with inline unified diffs
- Connects over Tailscale
- Platform-adaptive navigation (sidebar on iPad/Mac, stack on iPhone)

## Requirements

- iOS 26.2+ / macOS
- Xcode 17.0+
- Swift 6.2+

## Building

```bash
open Netclode.xcodeproj
# ⌘R
```

## Testing

Run unit tests from Xcode (`⌘U`) or via command line:

```bash
# From repo root
make test-ios

# Or directly
cd clients/ios
xcodebuild test -scheme NetclodeTests -destination 'platform=macOS'
```

Tests cover:
- `EventStore.loadEvents()` - aggregates thinking events by `thinkingId`, merges `tool_input_complete` into `tool_start`

## Usage

1. Open the app
2. Settings → enter your server URL: `https://netclode-control-plane-ingress.YOUR-TAILNET.ts.net`
3. The app will connect automatically
4. Tap + to create a session

### Server URL

The iOS app requires **HTTPS** to enable HTTP/2, which is needed for bidirectional streaming.

The control plane is exposed via Tailscale Ingress with automatic Let's Encrypt certificates.
Your server URL will be: `https://netclode-control-plane-ingress.YOUR-TAILNET.ts.net`

To find your tailnet name, check the [Tailscale admin console](https://login.tailscale.com/admin/machines) or run `tailscale status`.

For local development with HTTP (no streaming), use: `http://localhost:3001`

## Architecture

```
Netclode/
├── App/                    # Entry point
├── Models/                 # Session, Messages, Events, ChatMessage
├── Services/               # ConnectService, MessageRouter
├── Stores/                 # @Observable state (Session, Chat, Event, Terminal, Settings)
├── Features/
│   ├── Sessions/           # Session list, sidebar, creation
│   ├── Workspace/          # Chat + Terminal tabs
│   ├── Chat/               # Chat UI
│   ├── Terminal/           # SwiftTerm wrapper
│   └── Settings/           # Server config
├── Components/             # GlassCard, GlassButton, GlassTextField
├── Design/                 # Theme, colors
├── Generated/              # Protobuf generated code
└── Extensions/
```

## Connect protocol

The app communicates with the control plane via Connect protocol (gRPC-compatible) using bidirectional streaming.

### HTTP Client

The app uses `NIOHTTPClient` (from [ConnectNIO](https://github.com/connectrpc/connect-swift)) instead of `URLSessionHTTPClient` for HTTP/2 connections.

**Why?** URLSession's HTTP/2 implementation has compatibility issues with Tailscale's iOS network extension. On physical iPhones, bidirectional streams would drop after ~10-15 seconds. Tailscale also [disables TCP keep-alives on iOS](https://github.com/tailscale/tailscale/blob/main/net/netknob/netknob.go) to save battery, which exacerbates the issue.

`NIOHTTPClient` uses Swift NIO's HTTP/2 implementation with POSIX sockets, bypassing URLSession entirely. This provides stable long-lived connections through Tailscale.

Client → Server:

```swift
// Messages sent via ConnectService
createSession(name: "My Project", repo: "owner/repo", repoAccess: .write, initialPrompt: nil)
openSession(id: "xxx", lastNotificationId: nil)
resumeSession(id: "xxx")
pauseSession(id: "xxx")
sendPrompt(sessionId: "xxx", text: "Fix the bug")
terminalInput(sessionId: "xxx", data: "ls\n")
```

Server → Client:

```swift
// Messages received and routed by MessageRouter
sessionList(sessions: [...])
sessionCreated(session: Session)
agentMessage(sessionId: "xxx", content: "...", partial: true)
agentEvent(sessionId: "xxx", event: AgentEvent)
terminalOutput(sessionId: "xxx", data: "...")
```

On reconnect, the app sends `lastNotificationId` to resume from where it left off.

## State management

Uses `@Observable` + SwiftUI Environment:

```swift
@Observable
class SessionStore {
    var sessions: [Session] = []
    var currentSessionId: String?
}

@Environment(SessionStore.self) private var sessionStore
```

## Platform-Adaptive Navigation

The app uses `NavigationSplitView` which adapts to different screen sizes:

| Platform | Navigation Style |
|----------|-----------------|
| iPhone | Stack navigation (push/pop) |
| iPad | Sidebar + detail split view |
| Mac (Catalyst) | Persistent sidebar + detail |

On iPhone, tapping a session pushes the workspace view onto the stack. On iPad and Mac, the sidebar remains visible while the detail area shows the selected session's workspace.

```swift
// ContentView.swift
if horizontalSizeClass == .compact {
    NavigationStack { SessionsView() }
} else {
    NavigationSplitView {
        SidebarView(selectedSessionId: $selectedSessionId)
    } detail: {
        WorkspaceView(sessionId: selectedSessionId)
    }
}
```

## Liquid Glass

The app uses iOS 26's glass effects:

```swift
.glassEffect(.regular, in: RoundedRectangle(cornerRadius: 16))
.glassEffect(.regular.interactive().tint(color), in: .capsule)
```

## Terminal

Terminal emulation uses [SwiftTerm](https://github.com/migueldeicaza/SwiftTerm). The app sends terminal input messages to the control plane, which proxies them to the agent's PTY. Output comes back via terminal output messages.

```
SwiftTerminalView ──► ConnectService ──► Control Plane ──► Agent PTY
```

`SwiftTermBridge.swift` adapts SwiftTerm's `LocalProcessTerminalView` delegate to work over the Connect stream instead of a local process.

## License

MIT
