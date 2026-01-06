import SwiftUI

struct TerminalView: View {
    let sessionId: String

    @Environment(TerminalStore.self) private var terminalStore
    @Environment(WebSocketService.self) private var webSocketService
    @Environment(SettingsStore.self) private var settingsStore

    @State private var inputText = ""
    @FocusState private var isInputFocused: Bool

    var output: String {
        terminalStore.output(for: sessionId)
    }

    var body: some View {
        VStack(spacing: 0) {
            // Terminal output
            ScrollViewReader { proxy in
                ScrollView {
                    VStack(alignment: .leading, spacing: 0) {
                        TerminalRenderer(output: output)
                            .id("output")

                        // Scroll anchor
                        Color.clear
                            .frame(height: 1)
                            .id("bottom")
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(Theme.Spacing.sm)
                }
                .background(Theme.Colors.softCharcoal)
                .onChange(of: output) {
                    withAnimation {
                        proxy.scrollTo("bottom", anchor: .bottom)
                    }
                }
            }

            // Input bar
            TerminalInputBar(
                text: $inputText,
                isFocused: $isInputFocused,
                onSubmit: sendInput
            )
        }
        .onAppear {
            // Notify server of terminal size
            // Using standard terminal dimensions
            webSocketService.send(.terminalResize(sessionId: sessionId, cols: 80, rows: 24))
        }
    }

    private func sendInput() {
        guard !inputText.isEmpty else { return }

        if settingsStore.hapticFeedbackEnabled {
            HapticFeedback.light()
        }

        // Send input with newline
        webSocketService.send(.terminalInput(sessionId: sessionId, data: inputText + "\n"))
        inputText = ""
    }
}

// MARK: - Terminal Renderer

struct TerminalRenderer: View {
    let output: String

    var body: some View {
        Text(parseANSI(output))
            .font(.netclodeMonospaced)
            .foregroundStyle(.white)
            .textSelection(.enabled)
    }

    private func parseANSI(_ text: String) -> AttributedString {
        // Simple ANSI parsing - strip escape codes for basic display
        // A full implementation would handle colors, styles, cursor movement
        let stripped = text.replacingOccurrences(
            of: "\\x1B\\[[0-9;]*[a-zA-Z]",
            with: "",
            options: .regularExpression
        )

        return AttributedString(stripped)
    }
}

// MARK: - Terminal Input Bar

struct TerminalInputBar: View {
    @Binding var text: String
    var isFocused: FocusState<Bool>.Binding
    let onSubmit: () -> Void

    var body: some View {
        HStack(spacing: Theme.Spacing.sm) {
            // Prompt indicator
            Text("$")
                .font(.netclodeMonospaced)
                .foregroundStyle(Theme.Colors.cozySage)

            // Text field
            TextField("Enter command...", text: $text)
                .font(.netclodeMonospaced)
                .textFieldStyle(.plain)
                .focused(isFocused)
                .onSubmit(onSubmit)
                .submitLabel(.send)

            // Send button
            Button {
                onSubmit()
            } label: {
                Image(systemName: "return")
                    .font(.system(size: 14, weight: .medium))
                    .foregroundStyle(text.isEmpty ? Color.gray.opacity(0.5) : Theme.Colors.cozySage)
            }
            .disabled(text.isEmpty)
        }
        .padding(.horizontal, Theme.Spacing.md)
        .padding(.vertical, Theme.Spacing.sm)
        .background(Theme.Colors.softCharcoal.opacity(0.95))
    }
}

// MARK: - Preview

#Preview {
    let store = TerminalStore()
    store.appendOutput(sessionId: "test", data: """
    $ ls -la
    total 48
    drwxr-xr-x  12 user  staff   384 Jan  6 10:30 .
    drwxr-xr-x   5 user  staff   160 Jan  5 14:20 ..
    -rw-r--r--   1 user  staff  1234 Jan  6 10:30 README.md
    -rw-r--r--   1 user  staff  5678 Jan  6 10:25 package.json
    drwxr-xr-x   8 user  staff   256 Jan  6 10:30 src

    $ npm run build
    Building project...
    ✓ Compiled successfully in 2.3s

    $\u{0020}
    """)

    return NavigationStack {
        TerminalView(sessionId: "test")
    }
    .environment(store)
    .environment(WebSocketService())
    .environment(SettingsStore())
}
