import SwiftUI

struct SessionRow: View {
    let session: Session
    let onDelete: () -> Void

    @Environment(ChatStore.self) private var chatStore
    @Environment(WebSocketService.self) private var webSocketService
    @Environment(SettingsStore.self) private var settingsStore

    private var messageCount: Int {
        chatStore.messages(for: session.id).count
    }

    private var isDimmed: Bool {
        session.status == .paused || session.status == .error
    }

    var body: some View {
        HStack(spacing: Theme.Spacing.sm) {
            // Status dot (pulses if running)
            Circle()
                .fill(session.status.tintColor.color)
                .frame(width: 8, height: 8)
                .pulsing(session.status == .running)

            // Name + time
            VStack(alignment: .leading, spacing: 2) {
                Text(session.name)
                    .font(.netclodeHeadline)
                    .lineLimit(1)

                Text(session.lastActiveAt.formatted(.relative(presentation: .named)))
                    .font(.netclodeCaption)
                    .foregroundStyle(.secondary)
            }

            Spacer()

            // Message count
            if messageCount > 0 {
                Text("\(messageCount) msgs")
                    .font(.netclodeCaption)
                    .foregroundStyle(.tertiary)
            }
        }
        .padding(.vertical, Theme.Spacing.xs)
        .padding(.horizontal, Theme.Spacing.sm)
        .opacity(isDimmed ? 0.6 : 1.0)
        .contentShape(Rectangle())
        .swipeActions(edge: .trailing, allowsFullSwipe: true) {
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
                onDelete()
            } label: {
                Label("Delete", systemImage: "trash")
            }
        }
    }
}

// MARK: - Preview

#Preview {
    List {
        SessionRow(session: Session.previewList[0], onDelete: {})
        SessionRow(session: Session.previewList[1], onDelete: {})
        SessionRow(session: Session.previewList[2], onDelete: {})
        SessionRow(session: Session.previewList[3], onDelete: {})
    }
    .listStyle(.plain)
    .background(Theme.Colors.background)
    .environment(ChatStore())
    .environment(WebSocketService())
    .environment(SettingsStore())
}
