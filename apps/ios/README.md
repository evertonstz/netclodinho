# Netclode iOS

A beautiful iOS 26 app for Netclode - your self-hosted Claude Code Cloud platform.

## Features

- **Liquid Glass Design** - Built with Apple's iOS 26 Liquid Glass design system
- **Session Management** - Create, pause, resume, and delete coding sessions
- **Real-time Chat** - Stream responses from Claude with live updates
- **Terminal Emulator** - Full terminal access to your sandbox
- **Events Timeline** - Track agent activity (tool use, file changes, commands)
- **Configurable Server** - Connect to any Netclode instance on your Tailscale network

## Requirements

- iOS 26.2+
- Xcode 17.0+
- Swift 6.2+

## Architecture

```
Netclode/
├── App/                    # App entry point
├── Models/                 # Data models (Session, Messages, Events)
├── Services/               # WebSocket connection & message routing
├── Stores/                 # State management (@Observable)
├── Features/               # Feature modules
│   ├── Sessions/           # Session list & creation
│   ├── Workspace/          # Workspace container
│   ├── Chat/               # Chat interface
│   ├── Terminal/           # Terminal emulator
│   ├── Events/             # Events timeline
│   └── Settings/           # App settings
├── Components/             # Reusable UI components
├── Design/                 # Theme, colors, animations
└── Extensions/             # Swift extensions
```

## Design System

### Liquid Glass

The app uses iOS 26's Liquid Glass API throughout:

```swift
// Glass card
.glassEffect(.regular, in: RoundedRectangle(cornerRadius: 16))

// Interactive glass (buttons, inputs)
.glassEffect(.regular.interactive().tint(color), in: .capsule)

// Morphing glass groups
GlassEffectContainer(spacing: 12) { ... }
```

### Color Palette

Warm, cozy colors that complement the glass effects:

- **Warm Cream** - Base background
- **Warm Apricot** - Primary accent
- **Cozy Purple** - Interactive elements
- **Cozy Lavender** - Thinking/processing states
- **Gentle Blue** - User messages
- **Cozy Sage** - Success states

## WebSocket Protocol

The app communicates with the Netclode control plane via WebSocket:

### Client → Server

```swift
ClientMessage.sessionCreate(name: "My Project", repo: nil)
ClientMessage.sessionList
ClientMessage.sessionResume(id: "session-id")
ClientMessage.prompt(sessionId: "id", text: "Fix the bug")
ClientMessage.terminalInput(sessionId: "id", data: "ls -la\n")
```

### Server → Client

```swift
ServerMessage.sessionCreated(session: Session)
ServerMessage.agentMessage(sessionId: "id", content: "...", partial: true)
ServerMessage.agentEvent(sessionId: "id", event: AgentEvent)
ServerMessage.terminalOutput(sessionId: "id", data: "...")
```

## Building

1. Open `Netclode.xcodeproj` in Xcode 17+
2. Select your target device (iOS 26 Simulator or device)
3. Build and run (⌘R)

## Configuration

1. Launch the app
2. Go to Settings tab
3. Enter your Netclode server URL (e.g., `netclode.your-tailnet.ts.net`)
4. Tap Connect

## License

MIT
