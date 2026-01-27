import SwiftUI

struct SidebarView: View {
    @Environment(SessionStore.self) private var sessionStore
    @Environment(ConnectService.self) private var connectService
    @Environment(SettingsStore.self) private var settingsStore
    @Environment(GitHubStore.self) private var githubStore

    @Binding var selectedSessionId: String?
    @State private var showPromptSheet = false
    @State private var showSettingsSheet = false
    @State private var sessionToDelete: Session?

    var body: some View {
        ScrollView {
            LazyVStack(spacing: Theme.Spacing.xs) {
                ForEach(sessionStore.sortedSessions) { session in
                    SidebarSessionRow(
                        session: session,
                        isSelected: selectedSessionId == session.id,
                        onSelect: { selectedSessionId = session.id },
                        onDelete: { sessionToDelete = session }
                    )
                }
            }
            .padding(.horizontal, Theme.Spacing.sm)
            .padding(.top, Theme.Spacing.xs)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Theme.Colors.background)
        .safeAreaInset(edge: .top) {
            SidebarHeader()
        }
        .safeAreaInset(edge: .bottom) {
            SidebarFooter(
                onNewSession: { showPromptSheet = true },
                onSettings: { showSettingsSheet = true }
            )
        }
        .navigationTitle("")
        .navigationBarHidden(true)
        .sheet(isPresented: $showSettingsSheet) {
            NavigationStack {
                SettingsView()
                    .navigationBarTitleDisplayMode(.inline)
                    .toolbar {
                        ToolbarItem(placement: .confirmationAction) {
                            Button("Done") {
                                showSettingsSheet = false
                            }
                        }
                    }
            }
            .environment(connectService)
            .environment(settingsStore)
        }
        .fullScreenCover(isPresented: $showPromptSheet) {
            PromptSheet()
                .environment(connectService)
                .environment(settingsStore)
                .environment(sessionStore)
                .environment(githubStore)
        }
        .alert("Delete Session?", isPresented: .init(
            get: { sessionToDelete != nil },
            set: { if !$0 { sessionToDelete = nil } }
        )) {
            Button("Cancel", role: .cancel) {
                sessionToDelete = nil
            }
            Button("Delete", role: .destructive) {
                if let session = sessionToDelete {
                    if settingsStore.hapticFeedbackEnabled {
                        HapticFeedback.warning()
                    }
                    let sessionId = session.id
                    // Clear selection if deleting selected session
                    if selectedSessionId == sessionId {
                        selectedSessionId = nil
                    }
                    withAnimation {
                        sessionStore.removeSession(id: sessionId)
                    }
                    connectService.send(.sessionDelete(id: sessionId))
                    sessionToDelete = nil
                }
            }
        } message: {
            if let session = sessionToDelete {
                Text("This will permanently delete \"\(session.name)\" and all its data.")
            }
        }
        .onAppear {
            if connectService.connectionState.isConnected {
                connectService.send(.sessionList)
            }
        }
        .refreshable {
            // Reconnect if needed, then refresh session list
            if !connectService.connectionState.isConnected {
                connectService.ensureConnected(to: settingsStore.serverURL)
                // Wait for connection to actually establish
                let connected = await connectService.waitForConnection(timeout: 15)
                guard connected else { return }
            }
            connectService.send(.sessionList)
        }
        .onChange(of: sessionStore.pendingSessionId) { _, newId in
            // Auto-select newly created session
            if let sessionId = newId {
                selectedSessionId = sessionId
            }
        }
    }
}

// MARK: - Sidebar Header

struct SidebarHeader: View {
    @Environment(ConnectService.self) private var connectService

    private var connectionColor: Color {
        switch connectService.connectionState {
        case .connected: .green
        case .connecting, .reconnecting: .orange
        case .disconnected, .suspended: .red
        }
    }

    private var connectionText: String {
        switch connectService.connectionState {
        case .connected: "Connected"
        case .connecting: "Connecting..."
        case .reconnecting: "Reconnecting..."
        case .disconnected: "Disconnected"
        case .suspended: "Suspended"
        }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Spacing.xs) {
            HStack(spacing: Theme.Spacing.xs) {
                Text("netclode")
                    .font(.system(size: 20, weight: .semibold, design: .monospaced))
                    .foregroundStyle(.primary)

                Circle()
                    .fill(connectionColor)
                    .frame(width: 8, height: 8)
                    .pulsing(connectService.connectionState.isConnected)

                Spacer()
            }

            Text(connectionText)
                .font(.netclodeCaption)
                .foregroundStyle(.secondary)
        }
        .padding(.horizontal, Theme.Spacing.md)
        .padding(.vertical, Theme.Spacing.sm)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.ultraThinMaterial)
    }
}

// MARK: - Sidebar Footer

struct SidebarFooter: View {
    let onNewSession: () -> Void
    let onSettings: () -> Void

    @State private var isHoveringNew = false
    @State private var isHoveringSettings = false

    var body: some View {
        HStack(spacing: Theme.Spacing.sm) {
            Button(action: onNewSession) {
                HStack(spacing: Theme.Spacing.xs) {
                    Image(systemName: "plus")
                        .font(.system(size: 14, weight: .medium))
                    Text("New Session")
                        .font(.netclodeBody)
                }
                .foregroundStyle(isHoveringNew ? .primary : .secondary)
                .padding(.horizontal, Theme.Spacing.sm)
                .padding(.vertical, Theme.Spacing.xs)
                .background {
                    if isHoveringNew {
                        RoundedRectangle(cornerRadius: Theme.Radius.sm)
                            .fill(.quaternary)
                    }
                }
            }
            .buttonStyle(.plain)
            .onHover { isHoveringNew = $0 }

            Spacer()

            Button(action: onSettings) {
                Image(systemName: "gearshape")
                    .font(.system(size: 16, weight: .medium))
                    .foregroundStyle(isHoveringSettings ? .primary : .secondary)
                    .frame(width: 32, height: 32)
                    .background {
                        if isHoveringSettings {
                            Circle()
                                .fill(.quaternary)
                        }
                    }
            }
            .buttonStyle(.plain)
            .onHover { isHoveringSettings = $0 }
        }
        .padding(.horizontal, Theme.Spacing.sm)
        .padding(.vertical, Theme.Spacing.xs)
        .frame(maxWidth: .infinity)
        .background(.ultraThinMaterial)
    }
}

// MARK: - Sidebar Session Row

struct SidebarSessionRow: View {
    let session: Session
    let isSelected: Bool
    let onSelect: () -> Void
    let onDelete: () -> Void

    @Environment(ChatStore.self) private var chatStore
    @Environment(ConnectService.self) private var connectService
    @State private var isHovering = false

    private var messageCount: Int {
        chatStore.messages(for: session.id).count
    }

    private var isDimmed: Bool {
        session.status == .paused || session.status == .error
    }

    var body: some View {
        Button(action: onSelect) {
            HStack(spacing: Theme.Spacing.sm) {
                // Status indicator
                Circle()
                    .fill(session.status.tintColor.color)
                    .frame(width: 8, height: 8)
                    .pulsing(session.status == .running)

                // Content
                VStack(alignment: .leading, spacing: 3) {
                    Text(session.name)
                        .font(.system(size: 14, weight: isSelected ? .semibold : .medium))
                        .foregroundStyle(isSelected ? .primary : (isDimmed ? .secondary : .primary))
                        .lineLimit(1)

                    HStack(spacing: Theme.Spacing.xs) {
                        Text(session.lastActiveAt.formatted(.relative(presentation: .named)))
                            .font(.netclodeCaption)
                            .foregroundStyle(.secondary)

                        if messageCount > 0 {
                            Text("\(messageCount) msgs")
                                .font(.netclodeCaption)
                                .foregroundStyle(.tertiary)
                        }
                    }
                }

                Spacer(minLength: 0)

                // Running indicator
                if session.status == .running {
                    Image(systemName: "bolt.fill")
                        .font(.system(size: 10))
                        .foregroundStyle(Theme.Colors.brand)
                }
            }
            .padding(.horizontal, Theme.Spacing.sm)
            .padding(.vertical, Theme.Spacing.sm)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background {
                RoundedRectangle(cornerRadius: Theme.Radius.sm)
                    .fill(backgroundFill)
            }
            .overlay {
                if isSelected {
                    RoundedRectangle(cornerRadius: Theme.Radius.sm)
                        .strokeBorder(Theme.Colors.brand.opacity(0.3), lineWidth: 1)
                }
            }
            .contentShape(RoundedRectangle(cornerRadius: Theme.Radius.sm))
        }
        .buttonStyle(.plain)
        .opacity(isDimmed && !isSelected ? 0.7 : 1.0)
        .onHover { isHovering = $0 }
        .animation(.easeOut(duration: 0.15), value: isSelected)
        .animation(.easeOut(duration: 0.1), value: isHovering)
        .contextMenu {
            if session.status == .paused {
                Button {
                    connectService.send(.sessionResume(id: session.id))
                } label: {
                    Label("Resume", systemImage: "play.fill")
                }
            } else if session.status == .ready || session.status == .running {
                Button {
                    connectService.send(.sessionPause(id: session.id))
                } label: {
                    Label("Pause", systemImage: "pause.fill")
                }
            }

            Divider()

            Button(role: .destructive) {
                onDelete()
            } label: {
                Label("Delete", systemImage: "trash")
            }
        }
    }

    private var backgroundFill: some ShapeStyle {
        if isSelected {
            return AnyShapeStyle(Theme.Colors.brand.opacity(0.15))
        } else if isHovering {
            return AnyShapeStyle(Color.primary.opacity(0.05))
        } else {
            return AnyShapeStyle(Color.clear)
        }
    }
}

// MARK: - Preview

#Preview {
    let sessionStore = SessionStore()
    sessionStore.setSessions(Session.previewList)

    return NavigationSplitView {
        SidebarView(selectedSessionId: .constant(nil))
    } detail: {
        Text("Select a session")
    }
    .environment(sessionStore)
    .environment(ChatStore())
    .environment(EventStore())
    .environment(TerminalStore())
    .environment(SettingsStore())
    .environment(GitHubStore())
    .environment(ConnectService())
    .environment(MessageRouter.preview)
}
