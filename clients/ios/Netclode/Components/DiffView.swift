import SwiftUI

// MARK: - Diff View (for Edit tool old/new string comparison)

/// A view that displays a diff between two strings with word-level highlighting and optional syntax highlighting
struct DiffView: View {
    let oldString: String
    let newString: String
    let language: String?
    let maxLines: Int
    
    @Environment(\.colorScheme) private var colorScheme
    @State private var isFullyExpanded = false

    init(oldString: String, newString: String, language: String? = nil, maxLines: Int = 20) {
        self.oldString = oldString
        self.newString = newString
        self.language = language
        self.maxLines = maxLines
    }

    /// Initialize with a file path for automatic language detection
    init(oldString: String, newString: String, filePath: String?, maxLines: Int = 20) {
        self.oldString = oldString
        self.newString = newString
        self.language = filePath.flatMap { LanguageDetector.language(for: $0) }
        self.maxLines = maxLines
    }

    private var diffResult: DiffResult {
        DiffEngine.computeDiff(old: oldString, new: newString)
    }
    
    private var isTruncated: Bool {
        diffResult.lines.count > maxLines
    }
    
    private var displayedLines: ArraySlice<DiffLine> {
        if isFullyExpanded || !isTruncated {
            return diffResult.lines[...]
        }
        return diffResult.lines.prefix(maxLines)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Spacing.xs) {
            VStack(alignment: .leading, spacing: 0) {
                ForEach(displayedLines) { line in
                    DiffLineView(line: line, language: language, colorScheme: colorScheme)
                }
            }
            .font(.system(size: 12, design: .monospaced))
            .background(DiffColors.background)
            .clipShape(RoundedRectangle(cornerRadius: 6))
            
            // Show more button
            if isTruncated {
                Button {
                    withAnimation(.snappy(duration: 0.2)) {
                        isFullyExpanded.toggle()
                    }
                } label: {
                    HStack(spacing: 4) {
                        Text(isFullyExpanded ? "Show less" : "Show all \(diffResult.lines.count) lines")
                            .font(.system(size: TypeScale.caption, weight: .medium))
                        Image(systemName: isFullyExpanded ? "chevron.up" : "chevron.down")
                            .font(.system(size: TypeScale.tiny))
                    }
                    .foregroundStyle(Theme.Colors.brand)
                }
                .buttonStyle(.plain)
            }
        }
    }
}

// MARK: - Diff Line View

struct DiffLineView: View {
    let line: DiffLine
    let showLineNumbers: Bool
    let language: String?
    let colorScheme: ColorScheme

    init(
        line: DiffLine,
        showLineNumbers: Bool = false,
        language: String? = nil,
        colorScheme: ColorScheme = .dark
    ) {
        self.line = line
        self.showLineNumbers = showLineNumbers
        self.language = language
        self.colorScheme = colorScheme
    }

    var body: some View {
        HStack(alignment: .top, spacing: 0) {
            // Line numbers (optional)
            if showLineNumbers {
                // Old line number
                Text(line.oldLineNumber.map { "\($0)" } ?? "")
                    .frame(width: 28, alignment: .trailing)
                    .foregroundStyle(DiffColors.lineNumber)
                    .padding(.trailing, 2)

                // New line number
                Text(line.newLineNumber.map { "\($0)" } ?? "")
                    .frame(width: 28, alignment: .trailing)
                    .foregroundStyle(DiffColors.lineNumber)
                    .padding(.trailing, 6)
            }

            // Prefix (+/-/space)
            Text(line.type.prefix)
                .foregroundStyle(prefixColor)
                .frame(width: 14)

            // Content with syntax highlighting and word highlights
            SyntaxHighlightedDiffText(
                text: line.content,
                language: language,
                colorScheme: colorScheme,
                wordHighlights: line.wordHighlights,
                fallbackColor: textColor,
                highlightColor: wordHighlightColor
            )

            Spacer(minLength: 0)
        }
        .font(.system(size: 11, design: .monospaced))
        .padding(.horizontal, 6)
        .padding(.vertical, 1)
        .background(backgroundColor)
    }

    private var backgroundColor: Color {
        switch line.type {
        case .context:
            return .clear
        case .deletion:
            return DiffColors.deletionBackground
        case .addition:
            return DiffColors.additionBackground
        }
    }

    private var textColor: Color {
        switch line.type {
        case .context:
            return DiffColors.contextText
        case .deletion:
            return DiffColors.deletionText
        case .addition:
            return DiffColors.additionText
        }
    }

    private var prefixColor: Color {
        switch line.type {
        case .context:
            return DiffColors.contextText.opacity(0.5)
        case .deletion:
            return DiffColors.deletionText
        case .addition:
            return DiffColors.additionText
        }
    }

    private var wordHighlightColor: Color {
        switch line.type {
        case .context:
            return .clear
        case .deletion:
            return DiffColors.deletionHighlight
        case .addition:
            return DiffColors.additionHighlight
        }
    }
}

// MARK: - Syntax Highlighted Diff Text

/// A view that combines syntax highlighting with word-level diff highlights
struct SyntaxHighlightedDiffText: View {
    let text: String
    let language: String?
    let colorScheme: ColorScheme
    let wordHighlights: [HighlightRange]
    let fallbackColor: Color
    let highlightColor: Color

    var body: some View {
        Text(buildAttributedString())
    }

    private func buildAttributedString() -> AttributedString {
        var result = AttributedString(text)

        // First, apply syntax highlighting colors if language is provided
        if let language = language, !text.isEmpty {
            let segments = SyntaxHighlighter.shared.highlightLine(text, language: language, colorScheme: colorScheme)

            // Apply syntax colors to each segment
            var offset = 0
            for segment in segments {
                let segmentLength = segment.text.count
                guard segmentLength > 0, offset < text.count else { continue }

                let startIndex = text.index(text.startIndex, offsetBy: offset, limitedBy: text.endIndex) ?? text.endIndex
                let endIndex = text.index(startIndex, offsetBy: segmentLength, limitedBy: text.endIndex) ?? text.endIndex

                if startIndex < endIndex,
                   let range = Range(NSRange(startIndex..<endIndex, in: text), in: result) {
                    result[range].foregroundColor = segment.color ?? fallbackColor
                }
                offset += segmentLength
            }
        } else {
            // No syntax highlighting, use fallback color
            result.foregroundColor = fallbackColor
        }

        // Then, apply word highlights (background color for changed portions)
        for highlight in wordHighlights {
            guard highlight.start >= 0, highlight.length > 0 else { continue }

            let startIndex = text.index(text.startIndex, offsetBy: highlight.start, limitedBy: text.endIndex) ?? text.endIndex
            let endIndex = text.index(startIndex, offsetBy: highlight.length, limitedBy: text.endIndex) ?? text.endIndex

            if startIndex < endIndex,
               let range = Range(NSRange(startIndex..<endIndex, in: text), in: result) {
                result[range].backgroundColor = highlightColor
            }
        }

        return result
    }
}

// MARK: - Highlighted Text View (Legacy, kept for compatibility)

struct HighlightedTextView: View {
    let text: String
    let highlights: [HighlightRange]
    let baseColor: Color
    let highlightColor: Color

    var body: some View {
        // Build attributed string with highlights
        Text(buildAttributedString())
    }

    private func buildAttributedString() -> AttributedString {
        var result = AttributedString(text)
        result.foregroundColor = baseColor

        // Apply highlights
        for highlight in highlights {
            let startIndex = text.index(text.startIndex, offsetBy: highlight.start, limitedBy: text.endIndex) ?? text.endIndex
            let endIndex = text.index(startIndex, offsetBy: highlight.length, limitedBy: text.endIndex) ?? text.endIndex

            if let range = Range(NSRange(startIndex..<endIndex, in: text), in: result) {
                result[range].backgroundColor = highlightColor
            }
        }

        return result
    }
}

// MARK: - Diff Colors

enum DiffColors {
    static let background = Color.adaptive(
        light: Color(white: 0.98),
        dark: Color(white: 0.08)
    )
    
    // Deletion colors
    static let deletionBackground = Color.adaptive(
        light: Color(red: 1.0, green: 0.9, blue: 0.9),
        dark: Color(red: 0.3, green: 0.1, blue: 0.1)
    )
    static let deletionHighlight = Color.adaptive(
        light: Color(red: 1.0, green: 0.7, blue: 0.7),
        dark: Color(red: 0.5, green: 0.15, blue: 0.15)
    )
    static let deletionText = Color.adaptive(
        light: Color(red: 0.6, green: 0.1, blue: 0.1),
        dark: Color(red: 1.0, green: 0.6, blue: 0.6)
    )
    
    // Addition colors
    static let additionBackground = Color.adaptive(
        light: Color(red: 0.9, green: 1.0, blue: 0.9),
        dark: Color(red: 0.1, green: 0.25, blue: 0.1)
    )
    static let additionHighlight = Color.adaptive(
        light: Color(red: 0.7, green: 1.0, blue: 0.7),
        dark: Color(red: 0.15, green: 0.4, blue: 0.15)
    )
    static let additionText = Color.adaptive(
        light: Color(red: 0.1, green: 0.5, blue: 0.1),
        dark: Color(red: 0.6, green: 1.0, blue: 0.6)
    )
    
    // Context colors
    static let contextText = Color.adaptive(
        light: Color(white: 0.3),
        dark: Color(white: 0.7)
    )
    
    // Line numbers
    static let lineNumber = Color.adaptive(
        light: Color(white: 0.6),
        dark: Color(white: 0.4)
    )
    
    // Hunk header
    static let hunkHeader = Color.adaptive(
        light: Color(red: 0.9, green: 0.95, blue: 1.0),
        dark: Color(red: 0.1, green: 0.15, blue: 0.2)
    )
    static let hunkHeaderText = Color.adaptive(
        light: Color(red: 0.3, green: 0.4, blue: 0.6),
        dark: Color(red: 0.5, green: 0.6, blue: 0.8)
    )
}

// Note: Color.adaptive is defined in Theme.swift

// MARK: - Previews

#Preview("Diff View - Simple") {
    ScrollView {
        DiffView(
            oldString: "Hello World",
            newString: "Hello Swift World"
        )
        .padding()
    }
    .background(Color.black.opacity(0.9))
}

#Preview("Diff View - Multiline") {
    ScrollView {
        DiffView(
            oldString: "func greet() {\n    print(\"Hello\")\n}",
            newString: "func greet(name: String) {\n    print(\"Hello, World!\")\n    print(\"Welcome!\")\n}",
            language: "swift"
        )
        .padding()
    }
    .background(Color.black.opacity(0.9))
}

#Preview("Diff Line View") {
    VStack(spacing: 0) {
        DiffLineView(
            line: DiffLine(type: .context, content: "let a = 1", oldLineNumber: 10, newLineNumber: 10),
            showLineNumbers: true
        )
        DiffLineView(
            line: DiffLine(
                type: .deletion,
                content: "let b = 2",
                oldLineNumber: 11,
                newLineNumber: nil,
                wordHighlights: [HighlightRange(start: 8, length: 1)]
            ),
            showLineNumbers: true
        )
        DiffLineView(
            line: DiffLine(
                type: .addition,
                content: "let b = 3",
                oldLineNumber: nil,
                newLineNumber: 11,
                wordHighlights: [HighlightRange(start: 8, length: 1)]
            ),
            showLineNumbers: true
        )
        DiffLineView(
            line: DiffLine(type: .context, content: "let c = a + b", oldLineNumber: 12, newLineNumber: 12),
            showLineNumbers: true
        )
    }
    .font(.system(size: 12, design: .monospaced))
    .padding()
    .background(Color.black.opacity(0.9))
}
