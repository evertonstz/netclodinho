import SwiftUI

// MARK: - Diff View (for Edit tool old/new string comparison)

/// A view that displays a diff between two strings with word-level highlighting
struct DiffView: View {
    let oldString: String
    let newString: String
    
    private var diffResult: DiffResult {
        DiffEngine.computeDiff(old: oldString, new: newString)
    }
    
    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(diffResult.lines) { line in
                DiffLineView(line: line)
            }
        }
        .font(.system(size: 12, design: .monospaced))
        .background(DiffColors.background)
        .clipShape(RoundedRectangle(cornerRadius: 6))
    }
}

// MARK: - Diff Line View

struct DiffLineView: View {
    let line: DiffLine
    let showLineNumbers: Bool
    
    init(line: DiffLine, showLineNumbers: Bool = false) {
        self.line = line
        self.showLineNumbers = showLineNumbers
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
            
            // Content with word highlights
            if !line.wordHighlights.isEmpty {
                HighlightedTextView(
                    text: line.content,
                    highlights: line.wordHighlights,
                    baseColor: textColor,
                    highlightColor: wordHighlightColor
                )
            } else {
                Text(line.content)
                    .foregroundStyle(textColor)
            }
            
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

// MARK: - Highlighted Text View

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
            newString: "func greet(name: String) {\n    print(\"Hello, World!\")\n    print(\"Welcome!\")\n}"
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
