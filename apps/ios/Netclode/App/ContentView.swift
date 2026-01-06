import SwiftUI

struct ContentView: View {
    @Environment(SessionStore.self) private var sessionStore
    @Environment(WebSocketService.self) private var webSocketService
    @State private var selectedTab: AppTab = .sessions

    enum AppTab: String, CaseIterable {
        case sessions = "Sessions"
        case settings = "Settings"
    }

    var body: some View {
        ZStack {
            WarmGradientBackground()

            TabView(selection: $selectedTab) {
                Tab("Sessions", systemImage: "rectangle.stack.fill", value: .sessions) {
                    NavigationStack {
                        SessionsView()
                    }
                }

                Tab("Settings", systemImage: "gearshape.fill", value: .settings) {
                    NavigationStack {
                        SettingsView()
                    }
                }
            }
            .tabViewStyle(.tabBarOnly)
            .tabBarMinimizeBehavior(.onScrollDown)
        }
    }
}

struct WarmGradientBackground: View {
    var body: some View {
        MeshGradient(
            width: 3,
            height: 3,
            points: [
                [0.0, 0.0], [0.5, 0.0], [1.0, 0.0],
                [0.0, 0.5], [0.5, 0.5], [1.0, 0.5],
                [0.0, 1.0], [0.5, 1.0], [1.0, 1.0]
            ],
            colors: [
                Theme.Colors.warmCream,
                Theme.Colors.warmPeach.opacity(0.6),
                Theme.Colors.warmCream,
                Theme.Colors.warmPeach.opacity(0.4),
                Theme.Colors.warmCream,
                Theme.Colors.cozyLavender.opacity(0.3),
                Theme.Colors.cozyLavender.opacity(0.2),
                Theme.Colors.warmCream,
                Theme.Colors.warmPeach.opacity(0.3)
            ]
        )
        .ignoresSafeArea()
    }
}

#Preview {
    ContentView()
        .environment(SessionStore())
        .environment(ChatStore())
        .environment(EventStore())
        .environment(TerminalStore())
        .environment(SettingsStore())
        .environment(WebSocketService())
        .environment(MessageRouter.preview)
}
