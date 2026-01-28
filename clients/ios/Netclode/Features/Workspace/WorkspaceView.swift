import SwiftUI

struct WorkspaceView: View {
    let sessionId: String

    @Environment(SessionStore.self) private var sessionStore
    @Environment(ConnectService.self) private var connectService
    @Environment(TerminalStore.self) private var terminalStore
    @Environment(SnapshotStore.self) private var snapshotStore
    @Environment(UnifiedModelsStore.self) private var modelsStore
    @Environment(\.dismiss) private var dismiss

    @State private var selectedTab: WorkspaceTab = .chat
    @State private var hasOpenedSession = false
    @State private var showDeleteConfirmation = false
    @State private var showSnapshotSheet = false

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

    private var sessionModel: CopilotModel? {
        guard let session, let modelId = session.model, let sdkType = session.sdkType else { return nil }
        return modelsStore.model(id: modelId, sdkType: sdkType)
    }

    private var modelDisplayName: String? {
        guard let session, let modelId = session.model else { return nil }
        if let model = sessionModel {
            return model.name
        }
        // Fallback: format the model ID
        return formatModelId(modelId)
    }

    private func formatModelId(_ modelId: String) -> String {
        var id = modelId.contains("/") ? String(modelId.split(separator: "/").last ?? Substring(modelId)) : modelId
        if id.contains(":") {
            id = String(id.split(separator: ":").first ?? Substring(id))
        }
        return id
            .replacingOccurrences(of: "-", with: " ")
            .replacingOccurrences(of: "_", with: " ")
            .split(separator: " ")
            .map { $0.capitalized }
            .joined(separator: " ")
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
                .frame(width: 220)
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
                            
                            if let modelName = modelDisplayName {
                                let effort = sessionModel?.reasoningEffort.map { " \($0)" } ?? ""
                                let provider = sessionModel?.provider.map { " · \($0)" } ?? ""
                                Label("\(modelName)\(effort)\(provider)", systemImage: "cpu")
                            }
                        } header: {
                            Text(session.name)
                        }
                        
                        Divider()
                        
                        // Repo access picker (only show if session has a repo)
                        if session.repo != nil {
                            Menu {
                                ForEach(RepoAccess.allCases, id: \.self) { access in
                                    Button {
                                        connectService.send(.updateRepoAccess(sessionId: sessionId, repoAccess: access))
                                    } label: {
                                        if session.repoAccess == access {
                                            Label(access.displayName, systemImage: "checkmark")
                                        } else {
                                            Text(access.displayName)
                                        }
                                    }
                                }
                            } label: {
                                Label("GitHub Access", systemImage: "lock.shield")
                            }
                            
                            Divider()
                        }
                        
                        // History
                        Button {
                            showSnapshotSheet = true
                        } label: {
                            Label("History", systemImage: "clock.arrow.circlepath")
                        }
                        
                        Divider()
                        
                        // Actions
                        if session.status == .paused {
                            Button {
                                connectService.send(.sessionResume(id: sessionId))
                            } label: {
                                Label("Resume", systemImage: "play.fill")
                            }
                        } else if session.status == .ready || session.status == .running {
                            Button {
                                connectService.send(.sessionPause(id: sessionId))
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
                connectService.send(.sessionDelete(id: sessionId))
                dismiss()
            }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("Are you sure you want to delete this session? This action cannot be undone.")
        }
        .sheet(isPresented: $showSnapshotSheet) {
            SnapshotListSheet(sessionId: sessionId) { snapshotId in
                connectService.send(.restoreSnapshot(sessionId: sessionId, snapshotId: snapshotId))
            }
        }
        .onAppear {
            sessionStore.setCurrentSession(id: sessionId)
        }
        .task {
            // Wait for connection before opening session
            // This fetches messages and events from server
            while !connectService.connectionState.isConnected {
                try? await Task.sleep(nanoseconds: 100_000_000) // 0.1s
            }
            // Initial open - no cursor needed
            // Don't auto-resume: only resume when user sends a new message
            connectService.openSession(id: sessionId, resume: false)
            hasOpenedSession = true
        }
        .onChange(of: connectService.connectionState) { oldState, newState in
            // Detect reconnection: was disconnected/reconnecting, now connected
            let wasDisconnected = !oldState.isConnected
            let isNowConnected = newState.isConnected

            if wasDisconnected && isNowConnected && hasOpenedSession {
                // Reconnected - re-open session with cursor to resume from where we left off
                // Don't auto-resume: only resume when user sends a new message
                let cursor = sessionStore.lastNotificationId(for: sessionId)
                print("[WorkspaceView] Reconnected, reopening session with cursor: \(cursor ?? "nil")")
                connectService.openSession(id: sessionId, lastNotificationId: cursor, resume: false)
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
                    connectService.send(.terminalResize(
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
    .environment(SnapshotStore())
    .environment(ConnectService())
    .environment(UnifiedModelsStore())
    .environment(MessageRouter.preview)
}
