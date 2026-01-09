import Foundation

@MainActor
@Observable
final class TerminalStore {
    /// Raw output (with ANSI codes)
    private var rawOutputBySession: [String: String] = [:]
    /// Pre-processed output (ANSI stripped) for display
    private(set) var outputBySession: [String: String] = [:]

    private let maxOutputLength = 50_000

    // Pre-compiled regex for ANSI escape codes (compiled once, not per-render)
    private static let ansiPattern = try! NSRegularExpression(
        pattern: "\\x1B\\[[0-9;]*[a-zA-Z]",
        options: []
    )

    func output(for sessionId: String) -> String {
        outputBySession[sessionId] ?? ""
    }

    func appendOutput(sessionId: String, data: String) {
        var rawOutput = rawOutputBySession[sessionId] ?? ""
        rawOutput += data

        // Trim if too long, keeping the most recent content
        if rawOutput.count > maxOutputLength {
            let startIndex = rawOutput.index(rawOutput.endIndex, offsetBy: -maxOutputLength)
            rawOutput = String(rawOutput[startIndex...])
        }

        rawOutputBySession[sessionId] = rawOutput

        // Pre-process ANSI codes (do this once on append, not on every render)
        let stripped = Self.stripANSI(rawOutput)
        outputBySession[sessionId] = stripped
    }

    func clearOutput(for sessionId: String) {
        rawOutputBySession.removeValue(forKey: sessionId)
        outputBySession.removeValue(forKey: sessionId)
    }

    /// Strip ANSI escape codes from text
    private static func stripANSI(_ text: String) -> String {
        let range = NSRange(text.startIndex..., in: text)
        return ansiPattern.stringByReplacingMatches(
            in: text,
            options: [],
            range: range,
            withTemplate: ""
        )
    }
}
