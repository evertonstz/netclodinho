import SwiftUI

struct ContentView: View {
    @Environment(\.horizontalSizeClass) private var horizontalSizeClass
    @Environment(SessionStore.self) private var sessionStore
    @State private var selectedSessionId: String?
    @State private var columnVisibility: NavigationSplitViewVisibility = .all

    var body: some View {
        if horizontalSizeClass == .compact {
            // iPhone: Use original NavigationStack with SessionsView
            NavigationStack {
                SessionsView()
            }
            .toolbarBackgroundVisibility(.hidden, for: .navigationBar)
        } else {
            // iPad/Mac: Use NavigationSplitView with sidebar
            NavigationSplitView(columnVisibility: $columnVisibility) {
                SidebarView(selectedSessionId: $selectedSessionId)
            } detail: {
                if let sessionId = selectedSessionId {
                    WorkspaceView(sessionId: sessionId)
                        .id(sessionId)  // Force recreation when session changes
                } else {
                    NoSessionSelectedView()
                }
            }
            .navigationSplitViewStyle(.balanced)
        }
    }
}

// MARK: - Empty State for Detail

struct NoSessionSelectedView: View {
    var body: some View {
        VStack(spacing: Theme.Spacing.lg) {
            Image(systemName: "bubble.left.and.bubble.right")
                .font(.system(size: 64))
                .foregroundStyle(Theme.Colors.brand.opacity(0.4))

            Text("Select a Session")
                .font(.netclodeHeadline)
                .foregroundStyle(.secondary)

            Text("Choose a session from the sidebar or create a new one")
                .font(.netclodeBody)
                .foregroundStyle(.tertiary)
                .multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Theme.Colors.background)
    }
}

#Preview {
    let sessionStore = SessionStore()
    sessionStore.setSessions(Session.previewList)

    return ContentView()
        .environment(sessionStore)
        .environment(ChatStore())
        .environment(EventStore())
        .environment(TerminalStore())
        .environment(SettingsStore())
        .environment(GitStore())
        .environment(ConnectService())
        .environment(MessageRouter.preview)
}
