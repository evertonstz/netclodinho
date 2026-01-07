import SwiftUI

struct WorkspaceView: View {
    let sessionId: String

    @Environment(SessionStore.self) private var sessionStore
    @Environment(WebSocketService.self) private var webSocketService

    @State private var selectedTab: WorkspaceTab = .chat

    enum WorkspaceTab: String, CaseIterable {
        case chat = "Chat"
        case terminal = "Terminal"
    }

    var session: Session? {
        sessionStore.sessions.first { $0.id == sessionId }
    }

    var body: some View {
        ZStack {
            WarmGradientBackground()

            Group {
                switch selectedTab {
                case .chat:
                    ChatView(sessionId: sessionId)
                case .terminal:
                    TerminalView(sessionId: sessionId)
                }
            }
        }
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .principal) {
                Picker("Tab", selection: $selectedTab) {
                    ForEach(WorkspaceTab.allCases, id: \.self) { tab in
                        Text(tab.rawValue).tag(tab)
                    }
                }
                .pickerStyle(.segmented)
                .frame(width: 180)
            }

            ToolbarItem(placement: .topBarTrailing) {
                Menu {
                    if let session {
                        if session.status == .paused {
                            Button {
                                webSocketService.send(.sessionResume(id: sessionId))
                            } label: {
                                Label("Resume Session", systemImage: "play.fill")
                            }
                        } else if session.status == .ready || session.status == .running {
                            Button {
                                webSocketService.send(.sessionPause(id: sessionId))
                            } label: {
                                Label("Pause Session", systemImage: "pause.fill")
                            }
                        }
                    }
                } label: {
                    Image(systemName: "ellipsis.circle")
                }
            }
        }
        .onAppear {
            sessionStore.setCurrentSession(id: sessionId)
        }
        .task {
            // Wait for connection before opening session
            // This fetches messages and events from server
            while !webSocketService.connectionState.isConnected {
                try? await Task.sleep(nanoseconds: 100_000_000) // 0.1s
            }
            webSocketService.openSession(id: sessionId)
        }
        .onDisappear {
            sessionStore.setCurrentSession(id: nil)
        }
        .toolbar(.hidden, for: .tabBar)
    }
}

// MARK: - Preview

#Preview {
    let sessionStore = SessionStore()
    sessionStore.setSessions(Session.previewList)

    return NavigationStack {
        WorkspaceView(sessionId: "sess1")
    }
    .environment(sessionStore)
    .environment(ChatStore())
    .environment(EventStore())
    .environment(TerminalStore())
    .environment(SettingsStore())
    .environment(WebSocketService())
    .environment(MessageRouter.preview)
}
