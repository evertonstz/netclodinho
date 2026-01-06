import SwiftUI

struct ConnectionStatusBadge: View {
    let state: ConnectionState

    var body: some View {
        HStack(spacing: Theme.Spacing.xs) {
            Circle()
                .fill(statusColor)
                .frame(width: 8, height: 8)
                .overlay {
                    if state == .connecting || state != .connected && state != .disconnected {
                        Circle()
                            .stroke(statusColor.opacity(0.5), lineWidth: 2)
                            .scaleEffect(1.5)
                            .opacity(0.5)
                            .pulsing()
                    }
                }

            Text(state.displayName)
                .font(.netclodeCaption)
                .foregroundStyle(.secondary)
        }
        .padding(.horizontal, Theme.Spacing.sm)
        .padding(.vertical, Theme.Spacing.xxs)
        .glassEffect(
            .regular.tint(statusColor.opacity(0.1)),
            in: .capsule
        )
    }

    private var statusColor: Color {
        switch state {
        case .connected:
            .green
        case .connecting, .reconnecting:
            .orange
        case .disconnected:
            .red
        }
    }
}

// MARK: - Session Status Badge

struct SessionStatusBadge: View {
    let status: SessionStatus
    let compact: Bool

    init(status: SessionStatus, compact: Bool = false) {
        self.status = status
        self.compact = compact
    }

    var body: some View {
        HStack(spacing: Theme.Spacing.xxs) {
            Image(systemName: status.systemImage)
                .font(.system(size: compact ? 10 : 12))
                .foregroundStyle(status.tintColor.color)

            if !compact {
                Text(status.displayName)
                    .font(.netclodeCaption)
                    .foregroundStyle(.secondary)
            }
        }
        .padding(.horizontal, compact ? Theme.Spacing.xs : Theme.Spacing.sm)
        .padding(.vertical, Theme.Spacing.xxs)
        .glassEffect(
            .regular.tint(status.tintColor.glassTint),
            in: .capsule
        )
    }
}

// MARK: - Processing Indicator

struct ProcessingIndicator: View {
    let isProcessing: Bool

    var body: some View {
        if isProcessing {
            HStack(spacing: Theme.Spacing.xs) {
                ProgressView()
                    .tint(Theme.Colors.cozyPurple)

                Text("Processing...")
                    .font(.netclodeCaption)
                    .foregroundStyle(.secondary)
            }
            .padding(.horizontal, Theme.Spacing.sm)
            .padding(.vertical, Theme.Spacing.xxs)
            .glassEffect(
                .regular.tint(Theme.Colors.cozyPurple.opacity(0.1)),
                in: .capsule
            )
            .transition(.glassAppear)
        }
    }
}

// MARK: - Preview

#Preview {
    ZStack {
        WarmGradientBackground()

        VStack(spacing: 20) {
            Text("Connection States")
                .font(.netclodeHeadline)

            ConnectionStatusBadge(state: .connected)
            ConnectionStatusBadge(state: .connecting)
            ConnectionStatusBadge(state: .reconnecting(attempt: 2))
            ConnectionStatusBadge(state: .disconnected)

            Divider()
                .padding(.vertical)

            Text("Session Statuses")
                .font(.netclodeHeadline)

            HStack(spacing: 12) {
                SessionStatusBadge(status: .creating)
                SessionStatusBadge(status: .ready)
                SessionStatusBadge(status: .running)
            }

            HStack(spacing: 12) {
                SessionStatusBadge(status: .paused)
                SessionStatusBadge(status: .error)
            }

            HStack(spacing: 8) {
                SessionStatusBadge(status: .running, compact: true)
                SessionStatusBadge(status: .ready, compact: true)
            }

            Divider()
                .padding(.vertical)

            ProcessingIndicator(isProcessing: true)
        }
        .padding()
    }
}
