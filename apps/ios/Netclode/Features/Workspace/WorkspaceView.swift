import SwiftUI

struct WorkspaceView: View {
    let sessionId: String

    @Environment(SessionStore.self) private var sessionStore
    @Environment(WebSocketService.self) private var webSocketService

    @State private var selectedTab: WorkspaceTab = .chat

    enum WorkspaceTab: String, CaseIterable {
        case chat = "Chat"
        case terminal = "Terminal"
        case events = "Events"

        var systemImage: String {
            switch self {
            case .chat: "bubble.left.and.bubble.right.fill"
            case .terminal: "terminal.fill"
            case .events: "list.bullet.rectangle.fill"
            }
        }
    }

    var session: Session? {
        sessionStore.sessions.first { $0.id == sessionId }
    }

    var body: some View {
        ZStack {
            WarmGradientBackground()

            TabView(selection: $selectedTab) {
                Tab(WorkspaceTab.chat.rawValue, systemImage: WorkspaceTab.chat.systemImage, value: .chat) {
                    ChatView(sessionId: sessionId)
                }

                Tab(WorkspaceTab.terminal.rawValue, systemImage: WorkspaceTab.terminal.systemImage, value: .terminal) {
                    TerminalView(sessionId: sessionId)
                }

                Tab(WorkspaceTab.events.rawValue, systemImage: WorkspaceTab.events.systemImage, value: .events) {
                    EventsTimelineView(sessionId: sessionId)
                }
            }
            .tabViewStyle(.tabBarOnly)
            .tabBarMinimizeBehavior(.onScrollDown)
        }
        .navigationTitle(session?.name ?? "Workspace")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .principal) {
                if let session {
                    HStack(spacing: Theme.Spacing.xs) {
                        Text(session.name)
                            .font(.netclodeHeadline)

                        SessionStatusBadge(status: session.status, compact: true)
                    }
                }
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
            // Resume session when entering workspace
            if let session, session.status == .paused {
                webSocketService.send(.sessionResume(id: sessionId))
            }
            sessionStore.setCurrentSession(id: sessionId)
        }
        .onDisappear {
            sessionStore.setCurrentSession(id: nil)
        }
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
