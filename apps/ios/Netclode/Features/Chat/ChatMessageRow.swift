import SwiftUI

struct ChatMessageRow: View {
    let message: ChatMessage
    var isStreaming: Bool = false
    var turnDuration: TimeInterval? = nil

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Spacing.xxs) {
            HStack(alignment: .top, spacing: Theme.Spacing.sm) {
                // Content
                if message.role == .user {
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
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(
            message.role == .user
                ? Theme.Colors.brand.opacity(0.06)
                : Color.clear
        )
        .overlay(
            message.role == .user
                ? Rectangle()
                    .fill(Theme.Colors.brand)
                    .frame(width: 2)
                : nil,
            alignment: .leading
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

// MARK: - Message Content (with basic markdown support)

struct MessageContent: View {
    let content: String
    var isStreaming: Bool = false

    private var processedContent: String {
        guard isStreaming else { return content }

        // Close incomplete code blocks during streaming
        let tripleBackticks = content.components(separatedBy: "```").count - 1
        if tripleBackticks % 2 != 0 {
            return content + "\n```"
        }
        return content
    }

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Spacing.sm) {
            ForEach(Array(parseContent().enumerated()), id: \.offset) { _, block in
                switch block {
                case .text(let text):
                    Text(text)
                        .font(.netclodeBody)
                        .foregroundStyle(.primary)
                        .textSelection(.enabled)

                case .code(let code, let language):
                    CodeBlock(code: code, language: language, isStreaming: isStreaming)

                case .inlineCode(let code):
                    Text(code)
                        .font(.netclodeMonospacedSmall)
                        .foregroundStyle(Theme.Colors.codeText)
                        .padding(.horizontal, 4)
                        .padding(.vertical, 2)
                        .background(Theme.Colors.codeBackground)
                        .clipShape(RoundedRectangle(cornerRadius: 4))
                }
            }
        }
    }

    enum ContentBlock {
        case text(String)
        case code(String, String?)
        case inlineCode(String)
    }

    private func parseContent() -> [ContentBlock] {
        var blocks: [ContentBlock] = []
        var remaining = processedContent

        // Simple parsing for code blocks using Regex
        let codeBlockPattern = try! Regex(#"```(\w*)\n?([\s\S]*?)```"#)
        let inlineCodePattern = try! Regex(#"`([^`]+)`"#)

        while let match = remaining.firstMatch(of: codeBlockPattern) {
            let before = String(remaining[..<match.range.lowerBound])
            if !before.isEmpty {
                blocks.append(.text(before))
            }

            let language = match.output.count > 1 ? String(remaining[match.output[1].range!]) : nil
            let code = match.output.count > 2 ? String(remaining[match.output[2].range!]).trimmingCharacters(in: .whitespacesAndNewlines) : ""
            blocks.append(.code(code, language?.isEmpty == true ? nil : language))

            remaining = String(remaining[match.range.upperBound...])
        }

        if !remaining.isEmpty {
            // Check for inline code in remaining text
            var textWithInlineCode = remaining
            var hasInlineCode = false

            while let match = textWithInlineCode.firstMatch(of: inlineCodePattern) {
                hasInlineCode = true
                let before = String(textWithInlineCode[..<match.range.lowerBound])
                if !before.isEmpty {
                    blocks.append(.text(before))
                }
                if match.output.count > 1, let range = match.output[1].range {
                    blocks.append(.inlineCode(String(textWithInlineCode[range])))
                }
                textWithInlineCode = String(textWithInlineCode[match.range.upperBound...])
            }

            if !textWithInlineCode.isEmpty {
                blocks.append(.text(textWithInlineCode))
            } else if !hasInlineCode {
                blocks.append(.text(remaining))
            }
        }

        return blocks.isEmpty ? [.text(content)] : blocks
    }
}

// MARK: - Code Block

struct CodeBlock: View {
    let code: String
    let language: String?
    var isStreaming: Bool = false

    @State private var isCopied = false

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            // Header
            HStack {
                if let language, !language.isEmpty {
                    Text(language.uppercased())
                        .font(.netclodeCaption)
                        .foregroundStyle(.secondary)
                }

                if isStreaming {
                    ProgressView()
                        .scaleEffect(0.6)
                        .frame(width: 12, height: 12)
                }

                Spacer()

                if !isStreaming {
                    Button {
                        UIPasteboard.general.string = code
                        isCopied = true
                        HapticFeedback.success()

                        DispatchQueue.main.asyncAfter(deadline: .now() + 2) {
                            isCopied = false
                        }
                    } label: {
                        Label(isCopied ? "Copied!" : "Copy", systemImage: isCopied ? "checkmark" : "doc.on.doc")
                            .font(.netclodeCaption)
                    }
                }
            }
            .padding(.horizontal, Theme.Spacing.sm)
            .padding(.vertical, Theme.Spacing.xs)
            .background(Theme.Colors.codeBackground.opacity(0.5))

            // Code
            ScrollView(.horizontal, showsIndicators: false) {
                Text(code)
                    .font(.netclodeMonospacedSmall)
                    .foregroundStyle(Theme.Colors.codeText)
                    .textSelection(.enabled)
                    .padding(Theme.Spacing.sm)
            }
        }
        .background(Theme.Colors.codeBackground)
        .clipShape(RoundedRectangle(cornerRadius: Theme.Radius.sm))
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
