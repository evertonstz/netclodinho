import SwiftUI
import SwiftTerm

/// SwiftUI wrapper for SwiftTerm's TerminalView
struct SwiftTerminalView: UIViewRepresentable {
    let bridge: SwiftTermBridge
    @Environment(\.colorScheme) private var colorScheme
    
    func makeUIView(context: Context) -> SwiftTerm.TerminalView {
        let terminal = SwiftTerm.TerminalView(frame: .zero)
        
        // Configure terminal appearance
        configureAppearance(terminal)
        
        // Attach bridge
        bridge.attach(terminal)
        
        return terminal
    }
    
    func updateUIView(_ terminal: SwiftTerm.TerminalView, context: Context) {
        // Update colors when theme changes
        updateColors(terminal)
    }
    
    static func dismantleUIView(_ terminal: SwiftTerm.TerminalView, coordinator: ()) {
        // Bridge detachment is handled by TerminalStore
    }
    
    private func configureAppearance(_ terminal: SwiftTerm.TerminalView) {
        // Set font - use larger size for iPad and Mac Catalyst
        #if targetEnvironment(macCatalyst)
        let fontSize: CGFloat = 14
        #else
        let fontSize: CGFloat = UIDevice.current.userInterfaceIdiom == .pad ? 14 : 12
        #endif
        terminal.font = UIFont.monospacedSystemFont(ofSize: fontSize, weight: .regular)
        
        // Set colors based on current theme
        updateColors(terminal)
        
        // Enable blinking cursor
        terminal.getTerminal().setCursorStyle(.blinkBlock)
    }
    
    private func updateColors(_ terminal: SwiftTerm.TerminalView) {
        if colorScheme == .dark {
            // Dark theme colors
            terminal.nativeForegroundColor = UIColor.white
            terminal.nativeBackgroundColor = UIColor(red: 0.1, green: 0.1, blue: 0.12, alpha: 1.0)
            terminal.selectedTextBackgroundColor = UIColor(red: 0.3, green: 0.4, blue: 0.6, alpha: 0.5)
        } else {
            // Light theme colors
            terminal.nativeForegroundColor = UIColor(red: 0.15, green: 0.15, blue: 0.15, alpha: 1.0)
            terminal.nativeBackgroundColor = UIColor(red: 0.98, green: 0.98, blue: 0.98, alpha: 1.0)
            terminal.selectedTextBackgroundColor = UIColor(red: 0.6, green: 0.7, blue: 0.9, alpha: 0.5)
        }
    }
}

// MARK: - Preview

#Preview {
    let bridge = SwiftTermBridge(sessionId: "preview", webSocketService: nil)
    
    // Feed some sample output with ANSI colors
    bridge.feedData("""
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
    
    return SwiftTerminalView(bridge: bridge)
        .frame(height: 400)
}
