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

    var events: [AgentEvent] {
        eventStore.events(for: sessionId)
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
                        ForEach(Array(messages.enumerated()), id: \.element.id) { index, message in
                            let isLastAssistant = message.role == .assistant && index == messages.count - 1
                            ChatMessageRow(message: message, isStreaming: isLastAssistant && isProcessing)
                                .id(message.id)
                        }

                        // Show events inline (always visible)
                        if !events.isEmpty {
                            ForEach(events) { event in
                                InlineEventRow(event: event)
                            }
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
                .onChange(of: events.count) {
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
        case .toolInput: "streaming input..."
        case .toolEnd(let e): "\(e.tool) \(e.isSuccess ? "done" : "failed")"
        case .fileChange(let e): "\(e.action.displayName) \(e.fileName)"
        case .commandStart(let e): "Running: \(e.command.prefix(30))..."
        case .commandEnd(let e): "Exit \(e.exitCode)"
        case .thinking(let e): e.content.prefix(40) + "..."
        case .portDetected(let e): "Port \(e.port) detected"
        }
    }
}

// MARK: - Inline Event Row (shows event details inline in chat)

struct InlineEventRow: View {
    let event: AgentEvent

    // Skip tool_input events (too noisy)
    var shouldShow: Bool {
        if case .toolInput = event { return false }
        return true
    }

    var body: some View {
        if shouldShow {
            HStack(alignment: .top, spacing: Theme.Spacing.sm) {
                // Timeline line
                Rectangle()
                    .fill(Theme.Colors.gentleGray.opacity(0.3))
                    .frame(width: 2)
                    .frame(maxHeight: .infinity)
                    .padding(.leading, Theme.Spacing.md)

                // Event content
                VStack(alignment: .leading, spacing: Theme.Spacing.xs) {
                    // Header
                    HStack(spacing: Theme.Spacing.xs) {
                        Image(systemName: eventIcon)
                            .font(.system(size: 12))
                            .foregroundStyle(eventColor)

                        Text(eventLabel)
                            .font(.netclodeCaption)
                            .fontWeight(.medium)
                            .foregroundStyle(isError ? Theme.Colors.warmCoral : .secondary)

                        if isInProgress {
                            ProgressView()
                                .scaleEffect(0.6)
                        }

                        Spacer()
                    }

                    // Details
                    eventDetails
                }
                .padding(.vertical, Theme.Spacing.xs)
            }
        }
    }

    private var eventIcon: String {
        switch event {
        case .toolStart: "hourglass"
        case .toolInput: "ellipsis"
        case .toolEnd(let e): e.isSuccess ? "checkmark.circle.fill" : "xmark.circle.fill"
        case .fileChange: "doc.fill"
        case .commandStart: "play.fill"
        case .commandEnd(let e): e.isSuccess ? "checkmark.circle.fill" : "xmark.circle.fill"
        case .thinking: "brain.head.profile"
        case .portDetected: "network"
        }
    }

    private var eventLabel: String {
        switch event {
        case .toolStart(let e): "Using \(e.tool)"
        case .toolInput: "Streaming..."
        case .toolEnd(let e): "\(e.tool)\(e.isSuccess ? "" : " failed")"
        case .fileChange(let e): "\(e.action.displayName) \(e.fileName)"
        case .commandStart: "Running command"
        case .commandEnd(let e): "Command \(e.isSuccess ? "completed" : "failed")"
        case .thinking: "Thinking..."
        case .portDetected(let e): "Port \(e.port) opened"
        }
    }

    private var eventColor: Color {
        switch event {
        case .toolStart, .commandStart: Theme.Colors.gentleBlue
        case .toolInput: Theme.Colors.gentleBlue
        case .toolEnd(let e): e.isSuccess ? Theme.Colors.cozySage : Theme.Colors.warmCoral
        case .commandEnd(let e): e.isSuccess ? Theme.Colors.cozySage : Theme.Colors.warmCoral
        case .fileChange: Theme.Colors.cozyPurple
        case .thinking: Theme.Colors.cozyLavender
        case .portDetected: Theme.Colors.cozyTeal
        }
    }

    private var isInProgress: Bool {
        switch event {
        case .toolStart, .commandStart: true
        default: false
        }
    }

    private var isError: Bool {
        switch event {
        case .toolEnd(let e): !e.isSuccess
        case .commandEnd(let e): !e.isSuccess
        default: false
        }
    }

    @ViewBuilder
    private var eventDetails: some View {
        switch event {
        case .toolStart(let e):
            if !e.input.isEmpty {
                EventCodeBlock(title: "Input", content: formatInput(e.input))
            }

        case .toolEnd(let e):
            if let result = e.result, !result.isEmpty {
                EventCodeBlock(title: "Result", content: result)
            }
            if let error = e.error, !error.isEmpty {
                EventCodeBlock(title: "Error", content: error, isError: true)
            }

        case .commandStart(let e):
            EventCodeBlock(title: nil, content: "$ \(e.command)")
            if let cwd = e.cwd {
                Text("in \(cwd)")
                    .font(.netclodeCaption)
                    .foregroundStyle(.tertiary)
            }

        case .commandEnd(let e):
            HStack {
                Text("exit \(e.exitCode)")
                    .font(.netclodeMonospacedSmall)
                    .foregroundStyle(e.isSuccess ? Theme.Colors.cozySage : Theme.Colors.warmCoral)
            }
            if let output = e.output, !output.isEmpty {
                EventCodeBlock(title: "Output", content: output)
            }

        case .fileChange(let e):
            HStack(spacing: Theme.Spacing.xs) {
                Text(e.path)
                    .font(.netclodeMonospacedSmall)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)

                Spacer()

                if let added = e.linesAdded, added > 0 {
                    Text("+\(added)")
                        .font(.netclodeMonospacedSmall)
                        .foregroundStyle(Theme.Colors.cozySage)
                }
                if let removed = e.linesRemoved, removed > 0 {
                    Text("-\(removed)")
                        .font(.netclodeMonospacedSmall)
                        .foregroundStyle(Theme.Colors.warmCoral)
                }
            }

        case .thinking(let e):
            Text(e.content)
                .font(.netclodeCaption)
                .foregroundStyle(.secondary)
                .italic()

        case .portDetected(let e):
            if let process = e.process {
                Text(process)
                    .font(.netclodeCaption)
                    .foregroundStyle(.secondary)
            }
            if let url = e.previewUrl, let link = URL(string: url) {
                Link("Open preview", destination: link)
                    .font(.netclodeCaption)
            }

        case .toolInput:
            EmptyView()
        }
    }

    private func formatInput(_ input: [String: AnyCodableValue]) -> String {
        input.map { key, value in
            "\(key): \(formatValue(value))"
        }.joined(separator: "\n")
    }

    private func formatValue(_ value: AnyCodableValue) -> String {
        switch value {
        case .string(let s):
            return s.count > 200 ? String(s.prefix(200)) + "..." : s
        case .dictionary(let d):
            return "{\(d.map { "\($0): \($1)" }.joined(separator: ", "))}"
        case .array(let a):
            return "[\(a.map(\.description).joined(separator: ", "))]"
        default:
            return value.description
        }
    }
}

// MARK: - Event Code Block for event details

private struct EventCodeBlock: View {
    let title: String?
    let content: String
    var isError: Bool = false

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Spacing.xxs) {
            if let title {
                Text(title.uppercased())
                    .font(.system(size: 9, weight: .semibold))
                    .foregroundStyle(.tertiary)
                    .tracking(0.5)
            }

            ScrollView(.horizontal, showsIndicators: false) {
                Text(content)
                    .font(.netclodeMonospacedSmall)
                    .foregroundStyle(isError ? Theme.Colors.warmCoral : .secondary)
                    .lineLimit(6)
            }
            .padding(Theme.Spacing.xs)
            .background(Color.black.opacity(0.15))
            .clipShape(RoundedRectangle(cornerRadius: Theme.Radius.sm))
        }
        .frame(maxWidth: .infinity, alignment: .leading)
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
