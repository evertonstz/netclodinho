import Foundation

@Observable
final class MessageRouter: @unchecked Sendable {
    private let webSocketService: WebSocketService
    private let sessionStore: SessionStore
    private let chatStore: ChatStore
    private let eventStore: EventStore
    private let terminalStore: TerminalStore

    private var routingTask: Task<Void, Never>?

    init(
        webSocketService: WebSocketService,
        sessionStore: SessionStore,
        chatStore: ChatStore,
        eventStore: EventStore,
        terminalStore: TerminalStore
    ) {
        self.webSocketService = webSocketService
        self.sessionStore = sessionStore
        self.chatStore = chatStore
        self.eventStore = eventStore
        self.terminalStore = terminalStore

        startRouting()
    }

    private func startRouting() {
        routingTask = Task { [weak self] in
            guard let self else { return }

            for await message in webSocketService.messages {
                await MainActor.run {
                    self.route(message)
                }
            }
        }
    }

    @MainActor
    private func route(_ message: ServerMessage) {
        switch message {
        // Session messages
        case .sessionCreated(let session):
            sessionStore.addSession(session)

        case .sessionUpdated(let session):
            sessionStore.updateSession(session)

        case .sessionDeleted(let id):
            sessionStore.removeSession(id: id)
            chatStore.clearMessages(for: id)
            eventStore.clearEvents(for: id)
            terminalStore.clearOutput(for: id)

        case .sessionList(let sessions):
            sessionStore.setSessions(sessions)

        case .sessionError(let id, let error):
            print("Session error \(id ?? "unknown"): \(error)")
            if let id {
                sessionStore.setError(for: id, error: error)
            }

        // Agent messages
        case .agentMessage(let sessionId, let content, let partial):
            if partial {
                chatStore.appendAssistantPartial(sessionId: sessionId, delta: content)
            } else {
                chatStore.appendMessage(
                    sessionId: sessionId,
                    message: ChatMessage(role: .assistant, content: content)
                )
            }
            sessionStore.setProcessing(for: sessionId, processing: true)

        case .agentEvent(let sessionId, let event):
            eventStore.appendEvent(sessionId: sessionId, event: event)

        case .agentDone(let sessionId):
            sessionStore.setProcessing(for: sessionId, processing: false)
            chatStore.finalizeLastMessage(sessionId: sessionId)

        case .agentError(let sessionId, let error):
            sessionStore.setProcessing(for: sessionId, processing: false)
            chatStore.appendMessage(
                sessionId: sessionId,
                message: ChatMessage(role: .assistant, content: "Error: \(error)")
            )

        case .userMessage(let sessionId, let content):
            // User message from another client - add if not duplicate
            let messages = chatStore.messages(for: sessionId)
            if let lastMessage = messages.last,
               lastMessage.role == .user && lastMessage.content == content {
                // Skip duplicate (message was sent by this client)
                break
            }
            chatStore.appendMessage(
                sessionId: sessionId,
                message: ChatMessage(role: .user, content: content)
            )

        // Terminal messages
        case .terminalOutput(let sessionId, let data):
            terminalStore.appendOutput(sessionId: sessionId, data: data)

        // Port exposure responses (event comes via agent.event, this is just confirmation)
        case .portExposed(let sessionId, let port, let previewUrl):
            print("[MessageRouter] Port \(port) exposed for session \(sessionId): \(previewUrl)")

        case .portError(let sessionId, let port, let error):
            print("[MessageRouter] Failed to expose port \(port) for session \(sessionId): \(error)")

        // General errors
        case .error(let message):
            print("Server error: \(message)")

        // Sync responses
        case .syncResponse(let sessions, _):
            // Update sessions from server sync
            sessionStore.setSessions(sessions.map { $0.toSession() })

        case .sessionState(let session, let messages, let events, _, let lastNotificationId):
            // Load session history from server
            print("[MessageRouter] session.state received: \(messages.count) messages, \(events.count) events for session \(session.id)")
            sessionStore.updateSession(session)
            chatStore.loadMessages(sessionId: session.id, messages: messages)
            eventStore.loadEvents(sessionId: session.id, events: events)
            // Store the notification cursor for reconnection
            if let notificationId = lastNotificationId {
                sessionStore.setLastNotificationId(for: session.id, notificationId: notificationId)
            }
            print("[MessageRouter] Loaded messages and events for session \(session.id)")
        }
    }

    func stop() {
        routingTask?.cancel()
    }

    static var preview: MessageRouter {
        MessageRouter(
            webSocketService: WebSocketService(),
            sessionStore: SessionStore(),
            chatStore: ChatStore(),
            eventStore: EventStore(),
            terminalStore: TerminalStore()
        )
    }
}
