import Foundation

@Observable
final class TerminalStore: @unchecked Sendable {
    private(set) var outputBySession: [String: String] = [:]

    private let maxOutputLength = 50_000

    func output(for sessionId: String) -> String {
        outputBySession[sessionId] ?? ""
    }

    func appendOutput(sessionId: String, data: String) {
        var output = outputBySession[sessionId] ?? ""
        output += data

        // Trim if too long, keeping the most recent content
        if output.count > maxOutputLength {
            let startIndex = output.index(output.endIndex, offsetBy: -maxOutputLength)
            output = String(output[startIndex...])
        }

        outputBySession[sessionId] = output
    }

    func clearOutput(for sessionId: String) {
        outputBySession.removeValue(forKey: sessionId)
    }
}
