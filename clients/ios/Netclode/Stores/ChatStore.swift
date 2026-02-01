import Foundation

@MainActor
@Observable
final class ChatStore {
    private(set) var messagesBySession: [String: [ChatMessage]] = [:]
    
    /// Track pending message IDs that haven't been synced to server yet
    /// These will be preserved when loadMessages is called
    private var pendingMessageIds: Set<UUID> = []

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
    
    /// Append a message and mark it as pending (won't be wiped by loadMessages until cleared)
    func appendPendingMessage(sessionId: String, message: ChatMessage) {
        pendingMessageIds.insert(message.id)
        appendMessage(sessionId: sessionId, message: message)
    }
    
    /// Clear pending status for a message (called after server confirms receipt)
    func clearPendingMessage(id: UUID) {
        pendingMessageIds.remove(id)
    }
    
    /// Clear all pending messages for a session
    func clearPendingMessages(for sessionId: String) {
        let sessionMessages = messagesBySession[sessionId] ?? []
        let sessionMessageIds = Set(sessionMessages.map { $0.id })
        pendingMessageIds = pendingMessageIds.subtracting(sessionMessageIds)
    }
    
    /// Check if a message is pending (not yet synced to server)
    func isMessagePending(_ messageId: UUID) -> Bool {
        pendingMessageIds.contains(messageId)
    }

    /// Append partial content to an assistant message, creating a new one if messageId changes
    func appendAssistantPartial(sessionId: String, delta: String, messageId: String? = nil) {
        var messages = messagesBySession[sessionId] ?? []

        // Check if we should append to existing message or create a new one
        if let lastIndex = messages.indices.last,
           messages[lastIndex].role == .assistant {
            let lastMessage = messages[lastIndex]
            
            // If messageId is provided and different from current, create a new message
            if let newMessageId = messageId,
               let existingMessageId = lastMessage.serverMessageId,
               newMessageId != existingMessageId {
                // Different message ID - create a new message
                messages.append(ChatMessage(
                    role: .assistant,
                    content: delta,
                    timestamp: Date(),
                    serverMessageId: newMessageId
                ))
            } else {
                // Same message ID (or no ID) - append to existing
                messages[lastIndex].content += delta
                // Set the messageId if this is the first time we're seeing it
                if messages[lastIndex].serverMessageId == nil && messageId != nil {
                    messages[lastIndex].serverMessageId = messageId
                }
            }
        } else {
            // No existing assistant message - create new one
            messages.append(ChatMessage(
                role: .assistant,
                content: delta,
                timestamp: Date(),
                serverMessageId: messageId
            ))
        }

        messagesBySession[sessionId] = messages
        // Don't save partial messages to disk - wait for finalize
    }

    /// Called when agent.done is received to finalize and persist messages
    func finalizeLastMessage(sessionId: String) {
        // Just save - timestamps are already set correctly when messages were created
        scheduleSave()
    }

    func clearMessages(for sessionId: String) {
        messagesBySession.removeValue(forKey: sessionId)
        scheduleSave()
    }

    /// Load messages from server sync response
    /// Only updates if server has more messages than local, preserving local state
    func loadMessages(sessionId: String, messages: [PersistedMessage]) {
        let serverMessages = messages.map { $0.toChatMessage() }
        let existingMessages = messagesBySession[sessionId] ?? []
        
        // If we have more local messages than server, keep local state
        // (we likely have pending messages that haven't synced yet)
        if existingMessages.count >= serverMessages.count && !existingMessages.isEmpty {
            return
        }
        
        // Server has more messages - use server state but preserve any pending local messages
        let pendingMessages = existingMessages.filter { pendingMessageIds.contains($0.id) }
        messagesBySession[sessionId] = serverMessages + pendingMessages
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
