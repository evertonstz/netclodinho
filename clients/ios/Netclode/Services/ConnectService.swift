import Foundation
import Connect
import ConnectNIO
import SwiftProtobuf
import os.log

private let logger = Logger(subsystem: "com.netclode", category: "ConnectService")

/// Errors that can occur during Connect protocol operations.
enum ConnectError: Error, LocalizedError {
    case connectionTimeout
    case clientCreationFailed
    case streamCreationFailed
    case connectionFailed(message: String)
    case sendFailed(underlying: Error)
    
    var errorDescription: String? {
        switch self {
        case .connectionTimeout:
            return "Connection timed out"
        case .clientCreationFailed:
            return "Failed to create Connect client"
        case .streamCreationFailed:
            return "Failed to create bidirectional stream"
        case .connectionFailed(let message):
            return "Connection failed: \(message)"
        case .sendFailed(let error):
            return "Failed to send message: \(error.localizedDescription)"
        }
    }
}

/// Connection state for the service
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

/// ConnectService provides gRPC/Connect protocol communication with the control plane.
@MainActor
@Observable
final class ConnectService {
    private(set) var connectionState: ConnectionState = .disconnected
    
    private var client: ProtocolClient?
    private var serviceClient: Netclode_V1_ClientServiceClient?
    private var stream: (any BidirectionalAsyncStreamInterface<Netclode_V1_ClientMessage, Netclode_V1_ServerMessage>)?
    private var receiveTask: Task<Void, Never>?
    private var reconnectTask: Task<Void, Never>?
    private var keepAliveTask: Task<Void, Never>?
    private var serverURL: String = ""
    private var connectPortOverride: String = ""
    private var lastActivityAt = Date()
    
    // Message stream for consumers
    private var _messagesContinuation: AsyncStream<ServerMessage>.Continuation?
    private var _messagesStream: AsyncStream<ServerMessage>?
    
    static let maxReconnectAttempts = 5
    static let connectionTimeoutSeconds: UInt64 = 15
    private let keepAliveInterval: UInt64 = 30_000_000_000
    private let keepAliveIdleThreshold: TimeInterval = 30
    
    var messages: AsyncStream<ServerMessage> {
        if let stream = _messagesStream {
            return stream
        }
        let stream = AsyncStream<ServerMessage> { [weak self] continuation in
            self?._messagesContinuation = continuation
        }
        _messagesStream = stream
        return stream
    }
    
    /// Connect to the server.
    /// - Parameters:
    ///   - serverURL: The base server URL (e.g., "netclode-control-plane" or "http://localhost:3000")
    ///   - connectPort: Optional port override for the Connect protocol. If empty, uses default logic.
    func connect(to serverURL: String, connectPort: String = "") {
        guard connectionState == .disconnected else { return }
        self.serverURL = serverURL
        self.connectPortOverride = connectPort
        
        Task {
            await performConnect()
        }
    }
    
    private func performConnect(attempt: Int = 0) async {
        // Cancel any existing stream
        receiveTask?.cancel()
        keepAliveTask?.cancel()
        stream = nil
        
        if attempt > 0 {
            connectionState = .reconnecting(attempt: attempt)
        } else {
            connectionState = .connecting
        }
        
        let grpcHost = buildConnectHost()
        print("[Connect] Connecting to: \(grpcHost) (attempt \(attempt))")
        
        // Create the Connect client with timeout
        do {
            try await withThrowingTaskGroup(of: Void.self) { group in
                group.addTask {
                    try await Task.sleep(nanoseconds: Self.connectionTimeoutSeconds * 1_000_000_000)
                    throw ConnectError.connectionTimeout
                }
                
                group.addTask {
                    try await self.establishConnection(to: grpcHost)
                }
                
                // Wait for first to complete (either connection or timeout)
                try await group.next()
                group.cancelAll()
            }
        } catch ConnectError.connectionTimeout {
            print("[Connect] Connection timed out after \(Self.connectionTimeoutSeconds)s")
            connectionState = .disconnected
            return
        } catch {
            print("[Connect] Connection failed: \(error)")
            connectionState = .disconnected
            return
        }
        
        guard let currentStream = stream else {
            print("[Connect] Failed to create stream")
            connectionState = .disconnected
            return
        }
        
        // Use a continuation to wait for validation result
        let isValid: Bool = await withCheckedContinuation { continuation in
            var hasResumed = false
            
            // Start receiving with validation callback
            startReceiving(stream: currentStream, onValidation: { success in
                guard !hasResumed else { return }
                hasResumed = true
                continuation.resume(returning: success)
            })
            
            // Send sync to trigger the actual HTTP connection
            send(.sync)
            
            // Set up timeout
            Task {
                try? await Task.sleep(nanoseconds: 10_000_000_000) // 10 seconds
                guard !hasResumed else { return }
                hasResumed = true
                continuation.resume(returning: false)
            }
        }
        
        guard isValid else {
            print("[Connect] Connection validation failed or timed out")
            connectionState = .disconnected
            receiveTask?.cancel()
            stream = nil
            return
        }
        
        print("[Connect] Connected and validated successfully")
        connectionState = .connected
        recordActivity()

        // Keep-alive to detect dead connections
        startKeepAlive()
    }
    
    /// Build the Connect protocol host URL from serverURL and optional port override.
    private func buildConnectHost() -> String {
        let normalized = normalizeServerURL(serverURL)
        guard var components = URLComponents(string: normalized) else {
            return normalized
        }

        // If explicit port override provided, use it
        let override = connectPortOverride.trimmingCharacters(in: .whitespacesAndNewlines)
        if !override.isEmpty, let overridePort = Int(override) {
            components.port = overridePort
            return components.string ?? "\(normalized):\(overridePort)"
        }

        // For HTTPS (Tailscale Ingress), use default port 443
        if components.scheme == "https" {
            // Don't modify port for HTTPS - use default 443
            return components.string ?? normalized
        }
        
        // For HTTP (local dev), map 3000 → 3001 or default to 3001
        if components.port == 3000 {
            components.port = 3001
            return components.string ?? normalized.replacingOccurrences(of: ":3000", with: ":3001")
        }

        if components.port == nil {
            components.port = 3001
        }

        return components.string ?? normalized
    }

    private func normalizeServerURL(_ rawURL: String) -> String {
        var urlString = rawURL.trimmingCharacters(in: .whitespacesAndNewlines)

        if urlString.hasPrefix("ws://") {
            urlString = "http://" + String(urlString.dropFirst("ws://".count))
        } else if urlString.hasPrefix("wss://") {
            urlString = "https://" + String(urlString.dropFirst("wss://".count))
        } else if !urlString.hasPrefix("http://") && !urlString.hasPrefix("https://") {
            // Default to HTTPS for Tailscale domains (.ts.net)
            if urlString.contains(".ts.net") {
                urlString = "https://\(urlString)"
            } else {
                urlString = "http://\(urlString)"
            }
        }

        guard var components = URLComponents(string: urlString) else {
            return urlString
        }

        components.path = ""
        components.query = nil
        components.fragment = nil

        return components.string ?? urlString
    }
    
    /// Establish the actual connection (called within timeout context).
    private func establishConnection(to grpcHost: String) async throws {
        // Use NIOHTTPClient instead of URLSessionHTTPClient for better HTTP/2
        // handling through iOS Tailscale network extension. NIOHTTPClient uses
        // Swift NIO's HTTP/2 implementation which bypasses URLSession.
        client = ProtocolClient(
            httpClient: NIOHTTPClient(host: grpcHost, timeout: 120),
            config: ProtocolClientConfig(
                host: grpcHost,
                networkProtocol: .connect,
                codec: ProtoCodec()
            )
        )
        
        guard let client = client else {
            throw ConnectError.clientCreationFailed
        }
        
        serviceClient = Netclode_V1_ClientServiceClient(client: client)
        
        // Open the bidirectional stream
        stream = serviceClient?.connect(headers: [:])
        
        guard stream != nil else {
            throw ConnectError.streamCreationFailed
        }
    }
    
    private func startReceiving(
        stream: any BidirectionalAsyncStreamInterface<Netclode_V1_ClientMessage, Netclode_V1_ServerMessage>,
        onValidation: ((Bool) -> Void)? = nil
    ) {
        receiveTask = Task { [weak self] in
            var validationCallback = onValidation
            var hasValidated = false
            
            for await result in stream.results() {
                guard let self, !Task.isCancelled else { break }
                
                switch result {
                case .headers:
                    print("[Connect] Received headers")
                    self.recordActivity()
                    // Connection validated on first headers
                    if !hasValidated {
                        hasValidated = true
                        validationCallback?(true)
                        validationCallback = nil
                    }
                    
                case .message(let protoMessage):
                    self.recordActivity()
                    // Connection validated on first message (if headers weren't received first)
                    if !hasValidated {
                        hasValidated = true
                        validationCallback?(true)
                        validationCallback = nil
                    }
                    // Convert proto message to ServerMessage
                    if let serverMessage = self.convertProtoMessage(protoMessage) {
                        self._messagesContinuation?.yield(serverMessage)
                    }
                    
                case .complete(let code, let error, _):
                    print("[Connect] Stream completed: code=\(code), error=\(String(describing: error))")
                    // If we haven't validated yet, this is a connection failure
                    if !hasValidated {
                        hasValidated = true
                        validationCallback?(false)
                        validationCallback = nil
                    }
                    // Trigger reconnection
                    await self.handleDisconnection()
                }
            }
            
            // If loop exits without validation (shouldn't happen), mark as failed
            if !hasValidated {
                validationCallback?(false)
            }
        }
    }
    
    /// Convert proto ServerMessage to the app's ServerMessage type
    private func convertProtoMessage(_ proto: Netclode_V1_ServerMessage) -> ServerMessage? {
        switch proto.message {
        case .sessionCreated(let msg):
            return .sessionCreated(session: convertSession(msg.session))
            
        case .sessionUpdated(let msg):
            return .sessionUpdated(session: convertSession(msg.session))
            
        case .sessionDeleted(let msg):
            return .sessionDeleted(id: msg.sessionID)
            
        case .sessionsDeletedAll(let msg):
            return .sessionsDeletedAll(deletedIds: msg.deletedIds)
            
        case .sessionList(let msg):
            return .sessionList(sessions: msg.sessions.map { convertSession($0) })
            
        case .sessionState(let msg):
            let sessionId = msg.session.id
            return .sessionState(
                session: convertSession(msg.session),
                messages: msg.messages.map { convertPersistedMessage($0, sessionId: sessionId) },
                events: msg.events.map { convertPersistedEvent($0, sessionId: sessionId) },
                hasMore: msg.hasMore_p,
                lastNotificationId: msg.hasLastNotificationID ? msg.lastNotificationID : nil
            )
            
        case .syncResponse(let msg):
            return .syncResponse(
                sessions: msg.sessions.map { convertSessionSummary($0) },
                serverTime: msg.serverTime.date
            )
            
        case .agentMessage(let msg):
            return .agentMessage(sessionId: msg.sessionID, content: msg.content, partial: msg.partial)
            
        case .agentEvent(let msg):
            return .agentEvent(sessionId: msg.sessionID, event: convertAgentEvent(msg.event))
            
        case .agentDone(let msg):
            return .agentDone(sessionId: msg.sessionID)
            
        case .userMessage(let msg):
            return .userMessage(sessionId: msg.sessionID, content: msg.content)
            
        case .terminalOutput(let msg):
            return .terminalOutput(sessionId: msg.sessionID, data: msg.data)
            
        case .portExposed(let msg):
            return .portExposed(sessionId: msg.sessionID, port: Int(msg.port), previewUrl: msg.previewURL)
            
        case .githubRepos(let msg):
            return .githubRepos(repos: msg.repos.map { convertGitHubRepo($0) })
            
        case .gitStatus(let msg):
            return .gitStatusResponse(sessionId: msg.sessionID, files: msg.files.map { convertGitFileChange($0) })
            
        case .gitDiff(let msg):
            return .gitDiffResponse(sessionId: msg.sessionID, diff: msg.diff)
            
        case .error(let msg):
            // Unified error response - extract session ID if present
            let sessionId = msg.error.hasSessionID ? msg.error.sessionID : nil
            let errorMessage = msg.error.message
            let errorCode = msg.error.code
            
            // Route to appropriate error type based on code
            if let sid = sessionId {
                if errorCode == "SESSION_ERROR" {
                    return .sessionError(id: sid, error: errorMessage)
                } else if errorCode == "AGENT_ERROR" {
                    return .agentError(sessionId: sid, error: errorMessage)
                } else if errorCode == "PORT_ERROR" {
                    return .portError(sessionId: sid, port: 0, error: errorMessage)
                } else if errorCode == "GIT_ERROR" {
                    return .gitError(sessionId: sid, error: errorMessage)
                }
            }
            return .error(message: errorMessage)
            
        case .none:
            return nil
        }
    }
    
    // MARK: - Proto to Model Conversions
    
    private func convertSession(_ proto: Netclode_V1_Session) -> Session {
        Session(
            id: proto.id,
            name: proto.name,
            status: convertSessionStatus(proto.status),
            repo: proto.hasRepo ? proto.repo : nil,
            repoAccess: proto.hasRepoAccess ? convertRepoAccess(proto.repoAccess) : nil,
            createdAt: proto.createdAt.date,
            lastActiveAt: proto.lastActiveAt.date,
            sdkType: proto.hasSdkType ? convertSdkType(proto.sdkType) : nil,
            model: proto.hasModel ? proto.model : nil
        )
    }

    private func convertSdkType(_ proto: Netclode_V1_SdkType) -> SdkType {
        switch proto {
        case .claude: return .claude
        case .opencode: return .opencode
        case .unspecified, .UNRECOGNIZED: return .claude
        }
    }
    
    private func convertRepoAccess(_ proto: Netclode_V1_RepoAccess) -> RepoAccess {
        switch proto {
        case .read: return .read
        case .write: return .write
        case .unspecified, .UNRECOGNIZED: return .read
        }
    }
    
    private func convertToProtoRepoAccess(_ access: RepoAccess) -> Netclode_V1_RepoAccess {
        switch access {
        case .read: return .read
        case .write: return .write
        }
    }

    private func convertToProtoSdkType(_ sdkType: SdkType) -> Netclode_V1_SdkType {
        switch sdkType {
        case .claude: return .claude
        case .opencode: return .opencode
        }
    }
    
    private func convertSessionSummary(_ proto: Netclode_V1_SessionSummary) -> SessionWithMeta {
        let session = proto.session
        return SessionWithMeta(
            id: session.id,
            name: session.name,
            status: convertSessionStatus(session.status).rawValue,
            repo: session.hasRepo ? session.repo : nil,
            repoAccess: session.hasRepoAccess ? convertRepoAccess(session.repoAccess) : nil,
            createdAt: session.createdAt.date,
            lastActiveAt: session.lastActiveAt.date,
            messageCount: proto.hasMessageCount ? Int(proto.messageCount) : nil,
            lastMessageId: proto.hasLastMessageID ? proto.lastMessageID : nil,
            sdkType: session.hasSdkType ? convertSdkType(session.sdkType) : nil,
            model: session.hasModel ? session.model : nil
        )
    }
    
    private func convertSessionStatus(_ proto: Netclode_V1_SessionStatus) -> SessionStatus {
        switch proto {
        case .creating: return .creating
        case .resuming: return .resuming
        case .ready: return .ready
        case .running: return .running
        case .paused: return .paused
        case .error: return .error
        case .interrupted: return .interrupted
        case .unspecified, .UNRECOGNIZED: return .paused
        }
    }
    
    private func convertAgentEvent(_ proto: Netclode_V1_AgentEvent) -> AgentEvent {
        let id = UUID()
        let timestamp = proto.timestamp.date
        
        switch proto.kind {
        case .toolStart:
            let tool = proto.tool
            return .toolStart(ToolStartEvent(
                id: id,
                timestamp: timestamp,
                tool: tool.tool,
                toolUseId: tool.toolUseID,
                parentToolUseId: tool.hasParentToolUseID ? tool.parentToolUseID : nil,
                input: tool.hasInput ? convertProtoStruct(tool.input) : [:]
            ))
            
        case .toolInput:
            let tool = proto.tool
            return .toolInput(ToolInputEvent(
                id: id,
                timestamp: timestamp,
                toolUseId: tool.toolUseID,
                parentToolUseId: tool.hasParentToolUseID ? tool.parentToolUseID : nil,
                inputDelta: tool.hasInputDelta ? tool.inputDelta : ""
            ))
            
        case .toolInputComplete:
            let tool = proto.tool
            return .toolInputComplete(ToolInputCompleteEvent(
                id: id,
                timestamp: timestamp,
                toolUseId: tool.toolUseID,
                parentToolUseId: tool.hasParentToolUseID ? tool.parentToolUseID : nil,
                input: tool.hasInput ? convertProtoStruct(tool.input) : [:]
            ))
            
        case .toolEnd:
            let tool = proto.tool
            return .toolEnd(ToolEndEvent(
                id: id,
                timestamp: timestamp,
                tool: tool.tool,
                toolUseId: tool.toolUseID,
                parentToolUseId: tool.hasParentToolUseID ? tool.parentToolUseID : nil,
                result: tool.hasResult ? tool.result : nil,
                error: tool.hasError ? tool.error : nil
            ))
            
        case .fileChange:
            let fc = proto.fileChange
            let action: FileAction
            switch fc.action {
            case .create: action = .create
            case .delete: action = .delete
            case .edit, .unspecified, .UNRECOGNIZED: action = .edit
            }
            return .fileChange(FileChangeEvent(
                id: id,
                timestamp: timestamp,
                path: fc.path,
                action: action,
                linesAdded: fc.hasLinesAdded ? Int(fc.linesAdded) : nil,
                linesRemoved: fc.hasLinesRemoved ? Int(fc.linesRemoved) : nil
            ))
            
        case .commandStart:
            let cmd = proto.command
            return .commandStart(CommandStartEvent(
                id: id,
                timestamp: timestamp,
                command: cmd.command,
                cwd: cmd.hasCwd ? cmd.cwd : nil
            ))
            
        case .commandEnd:
            let cmd = proto.command
            return .commandEnd(CommandEndEvent(
                id: id,
                timestamp: timestamp,
                command: cmd.command,
                exitCode: cmd.hasExitCode ? Int(cmd.exitCode) : 0,
                output: cmd.hasOutput ? cmd.output : nil
            ))
            
        case .thinking:
            let th = proto.thinking
            return .thinking(ThinkingEvent(
                id: id,
                timestamp: timestamp,
                thinkingId: th.thinkingID.isEmpty ? "thinking_\(id.uuidString)" : th.thinkingID,
                content: th.content,
                partial: th.partial
            ))
            
        case .portExposed:
            let pe = proto.portExposed
            return .portExposed(PortExposedEvent(
                id: id,
                timestamp: timestamp,
                port: Int(pe.port),
                process: pe.hasProcess ? pe.process : nil,
                previewUrl: pe.hasPreviewURL ? pe.previewURL : nil
            ))
            
        case .repoClone:
            let rc = proto.repoClone
            let stage: RepoCloneStage
            switch rc.stage {
            case .starting: stage = .starting
            case .cloning: stage = .cloning
            case .error: stage = .error
            case .done, .unspecified, .UNRECOGNIZED: stage = .done
            }
            return .repoClone(RepoCloneEvent(
                id: id,
                timestamp: timestamp,
                repo: rc.repo,
                stage: stage,
                message: rc.message
            ))
            
        case .agentDisconnected:
            return .agentDisconnected(AgentDisconnectedEvent(
                id: id,
                timestamp: timestamp,
                message: "Agent connection lost. Send a message to continue when reconnected."
            ))
            
        case .agentReconnected:
            return .agentReconnected(AgentReconnectedEvent(
                id: id,
                timestamp: timestamp,
                message: "Agent reconnected. Send a message to continue."
            ))
            
        case .UNRECOGNIZED, .unspecified:
            // Return a placeholder thinking event for unknown types
            return .thinking(ThinkingEvent(
                id: id,
                timestamp: timestamp,
                thinkingId: "unknown_\(id.uuidString)",
                content: "Unknown event type",
                partial: false
            ))
        }
    }
    
    /// Convert protobuf Struct to Swift dictionary.
    private func convertProtoStruct(_ protoStruct: SwiftProtobuf.Google_Protobuf_Struct) -> [String: AnyCodableValue] {
        var result: [String: AnyCodableValue] = [:]
        for (key, value) in protoStruct.fields {
            result[key] = convertProtoValue(value)
        }
        return result
    }
    
    /// Convert protobuf Value to AnyCodableValue.
    ///
    /// Note: Protobuf only has `double` for numeric types, so we use a heuristic to convert
    /// whole numbers to Int for better JSON compatibility. This means values like `1.0` will
    /// become Int(1). If you need to preserve the original double type, the source system
    /// should encode numbers as strings or use a different serialization format.
    private func convertProtoValue(_ value: SwiftProtobuf.Google_Protobuf_Value) -> AnyCodableValue {
        switch value.kind {
        case .nullValue:
            return .null
        case .numberValue(let num):
            // Heuristic: treat whole numbers within Int range as integers
            // This matches typical JSON number handling behavior
            if num.truncatingRemainder(dividingBy: 1) == 0 && num >= Double(Int.min) && num <= Double(Int.max) {
                return .int(Int(num))
            }
            return .double(num)
        case .stringValue(let str):
            return .string(str)
        case .boolValue(let bool):
            return .bool(bool)
        case .listValue(let list):
            return .array(list.values.map { convertProtoValue($0) })
        case .structValue(let structVal):
            return .dictionary(convertProtoStruct(structVal))
        case .none:
            return .null
        }
    }
    
    private func convertPersistedMessage(_ proto: Netclode_V1_Message, sessionId: String) -> PersistedMessage {
        PersistedMessage(
            id: proto.id,
            sessionId: sessionId,
            role: convertMessageRole(proto.role),
            content: proto.content,
            timestamp: proto.timestamp.date
        )
    }
    
    private func convertMessageRole(_ proto: Netclode_V1_MessageRole) -> PersistedMessage.ChatRole {
        switch proto {
        case .user: return .user
        case .assistant: return .assistant
        case .unspecified, .UNRECOGNIZED: return .user
        }
    }
    
    private func convertPersistedEvent(_ proto: Netclode_V1_Event, sessionId: String) -> PersistedEvent {
        // Convert the embedded AgentEvent to RawAgentEventData
        let agentEvent = proto.event
        let eventData = convertAgentEventToRaw(agentEvent)
        
        return PersistedEvent(
            id: proto.id,
            sessionId: sessionId,
            event: eventData,
            timestamp: proto.timestamp.date
        )
    }
    
    private func convertAgentEventToRaw(_ proto: Netclode_V1_AgentEvent) -> PersistedEvent.RawAgentEventData {
        let kind: String
        var tool: String? = nil
        var toolUseId: String? = nil
        var parentToolUseId: String? = nil
        var input: [String: AnyCodableValue]? = nil
        var inputDelta: String? = nil
        var result: String? = nil
        var path: String? = nil
        var action: String? = nil
        var linesAdded: Int? = nil
        var linesRemoved: Int? = nil
        var command: String? = nil
        var cwd: String? = nil
        var exitCode: Int? = nil
        var output: String? = nil
        var content: String? = nil
        var thinkingId: String? = nil
        var partial: Bool? = nil
        var port: Int? = nil
        var process: String? = nil
        var previewUrl: String? = nil
        var repo: String? = nil
        var stage: String? = nil
        var message: String? = nil
        var error: String? = nil
        
        switch proto.kind {
        case .toolStart:
            kind = "tool_start"
            let t = proto.tool
            tool = t.tool
            toolUseId = t.toolUseID
            parentToolUseId = t.hasParentToolUseID ? t.parentToolUseID : nil
            input = t.hasInput ? convertProtoStruct(t.input) : nil
        case .toolInput:
            kind = "tool_input"
            let t = proto.tool
            toolUseId = t.toolUseID
            parentToolUseId = t.hasParentToolUseID ? t.parentToolUseID : nil
            inputDelta = t.hasInputDelta ? t.inputDelta : nil
        case .toolInputComplete:
            kind = "tool_input_complete"
            let t = proto.tool
            toolUseId = t.toolUseID
            parentToolUseId = t.hasParentToolUseID ? t.parentToolUseID : nil
            input = t.hasInput ? convertProtoStruct(t.input) : nil
        case .toolEnd:
            kind = "tool_end"
            let t = proto.tool
            tool = t.tool
            toolUseId = t.toolUseID
            parentToolUseId = t.hasParentToolUseID ? t.parentToolUseID : nil
            result = t.hasResult ? t.result : nil
            error = t.hasError ? t.error : nil
        case .fileChange:
            kind = "file_change"
            let fc = proto.fileChange
            path = fc.path
            switch fc.action {
            case .create: action = "create"
            case .delete: action = "delete"
            case .edit, .unspecified, .UNRECOGNIZED: action = "edit"
            }
            linesAdded = fc.hasLinesAdded ? Int(fc.linesAdded) : nil
            linesRemoved = fc.hasLinesRemoved ? Int(fc.linesRemoved) : nil
        case .commandStart:
            kind = "command_start"
            let cmd = proto.command
            command = cmd.command
            cwd = cmd.hasCwd ? cmd.cwd : nil
        case .commandEnd:
            kind = "command_end"
            let cmd = proto.command
            command = cmd.command
            exitCode = cmd.hasExitCode ? Int(cmd.exitCode) : nil
            output = cmd.hasOutput ? cmd.output : nil
        case .thinking:
            kind = "thinking"
            let th = proto.thinking
            thinkingId = th.thinkingID
            content = th.content
            partial = th.partial
        case .portExposed:
            kind = "port_exposed"
            let pe = proto.portExposed
            port = Int(pe.port)
            process = pe.hasProcess ? pe.process : nil
            previewUrl = pe.hasPreviewURL ? pe.previewURL : nil
        case .repoClone:
            kind = "repo_clone"
            let rc = proto.repoClone
            repo = rc.repo
            switch rc.stage {
            case .starting: stage = "starting"
            case .cloning: stage = "cloning"
            case .error: stage = "error"
            case .done, .unspecified, .UNRECOGNIZED: stage = "done"
            }
            message = rc.message
        case .agentDisconnected:
            kind = "agent_disconnected"
            message = "Agent connection lost. Send a message to continue when reconnected."
        case .agentReconnected:
            kind = "agent_reconnected"
            message = "Agent reconnected. Send a message to continue."
        case .unspecified, .UNRECOGNIZED:
            kind = "unknown"
        }
        
        return PersistedEvent.RawAgentEventData(
            kind: kind,
            timestamp: proto.timestamp.date,
            tool: tool, toolUseId: toolUseId, parentToolUseId: parentToolUseId, input: input, inputDelta: inputDelta, result: result,
            path: path, action: action, linesAdded: linesAdded, linesRemoved: linesRemoved,
            command: command, cwd: cwd, exitCode: exitCode, output: output,
            content: content, thinkingId: thinkingId, partial: partial,
            port: port, process: process, previewUrl: previewUrl,
            repo: repo, stage: stage, message: message, error: error
        )
    }
    
    private func convertGitHubRepo(_ proto: Netclode_V1_GitHubRepo) -> GitHubRepo {
        GitHubRepo(
            name: proto.name,
            fullName: proto.fullName,
            isPrivate: proto.`private`,
            description: proto.hasDescription_p ? proto.description_p : nil
        )
    }
    
    private func convertGitFileChange(_ proto: Netclode_V1_GitFileChange) -> GitFileChange {
        GitFileChange(
            path: proto.path,
            status: convertGitFileStatus(proto.status),
            staged: proto.staged
        )
    }
    
    private func convertGitFileStatus(_ proto: Netclode_V1_GitFileStatus) -> GitFileStatus {
        switch proto {
        case .modified: return .modified
        case .added: return .added
        case .deleted: return .deleted
        case .renamed: return .renamed
        case .untracked: return .untracked
        case .copied: return .copied
        case .ignored: return .ignored
        case .unmerged: return .unmerged
        case .UNRECOGNIZED, .unspecified:
            // Log unknown status for debugging
            return .modified
        }
    }
    
    // MARK: - Handle Disconnection
    
    private func handleDisconnection() async {
        connectionState = .disconnected
        receiveTask?.cancel()
        keepAliveTask?.cancel()
        stream = nil
        
        guard !serverURL.isEmpty else { return }
        
        // Cancel any existing reconnect task
        reconnectTask?.cancel()
        
        // Start reconnection in a detached task to avoid blocking the main actor
        reconnectTask = Task.detached { [weak self] in
            await self?.performReconnection()
        }
    }
    
    /// Perform reconnection attempts. Runs in a detached task to avoid blocking the main actor.
    private func performReconnection() async {
        for attempt in 1...Self.maxReconnectAttempts {
            guard !Task.isCancelled else {
                logger.info("Reconnection cancelled")
                return
            }
            
            await MainActor.run {
                self.connectionState = .reconnecting(attempt: attempt)
            }
            logger.info("Reconnect attempt \(attempt)/\(Self.maxReconnectAttempts)")
            
            // Exponential backoff: 2s, 4s, 8s, 16s, 32s
            let delaySeconds = UInt64(pow(2.0, Double(attempt)))
            try? await Task.sleep(nanoseconds: delaySeconds * 1_000_000_000)
            
            guard !Task.isCancelled else { return }
            
            await performConnect(attempt: attempt)
            
            let isConnected = connectionState == .connected
            if isConnected {
                logger.info("Reconnected successfully on attempt \(attempt)")
                return
            }
        }
        
        logger.warning("Max reconnect attempts (\(Self.maxReconnectAttempts)) reached")
        await MainActor.run {
            self.connectionState = .disconnected
        }
    }
    
    func disconnect() {
        reconnectTask?.cancel()
        receiveTask?.cancel()
        keepAliveTask?.cancel()
        stream = nil
        client = nil
        serviceClient = nil
        connectionState = .disconnected
    }
    
    func ensureConnected(to serverURL: String, connectPort: String = "") {
        self.serverURL = serverURL
        self.connectPortOverride = connectPort
        
        switch connectionState {
        case .connected:
            // Verify connection is alive with a lightweight sync
            send(.sync)
        case .disconnected:
            connect(to: serverURL, connectPort: connectPort)
        case .connecting, .reconnecting:
            // Already trying
            break
        }
    }
    
    // MARK: - Send Messages
    
    func send(_ message: ClientMessage) {
        // Allow sending during .connecting state for the initial sync message
        guard connectionState == .connected || connectionState == .connecting, let stream = stream else {
            logger.warning("send: dropped message (not connected): \(String(describing: message))")
            return
        }

        recordActivity()
        let protoMessage = convertToProtoMessage(message)
        
        Task {
            do {
                try await stream.send(protoMessage)
            } catch {
                logger.error("Failed to send message: \(error.localizedDescription)")
                // If send fails, the stream may be broken - trigger reconnection
                if !Task.isCancelled {
                    await handleDisconnection()
                }
            }
        }
    }

    private func startKeepAlive() {
        keepAliveTask?.cancel()
        keepAliveTask = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: self?.keepAliveInterval ?? 30_000_000_000)

                guard let self, !Task.isCancelled else { return }
                guard self.connectionState == .connected else { continue }

                let idleTime = Date().timeIntervalSince(self.lastActivityAt)
                if idleTime >= self.keepAliveIdleThreshold {
                    self.send(.sync)
                }
            }
        }
    }

    private func recordActivity() {
        lastActivityAt = Date()
    }
    
    private func convertToProtoMessage(_ message: ClientMessage) -> Netclode_V1_ClientMessage {
        var proto = Netclode_V1_ClientMessage()
        
        switch message {
        case .sync:
            proto.message = .sync(Netclode_V1_SyncRequest())
            
        case .sessionCreate(let name, let repo, let repoAccess, let initialPrompt, let sdkType, let model):
            var req = Netclode_V1_CreateSessionRequest()
            if let name = name {
                req.name = name
            }
            if let repo = repo {
                req.repo = repo
            }
            if let repoAccess = repoAccess {
                req.repoAccess = convertToProtoRepoAccess(repoAccess)
            }
            if let initialPrompt = initialPrompt {
                req.initialPrompt = initialPrompt
            }
            if let sdkType = sdkType {
                req.sdkType = convertToProtoSdkType(sdkType)
            }
            if let model = model {
                req.model = model
            }
            proto.message = .createSession(req)
            
        case .sessionList:
            proto.message = .listSessions(Netclode_V1_ListSessionsRequest())
            
        case .sessionResume(let id):
            var req = Netclode_V1_ResumeSessionRequest()
            req.sessionID = id
            proto.message = .resumeSession(req)
            
        case .sessionPause(let id):
            var req = Netclode_V1_PauseSessionRequest()
            req.sessionID = id
            proto.message = .pauseSession(req)
            
        case .sessionDelete(let id):
            var req = Netclode_V1_DeleteSessionRequest()
            req.sessionID = id
            proto.message = .deleteSession(req)
            
        case .sessionDeleteAll:
            proto.message = .deleteAllSessions(Netclode_V1_DeleteAllSessionsRequest())
            
        case .prompt(let sessionId, let text):
            var req = Netclode_V1_SendPromptRequest()
            req.sessionID = sessionId
            req.text = text
            proto.message = .sendPrompt(req)
            
        case .promptInterrupt(let sessionId):
            var req = Netclode_V1_InterruptPromptRequest()
            req.sessionID = sessionId
            proto.message = .interruptPrompt(req)
            
        case .terminalInput(let sessionId, let data):
            var req = Netclode_V1_TerminalInputRequest()
            req.sessionID = sessionId
            req.data = data
            proto.message = .terminalInput(req)
            
        case .terminalResize(let sessionId, let cols, let rows):
            var req = Netclode_V1_TerminalResizeRequest()
            req.sessionID = sessionId
            req.cols = Int32(cols)
            req.rows = Int32(rows)
            proto.message = .terminalResize(req)
            
        case .portExpose(let sessionId, let port):
            var req = Netclode_V1_ExposePortRequest()
            req.sessionID = sessionId
            req.port = Int32(port)
            proto.message = .exposePort(req)
            
        case .sessionOpen(let id, let lastMessageId, let lastNotificationId):
            var req = Netclode_V1_OpenSessionRequest()
            req.sessionID = id
            if let lastMessageId = lastMessageId {
                req.lastMessageID = lastMessageId
            }
            if let lastNotificationId = lastNotificationId {
                req.lastNotificationID = lastNotificationId
            }
            proto.message = .openSession(req)
            
        case .githubReposList:
            proto.message = .listGithubRepos(Netclode_V1_ListGitHubReposRequest())
            
        case .gitStatus(let sessionId):
            var req = Netclode_V1_GitStatusRequest()
            req.sessionID = sessionId
            proto.message = .gitStatus(req)
            
        case .gitDiff(let sessionId, let file):
            var req = Netclode_V1_GitDiffRequest()
            req.sessionID = sessionId
            if let file = file {
                req.file = file
            }
            proto.message = .gitDiff(req)
        }
        
        return proto
    }
    
    // MARK: - Session Operations
    
    func openSession(id: String, lastMessageId: String? = nil, lastNotificationId: String? = nil, resume: Bool = true) {
        print("[Connect] openSession called for \(id), connectionState=\(connectionState), resume=\(resume)")
        send(.sessionOpen(id: id, lastMessageId: lastMessageId, lastNotificationId: lastNotificationId))
        if resume {
            send(.sessionResume(id: id))
        }
    }
}
