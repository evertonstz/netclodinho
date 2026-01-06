import SwiftUI

struct ChatView: View {
    let sessionId: String

    @Environment(ChatStore.self) private var chatStore
    @Environment(EventStore.self) private var eventStore
    @Environment(SessionStore.self) private var sessionStore
    @Environment(WebSocketService.self) private var webSocketService
    @Environment(SettingsStore.self) private var settingsStore

    @State private var inputText = ""
    @FocusState private var isInputFocused: Bool

    var messages: [ChatMessage] {
        chatStore.messages(for: sessionId)
    }

    var recentEvents: [AgentEvent] {
        eventStore.recentEvents(for: sessionId, count: 3)
    }

    var isProcessing: Bool {
        sessionStore.isProcessing(sessionId)
    }

    var body: some View {
        VStack(spacing: 0) {
            // Messages scroll view
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(spacing: Theme.Spacing.md) {
                        ForEach(messages) { message in
                            ChatMessageRow(message: message)
                                .id(message.id)
                        }

                        // Show recent events during processing
                        if isProcessing && !recentEvents.isEmpty {
                            EventsPreview(events: recentEvents)
                                .transition(.slideUp)
                        }

                        // Streaming indicator
                        if isProcessing {
                            StreamingIndicator()
                                .id("streaming")
                        }

                        // Scroll anchor
                        Color.clear
                            .frame(height: 1)
                            .id("bottom")
                    }
                    .padding()
                }
                .onChange(of: messages.count) {
                    withAnimation(.glassSpring) {
                        proxy.scrollTo("bottom", anchor: .bottom)
                    }
                }
                .onChange(of: isProcessing) {
                    withAnimation(.glassSpring) {
                        proxy.scrollTo("bottom", anchor: .bottom)
                    }
                }
            }

            // Input bar
            ChatInputBar(
                text: $inputText,
                isProcessing: isProcessing,
                isFocused: $isInputFocused,
                onSend: sendMessage,
                onInterrupt: interruptAgent
            )
        }
    }

    private func sendMessage() {
        let text = inputText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return }

        if settingsStore.hapticFeedbackEnabled {
            HapticFeedback.light()
        }

        // Add user message locally
        chatStore.appendMessage(
            sessionId: sessionId,
            message: ChatMessage(role: .user, content: text)
        )

        // Send to server
        webSocketService.send(.prompt(sessionId: sessionId, text: text))

        // Clear input
        inputText = ""
    }

    private func interruptAgent() {
        if settingsStore.hapticFeedbackEnabled {
            HapticFeedback.warning()
        }
        webSocketService.send(.promptInterrupt(sessionId: sessionId))
    }
}

// MARK: - Events Preview (shown during processing)

struct EventsPreview: View {
    let events: [AgentEvent]

    var body: some View {
        GlassCard(tint: Theme.Colors.cozyLavender.opacity(0.15), padding: Theme.Spacing.sm) {
            VStack(alignment: .leading, spacing: Theme.Spacing.xs) {
                HStack {
                    Image(systemName: "bolt.fill")
                        .foregroundStyle(Theme.Colors.cozyPurple)
                    Text("Activity")
                        .font(.netclodeCaption)
                        .foregroundStyle(.secondary)
                }

                ForEach(events) { event in
                    HStack(spacing: Theme.Spacing.xs) {
                        Image(systemName: event.kind.systemImage)
                            .font(.system(size: 10))
                            .foregroundStyle(.tertiary)

                        Text(eventDescription(event))
                            .font(.netclodeCaption)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                    }
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func eventDescription(_ event: AgentEvent) -> String {
        switch event {
        case .toolStart(let e): "Using \(e.tool)..."
        case .toolEnd(let e): "\(e.tool) \(e.isSuccess ? "done" : "failed")"
        case .fileChange(let e): "\(e.action.displayName) \(e.fileName)"
        case .commandStart(let e): "Running: \(e.command.prefix(30))..."
        case .commandEnd(let e): "Exit \(e.exitCode)"
        case .thinking(let e): e.content.prefix(40) + "..."
        case .portDetected(let e): "Port \(e.port) detected"
        }
    }
}

// MARK: - Preview

#Preview {
    let chatStore = ChatStore()
    chatStore.appendMessage(sessionId: "test", message: ChatMessage.previewUser)
    chatStore.appendMessage(sessionId: "test", message: ChatMessage.previewAssistant)

    return NavigationStack {
        ChatView(sessionId: "test")
    }
    .environment(chatStore)
    .environment(EventStore())
    .environment(SessionStore())
    .environment(SettingsStore())
    .environment(WebSocketService())
}
