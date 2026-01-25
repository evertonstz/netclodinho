import Foundation
import Observation

/// Snapshot model representing a point-in-time workspace state
struct Snapshot: Identifiable, Hashable, Sendable, Decodable {
    let id: String
    let sessionId: String
    let name: String
    let createdAt: Date
    let sizeBytes: Int64
    let turnNumber: Int32
    let messageCount: Int32
}

/// Store for managing session snapshots
@Observable
@MainActor
final class SnapshotStore {
    /// Snapshots keyed by session ID, ordered newest first
    private(set) var snapshotsBySession: [String: [Snapshot]] = [:]
    
    /// Whether a restore is in progress for a session
    private(set) var restoreInProgress: [String: Bool] = [:]
    
    /// Returns snapshots for a session
    func snapshots(for sessionId: String) -> [Snapshot] {
        snapshotsBySession[sessionId] ?? []
    }
    
    /// Set snapshots for a session (from list response)
    func setSnapshots(for sessionId: String, snapshots: [Snapshot]) {
        snapshotsBySession[sessionId] = snapshots
    }
    
    /// Add a new snapshot (from auto-snapshot notification)
    func addSnapshot(_ snapshot: Snapshot) {
        var snapshots = snapshotsBySession[snapshot.sessionId] ?? []
        // Insert at beginning (newest first)
        snapshots.insert(snapshot, at: 0)
        // Keep only last 10
        if snapshots.count > 10 {
            snapshots = Array(snapshots.prefix(10))
        }
        snapshotsBySession[snapshot.sessionId] = snapshots
    }
    
    /// Mark restore as in progress
    func setRestoreInProgress(for sessionId: String, inProgress: Bool) {
        restoreInProgress[sessionId] = inProgress
    }
    
    /// Check if restore is in progress
    func isRestoreInProgress(for sessionId: String) -> Bool {
        restoreInProgress[sessionId] ?? false
    }
    
    /// Clear snapshots for a session (e.g., on session delete)
    func clearSnapshots(for sessionId: String) {
        snapshotsBySession.removeValue(forKey: sessionId)
        restoreInProgress.removeValue(forKey: sessionId)
    }
}
