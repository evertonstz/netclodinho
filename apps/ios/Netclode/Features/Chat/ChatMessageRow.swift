import SwiftUI

struct ChatMessageRow: View {
    let message: ChatMessage
    var isStreaming: Bool = false

    var body: some View {
        HStack(alignment: .top, spacing: Theme.Spacing.sm) {
            if message.role == .user {
                Spacer(minLength: 60)
            }

            VStack(alignment: message.role == .user ? .trailing : .leading, spacing: Theme.Spacing.xxs) {
                // Avatar and role
                HStack(spacing: Theme.Spacing.xs) {
                    if message.role == .assistant {
                        avatarView
                    }

                    Text(message.role == .user ? "You" : "Claude")
                        .font(.netclodeCaption)
                        .foregroundStyle(.secondary)

                    if message.role == .user {
                        avatarView
                    }
                }

                // Message content
                Group {
                    if message.role == .user {
                        UserMessageCard {
                            Text(message.content)
                                .font(.netclodeBody)
                                .textSelection(.enabled)
                        }
                    } else {
                        AssistantMessageCard {
                            MessageContent(content: message.content, isStreaming: isStreaming)
                        }
                    }
                }
            }

            if message.role == .assistant {
                Spacer(minLength: 40)
            }
        }
    }

    private var avatarView: some View {
        ZStack {
            Circle()
                .fill(message.role == .user ? Theme.Colors.gentleBlue : Theme.Colors.warmApricot)
                .frame(width: 24, height: 24)

            Image(systemName: message.role == .user ? "person.fill" : "brain.head.profile")
                .font(.system(size: 12))
                .foregroundStyle(.white)
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
                        .textSelection(.enabled)

                case .code(let code, let language):
                    CodeBlock(code: code, language: language, isStreaming: isStreaming)

                case .inlineCode(let code):
                    Text(code)
                        .font(.netclodeMonospacedSmall)
                        .padding(.horizontal, 4)
                        .padding(.vertical, 2)
                        .background(Theme.Colors.softCharcoal.opacity(0.1))
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
            .background(Theme.Colors.softCharcoal.opacity(0.05))

            // Code
            ScrollView(.horizontal, showsIndicators: false) {
                Text(code)
                    .font(.netclodeMonospacedSmall)
                    .textSelection(.enabled)
                    .padding(Theme.Spacing.sm)
            }
        }
        .background(Theme.Colors.softCharcoal.opacity(0.08))
        .clipShape(RoundedRectangle(cornerRadius: Theme.Radius.sm))
    }
}

// MARK: - Preview

#Preview {
    ZStack {
        WarmGradientBackground()

        ScrollView {
            VStack(spacing: 16) {
                ChatMessageRow(message: ChatMessage.previewUser)
                ChatMessageRow(message: ChatMessage.previewAssistant)
            }
            .padding()
        }
    }
}
