import Foundation

/// Owns one persistent `GhosttyTerminalBridge` per session.
///
/// Bridges (and the terminal views/surfaces they own) live for the duration
/// of the session — tab switches and SwiftUI view recreation never destroy
/// terminal state. Teardown happens only on session delete via
/// `clearOutput(for:)`.
@MainActor
@Observable
final class TerminalStore {
    /// Bridges per session
    private var bridgesBySession: [String: GhosttyTerminalBridge] = [:]

    /// Reference to Connect service (set during init)
    weak var connectService: ConnectService?

    @ObservationIgnored private var reconnectObserver: (any NSObjectProtocol)?

    init() {
        // Terminal PTY sizes are state, not events: resizes sent while the
        // stream is down are dropped, so every (re)connect re-syncs all
        // bridges' last-known sizes. Owned here — the terminal layer keeps
        // its own invariant instead of relying on views to restore it.
        reconnectObserver = NotificationCenter.default.addObserver(
            forName: ConnectService.didConnectNotification,
            object: nil,
            queue: .main
        ) { [weak self] _ in
            MainActor.assumeIsolated {
                self?.resyncPTYSizes()
            }
        }
    }

    /// Re-send every live terminal's last-known size after a (re)connect.
    func resyncPTYSizes() {
        for bridge in bridgesBySession.values {
            bridge.resyncSize()
        }
    }

    /// Re-send one session's last-known size (e.g. after its VM/agent
    /// restarted and the new PTY booted at the default size). Does not
    /// create a bridge.
    func resyncSize(sessionId: String) {
        bridgesBySession[sessionId]?.resyncSize()
    }

    /// Get or create the persistent bridge for a session
    func bridge(for sessionId: String) -> GhosttyTerminalBridge {
        if let existing = bridgesBySession[sessionId] {
            return existing
        }
        let bridge = GhosttyTerminalBridge(sessionId: sessionId, connectService: connectService)
        bridgesBySession[sessionId] = bridge
        return bridge
    }

    /// Append output for a session (called from MessageRouter)
    func appendOutput(sessionId: String, data: String) {
        guard let bytes = data.data(using: .utf8) else { return }
        bridge(for: sessionId).feed([UInt8](bytes))
    }

    /// Seed persisted scrollback from session history (app relaunch restore).
    /// No-op if the session's terminal already has content, so reconnect
    /// refreshes never duplicate scrollback.
    func seedHistory(sessionId: String, data: String) {
        guard let bytes = data.data(using: .utf8) else { return }
        bridge(for: sessionId).seedIfEmpty([UInt8](bytes))
    }

    /// Tear down the terminal for a session (session deleted)
    func clearOutput(for sessionId: String) {
        bridgesBySession.removeValue(forKey: sessionId)?.teardown()
    }
}
