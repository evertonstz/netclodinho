import Foundation

/// Represents a GitHub repository accessible to the GitHub App installation.
struct GitHubRepo: Codable, Identifiable, Hashable, Sendable {
    var id: String { fullName }
    
    /// Repository name (e.g., "my-repo")
    let name: String
    
    /// Full repository name including owner (e.g., "owner/my-repo")
    let fullName: String
    
    /// Whether the repository is private
    let isPrivate: Bool
    
    /// Repository description (optional)
    let description: String?
    
    private enum CodingKeys: String, CodingKey {
        case name
        case fullName
        case isPrivate = "private"
        case description
    }
}
