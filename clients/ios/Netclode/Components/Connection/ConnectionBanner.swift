import SwiftUI

/// Banner showing connection status, network state, and reconnection progress.
struct ConnectionBanner: View {
    @Environment(AppStateCoordinator.self) private var coordinator
    @Environment(ConnectService.self) private var connectService
    @Environment(SettingsStore.self) private var settingsStore
    
    var body: some View {
        Group {
            switch coordinator.status.connection {
            case .connected:
                if coordinator.status.pendingMessages > 0 {
                    pendingMessagesBanner
                } else {
                    EmptyView()
                }
            case .connecting:
                connectingBanner
            case .reconnecting(let attempt, let maxAttempts):
                reconnectingBanner(attempt: attempt, maxAttempts: maxAttempts)
            case .disconnected(let reason):
                disconnectedBanner(reason: reason)
            case .suspended:
                EmptyView() // Don't show when backgrounded
            }
        }
        .animation(.easeInOut(duration: 0.3), value: coordinator.status.connection.displayName)
    }
    
    private var connectingBanner: some View {
        HStack(spacing: 8) {
            ProgressView()
                .scaleEffect(0.8)
            Text("Connecting...")
                .font(.subheadline)
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 8)
        .background(Color.blue.opacity(0.1))
    }
    
    private func reconnectingBanner(attempt: Int, maxAttempts: Int) -> some View {
        HStack(spacing: 8) {
            ProgressView()
                .scaleEffect(0.8)
            VStack(alignment: .leading, spacing: 2) {
                Text("Reconnecting...")
                    .font(.subheadline)
                Text("Attempt \(attempt) of \(maxAttempts)")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Spacer()
            networkIndicator
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 8)
        .padding(.horizontal, 12)
        .background(Color.orange.opacity(0.1))
    }
    
    private func disconnectedBanner(reason: DisconnectReason) -> some View {
        HStack(spacing: 8) {
            Image(systemName: "wifi.slash")
                .foregroundStyle(.red)
            VStack(alignment: .leading, spacing: 2) {
                Text("Disconnected")
                    .font(.subheadline)
                Text(reason.description)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Spacer()
            if coordinator.status.network.isConnected {
                Button("Retry") {
                    connectService.connect(to: settingsStore.serverURL, connectPort: settingsStore.connectPort)
                }
                .buttonStyle(.bordered)
                .controlSize(.small)
            } else {
                networkIndicator
            }
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 8)
        .padding(.horizontal, 12)
        .background(Color.red.opacity(0.1))
    }
    
    private var pendingMessagesBanner: some View {
        HStack(spacing: 8) {
            Image(systemName: "arrow.up.circle")
                .foregroundStyle(.blue)
            Text("\(coordinator.status.pendingMessages) message(s) pending")
                .font(.subheadline)
            Spacer()
            ProgressView()
                .scaleEffect(0.8)
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 8)
        .padding(.horizontal, 12)
        .background(Color.blue.opacity(0.1))
    }
    
    private var networkIndicator: some View {
        HStack(spacing: 4) {
            Image(systemName: coordinator.status.network.systemImage)
                .font(.caption)
            Text(coordinator.status.network.description)
                .font(.caption)
        }
        .foregroundStyle(.secondary)
    }
}

#Preview {
    VStack(spacing: 20) {
        Text("Connection banners preview")
    }
    .environment(AppStateCoordinator())
    .environment(ConnectService())
    .environment(SettingsStore())
}
