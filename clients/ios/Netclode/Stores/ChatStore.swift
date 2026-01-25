import Foundation

@MainActor
@Observable
final class ChatStore {
    private(set) var messagesBySession: [String: [ChatMessage]] = [:]

    private let persistenceKey = "netclode_chat_messages"
    private var saveTask: Task<Void, Never>?
    private let saveDebounceInterval: TimeInterval = 0.5  // Debounce saves by 500ms

    init() {
        // Load from disk on background thread, then update on main
        Task.detached(priority: .userInitiated) { [weak self] in
            await self?.loadFromDiskAsync()
        }
    }

    func messages(for sessionId: String) -> [ChatMessage] {
        messagesBySession[sessionId] ?? []
    }

    func appendMessage(sessionId: String, message: ChatMessage) {
        var messages = messagesBySession[sessionId] ?? []
        messages.append(message)
        messagesBySession[sessionId] = messages
        scheduleSave()
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
        // Update the timestamp to now so the message sorts after all events
        var messages = messagesBySession[sessionId] ?? []
        if let lastIndex = messages.indices.last,
           messages[lastIndex].role == .assistant {
            messages[lastIndex].timestamp = Date()
            messagesBySession[sessionId] = messages
        }
        scheduleSave()
    }

    func clearMessages(for sessionId: String) {
        messagesBySession.removeValue(forKey: sessionId)
        scheduleSave()
    }

    /// Load messages from server sync response
    func loadMessages(sessionId: String, messages: [PersistedMessage]) {
        messagesBySession[sessionId] = messages.map { $0.toChatMessage() }
        scheduleSave()
    }

    /// Truncate messages to a specific count (used for snapshot restore)
    func truncateMessages(sessionId: String, keepCount: Int) {
        guard var messages = messagesBySession[sessionId], messages.count > keepCount else {
            return
        }
        messages = Array(messages.prefix(keepCount))
        messagesBySession[sessionId] = messages
        scheduleSave()
    }

    // MARK: - Persistence

    private func loadFromDiskAsync() async {
        // Capture Sendable value before detached task (avoid capturing self)
        let key = persistenceKey
        
        // Perform I/O on background thread
        let data = await Task.detached(priority: .userInitiated) {
            UserDefaults.standard.data(forKey: key)
        }.value

        guard let data else { return }

        // Decode on background thread
        let decoded = await Task.detached(priority: .userInitiated) { () -> [String: [ChatMessage]]? in
            let decoder = JSONDecoder()
            return try? decoder.decode([String: [ChatMessage]].self, from: data)
        }.value

        if let decoded {
            self.messagesBySession = decoded
        }
    }

    /// Debounced save - coalesces rapid save calls into a single write
    private func scheduleSave() {
        saveTask?.cancel()
        // Capture Sendable value before task (avoid capturing self in detached task)
        let key = persistenceKey
        saveTask = Task { [weak self] in
            // Debounce: wait before actually saving
            try? await Task.sleep(for: .milliseconds(500))

            guard !Task.isCancelled, let self else { return }

            // Capture data on main actor
            let dataToSave = self.messagesBySession

            // Perform encoding and I/O on background thread
            await Task.detached(priority: .utility) {
                do {
                    let encoder = JSONEncoder()
                    let data = try encoder.encode(dataToSave)
                    UserDefaults.standard.set(data, forKey: key)
                } catch {
                    print("Failed to save chat messages: \(error)")
                }
            }.value
        }
    }
}
