import SwiftUI

struct ConnectionStatusBadge: View {
    let state: ConnectionState

    var body: some View {
        HStack(spacing: 6) {
            Text("netclode")
                .font(.system(.headline, design: .monospaced))
                .fontWeight(.medium)

            Circle()
                .fill(statusColor)
                .frame(width: 6, height: 6)
        }
        .animation(.easeInOut(duration: 0.2), value: state)
        .onChange(of: state) { oldState, newState in
            handleStateChange(from: oldState, to: newState)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("netclode, \(accessibilityValue)")
    }

    private var statusColor: Color {
        switch state {
        case .connected:
            .green
        case .connecting, .reconnecting:
            .orange
        case .disconnected, .suspended:
            .red
        }
    }

    private var accessibilityValue: String {
        switch state {
        case .disconnected:
            "disconnected"
        case .connecting:
            "connecting"
        case .connected:
            "connected"
        case .reconnecting:
            "reconnecting"
        case .suspended:
            "suspended"
        }
    }

    private func handleStateChange(from oldState: ConnectionState, to newState: ConnectionState) {
        switch newState {
        case .connected:
            HapticFeedback.success()
        case .disconnected where oldState == .connected:
            HapticFeedback.error()
        case .reconnecting(let attempt, _) where attempt == 1:
            HapticFeedback.warning()
        default:
            break
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
        .glassEffect(.regular.tint(status.tintColor.glassTint), in: Capsule())
    }
}

// MARK: - Processing Indicator

struct ProcessingIndicator: View {
    let isProcessing: Bool

    var body: some View {
        if isProcessing {
            HStack(spacing: Theme.Spacing.xs) {
                ProgressView()
                    .tint(Theme.Colors.brand)

                Text("Processing...")
                    .font(.netclodeCaption)
                    .foregroundStyle(.secondary)
            }
            .padding(.horizontal, Theme.Spacing.sm)
            .padding(.vertical, Theme.Spacing.xs)
            .glassEffect(.regular.tint(Theme.Colors.brand.glassTint), in: Capsule())
            .transition(.glassAppear)
        }
    }
}

// MARK: - Preview

#Preview {
    VStack(spacing: 20) {
        Text("Connection States")
            .font(.netclodeHeadline)

        ConnectionStatusBadge(state: .connected)
        ConnectionStatusBadge(state: .connecting)
        ConnectionStatusBadge(state: .reconnecting(attempt: 2, maxAttempts: 10))
        ConnectionStatusBadge(state: .disconnected(reason: .networkLost))
        ConnectionStatusBadge(state: .suspended)

        Divider()
            .padding(.vertical)

        Text("Session Statuses")
            .font(.netclodeHeadline)

        HStack(spacing: 12) {
            SessionStatusBadge(status: .creating)
            SessionStatusBadge(status: .resuming)
            SessionStatusBadge(status: .ready)
        }

        HStack(spacing: 12) {
            SessionStatusBadge(status: .running)
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
    .background(Theme.Colors.background)
}
