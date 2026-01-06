import SwiftUI

struct SettingsView: View {
    @Environment(SettingsStore.self) private var settingsStore
    @Environment(WebSocketService.self) private var webSocketService

    var body: some View {
        ZStack {
            WarmGradientBackground()

            ScrollView {
                VStack(spacing: Theme.Spacing.lg) {
                    // Server Configuration
                    ServerConfigSection()

                    // Appearance
                    AppearanceSection()

                    // About
                    AboutSection()
                }
                .padding()
            }
        }
        .navigationTitle("Settings")
    }
}

// MARK: - Server Configuration Section

struct ServerConfigSection: View {
    @Environment(SettingsStore.self) private var settingsStore
    @Environment(WebSocketService.self) private var webSocketService

    @State private var serverURL: String = ""
    @State private var isConnecting = false

    var body: some View {
        GlassCard {
            VStack(alignment: .leading, spacing: Theme.Spacing.md) {
                // Section header
                HStack {
                    Image(systemName: "server.rack")
                        .foregroundStyle(Theme.Colors.cozyPurple)

                    Text("Server")
                        .font(.netclodeHeadline)
                }

                // Server URL input
                VStack(alignment: .leading, spacing: Theme.Spacing.xs) {
                    Text("Server URL")
                        .font(.netclodeCaption)
                        .foregroundStyle(.secondary)

                    GlassTextField(
                        "netclode.your-tailnet.ts.net:3000",
                        text: $serverURL,
                        icon: "link"
                    )
                    .textInputAutocapitalization(.never)
                    .keyboardType(.URL)
                    .autocorrectionDisabled()

                    Text("Include port if needed (default: 3000)")
                        .font(.netclodeCaption)
                        .foregroundStyle(.tertiary)
                }

                // Connection status
                HStack {
                    ConnectionStatusBadge(state: webSocketService.connectionState)

                    Spacer()

                    GlassButton(
                        webSocketService.connectionState.isConnected ? "Reconnect" : "Connect",
                        icon: "arrow.triangle.2.circlepath",
                        isLoading: isConnecting
                    ) {
                        connect()
                    }
                }
            }
        }
        .onAppear {
            serverURL = settingsStore.serverURL
        }
    }

    private func connect() {
        guard !serverURL.isEmpty else { return }

        isConnecting = true
        settingsStore.serverURL = serverURL

        // Disconnect if already connected
        if webSocketService.connectionState.isConnected {
            webSocketService.disconnect()
        }

        // Connect with slight delay for UI feedback
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.3) {
            webSocketService.connect(to: serverURL)
            isConnecting = false
        }
    }
}

// MARK: - Appearance Section

struct AppearanceSection: View {
    @Environment(SettingsStore.self) private var settingsStore

    var body: some View {
        @Bindable var settings = settingsStore

        GlassCard {
            VStack(alignment: .leading, spacing: Theme.Spacing.md) {
                // Section header
                HStack {
                    Image(systemName: "paintbrush.fill")
                        .foregroundStyle(Theme.Colors.warmApricot)

                    Text("Appearance")
                        .font(.netclodeHeadline)
                }

                // Color scheme picker
                VStack(alignment: .leading, spacing: Theme.Spacing.xs) {
                    Text("Color Scheme")
                        .font(.netclodeCaption)
                        .foregroundStyle(.secondary)

                    Picker("Color Scheme", selection: $settings.preferredColorScheme) {
                        Text("System").tag(nil as ColorScheme?)
                        Text("Light").tag(ColorScheme.light as ColorScheme?)
                        Text("Dark").tag(ColorScheme.dark as ColorScheme?)
                    }
                    .pickerStyle(.segmented)
                }

                // Haptic feedback toggle
                Toggle(isOn: $settings.hapticFeedbackEnabled) {
                    HStack {
                        Image(systemName: "hand.tap.fill")
                            .foregroundStyle(Theme.Colors.cozyPurple)

                        VStack(alignment: .leading, spacing: 2) {
                            Text("Haptic Feedback")
                                .font(.netclodeBody)

                            Text("Vibration on interactions")
                                .font(.netclodeCaption)
                                .foregroundStyle(.secondary)
                        }
                    }
                }
                .tint(Theme.Colors.cozyPurple)
            }
        }
    }
}

// MARK: - About Section

struct AboutSection: View {
    var body: some View {
        GlassCard {
            VStack(alignment: .leading, spacing: Theme.Spacing.md) {
                // Section header
                HStack {
                    Image(systemName: "info.circle.fill")
                        .foregroundStyle(Theme.Colors.gentleBlue)

                    Text("About")
                        .font(.netclodeHeadline)
                }

                // App info
                VStack(spacing: Theme.Spacing.sm) {
                    HStack {
                        Text("Version")
                            .foregroundStyle(.secondary)
                        Spacer()
                        Text("1.0.0")
                    }
                    .font(.netclodeBody)

                    Divider()

                    HStack {
                        Text("Build")
                            .foregroundStyle(.secondary)
                        Spacer()
                        Text("1")
                    }
                    .font(.netclodeBody)
                }

                // Description
                VStack(alignment: .leading, spacing: Theme.Spacing.xs) {
                    Text("Netclode")
                        .font(.netclodeSubheadline)

                    Text("A self-hosted Claude Code Cloud platform. Run AI coding agents in persistent sandboxed environments, accessible from anywhere on your Tailscale network.")
                        .font(.netclodeCaption)
                        .foregroundStyle(.secondary)
                }

                // Links
                VStack(spacing: Theme.Spacing.sm) {
                    Link(destination: URL(string: "https://github.com/your-username/netclode")!) {
                        HStack {
                            Image(systemName: "chevron.left.forwardslash.chevron.right")
                            Text("Source Code")
                            Spacer()
                            Image(systemName: "arrow.up.right.square")
                        }
                        .font(.netclodeBody)
                        .foregroundStyle(Theme.Colors.gentleBlue)
                    }
                }
            }
        }
    }
}

// MARK: - Preview

#Preview {
    NavigationStack {
        SettingsView()
    }
    .environment(SettingsStore())
    .environment(WebSocketService())
}
