import Foundation

@MainActor
@Observable
final class TerminalStore {
    /// Raw output buffer per session (for session restore)
    private var rawOutputBySession: [String: [UInt8]] = [:]
    
    /// Bridges per session
    private var bridgesBySession: [String: SwiftTermBridge] = [:]
    
    /// Reference to Connect service (set during init)
    weak var connectService: ConnectService?
    
    private let maxOutputLength = 100_000 // 100KB buffer per session
    
    /// Get or create a bridge for a session
    func bridge(for sessionId: String) -> SwiftTermBridge {
        if let existing = bridgesBySession[sessionId] {
            return existing
        }
        
        let bridge = SwiftTermBridge(sessionId: sessionId, connectService: connectService)
        bridgesBySession[sessionId] = bridge
        
        // Feed any buffered output to the new bridge
        if let bufferedOutput = rawOutputBySession[sessionId], !bufferedOutput.isEmpty {
            bridge.feedData(bufferedOutput)
        }
        
        return bridge
    }
    
    /// Append output for a session (called from MessageRouter)
    func appendOutput(sessionId: String, data: String) {
        guard let bytes = data.data(using: .utf8) else { return }
        let byteArray = [UInt8](bytes)
        
        // Buffer raw output
        var buffer = rawOutputBySession[sessionId] ?? []
        buffer.append(contentsOf: byteArray)
        
        // Trim if too long, keeping the most recent content
        if buffer.count > maxOutputLength {
            buffer.removeFirst(buffer.count - maxOutputLength)
        }
        rawOutputBySession[sessionId] = buffer
        
        // Feed to bridge if it exists
        if let bridge = bridgesBySession[sessionId] {
            bridge.feedData(byteArray)
        }
    }
    
    /// Clear output and bridge for a session
    func clearOutput(for sessionId: String) {
        rawOutputBySession.removeValue(forKey: sessionId)
        if let bridge = bridgesBySession.removeValue(forKey: sessionId) {
            bridge.detach()
        }
    }
    
    /// Get buffered output for a session (for debugging/export)
    func bufferedOutput(for sessionId: String) -> [UInt8] {
        rawOutputBySession[sessionId] ?? []
    }
}
