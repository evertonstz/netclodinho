import SwiftUI

struct ChatMessageRow: View {
    let message: ChatMessage
    var isStreaming: Bool = false
    var turnDuration: TimeInterval? = nil

    private var isUser: Bool {
        message.role == .user
    }

    var body: some View {
        VStack(alignment: isUser ? .trailing : .leading, spacing: Theme.Spacing.xxs) {
            HStack(alignment: .top, spacing: Theme.Spacing.sm) {
                // Content
                if isUser {
                    Text(message.content)
                        .font(.netclodeBody)
                        .foregroundStyle(.primary)
                        .textSelection(.enabled)
                } else {
                    MessageContent(content: message.content, isStreaming: isStreaming)
                }

                if isStreaming {
                    ProgressView()
                        .scaleEffect(0.6)
                }
            }

            // Duration indicator for completed assistant messages
            if message.role == .assistant, !isStreaming, let duration = turnDuration {
                Text(formatDuration(duration))
                    .font(.system(size: 10, weight: .medium, design: .monospaced))
                    .foregroundStyle(.tertiary)
            }
        }
        .padding(Theme.Spacing.sm)
        .frame(maxWidth: .infinity, alignment: isUser ? .trailing : .leading)
        .background(
            isUser
                ? Theme.Colors.brand.opacity(0.06)
                : Color.clear
        )
        .overlay(
            Rectangle()
                .fill(Theme.Colors.brand)
                .frame(width: 2),
            alignment: isUser ? .trailing : .leading
        )
    }

    private func formatDuration(_ duration: TimeInterval) -> String {
        if duration < 1 {
            return String(format: "%.0fms", duration * 1000)
        } else if duration < 60 {
            return String(format: "%.1fs", duration)
        } else {
            let minutes = Int(duration) / 60
            let seconds = Int(duration) % 60
            return "\(minutes)m \(seconds)s"
        }
    }
}

// MARK: - Message Content (with full markdown support via swift-markdown)

struct MessageContent: View {
    let content: String
    var isStreaming: Bool = false

    /// Process content for streaming - close incomplete markdown constructs
    private var processedContent: String {
        guard isStreaming else { return content }

        var result = content

        // Close incomplete code blocks
        let tripleBackticks = result.components(separatedBy: "```").count - 1
        if tripleBackticks % 2 != 0 {
            result += "\n```"
        }

        // Close incomplete bold markers
        let boldMarkers = result.components(separatedBy: "**").count - 1
        if boldMarkers % 2 != 0 {
            result += "**"
        }

        // Close incomplete italic markers (single *)
        // Count unescaped single asterisks that aren't part of **
        let withoutBold = result.replacingOccurrences(of: "**", with: "")
        let italicMarkers = withoutBold.components(separatedBy: "*").count - 1
        if italicMarkers % 2 != 0 {
            result += "*"
        }

        // Close incomplete strikethrough
        let strikeMarkers = result.components(separatedBy: "~~").count - 1
        if strikeMarkers % 2 != 0 {
            result += "~~"
        }

        // Close incomplete inline code
        let inlineCodeMarkers = withoutBold.filter { $0 == "`" }.count
        // Only close if we have an odd number of backticks not part of code blocks
        let codeBlockBackticks = tripleBackticks * 3
        let remainingBackticks = inlineCodeMarkers - codeBlockBackticks
        if remainingBackticks % 2 != 0 {
            result += "`"
        }

        return result
    }

    var body: some View {
        MarkdownView(content: processedContent)
    }
}

// MARK: - Preview

#Preview {
    ScrollView {
        VStack(spacing: 16) {
            ChatMessageRow(message: ChatMessage.previewUser)
            ChatMessageRow(message: ChatMessage.previewAssistant)
        }
        .padding()
    }
    .background(Theme.Colors.background)
}
