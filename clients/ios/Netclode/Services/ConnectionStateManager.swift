import Foundation
import os.log

private let logger = Logger(subsystem: "com.netclode", category: "ConnectionStateManager")

/// Manages connection state persistence across app launches.
/// Coordinates cursor positions, pending operations, and recovery data.
/// Uses actor isolation for thread-safe state access.
actor ConnectionStateManager {
    
    // MARK: - Types
    
    struct PersistedState: Codable {
        var cursors: [String: String]
        var activeSessionId: String?
        var lastConnectedAt: Date?
        var lastDisconnectReason: String?
    }
    
    // MARK: - Properties
    
    private var state: PersistedState
    private let persistenceKey = "com.netclode.connection-state"
    
    // MARK: - Initialization
    
    init() {
        if let data = UserDefaults.standard.data(forKey: persistenceKey),
           let loaded = try? JSONDecoder().decode(PersistedState.self, from: data) {
            state = loaded
            logger.debug("Loaded persisted connection state")
        } else {
            state = PersistedState(cursors: [:])
        }
    }
    
    // MARK: - Cursor Management
    
    func getCursor(for sessionId: String) -> String? {
        state.cursors[sessionId]
    }
    
    func setCursor(_ cursor: String, for sessionId: String) {
        state.cursors[sessionId] = cursor
        persist()
    }
    
    func getAllCursors() -> [String: String] {
        state.cursors
    }
    
    func clearCursor(for sessionId: String) {
        state.cursors.removeValue(forKey: sessionId)
        persist()
    }
    
    func clearAllCursors() {
        state.cursors.removeAll()
        persist()
    }
    
    // MARK: - Session Tracking
    
    func setActiveSession(_ sessionId: String?) {
        state.activeSessionId = sessionId
        persist()
    }
    
    func getActiveSession() -> String? {
        state.activeSessionId
    }
    
    // MARK: - Connection Events
    
    func recordConnection() {
        state.lastConnectedAt = Date()
        state.lastDisconnectReason = nil
        persist()
    }
    
    func recordDisconnection(reason: String) {
        state.lastDisconnectReason = reason
        persist()
    }
    
    func getLastConnectedAt() -> Date? {
        state.lastConnectedAt
    }
    
    func getLastDisconnectReason() -> String? {
        state.lastDisconnectReason
    }
    
    // MARK: - Persistence
    
    private func persist() {
        if let data = try? JSONEncoder().encode(state) {
            UserDefaults.standard.set(data, forKey: persistenceKey)
        }
    }
    
    func clear() {
        state = PersistedState(cursors: [:])
        UserDefaults.standard.removeObject(forKey: persistenceKey)
        logger.info("Connection state cleared")
    }
}
