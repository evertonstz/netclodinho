import SwiftUI

struct TerminalView: View {
    let sessionId: String

    @Environment(TerminalStore.self) private var terminalStore
    @Environment(ConnectService.self) private var connectService

    @Environment(\.colorScheme) private var colorScheme
    
    private var terminalBackgroundColor: Color {
        colorScheme == .dark
            ? Color(red: 0.1, green: 0.1, blue: 0.12)
            : Color(red: 0.98, green: 0.98, blue: 0.98)
    }
    
    var body: some View {
        SwiftTerminalView(bridge: terminalStore.bridge(for: sessionId))
            .id(sessionId)  // Force recreation when session changes
            #if targetEnvironment(macCatalyst)
            .padding(.leading, 8)
            #endif
            .ignoresSafeArea(.keyboard)
            .background(terminalBackgroundColor)
            .focusEffectDisabled()
            .onAppear {
                // Send initial terminal size to trigger PTY spawn
                let bridge = terminalStore.bridge(for: sessionId)
                if bridge.cols > 0 && bridge.rows > 0 {
                    connectService.send(.terminalResize(
                        sessionId: sessionId,
                        cols: bridge.cols,
                        rows: bridge.rows
                    ))
                }
            }
    }
}

// MARK: - Preview

#Preview {
    let terminalStore = TerminalStore()
    let connectService = ConnectService()
    terminalStore.connectService = connectService
    
    // Add sample output with ANSI colors
    terminalStore.appendOutput(sessionId: "test", data: """
    \u{1B}[32m$\u{1B}[0m ls -la
    total 48
    drwxr-xr-x  12 user  staff   384 Jan  6 10:30 \u{1B}[34m.\u{1B}[0m
    drwxr-xr-x   5 user  staff   160 Jan  5 14:20 \u{1B}[34m..\u{1B}[0m
    -rw-r--r--   1 user  staff  1234 Jan  6 10:30 README.md
    -rw-r--r--   1 user  staff  5678 Jan  6 10:25 \u{1B}[33mpackage.json\u{1B}[0m
    drwxr-xr-x   8 user  staff   256 Jan  6 10:30 \u{1B}[34msrc\u{1B}[0m

    \u{1B}[32m$\u{1B}[0m npm run build
    Building project...
    \u{1B}[32m✓\u{1B}[0m Compiled successfully in 2.3s

    \u{1B}[32m$\u{1B}[0m\u{0020}
    """)

    return NavigationStack {
        TerminalView(sessionId: "test")
    }
    .environment(terminalStore)
    .environment(connectService)
}
