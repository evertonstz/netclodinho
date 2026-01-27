import Foundation
import os.log

private let logger = Logger(subsystem: "com.netclode", category: "SessionCache")

/// Local cache for sessions, providing offline access and faster startup.
/// Uses UserDefaults for simplicity, suitable for the expected data size.
@MainActor
@Observable
final class SessionCache {
    
    // MARK: - Types
    
    struct CachedSession: Codable, Identifiable, Sendable {
        let id: String
        let name: String
        let status: String  // Raw SessionStatus value
        let repo: String?
        let repoAccessRaw: String?  // Raw RepoAccess value
        let createdAt: Date
        let lastActiveAt: Date
        let sdkTypeRaw: String?  // Raw SdkType value
        let model: String?
        let copilotBackendRaw: String?  // Raw CopilotBackend value
        var cachedAt: Date
        
        init(from session: Session) {
            self.id = session.id
            self.name = session.name
            self.status = session.status.rawValue
            self.repo = session.repo
            self.repoAccessRaw = session.repoAccess?.rawValue
            self.createdAt = session.createdAt
            self.lastActiveAt = session.lastActiveAt
            self.sdkTypeRaw = session.sdkType?.rawValue
            self.model = session.model
            self.copilotBackendRaw = session.copilotBackend?.rawValue
            self.cachedAt = Date()
        }
        
        func toSession() -> Session {
            Session(
                id: id,
                name: name,
                status: SessionStatus(rawValue: status) ?? .paused,
                repo: repo,
                repoAccess: repoAccessRaw.flatMap { RepoAccess(rawValue: $0) },
                createdAt: createdAt,
                lastActiveAt: lastActiveAt,
                sdkType: sdkTypeRaw.flatMap { SdkType(rawValue: $0) },
                model: model,
                copilotBackend: copilotBackendRaw.flatMap { CopilotBackend(rawValue: $0) }
            )
        }
    }
    
    struct CacheMetadata: Codable {
        var lastFullRefresh: Date?
        var lastPartialRefresh: Date?
        var version: Int = 1
    }
    
    // MARK: - Properties
    
    private(set) var sessions: [CachedSession] = []
    private(set) var metadata: CacheMetadata = CacheMetadata()
    private(set) var isStale: Bool = false
    
    var isEmpty: Bool { sessions.isEmpty }
    
    private let encoder = JSONEncoder()
    private let decoder = JSONDecoder()
    
    private let sessionsKey = "com.netclode.cached-sessions"
    private let metadataKey = "com.netclode.cache-metadata"
    private let staleDuration: TimeInterval = 300 // 5 minutes
    
    // MARK: - Initialization
    
    init() {
        loadFromStorage()
        checkStaleness()
    }
    
    // MARK: - Public Methods
    
    /// Update cache with fresh session data
    func update(with sessions: [Session], isFullRefresh: Bool = true) {
        self.sessions = sessions.map { CachedSession(from: $0) }
        
        if isFullRefresh {
            metadata.lastFullRefresh = Date()
        }
        metadata.lastPartialRefresh = Date()
        
        isStale = false
        saveToStorage()
        
        logger.info("Cache updated with \(sessions.count) sessions (full: \(isFullRefresh))")
    }
    
    /// Update a single session in cache
    func update(session: Session) {
        if let index = sessions.firstIndex(where: { $0.id == session.id }) {
            sessions[index] = CachedSession(from: session)
        } else {
            sessions.append(CachedSession(from: session))
        }
        
        metadata.lastPartialRefresh = Date()
        saveToStorage()
    }
    
    /// Remove a session from cache
    func remove(sessionId: String) {
        sessions.removeAll { $0.id == sessionId }
        saveToStorage()
    }
    
    /// Get a specific cached session
    func get(sessionId: String) -> CachedSession? {
        sessions.first { $0.id == sessionId }
    }
    
    /// Get all sessions as Session objects
    func getSessions() -> [Session] {
        sessions.map { $0.toSession() }
    }
    
    /// Mark cache as needing refresh
    func markStale() {
        isStale = true
        logger.debug("Cache marked as stale")
    }
    
    /// Clear all cached data
    func clear() {
        sessions = []
        metadata = CacheMetadata()
        UserDefaults.standard.removeObject(forKey: sessionsKey)
        UserDefaults.standard.removeObject(forKey: metadataKey)
        logger.info("Cache cleared")
    }
    
    /// Check if cache needs refresh
    func needsRefresh() -> Bool {
        guard let lastRefresh = metadata.lastFullRefresh else {
            return true
        }
        return Date().timeIntervalSince(lastRefresh) > staleDuration || isStale
    }
    
    // MARK: - Private Methods
    
    private func loadFromStorage() {
        // Load sessions
        if let data = UserDefaults.standard.data(forKey: sessionsKey),
           let cached = try? decoder.decode([CachedSession].self, from: data) {
            sessions = cached
            logger.debug("Loaded \(cached.count) sessions from cache")
        }
        
        // Load metadata
        if let data = UserDefaults.standard.data(forKey: metadataKey),
           let meta = try? decoder.decode(CacheMetadata.self, from: data) {
            metadata = meta
        }
    }
    
    private func saveToStorage() {
        do {
            let sessionsData = try encoder.encode(sessions)
            let metadataData = try encoder.encode(metadata)
            
            UserDefaults.standard.set(sessionsData, forKey: sessionsKey)
            UserDefaults.standard.set(metadataData, forKey: metadataKey)
        } catch {
            logger.error("Failed to save cache: \(error.localizedDescription)")
        }
    }
    
    private func checkStaleness() {
        if needsRefresh() {
            isStale = true
        }
    }
}
