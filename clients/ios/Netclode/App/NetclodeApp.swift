import SwiftUI

@main
struct NetclodeApp: App {
    @Environment(\.scenePhase) private var scenePhase

    @State private var sessionStore = SessionStore()
    @State private var chatStore = ChatStore()
    @State private var eventStore = EventStore()
    @State private var terminalStore = TerminalStore()
    @State private var settingsStore = SettingsStore()
    @State private var githubStore = GitHubStore()
    @State private var gitStore = GitStore()
    @State private var modelsStore = ModelsStore()
    @State private var copilotStore = CopilotStore()
    @State private var snapshotStore = SnapshotStore()
    @State private var connectService: ConnectService
    @State private var messageRouter: MessageRouter
    @State private var coordinator: AppStateCoordinator

    init() {
        let settings = SettingsStore()
        let sessions = SessionStore()
        let chat = ChatStore()
        let events = EventStore()
        let terminal = TerminalStore()
        let github = GitHubStore()
        let git = GitStore()
        let models = ModelsStore()
        let copilot = CopilotStore()
        let snapshots = SnapshotStore()
        let connect = ConnectService()
        let appCoordinator = AppStateCoordinator()
        
        // Wire up terminal store to Connect service for input handling
        terminal.connectService = connect
        
        let router = MessageRouter(
            connectService: connect,
            sessionStore: sessions,
            chatStore: chat,
            eventStore: events,
            terminalStore: terminal,
            githubStore: github,
            gitStore: git,
            copilotStore: copilot,
            snapshotStore: snapshots
        )

        _settingsStore = State(initialValue: settings)
        _sessionStore = State(initialValue: sessions)
        _chatStore = State(initialValue: chat)
        _eventStore = State(initialValue: events)
        _terminalStore = State(initialValue: terminal)
        _githubStore = State(initialValue: github)
        _gitStore = State(initialValue: git)
        _modelsStore = State(initialValue: models)
        _copilotStore = State(initialValue: copilot)
        _snapshotStore = State(initialValue: snapshots)
        _connectService = State(initialValue: connect)
        _messageRouter = State(initialValue: router)
        _coordinator = State(initialValue: appCoordinator)
    }

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environment(sessionStore)
                .environment(chatStore)
                .environment(eventStore)
                .environment(terminalStore)
                .environment(settingsStore)
                .environment(githubStore)
                .environment(gitStore)
                .environment(modelsStore)
                .environment(copilotStore)
                .environment(snapshotStore)
                .environment(connectService)
                .environment(messageRouter)
                .environment(coordinator)
                .preferredColorScheme(settingsStore.preferredColorScheme)
                .onAppear {
                    setupApp()
                }
        }
        .onChange(of: scenePhase) { _, newPhase in
            coordinator.handleScenePhase(newPhase)
        }
    }

    private func setupApp() {
        // Configure the coordinator with services
        coordinator.configure(
            connectService: connectService,
            sessionStore: sessionStore,
            settingsStore: settingsStore
        )
        
        // Connect if we have a server URL configured
        if !settingsStore.serverURL.isEmpty {
            connectService.connect(to: settingsStore.serverURL, connectPort: settingsStore.connectPort)
        }
    }
}
