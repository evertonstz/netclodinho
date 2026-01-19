import Foundation

/// Mock data for SwiftUI previews
@MainActor
enum PreviewData {

    // MARK: - Sessions

    static let sessions = Session.previewList

    static let activeSession = Session(
        id: "active-session",
        name: "My Active Project",
        status: .running,
        repo: "https://github.com/user/project",
        createdAt: Date().addingTimeInterval(-3600),
        lastActiveAt: Date()
    )

    // MARK: - Messages

    static let conversation: [ChatMessage] = [
        ChatMessage(
            role: .user,
            content: "Help me refactor the authentication module to use JWT tokens",
            timestamp: Date().addingTimeInterval(-600)
        ),
        ChatMessage(
            role: .assistant,
            content: """
            I'll help you refactor the authentication module to use JWT tokens. Let me first explore the current implementation.

            Looking at your codebase, I can see the authentication is currently using session-based auth. Here's my plan:

            1. **Install dependencies**
               ```bash
               npm install jsonwebtoken @types/jsonwebtoken
               ```

            2. **Create JWT utilities** in `src/auth/jwt.ts`
            3. **Update the auth middleware** to verify tokens
            4. **Modify login/logout endpoints**

            Let me start implementing these changes...
            """,
            timestamp: Date().addingTimeInterval(-580)
        ),
        ChatMessage(
            role: .user,
            content: "Looks good! Please proceed.",
            timestamp: Date().addingTimeInterval(-500)
        ),
        ChatMessage(
            role: .assistant,
            content: "I'm now creating the JWT utilities. I'll update you as I make progress.",
            timestamp: Date().addingTimeInterval(-490)
        )
    ]

    // MARK: - Events

    static let eventTimeline: [AgentEvent] = [
        .toolStart(ToolStartEvent(
            id: UUID(),
            timestamp: Date().addingTimeInterval(-300),
            tool: "Glob",
            toolUseId: "tool_1",
            parentToolUseId: nil,
            input: ["pattern": .string("**/auth/**/*.ts")]
        )),
        .toolEnd(ToolEndEvent(
            id: UUID(),
            timestamp: Date().addingTimeInterval(-298),
            tool: "Glob",
            toolUseId: "tool_1",
            parentToolUseId: nil,
            result: "Found 5 files",
            error: nil
        )),
        .fileChange(FileChangeEvent(
            id: UUID(),
            timestamp: Date().addingTimeInterval(-250),
            path: "/src/auth/jwt.ts",
            action: .create,
            linesAdded: 45,
            linesRemoved: nil
        )),
        .commandStart(CommandStartEvent(
            id: UUID(),
            timestamp: Date().addingTimeInterval(-200),
            command: "npm install jsonwebtoken @types/jsonwebtoken",
            cwd: "/workspace"
        )),
        .commandEnd(CommandEndEvent(
            id: UUID(),
            timestamp: Date().addingTimeInterval(-180),
            command: "npm install jsonwebtoken @types/jsonwebtoken",
            exitCode: 0,
            output: "added 2 packages in 1.2s"
        )),
        .fileChange(FileChangeEvent(
            id: UUID(),
            timestamp: Date().addingTimeInterval(-150),
            path: "/src/auth/middleware.ts",
            action: .edit,
            linesAdded: 20,
            linesRemoved: 15
        )),
        .thinking(ThinkingEvent(
            id: UUID(),
            timestamp: Date().addingTimeInterval(-100),
            thinkingId: "thinking_preview_1",
            content: "The middleware is updated. Now I need to modify the login endpoint to return JWT tokens instead of setting session cookies.",
            partial: false
        ))
    ]

    // MARK: - Terminal Output

    static let terminalOutput = """
    $ cd /workspace
    $ ls -la
    total 48
    drwxr-xr-x  12 user  staff   384 Jan  6 10:30 .
    drwxr-xr-x   5 user  staff   160 Jan  5 14:20 ..
    -rw-r--r--   1 user  staff  1234 Jan  6 10:30 README.md
    -rw-r--r--   1 user  staff  5678 Jan  6 10:25 package.json
    drwxr-xr-x   8 user  staff   256 Jan  6 10:30 src

    $ npm run build
    > project@1.0.0 build
    > tsc && vite build

    vite v5.0.0 building for production...
    ✓ 156 modules transformed.
    dist/index.html                   0.45 kB │ gzip:  0.30 kB
    dist/assets/index-BfX2mN8A.css    8.45 kB │ gzip:  2.10 kB
    dist/assets/index-DiwrgTda.js   245.67 kB │ gzip: 78.32 kB
    ✓ built in 2.34s

    $ npm run test
    > project@1.0.0 test
    > vitest run

     ✓ src/auth/jwt.test.ts (5)
     ✓ src/auth/middleware.test.ts (3)
     ✓ src/api/routes.test.ts (12)

     Test Files  3 passed (3)
          Tests  20 passed (20)
       Duration  1.23s

    $\u{0020}
    """

    // MARK: - Populated Stores

    static func populatedSessionStore() -> SessionStore {
        let store = SessionStore()
        store.setSessions(sessions)
        return store
    }

    static func populatedChatStore() -> ChatStore {
        let store = ChatStore()
        for message in conversation {
            store.appendMessage(sessionId: "active-session", message: message)
        }
        return store
    }

    static func populatedEventStore() -> EventStore {
        let store = EventStore()
        for event in eventTimeline {
            store.appendEvent(sessionId: "active-session", event: event)
        }
        return store
    }

    static func populatedTerminalStore() -> TerminalStore {
        let store = TerminalStore()
        store.appendOutput(sessionId: "active-session", data: terminalOutput)
        return store
    }
}
