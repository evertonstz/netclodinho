import Foundation

enum SessionStatus: String, Codable, CaseIterable, Sendable {
    case creating
    case resuming
    case ready
    case running
    case paused
    case error
    case interrupted

    var displayName: String {
        switch self {
        case .interrupted: "Interrupted"
        default: rawValue.capitalized
        }
    }

    var systemImage: String {
        switch self {
        case .creating: "hourglass"
        case .resuming: "arrow.clockwise"
        case .ready: "checkmark.circle.fill"
        case .running: "play.circle.fill"
        case .paused: "pause.circle.fill"
        case .error: "exclamationmark.triangle.fill"
        case .interrupted: "wifi.exclamationmark"
        }
    }

    var tintColor: Theme.StatusColor {
        switch self {
        case .creating: .creating
        case .resuming: .resuming
        case .ready: .ready
        case .running: .running
        case .paused: .paused
        case .error: .error
        case .interrupted: .interrupted
        }
    }
}

/// Repository access level for GitHub integration.
/// Only applies when a repo is selected. Write access is scoped to the selected repo only.
enum RepoAccess: String, Codable, CaseIterable, Sendable {
    case read
    case write

    var displayName: String {
        switch self {
        case .read: "Read Only"
        case .write: "Read & Write"
        }
    }

    var description: String {
        switch self {
        case .read: "Clone only (no push)"
        case .write: "Clone and push to this repo"
        }
    }

    var icon: String {
        switch self {
        case .read: "eye"
        case .write: "square.and.pencil"
        }
    }
}

/// SDK type for agent sessions.
enum SdkType: String, Codable, CaseIterable, Sendable {
    case claude
    case opencode
    case copilot

    var displayName: String {
        switch self {
        case .claude: "Claude Code"
        case .opencode: "OpenCode"
        case .copilot: "GitHub Copilot"
        }
    }

    var description: String {
        switch self {
        case .claude: "Direct Claude integration"
        case .opencode: "Multi-provider support"
        case .copilot: "GitHub Copilot"
        }
    }
}

/// Backend for Copilot SDK sessions.
enum CopilotBackend: String, Codable, CaseIterable, Sendable {
    case github
    case anthropic

    var displayName: String {
        switch self {
        case .github: "GitHub"
        case .anthropic: "Anthropic (BYOK)"
        }
    }

    var description: String {
        switch self {
        case .github: "Uses GitHub Copilot API with premium requests"
        case .anthropic: "Uses Anthropic API directly (Bring Your Own Key)"
        }
    }
}

struct Session: Identifiable, Codable, Hashable, Sendable {
    let id: String
    var name: String
    var status: SessionStatus
    var repo: String?
    var repoAccess: RepoAccess?
    let createdAt: Date
    var lastActiveAt: Date
    var sdkType: SdkType?
    var model: String?
    var copilotBackend: CopilotBackend?

    var isActive: Bool {
        status == .ready || status == .running
    }
}

extension Session {
    static let preview = Session(
        id: "abc123def456",
        name: "My Project",
        status: .ready,
        repo: "https://github.com/user/repo",
        createdAt: Date().addingTimeInterval(-3600),
        lastActiveAt: Date()
    )

    static let previewList: [Session] = [
        Session(id: "sess1", name: "Frontend Refactor", status: .running, createdAt: Date().addingTimeInterval(-7200), lastActiveAt: Date()),
        Session(id: "sess2", name: "API Integration", status: .ready, createdAt: Date().addingTimeInterval(-86400), lastActiveAt: Date().addingTimeInterval(-3600)),
        Session(id: "sess3", name: "Bug Fix #42", status: .paused, createdAt: Date().addingTimeInterval(-172800), lastActiveAt: Date().addingTimeInterval(-43200)),
        Session(id: "sess4", name: "New Feature", status: .creating, createdAt: Date(), lastActiveAt: Date()),
    ]
}
