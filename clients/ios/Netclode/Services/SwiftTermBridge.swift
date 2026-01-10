import Foundation
import SwiftTerm
import UIKit

/// Bridges WebSocket terminal data to/from SwiftTerm's TerminalView.
/// Each session has its own bridge instance.
@MainActor
final class SwiftTermBridge: TerminalViewDelegate {
    let sessionId: String
    weak var webSocketService: WebSocketService?
    
    /// Reference to the terminal view (set when view is created)
    weak var terminalView: SwiftTerm.TerminalView?
    
    /// Buffered output before terminal view is attached
    private var pendingData: [UInt8] = []
    
    /// Current terminal dimensions
    private(set) var cols: Int = 80
    private(set) var rows: Int = 24
    
    init(sessionId: String, webSocketService: WebSocketService?) {
        self.sessionId = sessionId
        self.webSocketService = webSocketService
    }
    
    /// Attach a terminal view to this bridge
    func attach(_ terminal: SwiftTerm.TerminalView) {
        self.terminalView = terminal
        terminal.terminalDelegate = self
        
        // Feed any pending data (already preprocessed)
        if !pendingData.isEmpty {
            terminal.feed(byteArray: ArraySlice(pendingData))
            pendingData.removeAll()
        }
    }
    
    /// Detach the terminal view
    func detach() {
        terminalView?.terminalDelegate = nil
        terminalView = nil
    }
    
    /// Feed data from WebSocket to the terminal
    func feedData(_ data: String) {
        guard let bytes = data.data(using: .utf8) else { return }
        let byteArray = [UInt8](bytes)
        let processed = preprocessBytes(byteArray)
        
        if let terminal = terminalView {
            terminal.feed(byteArray: ArraySlice(processed))
        } else {
            // Buffer until terminal is attached
            pendingData.append(contentsOf: processed)
            
            // Limit buffer size (100KB)
            if pendingData.count > 100_000 {
                pendingData.removeFirst(pendingData.count - 100_000)
            }
        }
    }
    
    /// Feed raw bytes from WebSocket to the terminal
    func feedData(_ bytes: [UInt8]) {
        let processed = preprocessBytes(bytes)
        
        if let terminal = terminalView {
            terminal.feed(byteArray: ArraySlice(processed))
        } else {
            pendingData.append(contentsOf: processed)
            if pendingData.count > 100_000 {
                pendingData.removeFirst(pendingData.count - 100_000)
            }
        }
    }
    
    /// Convert lone LF (\n) to CR+LF (\r\n) for proper terminal rendering
    /// This handles the case where the server sends Unix-style line endings
    private func preprocessBytes(_ bytes: [UInt8]) -> [UInt8] {
        var result: [UInt8] = []
        result.reserveCapacity(bytes.count)
        
        var i = 0
        while i < bytes.count {
            let byte = bytes[i]
            if byte == 0x0A { // LF
                // Check if preceded by CR
                if result.last != 0x0D {
                    result.append(0x0D) // Add CR before LF
                }
            }
            result.append(byte)
            i += 1
        }
        return result
    }
    
    // MARK: - TerminalViewDelegate
    
    /// Called when the terminal has data to send (user input)
    nonisolated func send(source: SwiftTerm.TerminalView, data: ArraySlice<UInt8>) {
        let bytes = Array(data)
        guard let string = String(bytes: bytes, encoding: .utf8) else { return }
        
        Task { @MainActor in
            self.webSocketService?.send(.terminalInput(sessionId: self.sessionId, data: string))
        }
    }
    
    /// Called when the terminal is scrolled
    nonisolated func scrolled(source: SwiftTerm.TerminalView, position: Double) {
        // No-op
    }
    
    /// Called when the terminal title changes (from escape sequences)
    nonisolated func setTerminalTitle(source: SwiftTerm.TerminalView, title: String) {
        // Could update UI title if needed
    }
    
    /// Called when the terminal size changes
    nonisolated func sizeChanged(source: SwiftTerm.TerminalView, newCols: Int, newRows: Int) {
        Task { @MainActor in
            self.cols = newCols
            self.rows = newRows
            self.webSocketService?.send(.terminalResize(sessionId: self.sessionId, cols: newCols, rows: newRows))
        }
    }
    
    /// Called when the terminal requests the clipboard contents
    nonisolated func clipboardCopy(source: SwiftTerm.TerminalView, content: Data) {
        if let string = String(data: content, encoding: .utf8) {
            Task { @MainActor in
                UIPasteboard.general.string = string
            }
        }
    }
    
    /// Called when host should be notified of current directory change
    nonisolated func hostCurrentDirectoryUpdate(source: SwiftTerm.TerminalView, directory: String?) {
        // No-op
    }
    
    /// Called for bell
    nonisolated func bell(source: SwiftTerm.TerminalView) {
        #if !targetEnvironment(macCatalyst)
        Task { @MainActor in
            let generator = UINotificationFeedbackGenerator()
            generator.notificationOccurred(.warning)
        }
        #endif
        // On Mac Catalyst, haptic feedback is not available - bell is silent
    }
    
    /// Request to open a URL
    nonisolated func requestOpenLink(source: SwiftTerm.TerminalView, link: String, params: [String: String]) {
        guard let url = URL(string: link) else { return }
        Task { @MainActor in
            UIApplication.shared.open(url)
        }
    }
    
    /// Called for iTerm2 content
    nonisolated func iTermContent(source: SwiftTerm.TerminalView, content: ArraySlice<UInt8>) {
        // No-op
    }
    
    /// Called when terminal buffer range changes
    nonisolated func rangeChanged(source: SwiftTerm.TerminalView, startY: Int, endY: Int) {
        // No-op
    }
}
