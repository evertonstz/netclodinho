import XCTest
@testable import Netclode

final class MessageRouterTests: XCTestCase {

    // MARK: - Session Update Tests

    @MainActor
    func testSessionUpdatedUpdatesExistingSession() {
        // Given: A router with a session in the store
        let sessionStore = SessionStore()
        let chatStore = ChatStore()
        let eventStore = EventStore()
        let terminalStore = TerminalStore()
        let connectService = ConnectService()

        let router = MessageRouter(
            connectService: connectService,
            sessionStore: sessionStore,
            chatStore: chatStore,
            eventStore: eventStore,
            terminalStore: terminalStore,
            githubStore: GitHubStore(),
            gitStore: GitStore(),
            modelsStore: UnifiedModelsStore(),
            snapshotStore: SnapshotStore(),
            settingsStore: SettingsStore()
        )

        // Add initial session with default name
        let initialSession = Session(
            id: "sess-1",
            name: "New Session",
            status: .ready,
            repos: [],
            createdAt: Date(),
            lastActiveAt: Date()
        )
        sessionStore.addSession(initialSession)

        // When: Receiving a session.updated message with new name
        let updatedSession = Session(
            id: "sess-1",
            name: "Auto-Generated Title",
            status: .running,
            repos: [],
            createdAt: initialSession.createdAt,
            lastActiveAt: Date()
        )
        router.route(.sessionUpdated(session: updatedSession))

        // Then: Session should be updated in the store
        let storedSession = sessionStore.sessions.first { $0.id == "sess-1" }
        XCTAssertNotNil(storedSession)
        XCTAssertEqual(storedSession?.name, "Auto-Generated Title")
        XCTAssertEqual(storedSession?.status, .running)

        router.stop()
    }

    @MainActor
    func testSessionUpdatedDoesNothingForUnknownSession() {
        // Given: A router with no sessions
        let sessionStore = SessionStore()
        let router = makeRouter(sessionStore: sessionStore)

        XCTAssertTrue(sessionStore.sessions.isEmpty)

        // When: Receiving session.updated for unknown session
        let unknownSession = Session(
            id: "unknown",
            name: "Unknown Session",
            status: .ready,
            repos: [],
            createdAt: Date(),
            lastActiveAt: Date()
        )
        router.route(.sessionUpdated(session: unknownSession))

        // Then: Store should still be empty (updateSession doesn't add new sessions)
        XCTAssertTrue(sessionStore.sessions.isEmpty)

        router.stop()
    }

    // MARK: - Session Created Tests

    @MainActor
    func testSessionCreatedAddsNewSession() {
        let sessionStore = SessionStore()
        let router = makeRouter(sessionStore: sessionStore)

        XCTAssertTrue(sessionStore.sessions.isEmpty)

        // When: Receiving session.created
        let newSession = Session(
            id: "sess-new",
            name: "New Session",
            status: .ready,
            repos: [],
            createdAt: Date(),
            lastActiveAt: Date()
        )
        router.route(.sessionCreated(session: newSession))

        // Then: Session should be added
        XCTAssertEqual(sessionStore.sessions.count, 1)
        XCTAssertEqual(sessionStore.sessions.first?.id, "sess-new")

        router.stop()
    }

    // MARK: - Session Deleted Tests

    @MainActor
    func testSessionDeletedRemovesSession() {
        let sessionStore = SessionStore()
        let chatStore = ChatStore()
        let eventStore = EventStore()
        let terminalStore = TerminalStore()
        let router = makeRouter(
            sessionStore: sessionStore,
            chatStore: chatStore,
            eventStore: eventStore,
            terminalStore: terminalStore
        )

        // Add a session
        let session = Session(
            id: "sess-del",
            name: "To Delete",
            status: .ready,
            repos: [],
            createdAt: Date(),
            lastActiveAt: Date()
        )
        sessionStore.addSession(session)
        chatStore.appendMessage(sessionId: "sess-del", message: ChatMessage(role: .user, content: "Hello"))

        XCTAssertEqual(sessionStore.sessions.count, 1)
        XCTAssertEqual(chatStore.messages(for: "sess-del").count, 1)

        // When: Receiving session.deleted
        router.route(.sessionDeleted(id: "sess-del"))

        // Then: Session and related data should be removed
        XCTAssertTrue(sessionStore.sessions.isEmpty)
        XCTAssertTrue(chatStore.messages(for: "sess-del").isEmpty)

        router.stop()
    }

    // MARK: - Agent Message Tests

    @MainActor
    func testAgentMessagePartialAppendsToChat() {
        let chatStore = ChatStore()
        let sessionStore = SessionStore()
        let router = makeRouter(sessionStore: sessionStore, chatStore: chatStore)

        // When: Receiving partial agent messages
        router.route(.agentMessage(sessionId: "sess-1", content: "Hello ", partial: true, messageId: nil))
        router.route(.agentMessage(sessionId: "sess-1", content: "world!", partial: true, messageId: nil))

        // Then: Should have one message with accumulated content
        let messages = chatStore.messages(for: "sess-1")
        XCTAssertEqual(messages.count, 1)
        XCTAssertEqual(messages.first?.content, "Hello world!")
        XCTAssertEqual(messages.first?.role, .assistant)

        router.stop()
    }

    // MARK: - GitHub Repos Tests

    @MainActor
    func testGitHubReposUpdatesStore() {
        let githubStore = GitHubStore()
        let router = makeRouter(githubStore: githubStore)

        XCTAssertTrue(githubStore.repos.isEmpty)

        // When: Receiving github.repos
        let repos = [
            GitHubRepo(name: "netclode", fullName: "angristan/netclode", isPrivate: false, description: "Self hosted coding agent"),
            GitHubRepo(name: "dotfiles", fullName: "angristan/dotfiles", isPrivate: true, description: nil)
        ]
        router.route(.githubRepos(repos: repos))

        // Then: Store should have repos
        XCTAssertEqual(githubStore.repos.count, 2)
        XCTAssertEqual(githubStore.repos.first?.fullName, "angristan/dotfiles") // Sorted alphabetically
        XCTAssertFalse(githubStore.isLoading)

        router.stop()
    }

    @MainActor
    func testSessionStateSeedsTerminalHistory() {
        let terminalStore = TerminalStore()
        let sessionStore = SessionStore()
        let router = makeRouter(sessionStore: sessionStore, terminalStore: terminalStore)

        let session = Session(
            id: "sess-term",
            name: "Terminal Session",
            status: .ready,
            repos: [],
            createdAt: Date(),
            lastActiveAt: Date()
        )
        router.route(.sessionState(
            session: session,
            messages: [],
            events: [],
            hasMore: false,
            lastNotificationId: nil,
            terminalHistory: "prompt$ ls\r\nfile.txt\r\n"
        ))

        // The persisted scrollback must be seeded into the session's terminal.
        let bridge = terminalStore.bridge(for: "sess-term")
        XCTAssertEqual(bridge.bufferedByteCount, "prompt$ ls\r\nfile.txt\r\n".utf8.count)

        // A second session.state (reconnect refresh) must not duplicate.
        router.route(.sessionState(
            session: session,
            messages: [],
            events: [],
            hasMore: false,
            lastNotificationId: nil,
            terminalHistory: "prompt$ ls\r\nfile.txt\r\n"
        ))
        XCTAssertEqual(bridge.bufferedByteCount, "prompt$ ls\r\nfile.txt\r\n".utf8.count)

        router.stop()
    }

    @MainActor
    func testSessionReadyTransitionDoesNotCreateTerminalBridges() {
        let terminalStore = TerminalStore()
        let sessionStore = SessionStore()
        let router = makeRouter(sessionStore: sessionStore, terminalStore: terminalStore)

        let session = Session(
            id: "sess-ready",
            name: "Becomes Ready",
            status: .creating,
            repos: [],
            createdAt: Date(),
            lastActiveAt: Date()
        )
        sessionStore.addSession(session)

        // Session transitions to ready (e.g. VM resume) — the resize resync
        // must be a no-op for sessions whose terminal was never opened.
        let readySession = Session(
            id: "sess-ready",
            name: "Becomes Ready",
            status: .ready,
            repos: [],
            createdAt: session.createdAt,
            lastActiveAt: Date()
        )
        router.route(.sessionUpdated(session: readySession))
        XCTAssertEqual(terminalStore.bridgeCount, 0)

        router.stop()
    }

    // MARK: - Helpers

    @MainActor
    private func makeRouter(
        sessionStore: SessionStore = SessionStore(),
        chatStore: ChatStore = ChatStore(),
        eventStore: EventStore = EventStore(),
        terminalStore: TerminalStore = TerminalStore(),
        githubStore: GitHubStore = GitHubStore(),
        gitStore: GitStore = GitStore()
    ) -> MessageRouter {
        MessageRouter(
            connectService: ConnectService(),
            sessionStore: sessionStore,
            chatStore: chatStore,
            eventStore: eventStore,
            terminalStore: terminalStore,
            githubStore: githubStore,
            gitStore: gitStore,
            modelsStore: UnifiedModelsStore(),
            snapshotStore: SnapshotStore(),
            settingsStore: SettingsStore()
        )
    }
}
