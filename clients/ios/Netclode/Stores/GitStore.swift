import Foundation
import SwiftUI

/// Store for git-related state (file changes, diffs)
@Observable
final class GitStore: @unchecked Sendable {
    
    // MARK: - State
    
    /// Changed files per session
    private(set) var filesBySession: [String: [GitFileChange]] = [:]
    
    /// Selected file path per session
    private(set) var selectedFileBySession: [String: String] = [:]
    
    /// Diff content for selected file per session
    private(set) var diffBySession: [String: String] = [:]
    
    /// Loading states
    private(set) var isLoadingStatus: [String: Bool] = [:]
    private(set) var isLoadingDiff: [String: Bool] = [:]
    
    /// Error messages
    private(set) var errorBySession: [String: String] = [:]
    
    // MARK: - Accessors
    
    func files(for sessionId: String) -> [GitFileChange] {
        filesBySession[sessionId] ?? []
    }
    
    func selectedFile(for sessionId: String) -> String? {
        selectedFileBySession[sessionId]
    }
    
    func diff(for sessionId: String) -> String? {
        diffBySession[sessionId]
    }
    
    func isLoadingStatus(for sessionId: String) -> Bool {
        isLoadingStatus[sessionId] ?? false
    }
    
    func isLoadingDiff(for sessionId: String) -> Bool {
        isLoadingDiff[sessionId] ?? false
    }
    
    func error(for sessionId: String) -> String? {
        errorBySession[sessionId]
    }
    
    // MARK: - Mutations
    
    @MainActor
    func setFiles(_ files: [GitFileChange], for sessionId: String) {
        filesBySession[sessionId] = files
        errorBySession[sessionId] = nil
    }
    
    @MainActor
    func setDiff(_ diff: String?, for sessionId: String) {
        diffBySession[sessionId] = diff
    }
    
    @MainActor
    func selectFile(_ path: String?, for sessionId: String) {
        selectedFileBySession[sessionId] = path
        // Clear diff when selection changes
        if path == nil {
            diffBySession[sessionId] = nil
        }
    }
    
    @MainActor
    func setLoadingStatus(_ loading: Bool, for sessionId: String) {
        isLoadingStatus[sessionId] = loading
    }
    
    @MainActor
    func setLoadingDiff(_ loading: Bool, for sessionId: String) {
        isLoadingDiff[sessionId] = loading
    }
    
    @MainActor
    func setError(_ error: String?, for sessionId: String) {
        errorBySession[sessionId] = error
    }
    
    @MainActor
    func clearSession(_ sessionId: String) {
        filesBySession.removeValue(forKey: sessionId)
        selectedFileBySession.removeValue(forKey: sessionId)
        diffBySession.removeValue(forKey: sessionId)
        isLoadingStatus.removeValue(forKey: sessionId)
        isLoadingDiff.removeValue(forKey: sessionId)
        errorBySession.removeValue(forKey: sessionId)
    }
}
