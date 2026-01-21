import Foundation

/// Represents a file with changes in the git working directory
struct GitFileChange: Identifiable, Codable, Equatable, Sendable {
    var id: String { path }
    
    let path: String
    let status: GitFileStatus
    let staged: Bool
    
    /// Display name (just the filename)
    var fileName: String {
        (path as NSString).lastPathComponent
    }
    
    /// Directory path
    var directory: String {
        (path as NSString).deletingLastPathComponent
    }
}

/// Git file status
enum GitFileStatus: String, Codable, Sendable {
    case modified
    case added
    case deleted
    case renamed
    case copied
    case untracked
    case ignored
    case unmerged
    
    var displayName: String {
        switch self {
        case .modified: return "Modified"
        case .added: return "Added"
        case .deleted: return "Deleted"
        case .renamed: return "Renamed"
        case .copied: return "Copied"
        case .untracked: return "Untracked"
        case .ignored: return "Ignored"
        case .unmerged: return "Unmerged"
        }
    }
    
    var icon: String {
        switch self {
        case .modified: return "pencil.circle.fill"
        case .added: return "plus.circle.fill"
        case .deleted: return "minus.circle.fill"
        case .renamed: return "arrow.right.circle.fill"
        case .copied: return "doc.on.doc.fill"
        case .untracked: return "questionmark.circle.fill"
        case .ignored: return "eye.slash.circle.fill"
        case .unmerged: return "exclamationmark.triangle.fill"
        }
    }
    
    var shortLabel: String {
        switch self {
        case .modified: return "M"
        case .added: return "A"
        case .deleted: return "D"
        case .renamed: return "R"
        case .copied: return "C"
        case .untracked: return "?"
        case .ignored: return "!"
        case .unmerged: return "U"
        }
    }
}

// MARK: - Git Status Response

/// Response from git status endpoint
struct GitStatusResponse: Codable, Sendable {
    let files: [GitFileChange]
}

/// Response from git diff endpoint
struct GitDiffResponse: Codable, Sendable {
    let diff: String
}
