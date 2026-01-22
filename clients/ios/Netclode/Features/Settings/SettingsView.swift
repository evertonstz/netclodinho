import SwiftUI

struct SettingsView: View {
    @Environment(SettingsStore.self) private var settingsStore
    @Environment(ConnectService.self) private var connectService

    var body: some View {
        ScrollView {
            VStack(spacing: Theme.Spacing.lg) {
                // Server Configuration
                ServerConfigSection()

                // Appearance
                AppearanceSection()

                // Danger Zone
                DangerZoneSection()

                // About
                AboutSection()
            }
            .padding()
        }
        .background(Theme.Colors.background)
        .navigationTitle("Settings")
    }
}

// MARK: - Server Configuration Section

struct ServerConfigSection: View {
    @Environment(SettingsStore.self) private var settingsStore
    @Environment(ConnectService.self) private var connectService

    @State private var serverURL: String = ""
    @State private var connectPort: String = ""
    @State private var isConnecting = false

    var body: some View {
        GlassCard {
            VStack(alignment: .leading, spacing: Theme.Spacing.md) {
                // Section header
                HStack {
                    Image(systemName: "server.rack")
                        .foregroundStyle(Theme.Colors.brand)

                    Text("Server")
                        .font(.netclodeHeadline)
                }

                // Server URL input
                VStack(alignment: .leading, spacing: Theme.Spacing.xs) {
                    Text("Server URL")
                        .font(.netclodeCaption)
                        .foregroundStyle(.secondary)

                    GlassTextField(
                        "netclode.your-tailnet.ts.net",
                        text: $serverURL,
                        icon: "link"
                    )
                    .textInputAutocapitalization(.never)
                    .keyboardType(.URL)
                    .autocorrectionDisabled()

                    Text("Your Tailscale hostname or IP address")
                        .font(.netclodeCaption)
                        .foregroundStyle(.tertiary)
                }

                // Connect Port override (advanced)
                VStack(alignment: .leading, spacing: Theme.Spacing.xs) {
                    Text("Connect Port (optional)")
                        .font(.netclodeCaption)
                        .foregroundStyle(.secondary)

                    GlassTextField(
                        "Leave empty for auto",
                        text: $connectPort,
                        icon: "number"
                    )
                    .textInputAutocapitalization(.never)
                    .keyboardType(.numberPad)
                    .autocorrectionDisabled()
                    .onChange(of: connectPort) { _, newValue in
                        settingsStore.connectPort = newValue
                    }

                    Text("Override the Connect protocol port (default: auto-detect)")
                        .font(.netclodeCaption)
                        .foregroundStyle(.tertiary)
                }

                // Connection status
                HStack {
                    ConnectionStatusBadge(state: connectService.connectionState)

                    Spacer()

                    GlassButton(
                        connectService.connectionState.isConnected ? "Reconnect" : "Connect",
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
            connectPort = settingsStore.connectPort
        }
    }

    private func connect() {
        guard !serverURL.isEmpty else { return }

        isConnecting = true
        settingsStore.serverURL = serverURL

        // Disconnect if already connected
        if connectService.connectionState.isConnected {
            connectService.disconnect()
        }

        // Connect with slight delay for UI feedback
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.3) {
            connectService.connect(to: serverURL, connectPort: settingsStore.connectPort)
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
                        .foregroundStyle(Theme.Colors.warning)

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
                            .foregroundStyle(Theme.Colors.brand)

                        VStack(alignment: .leading, spacing: 2) {
                            Text("Haptic Feedback")
                                .font(.netclodeBody)

                            Text("Vibration on interactions")
                                .font(.netclodeCaption)
                                .foregroundStyle(.secondary)
                        }
                    }
                }
                .tint(Theme.Colors.brand)
            }
        }
    }
}

// MARK: - Danger Zone Section

struct DangerZoneSection: View {
    @Environment(SettingsStore.self) private var settingsStore
    @Environment(ConnectService.self) private var connectService

    @State private var showDeleteAllConfirmation = false
    @State private var isDeleting = false

    private var isConnected: Bool {
        connectService.connectionState.isConnected
    }

    var body: some View {
        GlassCard {
            VStack(alignment: .leading, spacing: Theme.Spacing.md) {
                // Section header
                HStack {
                    Image(systemName: "exclamationmark.triangle.fill")
                        .foregroundStyle(Theme.Colors.error)

                    Text("Danger Zone")
                        .font(.netclodeHeadline)
                }

                // Delete all sessions button
                VStack(alignment: .leading, spacing: Theme.Spacing.xs) {
                    Text("Delete All Sessions")
                        .font(.netclodeBody)

                    Text("Permanently delete all sessions, including their sandboxes and data. This action cannot be undone.")
                        .font(.netclodeCaption)
                        .foregroundStyle(.secondary)

                    GlassButton(
                        "Delete All Sessions",
                        icon: "trash.fill",
                        tint: Theme.Colors.error,
                        isLoading: isDeleting
                    ) {
                        if settingsStore.hapticFeedbackEnabled {
                            HapticFeedback.warning()
                        }
                        showDeleteAllConfirmation = true
                    }
                    .disabled(!isConnected || isDeleting)
                    .opacity(isConnected ? 1.0 : 0.5)

                    if !isConnected {
                        Text("Connect to server to delete sessions")
                            .font(.netclodeCaption)
                            .foregroundStyle(Theme.Colors.warning)
                    }
                }
            }
        }
        .alert("Delete All Sessions?", isPresented: $showDeleteAllConfirmation) {
            Button("Cancel", role: .cancel) {}
            Button("Delete All", role: .destructive) {
                deleteAllSessions()
            }
        } message: {
            Text("This will permanently delete ALL sessions and their data. This action cannot be undone.")
        }
    }

    private func deleteAllSessions() {
        isDeleting = true
        if settingsStore.hapticFeedbackEnabled {
            HapticFeedback.warning()
        }
        connectService.send(.sessionDeleteAll)
        // Reset after a short delay (server will broadcast the result)
        DispatchQueue.main.asyncAfter(deadline: .now() + 1.0) {
            isDeleting = false
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
                        .foregroundStyle(Theme.Colors.info)

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
                        .foregroundStyle(Theme.Colors.info)
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
    .environment(ConnectService())
}
