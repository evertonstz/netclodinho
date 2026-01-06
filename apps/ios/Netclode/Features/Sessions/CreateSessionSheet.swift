import SwiftUI

struct CreateSessionSheet: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(WebSocketService.self) private var webSocketService
    @Environment(SettingsStore.self) private var settingsStore

    @State private var sessionName = ""
    @State private var repoURL = ""
    @State private var isCreating = false

    var body: some View {
        NavigationStack {
            ZStack {
                WarmGradientBackground()

                ScrollView {
                    VStack(spacing: Theme.Spacing.lg) {
                        // Header illustration
                        VStack(spacing: Theme.Spacing.md) {
                            Image(systemName: "sparkles.rectangle.stack.fill")
                                .font(.system(size: 56))
                                .foregroundStyle(
                                    LinearGradient(
                                        colors: [Theme.Colors.warmApricot, Theme.Colors.cozyPurple],
                                        startPoint: .topLeading,
                                        endPoint: .bottomTrailing
                                    )
                                )

                            Text("New Session")
                                .font(.netclodeTitle)
                        }
                        .padding(.top, Theme.Spacing.xl)

                        // Form fields
                        VStack(spacing: Theme.Spacing.md) {
                            VStack(alignment: .leading, spacing: Theme.Spacing.xs) {
                                Text("Session Name")
                                    .font(.netclodeSubheadline)
                                    .foregroundStyle(.secondary)

                                GlassTextField(
                                    "My Awesome Project",
                                    text: $sessionName,
                                    icon: "tag.fill"
                                )
                            }

                            VStack(alignment: .leading, spacing: Theme.Spacing.xs) {
                                Text("Repository URL (Optional)")
                                    .font(.netclodeSubheadline)
                                    .foregroundStyle(.secondary)

                                GlassTextField(
                                    "https://github.com/user/repo",
                                    text: $repoURL,
                                    icon: "link"
                                )
                                .textInputAutocapitalization(.never)
                                .keyboardType(.URL)
                            }
                        }
                        .padding(.horizontal)

                        // Info card
                        GlassCard(tint: Theme.Colors.gentleBlue.opacity(0.15)) {
                            HStack(spacing: Theme.Spacing.sm) {
                                Image(systemName: "info.circle.fill")
                                    .foregroundStyle(Theme.Colors.gentleBlue)

                                VStack(alignment: .leading, spacing: 2) {
                                    Text("About Sessions")
                                        .font(.netclodeSubheadline)

                                    Text("Each session runs in an isolated sandbox with its own workspace. Your data persists even when paused.")
                                        .font(.netclodeCaption)
                                        .foregroundStyle(.secondary)
                                }
                            }
                        }
                        .padding(.horizontal)

                        Spacer(minLength: Theme.Spacing.xl)

                        // Create button
                        GlassButton(
                            "Create Session",
                            icon: "plus.circle.fill",
                            tint: Theme.Colors.warmApricot.opacity(0.4),
                            isLoading: isCreating
                        ) {
                            createSession()
                        }
                        .padding(.horizontal)
                        .padding(.bottom, Theme.Spacing.lg)
                    }
                }
            }
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") {
                        dismiss()
                    }
                }
            }
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
    }

    private func createSession() {
        isCreating = true

        if settingsStore.hapticFeedbackEnabled {
            HapticFeedback.medium()
        }

        let name = sessionName.isEmpty ? nil : sessionName
        let repo = repoURL.isEmpty ? nil : repoURL

        webSocketService.send(.sessionCreate(name: name, repo: repo))

        // Dismiss after a short delay to show the loading state
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.5) {
            dismiss()
        }
    }
}

// MARK: - Preview

#Preview {
    CreateSessionSheet()
        .environment(WebSocketService())
        .environment(SettingsStore())
}
