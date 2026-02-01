import Foundation
import SwiftUI
import BackgroundTasks
import os.log

private let logger = Logger(subsystem: "com.netclode", category: "AppStateCoordinator")

/// Central coordinator for app lifecycle, network state, and connection management.
/// Orchestrates interactions between NetworkMonitor, ConnectService, and background tasks.
@MainActor
@Observable
final class AppStateCoordinator {
    
    // MARK: - Types
    
    enum AppPhase: Equatable {
        case launching
        case active
        case inactive
        case background
    }
    
    struct Status {
        var app: AppPhase = .launching
        var network: NetworkMonitor.NetworkState = .unknown
        var connection: ConnectionState = .disconnected(reason: .initial)
        var pendingMessages: Int = 0
        var cachedSessions: Int = 0
        var isRefreshing: Bool = false
    }
    
    // MARK: - Properties
    
    private(set) var status = Status()
    
    let networkMonitor: NetworkMonitor
    let messageQueue: MessageQueue
    let sessionCache: SessionCache
    let connectionStateManager: ConnectionStateManager
    
    private weak var connectService: ConnectService?
    private weak var sessionStore: SessionStore?
    private weak var settingsStore: SettingsStore?
    private weak var chatStore: ChatStore?
    
    private var networkObserverTask: Task<Void, Never>?
    
    // Background task identifiers
    static let backgroundRefreshIdentifier = "com.netclode.background-refresh"
    
    // MARK: - Initialization
    
    init() {
        self.networkMonitor = NetworkMonitor()
        self.messageQueue = MessageQueue()
        self.sessionCache = SessionCache()
        self.connectionStateManager = ConnectionStateManager()
        
        status.pendingMessages = messageQueue.pendingCount
        status.cachedSessions = sessionCache.sessions.count
    }
    
    // MARK: - Setup
    
    func configure(
        connectService: ConnectService,
        sessionStore: SessionStore,
        settingsStore: SettingsStore,
        chatStore: ChatStore
    ) {
        self.connectService = connectService
        self.sessionStore = sessionStore
        self.chatStore = chatStore
        self.settingsStore = settingsStore
        
        // Inject network monitor into connect service
        connectService.networkMonitor = networkMonitor
        
        // Start network monitoring
        networkMonitor.start()
        observeNetworkChanges()
        
        // Load persisted cursors
        Task {
            let cursors = await connectionStateManager.getAllCursors()
            sessionStore.loadCursors(from: cursors)
        }
        
        // Load cached sessions for immediate display
        if !sessionCache.isEmpty {
            sessionStore.loadFromCache(sessionCache.getSessions())
        }
        
        // Register background tasks
        registerBackgroundTasks()
        
        logger.info("AppStateCoordinator configured")
    }
    
    // MARK: - Scene Phase Handling
    
    func handleScenePhase(_ phase: ScenePhase) {
        switch phase {
        case .active:
            handleBecameActive()
        case .inactive:
            handleBecameInactive()
        case .background:
            handleEnteredBackground()
        @unknown default:
            break
        }
    }
    
    private func handleBecameActive() {
        logger.info("App became active")
        status.app = .active
        
        // Force network state check
        networkMonitor.checkNow()
        status.network = networkMonitor.currentState
        
        // Restore connection
        connectService?.restoreFromBackground()
        
        // Update connection status
        if let connectionState = connectService?.connectionState {
            status.connection = connectionState
        }
        
        // Refresh sessions if cache is stale
        if sessionCache.needsRefresh() {
            Task {
                await refreshSessions()
            }
        }
        
        // Replay pending messages after connection establishes
        Task {
            // Wait a bit for connection to establish
            try? await Task.sleep(nanoseconds: 2_000_000_000) // 2s
            await replayPendingMessages()
        }
    }
    
    private func handleBecameInactive() {
        logger.info("App became inactive")
        status.app = .inactive
        // No action needed - transitional state
    }
    
    private func handleEnteredBackground() {
        logger.info("App entered background")
        status.app = .background
        
        // Prepare connection for background
        connectService?.prepareForBackground()
        
        // Persist cursors
        Task {
            if let cursors = sessionStore?.lastNotificationIds {
                for (sessionId, cursor) in cursors {
                    await connectionStateManager.setCursor(cursor, for: sessionId)
                }
            }
            
            // Record active session
            await connectionStateManager.setActiveSession(sessionStore?.currentSessionId)
        }
        
        // Schedule background refresh
        scheduleBackgroundRefresh()
    }
    
    // MARK: - Network Observation
    
    private func observeNetworkChanges() {
        networkObserverTask?.cancel()
        
        networkObserverTask = Task {
            for await transition in networkMonitor.transitions {
                await handleNetworkTransition(transition)
            }
        }
    }
    
    private func handleNetworkTransition(_ transition: NetworkMonitor.NetworkTransition) async {
        status.network = transition.to
        
        // Inform connect service
        connectService?.handleNetworkTransition(transition)
        
        // If network restored and we have pending messages, try to replay
        if transition.isReconnection {
            // Wait for connection to be established
            try? await Task.sleep(nanoseconds: 2_000_000_000) // 2s
            await replayPendingMessages()
        }
    }
    
    // MARK: - Message Queue Operations
    
    func queueMessage(sessionId: String, content: String) {
        messageQueue.enqueue(sessionId: sessionId, content: content)
        status.pendingMessages = messageQueue.pendingCount
    }
    
    private func replayPendingMessages() async {
        guard let sessionStore = sessionStore,
              let connectService = connectService,
              connectService.connectionState.isUsable,
              let currentSessionId = sessionStore.currentSessionId else {
            return
        }
        
        let sent = await messageQueue.replay(for: currentSessionId) { (sessionId: String, content: String) in
            // Mark as processing
            sessionStore.setProcessing(for: sessionId, processing: true)
            
            // Resume session if paused (like normal sendMessage does)
            if let session = sessionStore.sessions.first(where: { $0.id == sessionId }),
               session.status == .paused {
                connectService.send(.sessionResume(id: sessionId))
            }
            
            // Send the prompt
            connectService.send(.prompt(sessionId: sessionId, text: content))
        }
        
        if sent > 0 {
            logger.info("Replayed \(sent) pending messages")
            // Clear pending status from chat store since messages were sent
            chatStore?.clearPendingMessages(for: currentSessionId)
        }
        
        status.pendingMessages = messageQueue.pendingCount
    }
    
    // MARK: - Session Cache Operations
    
    func refreshSessions() async {
        guard let connectService = connectService else { return }
        
        status.isRefreshing = true
        defer { status.isRefreshing = false }
        
        // Request session list from server
        connectService.send(.sessionList)
        
        // Wait for response (handled by MessageRouter which updates SessionStore)
        // The cache will be updated when we receive the session list
        try? await Task.sleep(nanoseconds: 2_000_000_000) // 2s timeout
    }
    
    /// Called by MessageRouter when sessions are received
    func updateSessionCache(with sessions: [Session]) {
        sessionCache.update(with: sessions)
        status.cachedSessions = sessionCache.sessions.count
    }
    
    /// Update connection status for UI display
    func updateConnectionStatus(_ state: ConnectionState) {
        status.connection = state
    }
    
    // MARK: - Background Tasks
    
    private func registerBackgroundTasks() {
        BGTaskScheduler.shared.register(
            forTaskWithIdentifier: Self.backgroundRefreshIdentifier,
            using: nil
        ) { [weak self] task in
            Task { @MainActor in
                self?.handleBackgroundRefresh(task as! BGAppRefreshTask)
            }
        }
    }
    
    private func scheduleBackgroundRefresh() {
        let request = BGAppRefreshTaskRequest(identifier: Self.backgroundRefreshIdentifier)
        request.earliestBeginDate = Date(timeIntervalSinceNow: 15 * 60) // 15 minutes
        
        do {
            try BGTaskScheduler.shared.submit(request)
            logger.info("Scheduled background refresh")
        } catch {
            logger.error("Failed to schedule background refresh: \(error.localizedDescription)")
        }
    }
    
    private func handleBackgroundRefresh(_ task: BGAppRefreshTask) {
        logger.info("Background refresh starting")
        
        // Schedule next refresh
        scheduleBackgroundRefresh()
        
        let refreshTask = Task {
            // Quick session status check - minimal work
            sessionCache.markStale()
            task.setTaskCompleted(success: true)
        }
        
        task.expirationHandler = {
            refreshTask.cancel()
        }
    }
    
    // MARK: - Cleanup
    
    func cleanup() {
        networkObserverTask?.cancel()
        networkMonitor.stop()
    }
}
