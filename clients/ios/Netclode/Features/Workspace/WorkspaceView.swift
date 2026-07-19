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
        // The terminal view can be freely destroyed on tab switches: the
        // bridge-owned UITerminalView (and its ghostty surface, thanks to the
        // patched wrapper preserving surfaces across window detach) keeps all
        // terminal state; SwiftUI only re-presents it.
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
                .scaleEffect(0.95)
            }

            ToolbarItem(placement: .topBarTrailing) {
                Menu {
                    if let session {
                        // Session info section
                        Section {
                            Label(session.status.displayName, systemImage: session.status.systemImage)

                            if !session.repos.isEmpty {
                                let displayRepos = session.repos.map {
                                    $0.replacingOccurrences(of: "https://github.com/", with: "")
                                        .replacingOccurrences(of: ".git", with: "")
                                }
                                let summary = displayRepos.count > 1 ? "\(displayRepos[0]) (+\(displayRepos.count - 1))" : displayRepos[0]
                                Label(summary, systemImage: "arrow.triangle.branch")
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

                        // Repo access picker (only show if session has repos)
                        if !session.repos.isEmpty {
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

            // Pre-spawn the terminal PTY while the user is still on Chat:
            // the zmx daemon + shell boot in the VM (hundreds of ms over the
            // network) so the prompt is already buffered client-side when the
            // Terminal tab is first opened. The dimensions are corrected by
            // the real surface resize on first render; ghostty reflows.
            if session?.status == .ready || session?.status == .running {
                let bridge = terminalStore.bridge(for: sessionId)
                connectService.send(.terminalResize(
                    sessionId: sessionId,
                    cols: bridge.cols > 0 ? bridge.cols : 80,
                    rows: bridge.rows > 0 ? bridge.rows : 24
                ))
            }
        }
        .onChange(of: connectService.connectionState) { oldState, newState in
            // Detect reconnection: was disconnected/reconnecting, now connected
            let wasDisconnected = !oldState.isConnected
            let isNowConnected = newState.isConnected

            if wasDisconnected && isNowConnected && hasOpenedSession {
                // Reconnected - fetch full session state (no cursor = full history)
                print("[WorkspaceView] Reconnected, reopening session (full refresh)")
                connectService.openSession(id: sessionId, resume: false)
                // (PTY size re-sync on reconnect is owned by TerminalStore.)
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
