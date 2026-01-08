import SwiftUI

// MARK: - Timeline Item

/// Represents an item in the unified chat timeline
enum TimelineItem: Identifiable {
    case message(ChatMessage, isStreaming: Bool, turnDuration: TimeInterval?)
    case event(GroupedEvent)

    var id: UUID {
        switch self {
        case .message(let msg, _, _): return msg.id
        case .event(let grouped): return grouped.id
        }
    }

    var timestamp: Date {
        switch self {
        case .message(let msg, _, _): return msg.timestamp
        case .event(let grouped): return grouped.timestamp
        }
    }
}

/// A grouped event combining start/end events
struct GroupedEvent: Identifiable {
    let id: UUID
    let event: AgentEvent
    let timestamp: Date
    var endEvent: AgentEvent?
}

// MARK: - Chat View

struct ChatView: View {
    let sessionId: String

    @Environment(ChatStore.self) private var chatStore
    @Environment(EventStore.self) private var eventStore
    @Environment(SessionStore.self) private var sessionStore
    @Environment(WebSocketService.self) private var webSocketService
    @Environment(SettingsStore.self) private var settingsStore

    @State private var inputText = ""
    @State private var showExposePortSheet = false
    @State private var portToExpose = ""
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

    /// Unified timeline combining messages and events, sorted by timestamp
    var timeline: [TimelineItem] {
        var items: [TimelineItem] = []

        // Add messages with turn duration calculation
        var precedingUserTimestamp: Date?
        for (index, message) in messages.enumerated() {
            let isLastAssistant = message.role == .assistant && index == messages.count - 1
            let isStreaming = isLastAssistant && isProcessing

            var turnDuration: TimeInterval? = nil

            if message.role == .user {
                precedingUserTimestamp = message.timestamp
            } else if message.role == .assistant, !isStreaming, let userTime = precedingUserTimestamp {
                // Find the end of this turn (must be after user message, before next user message)
                var turnEndTime: Date?

                if index < messages.count - 1 {
                    // Find next user message to bound this turn
                    let nextUserTime = messages[(index + 1)...].first { $0.role == .user }?.timestamp

                    if let nextUserTime {
                        // Find the last event within this turn window
                        turnEndTime = events.last { $0.timestamp > userTime && $0.timestamp < nextUserTime }?.timestamp
                    } else {
                        // No more user messages, use last event after user message
                        turnEndTime = events.last { $0.timestamp > userTime }?.timestamp
                    }
                } else {
                    // Last message - use last event after user message
                    turnEndTime = events.last { $0.timestamp > userTime }?.timestamp
                }

                // Fall back to the message's own timestamp if no events found
                let endTime = turnEndTime ?? message.timestamp
                turnDuration = endTime.timeIntervalSince(userTime)
            }

            items.append(.message(message, isStreaming: isStreaming, turnDuration: turnDuration))
        }

        // Add grouped events
        let groupedEvents = groupEvents(events)
        for grouped in groupedEvents {
            items.append(.event(grouped))
        }

        // Sort by timestamp
        return items.sorted { $0.timestamp < $1.timestamp }
    }

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(spacing: Theme.Spacing.md) {
                    ForEach(timeline) { item in
                        timelineItemView(item)
                            .id(item.id)
                    }

                    // Streaming indicator (shows at end when processing)
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
            .scrollDismissesKeyboard(.interactively)
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
        .safeAreaInset(edge: .bottom) {
            ChatInputBar(
                text: $inputText,
                isProcessing: isProcessing,
                isFocused: $isInputFocused,
                onSend: sendMessage,
                onInterrupt: interruptAgent
            )
        }
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Button {
                    showExposePortSheet = true
                } label: {
                    Image(systemName: "network")
                        .foregroundStyle(.cyan)
                }
            }
        }
        .sheet(isPresented: $showExposePortSheet) {
            ExposePortSheet(
                portText: $portToExpose,
                onExpose: { port in
                    webSocketService.send(.portExpose(sessionId: sessionId, port: port))
                    showExposePortSheet = false
                    portToExpose = ""
                }
            )
            .presentationDetents([.height(200)])
        }
    }

    @ViewBuilder
    private func timelineItemView(_ item: TimelineItem) -> some View {
        switch item {
        case .message(let message, let isStreaming, let turnDuration):
            ChatMessageRow(message: message, isStreaming: isStreaming, turnDuration: turnDuration)
        case .event(let grouped):
            groupedEventView(grouped)
        }
    }

    @ViewBuilder
    private func groupedEventView(_ grouped: GroupedEvent) -> some View {
        switch grouped.event {
        case .toolStart:
            let endEvent: ToolEndEvent? = {
                if case .toolEnd(let e) = grouped.endEvent {
                    return e
                }
                return nil
            }()
            ToolEventCard(event: grouped.event, endEvent: endEvent)

        case .commandStart(let start):
            let endEvent: CommandEndEvent? = {
                if case .commandEnd(let e) = grouped.endEvent {
                    return e
                }
                return nil
            }()
            CommandEventCard(startEvent: start, endEvent: endEvent)

        case .fileChange(let e):
            FileChangeCard(event: e)

        case .thinking(let e):
            ThinkingCard(event: e)

        case .portDetected(let e):
            PortDetectedCard(event: e)

        default:
            EmptyView()
        }
    }

    /// Groups related events (tool_start with tool_end, etc.)
    private func groupEvents(_ events: [AgentEvent]) -> [GroupedEvent] {
        var result: [GroupedEvent] = []
        var toolStartMap: [String: Int] = [:]

        for event in events {
            switch event {
            case .toolStart(let e):
                let grouped = GroupedEvent(id: e.id, event: event, timestamp: e.timestamp)
                toolStartMap[e.toolUseId] = result.count
                result.append(grouped)

            case .toolEnd(let e):
                if let index = toolStartMap[e.toolUseId] {
                    result[index].endEvent = event
                }

            case .toolInput:
                break // Skip tool input events

            case .commandStart(let e):
                result.append(GroupedEvent(id: e.id, event: event, timestamp: e.timestamp))

            case .commandEnd(let e):
                if let index = result.lastIndex(where: {
                    if case .commandStart(let start) = $0.event {
                        return start.command == e.command && $0.endEvent == nil
                    }
                    return false
                }) {
                    result[index].endEvent = event
                }

            case .fileChange(let e):
                result.append(GroupedEvent(id: e.id, event: event, timestamp: e.timestamp))

            case .thinking(let e):
                result.append(GroupedEvent(id: e.id, event: event, timestamp: e.timestamp))

            case .portDetected(let e):
                result.append(GroupedEvent(id: e.id, event: event, timestamp: e.timestamp))
            }
        }

        return result
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

// MARK: - Expose Port Sheet

struct ExposePortSheet: View {
    @Binding var portText: String
    let onExpose: (Int) -> Void

    @Environment(\.dismiss) private var dismiss

    private var portNumber: Int? {
        Int(portText)
    }

    private var isValidPort: Bool {
        guard let port = portNumber else { return false }
        return port > 0 && port <= 65535
    }

    var body: some View {
        NavigationStack {
            VStack(spacing: Theme.Spacing.lg) {
                Text("Expose a port to make it accessible via Tailscale")
                    .font(.netclodeCaption)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)

                TextField("Port number", text: $portText)
                    .keyboardType(.numberPad)
                    .textFieldStyle(.roundedBorder)
                    .frame(maxWidth: 200)

                Button {
                    if let port = portNumber, isValidPort {
                        onExpose(port)
                    }
                } label: {
                    Label("Expose Port", systemImage: "network")
                }
                .buttonStyle(.borderedProminent)
                .tint(.cyan)
                .disabled(!isValidPort)
            }
            .padding()
            .navigationTitle("Expose Port")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarLeading) {
                    Button("Cancel") {
                        dismiss()
                    }
                }
            }
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
