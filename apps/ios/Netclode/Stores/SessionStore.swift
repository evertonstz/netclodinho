import Foundation

@Observable
final class SessionStore: @unchecked Sendable {
    private(set) var sessions: [Session] = []
    private(set) var currentSessionId: String?
    private(set) var processingSessionIds: Set<String> = []
    private(set) var errorsBySession: [String: String] = [:]
    private(set) var lastNotificationIds: [String: String] = [:] // sessionId -> Redis Stream ID

    var currentSession: Session? {
        guard let id = currentSessionId else { return nil }
        return sessions.first { $0.id == id }
    }

    var sortedSessions: [Session] {
        sessions.sorted { $0.lastActiveAt > $1.lastActiveAt }
    }

    func setSessions(_ sessions: [Session]) {
        self.sessions = sessions
    }

    func addSession(_ session: Session) {
        if let index = sessions.firstIndex(where: { $0.id == session.id }) {
            sessions[index] = session
        } else {
            sessions.append(session)
        }
    }

    func updateSession(_ session: Session) {
        if let index = sessions.firstIndex(where: { $0.id == session.id }) {
            sessions[index] = session
        }
    }

    func removeSession(id: String) {
        sessions.removeAll { $0.id == id }
        if currentSessionId == id {
            currentSessionId = nil
        }
        processingSessionIds.remove(id)
        errorsBySession.removeValue(forKey: id)
        lastNotificationIds.removeValue(forKey: id)
    }

    func setCurrentSession(id: String?) {
        currentSessionId = id
    }

    func setProcessing(for sessionId: String, processing: Bool) {
        if processing {
            processingSessionIds.insert(sessionId)
        } else {
            processingSessionIds.remove(sessionId)
        }
    }

    func isProcessing(_ sessionId: String) -> Bool {
        processingSessionIds.contains(sessionId)
    }

    func setError(for sessionId: String, error: String?) {
        if let error {
            errorsBySession[sessionId] = error
        } else {
            errorsBySession.removeValue(forKey: sessionId)
        }
    }

    func error(for sessionId: String) -> String? {
        errorsBySession[sessionId]
    }

    // MARK: - Notification Cursor (for reconnection)

    func setLastNotificationId(for sessionId: String, notificationId: String) {
        lastNotificationIds[sessionId] = notificationId
    }

    func lastNotificationId(for sessionId: String) -> String? {
        lastNotificationIds[sessionId]
    }
}
