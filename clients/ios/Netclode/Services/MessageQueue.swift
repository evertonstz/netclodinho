import Foundation
import os.log

private let logger = Logger(subsystem: "com.netclode", category: "MessageQueue")

/// Persistent queue for messages that couldn't be sent due to connection issues.
/// Messages are persisted to disk and replayed on reconnection.
@MainActor
@Observable
final class MessageQueue {
    
    // MARK: - Types
    
    struct QueuedMessage: Codable, Identifiable, Sendable {
        let id: UUID
        let sessionId: String
        let content: String
        let queuedAt: Date
        var attempts: Int
        var lastAttemptAt: Date?
        var lastError: String?
        
        init(sessionId: String, content: String) {
            self.id = UUID()
            self.sessionId = sessionId
            self.content = content
            self.queuedAt = Date()
            self.attempts = 0
            self.lastAttemptAt = nil
            self.lastError = nil
        }
    }
    
    enum QueueError: Error, LocalizedError {
        case sessionMismatch(expected: String, current: String?)
        case maxRetriesExceeded(messageId: UUID)
        case persistenceFailed(underlying: Error)
        
        var errorDescription: String? {
            switch self {
            case .sessionMismatch(let expected, let current):
                return "Session changed from \(expected) to \(current ?? "none")"
            case .maxRetriesExceeded(let id):
                return "Message \(id) exceeded max retry attempts"
            case .persistenceFailed(let error):
                return "Failed to persist queue: \(error.localizedDescription)"
            }
        }
    }
    
    // MARK: - Configuration
    
    struct Configuration {
        var maxRetries: Int = 3
        var maxQueueSize: Int = 50
        var maxMessageAge: TimeInterval = 3600 // 1 hour
        var persistenceKey: String = "com.netclode.message-queue"
    }
    
    // MARK: - Properties
    
    private(set) var messages: [QueuedMessage] = []
    private(set) var isSending: Bool = false
    
    var pendingCount: Int { messages.count }
    var hasPending: Bool { !messages.isEmpty }
    
    private let configuration: Configuration
    private let encoder = JSONEncoder()
    private let decoder = JSONDecoder()
    
    // File-based persistence for reliability
    private var persistenceURL: URL {
        FileManager.default.urls(for: .documentDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("pending_messages.json")
    }
    
    // MARK: - Initialization
    
    init(configuration: Configuration = Configuration()) {
        self.configuration = configuration
        loadFromDisk()
        pruneStaleMessages()
    }
    
    // MARK: - Public Methods
    
    /// Queue a message for later delivery
    func enqueue(sessionId: String, content: String) {
        guard messages.count < configuration.maxQueueSize else {
            logger.warning("Queue full, dropping oldest message")
            messages.removeFirst()
            return
        }
        
        let message = QueuedMessage(sessionId: sessionId, content: content)
        messages.append(message)
        
        logger.info("Queued message for session \(sessionId), queue size: \(self.messages.count)")
        saveToDisk()
    }
    
    /// Replay all queued messages for a session
    /// - Parameters:
    ///   - sessionId: Current active session (for validation)
    ///   - sender: Async closure that sends a message
    /// - Returns: Number of successfully sent messages
    @discardableResult
    func replay(
        for sessionId: String,
        sender: @escaping (String, String) async throws -> Void
    ) async -> Int {
        guard !isSending else {
            logger.debug("Replay already in progress")
            return 0
        }
        
        isSending = true
        defer { isSending = false }
        
        var successCount = 0
        var indicesToRemove: [Int] = []
        
        for (index, message) in messages.enumerated() {
            // Validate session hasn't changed
            guard message.sessionId == sessionId else {
                logger.warning(
                    "Skipping message for different session: \(message.sessionId) != \(sessionId)"
                )
                indicesToRemove.append(index)
                continue
            }
            
            // Check retry limit
            guard message.attempts < configuration.maxRetries else {
                logger.error("Message \(message.id) exceeded max retries, dropping")
                indicesToRemove.append(index)
                continue
            }
            
            // Attempt to send
            messages[index].attempts += 1
            messages[index].lastAttemptAt = Date()
            
            do {
                try await sender(message.sessionId, message.content)
                logger.info("Successfully sent queued message \(message.id)")
                indicesToRemove.append(index)
                successCount += 1
            } catch {
                logger.error("Failed to send message \(message.id): \(error.localizedDescription)")
                messages[index].lastError = error.localizedDescription
            }
        }
        
        // Remove sent/failed messages (in reverse to preserve indices)
        for index in indicesToRemove.reversed() {
            messages.remove(at: index)
        }
        
        saveToDisk()
        return successCount
    }
    
    /// Get messages for a specific session
    func messages(for sessionId: String) -> [QueuedMessage] {
        messages.filter { $0.sessionId == sessionId }
    }
    
    /// Clear all messages for a session (e.g., session deleted)
    func clear(for sessionId: String) {
        messages.removeAll { $0.sessionId == sessionId }
        saveToDisk()
    }
    
    /// Clear all queued messages
    func clearAll() {
        messages.removeAll()
        saveToDisk()
    }
    
    // MARK: - Persistence
    
    private func saveToDisk() {
        do {
            let data = try encoder.encode(messages)
            try data.write(to: persistenceURL, options: .atomic)
            logger.debug("Saved \(self.messages.count) messages to disk")
        } catch {
            logger.error("Failed to save message queue: \(error.localizedDescription)")
        }
    }
    
    private func loadFromDisk() {
        guard FileManager.default.fileExists(atPath: persistenceURL.path) else {
            return
        }
        
        do {
            let data = try Data(contentsOf: persistenceURL)
            messages = try decoder.decode([QueuedMessage].self, from: data)
            logger.info("Loaded \(self.messages.count) messages from disk")
        } catch {
            logger.error("Failed to load message queue: \(error.localizedDescription)")
            messages = []
        }
    }
    
    private func pruneStaleMessages() {
        let cutoff = Date().addingTimeInterval(-configuration.maxMessageAge)
        let originalCount = messages.count
        messages.removeAll { $0.queuedAt < cutoff }
        
        if messages.count != originalCount {
            logger.info("Pruned \(originalCount - self.messages.count) stale messages")
            saveToDisk()
        }
    }
}
