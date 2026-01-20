import Foundation
import SwiftUI

@MainActor
@Observable
final class MessageRouter {
    private let webSocketService: WebSocketService
    private let sessionStore: SessionStore
    private let chatStore: ChatStore
    private let eventStore: EventStore
    private let terminalStore: TerminalStore
    private let githubStore: GitHubStore

    private var routingTask: Task<Void, Never>?

    init(
        webSocketService: WebSocketService,
        sessionStore: SessionStore,
        chatStore: ChatStore,
        eventStore: EventStore,
        terminalStore: TerminalStore,
        githubStore: GitHubStore
    ) {
        self.webSocketService = webSocketService
        self.sessionStore = sessionStore
        self.chatStore = chatStore
        self.eventStore = eventStore
        self.terminalStore = terminalStore
        self.githubStore = githubStore

        startRouting()
    }

    private func startRouting() {
        routingTask = Task { [weak self] in
            guard let self else { return }

            for await message in webSocketService.messages {
                self.route(message)
            }
        }
    }

    /// Routes a server message to the appropriate store.
    /// Internal for testing - allows tests to call this directly with mock messages.
    func route(_ message: ServerMessage) {
        switch message {
        // Session messages
        case .sessionCreated(let session):
            print("[MessageRouter] session.created received: id=\(session.id), pendingPromptText=\(sessionStore.pendingPromptText ?? "nil")")
            sessionStore.addSession(session)

            // If there's a pending prompt, set up navigation
            // Note: The prompt itself is sent via initialPrompt in session.create,
            // and the backend will broadcast user.message which we handle separately
            if sessionStore.pendingPromptText != nil {
                sessionStore.pendingSessionId = session.id
                print("[MessageRouter] Session created with initial prompt, navigating to session \(session.id)")
                sessionStore.pendingPromptText = nil
            }

        case .sessionUpdated(let session):
            print("[MessageRouter] session.updated received: id=\(session.id), name=\(session.name), status=\(session.status)")
            sessionStore.updateSession(session)

        case .sessionDeleted(let id):
            withAnimation {
                sessionStore.removeSession(id: id)
            }
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
            print("[MessageRouter] agentMessage received: partial=\(partial), contentLength=\(content.count), preview=\"\(String(content.prefix(50)))\"")
            if partial {
                chatStore.appendAssistantPartial(sessionId: sessionId, delta: content)
            } else {
                // Final message - only add if we don't already have an assistant message being built
                // (the content was already accumulated via partials)
                let existingMessages = chatStore.messages(for: sessionId)
                let hasStreamingAssistant = existingMessages.last?.role == .assistant
                if !hasStreamingAssistant {
                    // No streaming in progress - this is a complete message (no partials were sent)
                    chatStore.appendMessage(
                        sessionId: sessionId,
                        message: ChatMessage(role: .assistant, content: content)
                    )
                }
                // If hasStreamingAssistant, the content is already there from partials - nothing to do
            }

        case .agentEvent(let sessionId, let event):
            // Handle thinking events specially for streaming
            if case .thinking(let thinkingEvent) = event {
                if thinkingEvent.partial {
                    // Streaming thinking - accumulate content
                    eventStore.appendThinkingPartial(
                        sessionId: sessionId,
                        thinkingId: thinkingEvent.thinkingId,
                        content: thinkingEvent.content,
                        timestamp: thinkingEvent.timestamp
                    )
                } else {
                    // Complete thinking block - finalize existing or add new if no partials were sent
                    let existingEvents = eventStore.events(for: sessionId)
                    let hasStreamingThinking = existingEvents.contains { e in
                        if case .thinking(let t) = e, t.thinkingId == thinkingEvent.thinkingId {
                            return true
                        }
                        return false
                    }
                    if hasStreamingThinking {
                        // Finalize the existing thinking event
                        eventStore.finalizeThinking(sessionId: sessionId, thinkingId: thinkingEvent.thinkingId)
                    } else {
                        // No partials were sent - add as complete event
                        eventStore.appendEvent(sessionId: sessionId, event: event)
                    }
                }
            } else if case .toolInputComplete(let inputEvent) = event {
                // Update existing tool_start event with full input
                eventStore.updateToolInput(
                    sessionId: sessionId,
                    toolUseId: inputEvent.toolUseId,
                    input: inputEvent.input
                )
            } else {
                eventStore.appendEvent(sessionId: sessionId, event: event)
            }

        case .agentDone(let sessionId):
            sessionStore.setProcessing(for: sessionId, processing: false)
            chatStore.finalizeLastMessage(sessionId: sessionId)
            // Clear pending state - agent has responded
            if sessionStore.pendingSessionId == sessionId {
                sessionStore.pendingPromptText = nil
                sessionStore.pendingSessionId = nil
            }

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
            // Notify GitHubStore in case it's waiting for a response
            if githubStore.isLoading {
                githubStore.handleError(message)
            }

        // Sync responses
        case .syncResponse(let sessions, _):
            // Update sessions from server sync
            sessionStore.setSessions(sessions.map { $0.toSession() })

        case .sessionState(let session, let messages, let events, _, let lastNotificationId):
            // Load session history from server
            let currentSession = sessionStore.sessions.first { $0.id == session.id }
            print("[MessageRouter] session.state received: status=\(session.status) (was \(currentSession?.status.rawValue ?? "nil")), \(messages.count) messages, \(events.count) events")
            sessionStore.updateSession(session)
            
            // Only load server messages if we don't already have local messages
            // (to avoid overwriting locally added user message from initial prompt)
            let existingMessages = chatStore.messages(for: session.id)
            if existingMessages.isEmpty {
                chatStore.loadMessages(sessionId: session.id, messages: messages)
            }
            
            eventStore.loadEvents(sessionId: session.id, events: events)
            // Store the notification cursor for reconnection
            if let notificationId = lastNotificationId {
                sessionStore.setLastNotificationId(for: session.id, notificationId: notificationId)
            }
            print("[MessageRouter] Loaded events for session \(session.id)")

        // GitHub messages
        case .githubRepos(let repos):
            print("[MessageRouter] github.repos received: \(repos.count) repos")
            githubStore.handleReposReceived(repos)
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
            terminalStore: TerminalStore(),
            githubStore: GitHubStore()
        )
    }
}
