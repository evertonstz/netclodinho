import SwiftUI

struct WorkspaceView: View {
    let sessionId: String

    @Environment(SessionStore.self) private var sessionStore
    @Environment(WebSocketService.self) private var webSocketService
    @Environment(TerminalStore.self) private var terminalStore
    @Environment(\.dismiss) private var dismiss

    @State private var selectedTab: WorkspaceTab = .chat
    @State private var hasOpenedSession = false
    @State private var showDeleteConfirmation = false

    enum WorkspaceTab: CaseIterable {
        case chat
        case changes
        case terminal
        case previews

        var icon: String {
            switch self {
            case .chat: return "bubble.left.and.bubble.right"
            case .changes: return "arrow.triangle.branch"
            case .terminal: return "terminal"
            case .previews: return "globe"
            }
        }
    }

    var session: Session? {
        sessionStore.sessions.first { $0.id == sessionId }
    }

    var body: some View {
        Group {
            switch selectedTab {
            case .chat:
                ChatView(sessionId: sessionId)
            case .changes:
                ChangesView(sessionId: sessionId)
            case .terminal:
                TerminalView(sessionId: sessionId)
            case .previews:
                PreviewsView(sessionId: sessionId)
            }
        }
        .navigationBarTitleDisplayMode(.inline)
        .toolbarBackgroundVisibility(.hidden, for: .navigationBar)
        .toolbar {
            ToolbarItem(placement: .principal) {
                Picker("Tab", selection: $selectedTab) {
                    ForEach(WorkspaceTab.allCases, id: \.self) { tab in
                        Image(systemName: tab.icon).tag(tab)
                    }
                }
                .pickerStyle(.segmented)
                .frame(width: 180)
            }

            ToolbarItem(placement: .topBarTrailing) {
                Menu {
                    if let session {
                        // Session info section
                        Section {
                            Label(session.status.displayName, systemImage: session.status.systemImage)
                            
                            if let repo = session.repo {
                                Label(repo.replacingOccurrences(of: "https://github.com/", with: ""), systemImage: "arrow.triangle.branch")
                            }
                            
                            Label(session.createdAt.formatted(.relative(presentation: .named)), systemImage: "clock")
                        } header: {
                            Text(session.name)
                        }
                        
                        Divider()
                        
                        // Actions
                        if session.status == .paused {
                            Button {
                                webSocketService.send(.sessionResume(id: sessionId))
                            } label: {
                                Label("Resume", systemImage: "play.fill")
                            }
                        } else if session.status == .ready || session.status == .running {
                            Button {
                                webSocketService.send(.sessionPause(id: sessionId))
                            } label: {
                                Label("Pause", systemImage: "pause.fill")
                            }
                        }
                        
                        Divider()
                        
                        Button(role: .destructive) {
                            showDeleteConfirmation = true
                        } label: {
                            Label("Delete Session", systemImage: "trash")
                        }
                    }
                } label: {
                    Image(systemName: "ellipsis.circle")
                }
            }
        }
        .confirmationDialog(
            "Delete Session",
            isPresented: $showDeleteConfirmation,
            titleVisibility: .visible
        ) {
            Button("Delete", role: .destructive) {
                webSocketService.send(.sessionDelete(id: sessionId))
                dismiss()
            }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("Are you sure you want to delete this session? This action cannot be undone.")
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
            // Initial open - no cursor needed
            // Only resume if actually paused (to avoid brief status flash)
            let session = sessionStore.sessions.first { $0.id == sessionId }
            let needsResume = session?.status == .paused
            webSocketService.openSession(id: sessionId, resume: needsResume)
            hasOpenedSession = true
        }
        .onChange(of: webSocketService.connectionState) { oldState, newState in
            // Detect reconnection: was disconnected/reconnecting, now connected
            let wasDisconnected = !oldState.isConnected
            let isNowConnected = newState.isConnected

            if wasDisconnected && isNowConnected && hasOpenedSession {
                // Reconnected - re-open session with cursor to resume from where we left off
                let cursor = sessionStore.lastNotificationId(for: sessionId)
                let session = sessionStore.sessions.first { $0.id == sessionId }
                let needsResume = session?.status == .paused
                print("[WorkspaceView] Reconnected, reopening session with cursor: \(cursor ?? "nil"), resume: \(needsResume)")
                webSocketService.openSession(id: sessionId, lastNotificationId: cursor, resume: needsResume)
            }
        }
        .onDisappear {
            sessionStore.setCurrentSession(id: nil)
        }
        .onChange(of: selectedTab) { oldTab, newTab in
            // When switching to terminal tab, send resize to ensure PTY is spawned
            if newTab == .terminal {
                let bridge = terminalStore.bridge(for: sessionId)
                if bridge.cols > 0 && bridge.rows > 0 {
                    webSocketService.send(.terminalResize(
                        sessionId: sessionId,
                        cols: bridge.cols,
                        rows: bridge.rows
                    ))
                }
            }
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
    .environment(GitStore())
    .environment(WebSocketService())
    .environment(MessageRouter.preview)
}
