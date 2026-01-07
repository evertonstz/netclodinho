import SwiftUI

struct SessionRow: View {
    let session: Session

    @Environment(SessionStore.self) private var sessionStore
    @Environment(WebSocketService.self) private var webSocketService
    @Environment(SettingsStore.self) private var settingsStore

    @State private var showDeleteAlert = false

    var body: some View {
        GlassCard(tint: session.status.tintColor.glassTint.opacity(0.5)) {
            HStack(spacing: Theme.Spacing.md) {
                // Status indicator
                VStack {
                    Image(systemName: session.status.systemImage)
                        .font(.system(size: 24))
                        .foregroundStyle(session.status.tintColor.color)
                        .frame(width: 40, height: 40)
                }

                // Session info
                VStack(alignment: .leading, spacing: Theme.Spacing.xxs) {
                    Text(session.name)
                        .font(.netclodeHeadline)
                        .lineLimit(1)

                    HStack(spacing: Theme.Spacing.sm) {
                        SessionStatusBadge(status: session.status, compact: true)

                        Text(session.lastActiveAt.formatted(.relative(presentation: .named)))
                            .font(.netclodeCaption)
                            .foregroundStyle(.tertiary)
                    }

                    if let repo = session.repo, !repo.isEmpty {
                        Text(repo)
                            .font(.netclodeCaption)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                    }
                }

                Spacer()

                // Arrow indicator
                Image(systemName: "chevron.right")
                    .font(.system(size: 14, weight: .semibold))
                    .foregroundStyle(.tertiary)
            }
        }
        .contentShape(Rectangle())
        .contextMenu {
            if session.status == .paused {
                Button {
                    webSocketService.send(.sessionResume(id: session.id))
                } label: {
                    Label("Resume", systemImage: "play.fill")
                }
            } else if session.status == .ready || session.status == .running {
                Button {
                    webSocketService.send(.sessionPause(id: session.id))
                } label: {
                    Label("Pause", systemImage: "pause.fill")
                }
            }

            Divider()

            Button(role: .destructive) {
                showDeleteAlert = true
            } label: {
                Label("Delete", systemImage: "trash")
            }
        }
        .swipeActions(edge: .trailing, allowsFullSwipe: false) {
            Button(role: .destructive) {
                showDeleteAlert = true
            } label: {
                Label("Delete", systemImage: "trash")
            }

            if session.status == .paused {
                Button {
                    webSocketService.send(.sessionResume(id: session.id))
                } label: {
                    Label("Resume", systemImage: "play.fill")
                }
                .tint(.green)
            } else if session.status == .ready || session.status == .running {
                Button {
                    webSocketService.send(.sessionPause(id: session.id))
                } label: {
                    Label("Pause", systemImage: "pause.fill")
                }
                .tint(.orange)
            }
        }
        .alert("Delete Session?", isPresented: $showDeleteAlert) {
            Button("Cancel", role: .cancel) {}
            Button("Delete", role: .destructive) {
                if settingsStore.hapticFeedbackEnabled {
                    HapticFeedback.warning()
                }
                webSocketService.send(.sessionDelete(id: session.id))
            }
        } message: {
            Text("This will permanently delete \"\(session.name)\" and all its data.")
        }
    }
}

// MARK: - Preview

#Preview {
    ZStack {
        WarmGradientBackground()

        VStack(spacing: 12) {
            SessionRow(session: Session.previewList[0])
            SessionRow(session: Session.previewList[1])
            SessionRow(session: Session.previewList[2])
            SessionRow(session: Session.previewList[3])
        }
        .padding()
    }
    .environment(SessionStore())
    .environment(WebSocketService())
    .environment(SettingsStore())
}
