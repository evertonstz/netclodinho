import SwiftUI

@main
struct NetclodeApp: App {
    @Environment(\.scenePhase) private var scenePhase

    @State private var sessionStore = SessionStore()
    @State private var chatStore = ChatStore()
    @State private var eventStore = EventStore()
    @State private var terminalStore = TerminalStore()
    @State private var settingsStore = SettingsStore()
    @State private var webSocketService: WebSocketService
    @State private var messageRouter: MessageRouter

    init() {
        let settings = SettingsStore()
        let sessions = SessionStore()
        let chat = ChatStore()
        let events = EventStore()
        let terminal = TerminalStore()
        let ws = WebSocketService()
        let router = MessageRouter(
            webSocketService: ws,
            sessionStore: sessions,
            chatStore: chat,
            eventStore: events,
            terminalStore: terminal
        )

        _settingsStore = State(initialValue: settings)
        _sessionStore = State(initialValue: sessions)
        _chatStore = State(initialValue: chat)
        _eventStore = State(initialValue: events)
        _terminalStore = State(initialValue: terminal)
        _webSocketService = State(initialValue: ws)
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
                .environment(webSocketService)
                .environment(messageRouter)
                .preferredColorScheme(settingsStore.preferredColorScheme)
                .onAppear {
                    if !settingsStore.serverURL.isEmpty {
                        webSocketService.connect(to: settingsStore.serverURL)
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
                webSocketService.ensureConnected(to: settingsStore.serverURL)
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
