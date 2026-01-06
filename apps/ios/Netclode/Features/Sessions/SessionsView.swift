import SwiftUI

struct SessionsView: View {
    @Environment(SessionStore.self) private var sessionStore
    @Environment(WebSocketService.self) private var webSocketService
    @Environment(SettingsStore.self) private var settingsStore

    @State private var showCreateSheet = false
    @State private var selectedSession: Session?

    var body: some View {
        ZStack {
            WarmGradientBackground()

            Group {
                if sessionStore.sessions.isEmpty {
                    EmptySessionsView(onCreateTapped: { showCreateSheet = true })
                } else {
                    sessionListContent
                }
            }

            // Floating action button
            VStack {
                Spacer()
                HStack {
                    Spacer()
                    FloatingActionButton(icon: "plus") {
                        showCreateSheet = true
                    }
                }
            }
            .padding()
        }
        .navigationTitle("Sessions")
        .toolbar {
            ToolbarItem(placement: .topBarLeading) {
                ConnectionStatusBadge(state: webSocketService.connectionState)
            }

            ToolbarItem(placement: .topBarTrailing) {
                Button {
                    webSocketService.send(.sessionList)
                } label: {
                    Image(systemName: "arrow.clockwise")
                }
            }
        }
        .sheet(isPresented: $showCreateSheet) {
            CreateSessionSheet()
        }
        .navigationDestination(item: $selectedSession) { session in
            WorkspaceView(sessionId: session.id)
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
            .padding(.bottom, 80) // Space for FAB
        }
        .animation(.glassSpring, value: sessionStore.sessions.count)
    }
}

// MARK: - Empty State

struct EmptySessionsView: View {
    let onCreateTapped: () -> Void

    var body: some View {
        VStack(spacing: Theme.Spacing.lg) {
            Image(systemName: "rectangle.stack.badge.plus")
                .font(.system(size: 64))
                .foregroundStyle(Theme.Colors.cozyPurple.opacity(0.6))

            VStack(spacing: Theme.Spacing.xs) {
                Text("No Sessions Yet")
                    .font(.netclodeHeadline)

                Text("Create your first coding session to get started")
                    .font(.netclodeBody)
                    .foregroundStyle(.secondary)
                    .multilineTextAlignment(.center)
            }

            GlassButton("Create Session", icon: "plus") {
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
