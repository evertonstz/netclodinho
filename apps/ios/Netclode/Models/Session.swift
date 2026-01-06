import Foundation

enum SessionStatus: String, Codable, CaseIterable, Sendable {
    case creating
    case ready
    case running
    case paused
    case error

    var displayName: String {
        rawValue.capitalized
    }

    var systemImage: String {
        switch self {
        case .creating: "hourglass"
        case .ready: "checkmark.circle.fill"
        case .running: "play.circle.fill"
        case .paused: "pause.circle.fill"
        case .error: "exclamationmark.triangle.fill"
        }
    }

    var tintColor: Theme.StatusColor {
        switch self {
        case .creating: .creating
        case .ready: .ready
        case .running: .running
        case .paused: .paused
        case .error: .error
        }
    }
}

struct Session: Identifiable, Codable, Hashable, Sendable {
    let id: String
    var name: String
    var status: SessionStatus
    var repo: String?
    let createdAt: Date
    var lastActiveAt: Date

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
