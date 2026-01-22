import Foundation

/// Store for managing GitHub repository data with persistent caching.
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
    
    /// UserDefaults keys for persistence
    private enum StorageKeys {
        static let repos = "github_repos_cache"
        static let lastFetched = "github_repos_last_fetched"
    }
    
    /// Whether the cache is stale
    var isCacheStale: Bool {
        guard let lastFetched else { return true }
        return Date().timeIntervalSince(lastFetched) > cacheTTL
    }
    
    init() {
        // Load from disk on background thread to avoid blocking main thread
        Task.detached(priority: .userInitiated) { [weak self] in
            await self?.loadFromStorageAsync()
        }
    }
    
    // MARK: - Persistence
    
    /// Load cached repos from UserDefaults asynchronously
    private func loadFromStorageAsync() async {
        // Capture keys before detached task (Sendable)
        let reposKey = StorageKeys.repos
        let lastFetchedKey = StorageKeys.lastFetched
        
        // Perform I/O on background thread
        let result = await Task.detached(priority: .userInitiated) { () -> ([GitHubRepo]?, Date?) in
            let repos: [GitHubRepo]?
            if let data = UserDefaults.standard.data(forKey: reposKey) {
                repos = try? JSONDecoder().decode([GitHubRepo].self, from: data)
            } else {
                repos = nil
            }
            
            let lastFetched = UserDefaults.standard.object(forKey: lastFetchedKey) as? Date
            return (repos, lastFetched)
        }.value
        
        if let repos = result.0 {
            self.repos = repos
        }
        if let lastFetched = result.1 {
            self.lastFetched = lastFetched
        }
    }
    
    /// Save repos to UserDefaults asynchronously
    private func saveToStorage() {
        // Capture values before detached task (Sendable)
        let reposToSave = repos
        let lastFetchedToSave = lastFetched
        let reposKey = StorageKeys.repos
        let lastFetchedKey = StorageKeys.lastFetched
        
        Task.detached(priority: .utility) {
            if let encoded = try? JSONEncoder().encode(reposToSave) {
                UserDefaults.standard.set(encoded, forKey: reposKey)
            }
            
            if let lastFetched = lastFetchedToSave {
                UserDefaults.standard.set(lastFetched, forKey: lastFetchedKey)
            }
        }
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
    /// - Parameter connectService: The Connect service to send the request
    /// - Parameter force: Force refresh even if cache is valid
    func fetchIfNeeded(connectService: ConnectService, force: Bool = false) {
        guard force || isCacheStale else { return }
        guard !isLoading else { return }
        guard connectService.connectionState.isConnected else { return }
        
        isLoading = true
        errorMessage = nil
        connectService.send(.githubReposList)
    }
    
    /// Handle incoming repos from server
    func handleReposReceived(_ repos: [GitHubRepo]) {
        self.repos = repos.sorted { $0.fullName.lowercased() < $1.fullName.lowercased() }
        self.lastFetched = Date()
        self.isLoading = false
        self.errorMessage = nil
        saveToStorage()
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
        UserDefaults.standard.removeObject(forKey: StorageKeys.repos)
        UserDefaults.standard.removeObject(forKey: StorageKeys.lastFetched)
    }
}
