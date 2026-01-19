import Foundation

/// Store for managing GitHub repository data with caching.
@MainActor
@Observable
final class GitHubStore {
    /// All cached repositories
    private(set) var repos: [GitHubRepo] = []
    
    /// Whether a fetch is in progress
    private(set) var isLoading = false
    
    /// Last time repos were fetched
    private(set) var lastFetched: Date?
    
    /// Error message if last fetch failed
    private(set) var errorMessage: String?
    
    /// Cache TTL in seconds (5 minutes)
    private let cacheTTL: TimeInterval = 300
    
    /// Whether the cache is stale
    var isCacheStale: Bool {
        guard let lastFetched else { return true }
        return Date().timeIntervalSince(lastFetched) > cacheTTL
    }
    
    /// Filter repos by query string (matches name or fullName)
    func filteredRepos(query: String) -> [GitHubRepo] {
        let trimmed = query.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        guard !trimmed.isEmpty else { return repos }
        
        return repos.filter { repo in
            repo.name.lowercased().contains(trimmed) ||
            repo.fullName.lowercased().contains(trimmed)
        }
    }
    
    /// Request repos from server if cache is stale
    /// - Parameter webSocketService: The WebSocket service to send the request
    /// - Parameter force: Force refresh even if cache is valid
    func fetchIfNeeded(webSocketService: WebSocketService, force: Bool = false) {
        guard force || isCacheStale else { return }
        guard !isLoading else { return }
        guard webSocketService.connectionState.isConnected else { return }
        
        isLoading = true
        errorMessage = nil
        webSocketService.send(.githubReposList)
    }
    
    /// Handle incoming repos from server
    func handleReposReceived(_ repos: [GitHubRepo]) {
        self.repos = repos.sorted { $0.fullName.lowercased() < $1.fullName.lowercased() }
        self.lastFetched = Date()
        self.isLoading = false
        self.errorMessage = nil
    }
    
    /// Handle error response
    func handleError(_ message: String) {
        self.isLoading = false
        self.errorMessage = message
    }
    
    /// Clear the cache
    func clearCache() {
        repos = []
        lastFetched = nil
        errorMessage = nil
    }
}
