import SwiftUI

struct SessionsView: View {
    @Environment(SessionStore.self) private var sessionStore
    @Environment(WebSocketService.self) private var webSocketService
    @Environment(SettingsStore.self) private var settingsStore

    @State private var showPromptSheet = false
    @State private var showSettingsSheet = false
    @State private var selectedSession: Session?

    var body: some View {
        VStack(spacing: 0) {
            // Main content
            Group {
                if sessionStore.sessions.isEmpty {
                    EmptySessionsView(onCreateTapped: { showPromptSheet = true })
                } else {
                    sessionListContent
                }
            }
            .frame(maxHeight: .infinity)

            // Bottom input bar
            PromptInputBar {
                showPromptSheet = true
            }
        }
        .background(Theme.Colors.background)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .principal) {
                HStack(spacing: 6) {
                    Text("netclode")
                        .font(.system(.headline, design: .monospaced))
                        .fontWeight(.medium)

                    Circle()
                        .fill(connectionColor)
                        .frame(width: 6, height: 6)
                }
                .onChange(of: webSocketService.connectionState) { oldState, newState in
                    handleConnectionChange(from: oldState, to: newState)
                }
            }

            ToolbarItem(placement: .topBarTrailing) {
                Button {
                    showSettingsSheet = true
                } label: {
                    Image(systemName: "gearshape.fill")
                        .font(.system(size: 16))
                        .foregroundStyle(.primary)
                        .frame(width: 34, height: 34)
                        .glassEffect(.regular.interactive(), in: Circle())
                }
            }
        }
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
        }
        .fullScreenCover(isPresented: $showPromptSheet) {
            PromptSheet()
        }
        .navigationDestination(item: $selectedSession) { session in
            WorkspaceView(sessionId: session.id)
        }
        .onChange(of: sessionStore.pendingSessionId) { _, newId in
            // Auto-navigate to newly created session
            if let sessionId = newId,
               let session = sessionStore.sessions.first(where: { $0.id == sessionId }) {
                selectedSession = session
            }
        }
        .onAppear {
            if webSocketService.connectionState.isConnected {
                webSocketService.send(.sessionList)
            }
        }
        .refreshable {
            webSocketService.send(.sessionList)
        }
    }

    private var connectionColor: Color {
        switch webSocketService.connectionState {
        case .connected: .green
        case .connecting, .reconnecting: .orange
        case .disconnected: .red
        }
    }

    private func handleConnectionChange(from oldState: ConnectionState, to newState: ConnectionState) {
        switch newState {
        case .connected:
            HapticFeedback.success()
        case .disconnected where oldState == .connected:
            HapticFeedback.error()
        case .reconnecting(let attempt) where attempt == 1:
            HapticFeedback.warning()
        default:
            break
        }
    }

    private var sessionListContent: some View {
        ScrollView {
            LazyVStack(spacing: Theme.Spacing.sm) {
                ForEach(sessionStore.sortedSessions) { session in
                    SessionRow(session: session)
                        .onTapGesture {
                            if settingsStore.hapticFeedbackEnabled {
                                HapticFeedback.selection()
                            }
                            selectedSession = session
                        }
                        .transition(.glassAppear)
                }
            }
            .padding()
        }
        .animation(.glassSpring, value: sessionStore.sessions.count)
    }
}

// MARK: - Bottom Input Bar

struct PromptInputBar: View {
    let onTap: () -> Void

    var body: some View {
        Button(action: onTap) {
            HStack(spacing: Theme.Spacing.sm) {
                Image(systemName: "plus.circle.fill")
                    .font(.system(size: 20))
                    .foregroundStyle(Theme.Colors.brand)

                Text("Start a new session...")
                    .font(.netclodeBody)
                    .foregroundStyle(.secondary)

                Spacer()

                Image(systemName: "arrow.up.circle.fill")
                    .font(.system(size: 24))
                    .foregroundStyle(Theme.Colors.brand)
            }
            .padding(.horizontal, Theme.Spacing.md)
            .padding(.vertical, Theme.Spacing.sm)
            .glassEffect(.regular.interactive(), in: Capsule())
        }
        .buttonStyle(.plain)
        .padding(.horizontal, Theme.Spacing.md)
        .padding(.vertical, Theme.Spacing.xs)
    }
}

// MARK: - Empty State

struct EmptySessionsView: View {
    let onCreateTapped: () -> Void

    var body: some View {
        VStack(spacing: Theme.Spacing.lg) {
            Image(systemName: "rectangle.stack.badge.plus")
                .font(.system(size: 64))
                .foregroundStyle(Theme.Colors.brand.opacity(0.6))

            VStack(spacing: Theme.Spacing.xs) {
                Text("No Sessions Yet")
                    .font(.netclodeHeadline)

                Text("Start a conversation with Claude to create your first session")
                    .font(.netclodeBody)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
            }

            GlassButton("Start Session", icon: "plus") {
                onCreateTapped()
            }
        }
        .padding()
    }
}

// MARK: - Preview

#Preview {
    NavigationStack {
        SessionsView()
    }
    .environment(SessionStore())
    .environment(ChatStore())
    .environment(EventStore())
    .environment(TerminalStore())
    .environment(SettingsStore())
    .environment(WebSocketService())
    .environment(MessageRouter.preview)
}

#Preview("With Sessions") {
    let store = SessionStore()
    store.setSessions(Session.previewList)

    return NavigationStack {
        SessionsView()
    }
    .environment(store)
    .environment(ChatStore())
    .environment(EventStore())
    .environment(TerminalStore())
    .environment(SettingsStore())
    .environment(WebSocketService())
    .environment(MessageRouter.preview)
}
