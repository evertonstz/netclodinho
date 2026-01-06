import Foundation

enum MessageRole: String, Codable, Sendable {
    case user
    case assistant
}

struct ChatMessage: Identifiable, Equatable, Sendable {
    let id: UUID
    let role: MessageRole
    var content: String
    let timestamp: Date

    init(id: UUID = UUID(), role: MessageRole, content: String, timestamp: Date = Date()) {
        self.id = id
        self.role = role
        self.content = content
        self.timestamp = timestamp
    }
}

extension ChatMessage: Codable {
    enum CodingKeys: String, CodingKey {
        case id, role, content, timestamp
    }
}

extension ChatMessage {
    static let previewUser = ChatMessage(
        role: .user,
        content: "Can you help me refactor the authentication module?"
    )

    static let previewAssistant = ChatMessage(
        role: .assistant,
        content: """
        I'll help you refactor the authentication module. Let me first explore the current implementation to understand the structure.

        Looking at the codebase, I can see:
        - `AuthService.swift` handles the main authentication logic
        - `TokenManager.swift` manages JWT tokens
        - `KeychainWrapper.swift` provides secure storage

        Here's my suggested approach:

        1. Extract the token refresh logic into a separate `TokenRefreshService`
        2. Implement a proper state machine for auth states
        3. Add better error handling with typed errors

        Let me start implementing these changes...
        """
    )

    static let previewConversation: [ChatMessage] = [
        previewUser,
        previewAssistant,
        ChatMessage(role: .user, content: "That sounds good! Please proceed."),
        ChatMessage(role: .assistant, content: "I'm now making the changes to the authentication module. I'll update you as I progress through each file.")
    ]
}
