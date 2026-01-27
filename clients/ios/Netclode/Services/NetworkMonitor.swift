import Foundation
import Network
import os.log

private let logger = Logger(subsystem: "com.netclode", category: "NetworkMonitor")

/// Monitors network connectivity and path changes using NWPathMonitor.
/// Publishes network state changes for connection management decisions.
@MainActor
@Observable
final class NetworkMonitor {
    
    // MARK: - Types
    
    enum NetworkState: Equatable, Sendable {
        case unknown
        case disconnected
        case wifi
        case cellular
        case wired
        case other
        
        var isConnected: Bool {
            switch self {
            case .wifi, .cellular, .wired, .other: return true
            case .unknown, .disconnected: return false
            }
        }
        
        var description: String {
            switch self {
            case .unknown: return "Unknown"
            case .disconnected: return "No Connection"
            case .wifi: return "Wi-Fi"
            case .cellular: return "Cellular"
            case .wired: return "Wired"
            case .other: return "Connected"
            }
        }
        
        var systemImage: String {
            switch self {
            case .unknown: return "questionmark.circle"
            case .disconnected: return "wifi.slash"
            case .wifi: return "wifi"
            case .cellular: return "antenna.radiowaves.left.and.right"
            case .wired: return "cable.connector"
            case .other: return "network"
            }
        }
    }
    
    struct NetworkTransition: Sendable {
        let from: NetworkState
        let to: NetworkState
        let timestamp: Date
        
        var isReconnection: Bool {
            !from.isConnected && to.isConnected
        }
        
        var isDisconnection: Bool {
            from.isConnected && !to.isConnected
        }
        
        var isInterfaceChange: Bool {
            from.isConnected && to.isConnected && from != to
        }
    }
    
    // MARK: - Properties
    
    private(set) var currentState: NetworkState = .unknown
    private(set) var isExpensive: Bool = false
    private(set) var isConstrained: Bool = false
    private(set) var lastTransition: NetworkTransition?
    
    private let monitor: NWPathMonitor
    private let queue = DispatchQueue(label: "com.netclode.network-monitor", qos: .utility)
    
    // Continuation for async stream of transitions
    private var transitionContinuation: AsyncStream<NetworkTransition>.Continuation?
    private var _transitionsStream: AsyncStream<NetworkTransition>?
    
    /// Async stream of network transitions for reactive handling
    var transitions: AsyncStream<NetworkTransition> {
        if let stream = _transitionsStream {
            return stream
        }
        let stream = AsyncStream<NetworkTransition> { [weak self] continuation in
            self?.transitionContinuation = continuation
            continuation.onTermination = { @Sendable _ in
                // Cleanup if needed
            }
        }
        _transitionsStream = stream
        return stream
    }
    
    // MARK: - Initialization
    
    init() {
        self.monitor = NWPathMonitor()
    }
    
    // Note: cleanup is handled via stop() which should be called before deallocation
    // We can't call stop() in deinit because it's MainActor-isolated
    
    // MARK: - Public Methods
    
    func start() {
        logger.info("Starting network monitor")
        
        monitor.pathUpdateHandler = { [weak self] path in
            Task { @MainActor [weak self] in
                self?.handlePathUpdate(path)
            }
        }
        
        monitor.start(queue: queue)
    }
    
    func stop() {
        logger.info("Stopping network monitor")
        monitor.cancel()
        transitionContinuation?.finish()
    }
    
    /// Force a path check - useful after app foregrounds
    func checkNow() {
        let path = monitor.currentPath
        handlePathUpdate(path)
    }
    
    // MARK: - Private Methods
    
    private func handlePathUpdate(_ path: NWPath) {
        let newState = mapPathToState(path)
        let previousState = currentState
        
        isExpensive = path.isExpensive
        isConstrained = path.isConstrained
        
        guard newState != previousState else { return }
        
        let transition = NetworkTransition(
            from: previousState,
            to: newState,
            timestamp: Date()
        )
        
        currentState = newState
        lastTransition = transition
        
        logger.info("Network transition: \(previousState.description) → \(newState.description)")
        
        transitionContinuation?.yield(transition)
    }
    
    private func mapPathToState(_ path: NWPath) -> NetworkState {
        guard path.status == .satisfied else {
            return .disconnected
        }
        
        if path.usesInterfaceType(.wifi) {
            return .wifi
        } else if path.usesInterfaceType(.cellular) {
            return .cellular
        } else if path.usesInterfaceType(.wiredEthernet) {
            return .wired
        } else {
            return .other
        }
    }
}
