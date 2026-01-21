import Foundation

// MARK: - Diff Line Types

enum DiffLineType: Equatable {
    case context    // Unchanged line
    case deletion   // Removed line
    case addition   // Added line
    
    var prefix: String {
        switch self {
        case .context: return " "
        case .deletion: return "-"
        case .addition: return "+"
        }
    }
}

// MARK: - Diff Line

struct DiffLine: Identifiable, Equatable {
    let id = UUID()
    let type: DiffLineType
    let content: String
    let oldLineNumber: Int?
    let newLineNumber: Int?
    var wordHighlights: [HighlightRange]
    
    init(
        type: DiffLineType,
        content: String,
        oldLineNumber: Int? = nil,
        newLineNumber: Int? = nil,
        wordHighlights: [HighlightRange] = []
    ) {
        self.type = type
        self.content = content
        self.oldLineNumber = oldLineNumber
        self.newLineNumber = newLineNumber
        self.wordHighlights = wordHighlights
    }
    
    static func == (lhs: DiffLine, rhs: DiffLine) -> Bool {
        lhs.id == rhs.id
    }
}

// MARK: - Highlight Range

struct HighlightRange: Equatable {
    let start: Int  // Character offset
    let length: Int
    
    var range: Range<Int> {
        start..<(start + length)
    }
}

// MARK: - Diff Result

struct DiffResult {
    let lines: [DiffLine]
    let stats: DiffStats
}

struct DiffStats {
    let additions: Int
    let deletions: Int
    
    var totalChanges: Int { additions + deletions }
}

// MARK: - Diff Engine

enum DiffEngine {
    
    /// Compute a diff between two strings, with word-level highlighting for modifications
    static func computeDiff(old: String, new: String) -> DiffResult {
        let oldLines = old.components(separatedBy: "\n")
        let newLines = new.components(separatedBy: "\n")
        
        // Use Swift's built-in difference algorithm
        let diff = newLines.difference(from: oldLines)
        
        // Build a list of changes with their positions
        var removals: [(offset: Int, element: String)] = []
        var insertions: [(offset: Int, element: String)] = []
        
        for change in diff {
            switch change {
            case .remove(let offset, let element, _):
                removals.append((offset, element))
            case .insert(let offset, let element, _):
                insertions.append((offset, element))
            }
        }
        
        // Sort by offset
        removals.sort { $0.offset < $1.offset }
        insertions.sort { $0.offset < $1.offset }
        
        // Build the diff lines by walking through both old and new
        var result: [DiffLine] = []
        var oldIdx = 0
        var newIdx = 0
        var removalIdx = 0
        var insertionIdx = 0
        
        // Track line numbers
        var oldLineNum = 1
        var newLineNum = 1
        
        while oldIdx < oldLines.count || newIdx < newLines.count {
            // Check if current old line is removed
            let isRemoval = removalIdx < removals.count && removals[removalIdx].offset == oldIdx
            // Check if current new line is inserted
            let isInsertion = insertionIdx < insertions.count && insertions[insertionIdx].offset == newIdx
            
            if isRemoval && isInsertion {
                // Both removal and insertion at same conceptual position - this is a modification
                let oldLine = removals[removalIdx].element
                let newLine = insertions[insertionIdx].element
                
                // Compute word-level diff
                let (oldHighlights, newHighlights) = computeWordDiff(old: oldLine, new: newLine)
                
                result.append(DiffLine(
                    type: .deletion,
                    content: oldLine,
                    oldLineNumber: oldLineNum,
                    newLineNumber: nil,
                    wordHighlights: oldHighlights
                ))
                result.append(DiffLine(
                    type: .addition,
                    content: newLine,
                    oldLineNumber: nil,
                    newLineNumber: newLineNum,
                    wordHighlights: newHighlights
                ))
                
                oldIdx += 1
                newIdx += 1
                oldLineNum += 1
                newLineNum += 1
                removalIdx += 1
                insertionIdx += 1
                
            } else if isRemoval {
                // Pure deletion
                result.append(DiffLine(
                    type: .deletion,
                    content: removals[removalIdx].element,
                    oldLineNumber: oldLineNum,
                    newLineNumber: nil
                ))
                oldIdx += 1
                oldLineNum += 1
                removalIdx += 1
                
            } else if isInsertion {
                // Pure insertion
                result.append(DiffLine(
                    type: .addition,
                    content: insertions[insertionIdx].element,
                    oldLineNumber: nil,
                    newLineNumber: newLineNum
                ))
                newIdx += 1
                newLineNum += 1
                insertionIdx += 1
                
            } else if oldIdx < oldLines.count && newIdx < newLines.count {
                // Context line (unchanged)
                result.append(DiffLine(
                    type: .context,
                    content: oldLines[oldIdx],
                    oldLineNumber: oldLineNum,
                    newLineNumber: newLineNum
                ))
                oldIdx += 1
                newIdx += 1
                oldLineNum += 1
                newLineNum += 1
            } else {
                break
            }
        }
        
        let stats = DiffStats(
            additions: insertions.count,
            deletions: removals.count
        )
        
        return DiffResult(lines: result, stats: stats)
    }
    
    /// Compute word-level diff highlights between two lines
    static func computeWordDiff(old: String, new: String) -> (old: [HighlightRange], new: [HighlightRange]) {
        let oldTokens = tokenize(old)
        let newTokens = tokenize(new)
        
        // Use Swift's built-in diff on tokens
        let diff = newTokens.difference(from: oldTokens)
        
        var removedIndices = Set<Int>()
        var insertedIndices = Set<Int>()
        
        for change in diff {
            switch change {
            case .remove(let offset, _, _):
                removedIndices.insert(offset)
            case .insert(let offset, _, _):
                insertedIndices.insert(offset)
            }
        }
        
        // Convert token indices to character ranges
        let oldHighlights = computeHighlightRanges(tokens: oldTokens, highlightedIndices: removedIndices)
        let newHighlights = computeHighlightRanges(tokens: newTokens, highlightedIndices: insertedIndices)
        
        return (oldHighlights, newHighlights)
    }
    
    /// Tokenize a string into words and whitespace, preserving positions
    private static func tokenize(_ text: String) -> [Token] {
        var tokens: [Token] = []
        var currentToken = ""
        var currentStart = 0
        var isInWord = false
        
        for (index, char) in text.enumerated() {
            let charIsWord = !char.isWhitespace
            
            if charIsWord != isInWord {
                // Transition - save current token if any
                if !currentToken.isEmpty {
                    tokens.append(Token(text: currentToken, start: currentStart))
                }
                currentToken = String(char)
                currentStart = index
                isInWord = charIsWord
            } else {
                currentToken.append(char)
            }
        }
        
        // Don't forget the last token
        if !currentToken.isEmpty {
            tokens.append(Token(text: currentToken, start: currentStart))
        }
        
        return tokens
    }
    
    private static func computeHighlightRanges(tokens: [Token], highlightedIndices: Set<Int>) -> [HighlightRange] {
        var ranges: [HighlightRange] = []
        
        for (index, token) in tokens.enumerated() {
            if highlightedIndices.contains(index) {
                ranges.append(HighlightRange(start: token.start, length: token.text.count))
            }
        }
        
        // Merge adjacent ranges
        return mergeAdjacentRanges(ranges)
    }
    
    private static func mergeAdjacentRanges(_ ranges: [HighlightRange]) -> [HighlightRange] {
        guard !ranges.isEmpty else { return [] }
        
        let sorted = ranges.sorted { $0.start < $1.start }
        var merged: [HighlightRange] = []
        var current = sorted[0]
        
        for range in sorted.dropFirst() {
            if range.start <= current.start + current.length {
                // Overlapping or adjacent - merge
                let newEnd = max(current.start + current.length, range.start + range.length)
                current = HighlightRange(start: current.start, length: newEnd - current.start)
            } else {
                merged.append(current)
                current = range
            }
        }
        merged.append(current)
        
        return merged
    }
}

// MARK: - Token

private struct Token: Equatable, Hashable {
    let text: String
    let start: Int
    
    // For diff comparison, only compare text content
    static func == (lhs: Token, rhs: Token) -> Bool {
        lhs.text == rhs.text
    }
    
    func hash(into hasher: inout Hasher) {
        hasher.combine(text)
    }
}

// MARK: - Unified Diff Parser

struct UnifiedDiffFile: Identifiable {
    let id = UUID()
    let oldPath: String
    let newPath: String
    let status: DiffFileStatus
    let hunks: [DiffHunk]
}

enum DiffFileStatus: String {
    case modified
    case added
    case deleted
    case renamed
    
    var displayName: String {
        rawValue.capitalized
    }
}

struct DiffHunk: Identifiable {
    let id = UUID()
    let header: String              // "@@ -10,7 +10,9 @@"
    let oldStart: Int
    let oldCount: Int
    let newStart: Int
    let newCount: Int
    let contextLabel: String?       // e.g., "struct App {"
    let lines: [DiffLine]
}

enum UnifiedDiffParser {
    
    /// Parse git unified diff output into structured data
    static func parse(_ content: String) -> [UnifiedDiffFile] {
        guard !content.isEmpty else { return [] }
        
        // Split by file boundaries
        let filePattern = "diff --git "
        let fileBlocks = content.components(separatedBy: filePattern).dropFirst()
        
        return fileBlocks.compactMap { parseFileBlock(String($0)) }
    }
    
    private static func parseFileBlock(_ block: String) -> UnifiedDiffFile? {
        let lines = block.components(separatedBy: "\n")
        guard !lines.isEmpty else { return nil }
        
        // First line: "a/path b/path"
        let pathLine = lines[0]
        let paths = pathLine.split(separator: " ", maxSplits: 1)
        guard paths.count >= 2 else { return nil }
        
        let oldPath = String(paths[0]).replacingOccurrences(of: "a/", with: "", options: .anchored)
        let newPath = String(paths[1]).replacingOccurrences(of: "b/", with: "", options: .anchored)
        
        // Determine status from header lines
        var status: DiffFileStatus = .modified
        var hunkStartIndex = 0
        
        for (index, line) in lines.enumerated() {
            if line.hasPrefix("new file") {
                status = .added
            } else if line.hasPrefix("deleted file") {
                status = .deleted
            } else if line.hasPrefix("rename from") {
                status = .renamed
            } else if line.hasPrefix("@@") {
                hunkStartIndex = index
                break
            }
        }
        
        // Parse hunks
        let hunks = parseHunks(Array(lines[hunkStartIndex...]))
        
        return UnifiedDiffFile(
            oldPath: oldPath,
            newPath: newPath,
            status: status,
            hunks: hunks
        )
    }
    
    private static func parseHunks(_ lines: [String]) -> [DiffHunk] {
        var hunks: [DiffHunk] = []
        var currentHunkLines: [String] = []
        var currentHeader: String?
        var currentOldStart = 0
        var currentOldCount = 0
        var currentNewStart = 0
        var currentNewCount = 0
        var currentContext: String?
        
        for line in lines {
            if line.hasPrefix("@@") {
                // Save previous hunk if exists
                if let header = currentHeader {
                    let parsedLines = parseHunkLines(currentHunkLines, oldStart: currentOldStart, newStart: currentNewStart)
                    let enhancedLines = enhanceWithWordHighlights(parsedLines)
                    hunks.append(DiffHunk(
                        header: header,
                        oldStart: currentOldStart,
                        oldCount: currentOldCount,
                        newStart: currentNewStart,
                        newCount: currentNewCount,
                        contextLabel: currentContext,
                        lines: enhancedLines
                    ))
                }
                
                // Parse new hunk header
                currentHeader = line
                currentHunkLines = []
                
                if let (oldStart, oldCount, newStart, newCount, context) = parseHunkHeader(line) {
                    currentOldStart = oldStart
                    currentOldCount = oldCount
                    currentNewStart = newStart
                    currentNewCount = newCount
                    currentContext = context
                }
            } else if currentHeader != nil {
                currentHunkLines.append(line)
            }
        }
        
        // Save last hunk
        if let header = currentHeader {
            let parsedLines = parseHunkLines(currentHunkLines, oldStart: currentOldStart, newStart: currentNewStart)
            let enhancedLines = enhanceWithWordHighlights(parsedLines)
            hunks.append(DiffHunk(
                header: header,
                oldStart: currentOldStart,
                oldCount: currentOldCount,
                newStart: currentNewStart,
                newCount: currentNewCount,
                contextLabel: currentContext,
                lines: enhancedLines
            ))
        }
        
        return hunks
    }
    
    private static func parseHunkHeader(_ line: String) -> (Int, Int, Int, Int, String?)? {
        // Parse "@@ -10,7 +10,9 @@ optional context"
        // Or "@@ -10 +10 @@" (count defaults to 1)
        let pattern = #"@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@\s*(.*)?$"#
        
        guard let regex = try? NSRegularExpression(pattern: pattern),
              let match = regex.firstMatch(in: line, range: NSRange(line.startIndex..., in: line)) else {
            return nil
        }
        
        func extractInt(_ index: Int) -> Int? {
            guard let range = Range(match.range(at: index), in: line) else { return nil }
            return Int(line[range])
        }
        
        func extractString(_ index: Int) -> String? {
            guard let range = Range(match.range(at: index), in: line) else { return nil }
            let str = String(line[range])
            return str.isEmpty ? nil : str
        }
        
        let oldStart = extractInt(1) ?? 1
        let oldCount = extractInt(2) ?? 1
        let newStart = extractInt(3) ?? 1
        let newCount = extractInt(4) ?? 1
        let context = extractString(5)
        
        return (oldStart, oldCount, newStart, newCount, context)
    }
    
    private static func parseHunkLines(_ lines: [String], oldStart: Int, newStart: Int) -> [DiffLine] {
        var result: [DiffLine] = []
        var oldLine = oldStart
        var newLine = newStart
        
        for line in lines {
            guard !line.isEmpty else { continue }
            
            let prefix = line.first!
            let content = String(line.dropFirst())
            
            switch prefix {
            case " ":
                result.append(DiffLine(
                    type: .context,
                    content: content,
                    oldLineNumber: oldLine,
                    newLineNumber: newLine
                ))
                oldLine += 1
                newLine += 1
            case "-":
                result.append(DiffLine(
                    type: .deletion,
                    content: content,
                    oldLineNumber: oldLine,
                    newLineNumber: nil
                ))
                oldLine += 1
            case "+":
                result.append(DiffLine(
                    type: .addition,
                    content: content,
                    oldLineNumber: nil,
                    newLineNumber: newLine
                ))
                newLine += 1
            case "\\":
                // "\ No newline at end of file" - skip
                continue
            default:
                // Unknown prefix, treat as context
                result.append(DiffLine(
                    type: .context,
                    content: line,
                    oldLineNumber: oldLine,
                    newLineNumber: newLine
                ))
                oldLine += 1
                newLine += 1
            }
        }
        
        return result
    }
    
    /// Enhance diff lines with word-level highlights for adjacent deletion/addition pairs
    private static func enhanceWithWordHighlights(_ lines: [DiffLine]) -> [DiffLine] {
        var result: [DiffLine] = []
        var i = 0
        
        while i < lines.count {
            // Look for deletion followed by addition (modification)
            if i + 1 < lines.count,
               lines[i].type == .deletion,
               lines[i + 1].type == .addition {
                
                let (oldHighlights, newHighlights) = DiffEngine.computeWordDiff(
                    old: lines[i].content,
                    new: lines[i + 1].content
                )
                
                var deletionLine = lines[i]
                deletionLine.wordHighlights = oldHighlights
                
                var additionLine = lines[i + 1]
                additionLine.wordHighlights = newHighlights
                
                result.append(deletionLine)
                result.append(additionLine)
                i += 2
            } else {
                result.append(lines[i])
                i += 1
            }
        }
        
        return result
    }
}
