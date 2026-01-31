import SwiftUI

// MARK: - Unified Diff View (for git diff output)

/// A view that renders parsed unified diff content (git diff format)
struct UnifiedDiffView: View {
    let files: [UnifiedDiffFile]
    let showFileHeaders: Bool
    
    init(diffContent: String, showFileHeaders: Bool = true) {
        self.files = UnifiedDiffParser.parse(diffContent)
        self.showFileHeaders = showFileHeaders
    }
    
    init(files: [UnifiedDiffFile], showFileHeaders: Bool = true) {
        self.files = files
        self.showFileHeaders = showFileHeaders
    }
    
    var body: some View {
        if files.isEmpty {
            emptyState
        } else {
            ScrollView(.horizontal, showsIndicators: false) {
                LazyVStack(alignment: .leading, spacing: Theme.Spacing.md) {
                    ForEach(files) { file in
                        FileDiffSection(file: file, showHeader: showFileHeaders)
                    }
                }
            }
        }
    }
    
    private var emptyState: some View {
        VStack(spacing: Theme.Spacing.sm) {
            Image(systemName: "checkmark.circle")
                .font(.system(size: 32))
                .foregroundStyle(Theme.Colors.success)
            Text("No changes")
                .font(.netclodeBody)
                .foregroundStyle(.secondary)
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, Theme.Spacing.xl)
    }
}

// MARK: - File Diff Section

struct FileDiffSection: View {
    let file: UnifiedDiffFile
    var showHeader: Bool = true
    @State private var isExpanded = true
    @Environment(\.colorScheme) private var colorScheme

    /// Detect language from file path
    private var detectedLanguage: String? {
        LanguageDetector.language(for: file.newPath)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            // File header (optional)
            if showHeader {
                Button {
                    withAnimation(.snappy(duration: 0.2)) {
                        isExpanded.toggle()
                    }
                } label: {
                    HStack(spacing: Theme.Spacing.xs) {
                        // Expand/collapse chevron
                        Image(systemName: "chevron.right")
                            .font(.system(size: TypeScale.micro, weight: .semibold))
                            .foregroundStyle(.tertiary)
                            .rotationEffect(.degrees(isExpanded ? 90 : 0))

                        // Status badge
                        FileStatusBadge(status: file.status)

                        // File path
                        Text(file.displayPath)
                            .font(.netclodeMonospacedSmall)
                            .foregroundStyle(.primary)
                            .lineLimit(1)
                            .truncationMode(.middle)

                        Spacer()

                        // Stats
                        HStack(spacing: Theme.Spacing.xxs) {
                            if stats.additions > 0 {
                                Text("+\(stats.additions)")
                                    .foregroundStyle(DiffColors.additionText)
                            }
                            if stats.deletions > 0 {
                                Text("-\(stats.deletions)")
                                    .foregroundStyle(DiffColors.deletionText)
                            }
                        }
                        .font(.system(size: TypeScale.caption, weight: .medium, design: .monospaced))
                    }
                    .padding(.horizontal, Theme.Spacing.sm)
                    .padding(.vertical, Theme.Spacing.xs)
                    .background(DiffColors.hunkHeader)
                }
                .buttonStyle(.plain)
            }

            // Hunks (always expanded if header is hidden)
            if isExpanded || !showHeader {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(file.hunks) { hunk in
                        HunkSection(hunk: hunk, language: detectedLanguage, colorScheme: colorScheme)
                    }
                }
            }
        }
    }

    private var stats: (additions: Int, deletions: Int) {
        var additions = 0
        var deletions = 0
        for hunk in file.hunks {
            for line in hunk.lines {
                switch line.type {
                case .addition: additions += 1
                case .deletion: deletions += 1
                case .context: break
                }
            }
        }
        return (additions, deletions)
    }
}

// MARK: - File Status Badge

struct FileStatusBadge: View {
    let status: DiffFileStatus
    
    var body: some View {
        Text(status.shortLabel)
            .font(.system(size: TypeScale.micro, weight: .semibold, design: .monospaced))
            .foregroundStyle(statusColor)
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .background(statusColor.opacity(0.15))
            .clipShape(RoundedRectangle(cornerRadius: 4))
    }
    
    private var statusColor: Color {
        switch status {
        case .added: return DiffColors.additionText
        case .deleted: return DiffColors.deletionText
        case .modified: return .orange
        case .renamed: return .purple
        }
    }
}

extension DiffFileStatus {
    var shortLabel: String {
        switch self {
        case .added: return "A"
        case .deleted: return "D"
        case .modified: return "M"
        case .renamed: return "R"
        }
    }
}

// MARK: - Hunk Section

struct HunkSection: View {
    let hunk: DiffHunk
    let language: String?
    let colorScheme: ColorScheme

    init(hunk: DiffHunk, language: String? = nil, colorScheme: ColorScheme = .dark) {
        self.hunk = hunk
        self.language = language
        self.colorScheme = colorScheme
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            // Hunk header
            HStack(spacing: 0) {
                Text(hunk.header)
                    .font(.system(size: TypeScale.tiny, weight: .medium, design: .monospaced))
                    .foregroundStyle(DiffColors.hunkHeaderText)

                if let context = hunk.contextLabel, !context.isEmpty {
                    Text(" ")
                    Text(context)
                        .font(.system(size: TypeScale.tiny, design: .monospaced))
                        .foregroundStyle(DiffColors.hunkHeaderText.opacity(0.7))
                }

                Spacer()
            }
            .padding(.horizontal, Theme.Spacing.sm)
            .padding(.vertical, Theme.Spacing.xxs)
            .background(DiffColors.hunkHeader.opacity(0.5))

            // Lines with syntax highlighting
            ForEach(hunk.lines) { line in
                DiffLineView(line: line, showLineNumbers: true, language: language, colorScheme: colorScheme)
            }
        }
    }
}

// MARK: - Previews

#Preview("Unified Diff View") {
    let sampleDiff = """
    diff --git a/src/App.swift b/src/App.swift
    index abc1234..def5678 100644
    --- a/src/App.swift
    +++ b/src/App.swift
    @@ -10,7 +10,9 @@ struct App {
         let name: String
         
         func greet() {
    -        print("Hello")
    +        print("Hello, World!")
    +        print("Welcome")
         }
         
         func run() {
    @@ -25,6 +27,7 @@ struct App {
         func stop() {
             isRunning = false
    +        cleanup()
         }
     }
    """
    
    ScrollView {
        UnifiedDiffView(diffContent: sampleDiff)
            .padding()
    }
    .background(Color.black.opacity(0.9))
}

#Preview("Unified Diff View - Multiple Files") {
    let sampleDiff = """
    diff --git a/README.md b/README.md
    new file mode 100644
    --- /dev/null
    +++ b/README.md
    @@ -0,0 +1,3 @@
    +# My Project
    +
    +This is a sample project.
    diff --git a/src/main.swift b/src/main.swift
    index 111111..222222 100644
    --- a/src/main.swift
    +++ b/src/main.swift
    @@ -1,5 +1,6 @@
     import Foundation
     
    +import MyLibrary
     
     func main() {
    -    print("Starting...")
    +    MyLibrary.start()
     }
    """
    
    ScrollView {
        UnifiedDiffView(diffContent: sampleDiff)
            .padding()
    }
    .background(Color.black.opacity(0.9))
}

#Preview("Empty Diff") {
    UnifiedDiffView(diffContent: "")
        .padding()
        .background(Color.black.opacity(0.9))
}
