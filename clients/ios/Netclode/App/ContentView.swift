import SwiftUI

struct ContentView: View {
    var body: some View {
        NavigationStack {
            SessionsView()
        }
        .toolbarBackgroundVisibility(.hidden, for: .navigationBar)
    }
}

#Preview {
    ContentView()
        .environment(SessionStore())
        .environment(ChatStore())
        .environment(EventStore())
        .environment(TerminalStore())
        .environment(SettingsStore())
        .environment(ConnectService())
        .environment(MessageRouter.preview)
}
