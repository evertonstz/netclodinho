import Foundation

@Observable
final class ChatStore: @unchecked Sendable {
    private(set) var messagesBySession: [String: [ChatMessage]] = [:]

    private let persistenceKey = "netclode_chat_messages"

    init() {
        loadFromDisk()
    }

    func messages(for sessionId: String) -> [ChatMessage] {
        messagesBySession[sessionId] ?? []
    }

    func appendMessage(sessionId: String, message: ChatMessage) {
        var messages = messagesBySession[sessionId] ?? []
        messages.append(message)
        messagesBySession[sessionId] = messages
        saveToDisk()
    }

    /// Append partial content to the last assistant message, or create a new one
    func appendAssistantPartial(sessionId: String, delta: String) {
        var messages = messagesBySession[sessionId] ?? []

        if let lastIndex = messages.indices.last,
           messages[lastIndex].role == .assistant {
            // Append to existing assistant message
            messages[lastIndex].content += delta
        } else {
            // Create new assistant message
            messages.append(ChatMessage(
                role: .assistant,
                content: delta,
                timestamp: Date()
            ))
        }

        messagesBySession[sessionId] = messages
        // Don't save partial messages to disk - wait for finalize
    }

    /// Called when agent.done is received to finalize the message
    func finalizeLastMessage(sessionId: String) {
        saveToDisk()
    }

    func clearMessages(for sessionId: String) {
        messagesBySession.removeValue(forKey: sessionId)
        saveToDisk()
    }

    // MARK: - Persistence

    private func loadFromDisk() {
        guard let data = UserDefaults.standard.data(forKey: persistenceKey) else { return }

        do {
            let decoder = JSONDecoder()
            messagesBySession = try decoder.decode([String: [ChatMessage]].self, from: data)
        } catch {
            print("Failed to load chat messages: \(error)")
        }
    }

    private func saveToDisk() {
        do {
            let encoder = JSONEncoder()
            let data = try encoder.encode(messagesBySession)
            UserDefaults.standard.set(data, forKey: persistenceKey)
        } catch {
            print("Failed to save chat messages: \(error)")
        }
    }
}
