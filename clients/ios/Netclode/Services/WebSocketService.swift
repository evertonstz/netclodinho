import Foundation

enum ConnectionState: Equatable, Sendable {
    case disconnected
    case connecting
    case connected
    case reconnecting(attempt: Int)

    var isConnected: Bool {
        if case .connected = self { return true }
        return false
    }

    var displayName: String {
        switch self {
        case .disconnected: "Disconnected"
        case .connecting: "Connecting..."
        case .connected: "Connected"
        case .reconnecting(let attempt): "Reconnecting (\(attempt))..."
        }
    }

    var systemImage: String {
        switch self {
        case .disconnected: "wifi.slash"
        case .connecting, .reconnecting: "wifi.exclamationmark"
        case .connected: "wifi"
        }
    }
}

@Observable
final class WebSocketService: @unchecked Sendable {
    private(set) var connectionState: ConnectionState = .disconnected
    private var webSocketTask: URLSessionWebSocketTask?
    private var receiveTask: Task<Void, Never>?
    private var reconnectTask: Task<Void, Never>?
    private var serverURL: String = ""
    private var isReconnecting = false

    private var continuation: AsyncStream<ServerMessage>.Continuation?
    private let encoder = JSONEncoder()
    private let decoder: JSONDecoder = {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return decoder
    }()

    static let maxReconnectAttempts = 5
    private let reconnectDelay: UInt64 = 3_000_000_000 // 3 seconds

    var messages: AsyncStream<ServerMessage> {
        AsyncStream { continuation in
            self.continuation = continuation
        }
    }

    func connect(to serverURL: String) {
        guard connectionState == .disconnected else { return }
        self.serverURL = serverURL
        isReconnecting = false

        Task { @MainActor in
            await performConnect()
        }
    }

    private func performConnect(attempt: Int = 0) async {
        // Cancel any existing tasks
        receiveTask?.cancel()
        webSocketTask?.cancel(with: .goingAway, reason: nil)
        webSocketTask = nil

        if attempt > 0 {
            connectionState = .reconnecting(attempt: attempt)
        } else {
            connectionState = .connecting
        }

        let urlString = serverURL.hasPrefix("ws://") || serverURL.hasPrefix("wss://")
            ? serverURL
            : "ws://\(serverURL)/ws"

        print("[WebSocket] Connecting to: \(urlString) (attempt \(attempt))")

        guard let url = URL(string: urlString) else {
            print("[WebSocket] Invalid URL: \(urlString)")
            connectionState = .disconnected
            return
        }

        let session = URLSession(configuration: .default)
        webSocketTask = session.webSocketTask(with: url)
        webSocketTask?.resume()

        // Wait a moment for connection to establish, then verify with ping
        try? await Task.sleep(nanoseconds: 500_000_000) // 0.5 seconds

        do {
            try await withCheckedThrowingContinuation { (cont: CheckedContinuation<Void, Error>) in
                webSocketTask?.sendPing { error in
                    if let error {
                        cont.resume(throwing: error)
                    } else {
                        cont.resume()
                    }
                }
            }

            // Ping succeeded - we're connected
            print("[WebSocket] Connected successfully")
            connectionState = .connected
            startReceiving()

            // Send sync request to get all sessions
            send(.sync)

        } catch {
            print("[WebSocket] Connection failed: \(error.localizedDescription)")
            webSocketTask?.cancel(with: .goingAway, reason: nil)
            webSocketTask = nil
            connectionState = .disconnected
        }
    }

    private func startReceiving() {
        receiveTask = Task { [weak self] in
            guard let self else { return }

            while !Task.isCancelled && connectionState == .connected {
                do {
                    guard let message = try await webSocketTask?.receive() else {
                        break
                    }

                    switch message {
                    case .string(let text):
                        print("[WebSocket] Received: \(text.prefix(200))")
                        if let data = text.data(using: .utf8) {
                            do {
                                let serverMessage = try decoder.decode(ServerMessage.self, from: data)
                                continuation?.yield(serverMessage)
                            } catch {
                                print("[WebSocket] Decode error: \(error)")
                            }
                        }
                    case .data(let data):
                        print("[WebSocket] Received binary data: \(data.count) bytes")
                        if let serverMessage = try? decoder.decode(ServerMessage.self, from: data) {
                            continuation?.yield(serverMessage)
                        }
                    @unknown default:
                        break
                    }
                } catch {
                    if !Task.isCancelled {
                        print("[WebSocket] Receive error: \(error.localizedDescription)")
                        await handleDisconnection()
                    }
                    break
                }
            }
        }
    }

    private func handleDisconnection() async {
        // Prevent multiple simultaneous reconnection attempts
        guard !isReconnecting else { return }
        isReconnecting = true

        connectionState = .disconnected
        webSocketTask?.cancel(with: .goingAway, reason: nil)
        webSocketTask = nil

        guard !serverURL.isEmpty else {
            isReconnecting = false
            return
        }

        // Auto-reconnect with delay
        for attempt in 1...Self.maxReconnectAttempts {
            connectionState = .reconnecting(attempt: attempt)
            print("[WebSocket] Reconnect attempt \(attempt)/\(Self.maxReconnectAttempts) in 3s...")

            try? await Task.sleep(nanoseconds: reconnectDelay)

            if Task.isCancelled {
                break
            }

            await performConnect(attempt: attempt)

            if connectionState == .connected {
                print("[WebSocket] Reconnected successfully")
                isReconnecting = false
                return
            }
        }

        print("[WebSocket] Max reconnect attempts reached")
        connectionState = .disconnected
        isReconnecting = false
    }

    func disconnect() {
        isReconnecting = false
        receiveTask?.cancel()
        reconnectTask?.cancel()
        webSocketTask?.cancel(with: .goingAway, reason: nil)
        webSocketTask = nil
        connectionState = .disconnected
    }

    func send(_ message: ClientMessage) {
        guard connectionState == .connected else {
            print("[WebSocket] send: dropped message (not connected), state=\(connectionState)")
            return
        }

        do {
            let data = try encoder.encode(message)
            guard let string = String(data: data, encoding: .utf8) else { return }
            print("[WebSocket] Sending: \(string.prefix(100))")

            webSocketTask?.send(.string(string)) { error in
                if let error {
                    print("[WebSocket] send error: \(error)")
                }
            }
        } catch {
            print("[WebSocket] encode error: \(error)")
        }
    }

    /// Open a session and load its history
    /// - Parameters:
    ///   - id: Session ID to open
    ///   - lastMessageId: Optional cursor for message history
    ///   - lastNotificationId: Optional cursor for reconnection (Redis Stream ID)
    func openSession(id: String, lastMessageId: String? = nil, lastNotificationId: String? = nil) {
        print("[WebSocket] openSession called for \(id), connectionState=\(connectionState)")
        send(.sessionOpen(id: id, lastMessageId: lastMessageId, lastNotificationId: lastNotificationId))
        send(.sessionResume(id: id))
    }
}
