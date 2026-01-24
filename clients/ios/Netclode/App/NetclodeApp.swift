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
    @State private var connectService: ConnectService
    @State private var messageRouter: MessageRouter

    init() {
        let settings = SettingsStore()
        let sessions = SessionStore()
        let chat = ChatStore()
        let events = EventStore()
        let terminal = TerminalStore()
        let github = GitHubStore()
        let git = GitStore()
        let models = ModelsStore()
        let connect = ConnectService()
        
        // Wire up terminal store to Connect service for input handling
        terminal.connectService = connect
        
        let router = MessageRouter(
            connectService: connect,
            sessionStore: sessions,
            chatStore: chat,
            eventStore: events,
            terminalStore: terminal,
            githubStore: github,
            gitStore: git
        )

        _settingsStore = State(initialValue: settings)
        _sessionStore = State(initialValue: sessions)
        _chatStore = State(initialValue: chat)
        _eventStore = State(initialValue: events)
        _terminalStore = State(initialValue: terminal)
        _githubStore = State(initialValue: github)
        _gitStore = State(initialValue: git)
        _modelsStore = State(initialValue: models)
        _connectService = State(initialValue: connect)
        _messageRouter = State(initialValue: router)
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
                .environment(connectService)
                .environment(messageRouter)
                .preferredColorScheme(settingsStore.preferredColorScheme)
                .onAppear {
                    if !settingsStore.serverURL.isEmpty {
                        connectService.connect(to: settingsStore.serverURL, connectPort: settingsStore.connectPort)
                    }
                }
        }
        .onChange(of: scenePhase) { _, newPhase in
            handleScenePhaseChange(newPhase)
        }
    }

    private func handleScenePhaseChange(_ phase: ScenePhase) {
        switch phase {
        case .active:
            // App came to foreground - ensure connection is alive
            print("[App] Scene became active, checking connection")
            if !settingsStore.serverURL.isEmpty {
                connectService.ensureConnected(to: settingsStore.serverURL, connectPort: settingsStore.connectPort)
            }
        case .inactive:
            // Transitioning (e.g., control center opened)
            print("[App] Scene became inactive")
        case .background:
            // App went to background - iOS may suspend network
            print("[App] Scene went to background")
        @unknown default:
            break
        }
    }
}
