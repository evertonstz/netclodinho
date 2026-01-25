import SwiftUI

/// A card that displays a tool invocation in Claude Code CLI style
struct ToolEventCard: View {
    let event: AgentEvent
    let endEvent: ToolEndEvent?
    let children: [GroupedEvent]  // Nested tool events for Task/subagent

    @State private var isExpanded = false

    private var isRunning: Bool {
        endEvent == nil
    }

    private var isSuccess: Bool {
        endEvent?.isSuccess ?? true
    }

    private var durationText: String? {
        guard let ms = endEvent?.durationMs, ms > 0 else { return nil }
        if ms < 1000 {
            return "\(ms)ms"
        } else if ms < 60000 {
            return String(format: "%.1fs", Double(ms) / 1000)
        } else {
            return String(format: "%.1fm", Double(ms) / 60000)
        }
    }

    private var toolName: String {
        switch event {
        case .toolStart(let e): e.tool
        case .toolEnd(let e): e.tool
        default: "Tool"
        }
    }

    private var toolInput: [String: AnyCodableValue]? {
        if case .toolStart(let e) = event {
            return e.input
        }
        return nil
    }
    
    private var hasChildren: Bool {
        !children.isEmpty
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            // Header row
            Button {
                withAnimation(.snappy(duration: 0.2)) {
                    isExpanded.toggle()
                }
            } label: {
                HStack(spacing: Theme.Spacing.sm) {
                    // Tool badge
                    toolBadge

                    // Summary
                    Text(summaryText)
                        .font(.netclodeMonospacedSmall)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                        .truncationMode(.middle)

                    Spacer()
                    
                    // Child count badge (for Task tools with nested operations)
                    if hasChildren {
                        Text("\(children.count)")
                            .font(.system(size: TypeScale.tiny, weight: .semibold, design: .monospaced))
                            .foregroundStyle(.secondary)
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(Color.secondary.opacity(0.15))
                            .clipShape(Capsule())
                    }

                    // Duration (if completed)
                    if let duration = durationText {
                        Text(duration)
                            .font(.system(size: TypeScale.micro, weight: .medium, design: .monospaced))
                            .foregroundStyle(.tertiary)
                    }

                    // Status indicator
                    statusIndicator

                    // Expand chevron
                    Image(systemName: "chevron.right")
                        .font(.system(size: TypeScale.micro, weight: .semibold))
                        .foregroundStyle(.tertiary)
                        .rotationEffect(.degrees(isExpanded ? 90 : 0))
                }
                .padding(.horizontal, Theme.Spacing.sm)
                .padding(.vertical, Theme.Spacing.xs)
                .frame(minHeight: 44)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)

            // Expanded content
            if isExpanded {
                expandedContent
                    .padding(.horizontal, Theme.Spacing.sm)
                    .padding(.bottom, Theme.Spacing.sm)
            }
        }
        .background(Theme.Colors.codeBackground.opacity(0.5))
        .clipShape(RoundedRectangle(cornerRadius: Theme.Radius.sm))
    }

    // MARK: - Tool Badge

    @ViewBuilder
    private var toolBadge: some View {
        HStack(spacing: 4) {
            Image(systemName: toolIcon)
                .font(.system(size: TypeScale.tiny, weight: .semibold))
            Text(toolName)
                .font(.system(size: TypeScale.caption, weight: .semibold, design: .monospaced))
        }
        .foregroundStyle(badgeColor)
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
        .background(badgeColor.opacity(0.15))
        .clipShape(Capsule())
    }

    private var toolIcon: String {
        switch toolName.lowercased() {
        case "read": return "doc.text"
        case "write": return "square.and.pencil"
        case "edit": return "pencil"
        case "bash": return "terminal"
        case "glob": return "folder.badge.gearshape"
        case "grep": return "magnifyingglass"
        case "task": return "arrow.triangle.branch"
        case "webfetch": return "globe"
        case "websearch": return "magnifyingglass.circle"
        case "todowrite": return "checklist"
        default: return "wrench"
        }
    }

    private var badgeColor: Color {
        if !isSuccess {
            return Theme.Colors.error
        }
        switch toolName.lowercased() {
        case "read", "glob", "grep": return .blue
        case "write", "edit": return .orange
        case "bash": return .green
        case "task": return .purple
        case "webfetch", "websearch": return .cyan
        default: return Theme.Colors.brand
        }
    }

    // MARK: - Status Indicator

    @ViewBuilder
    private var statusIndicator: some View {
        if isRunning {
            ProgressView()
                .scaleEffect(0.6)
        } else if isSuccess {
            Image(systemName: "checkmark.circle.fill")
                .font(.system(size: TypeScale.body))
                .foregroundStyle(Theme.Colors.success)
        } else {
            Image(systemName: "xmark.circle.fill")
                .font(.system(size: TypeScale.body))
                .foregroundStyle(Theme.Colors.error)
        }
    }

    // MARK: - Summary Text

    private var summaryText: String {
        guard let input = toolInput else {
            return ""
        }

        // Extract the most relevant parameter based on tool type
        switch toolName.lowercased() {
        case "read":
            if let path = input["file_path"]?.description {
                return formatPath(path)
            }
        case "write":
            if let path = input["file_path"]?.description {
                return formatPath(path)
            }
        case "edit":
            if let path = input["file_path"]?.description {
                return formatPath(path)
            }
        case "bash":
            if let cmd = input["command"]?.description {
                return cmd.prefix(50) + (cmd.count > 50 ? "..." : "")
            }
        case "glob":
            if let pattern = input["pattern"]?.description {
                return pattern
            }
        case "grep":
            if let pattern = input["pattern"]?.description {
                return pattern
            }
        case "webfetch":
            if let url = input["url"]?.description {
                return url
            }
        case "websearch":
            if let query = input["query"]?.description {
                return query
            }
        case "task":
            if let desc = input["description"]?.description {
                return desc
            }
        default:
            break
        }

        // Fallback: show first string parameter
        for (_, value) in input {
            if case .string(let s) = value, !s.isEmpty {
                return String(s.prefix(40)) + (s.count > 40 ? "..." : "")
            }
        }

        return ""
    }

    private func formatPath(_ path: String) -> String {
        // Show just the filename or last 2 components
        let components = path.split(separator: "/")
        if components.count <= 2 {
            return path
        }
        return ".../" + components.suffix(2).joined(separator: "/")
    }

    // MARK: - Expanded Content

    @ViewBuilder
    private var expandedContent: some View {
        VStack(alignment: .leading, spacing: Theme.Spacing.xs) {
            Divider()
                .padding(.bottom, Theme.Spacing.xs)

            // Special handling for Edit tool - show diff view
            if toolName.lowercased() == "edit", let input = toolInput {
                EditToolDiffSection(input: input)
            } else if let input = toolInput, !input.isEmpty {
                // Generic input section for other tools
                ExpandableSection(title: "INPUT") {
                    ForEach(Array(input.keys.sorted()), id: \.self) { key in
                        if let value = input[key] {
                            InputRow(key: key, value: formatValue(value))
                        }
                    }
                }
            }
            
            // Nested children section (for Task/subagent tools)
            if hasChildren {
                ExpandableSection(title: "OPERATIONS", defaultExpanded: true) {
                    VStack(alignment: .leading, spacing: Theme.Spacing.xxs) {
                        ForEach(children) { child in
                            ChildToolEventRow(grouped: child)
                        }
                    }
                }
            }

            // Output/Result section
            if let end = endEvent {
                if let result = end.result, !result.isEmpty {
                    ExpandableSection(title: "OUTPUT", defaultExpanded: false) {
                        ScrollView(.horizontal, showsIndicators: false) {
                            Text(result)
                                .font(.netclodeMonospacedSmall)
                                .foregroundStyle(.secondary)
                        }
                    }
                }

                if let error = end.error, !error.isEmpty {
                    ExpandableSection(title: "ERROR", defaultExpanded: true) {
                        Text(error)
                            .font(.netclodeMonospacedSmall)
                            .foregroundStyle(Theme.Colors.error)
                    }
                }
            }
        }
    }

    private func formatValue(_ value: AnyCodableValue) -> String {
        switch value {
        case .string(let s):
            return s
        case .bool(let b):
            return b ? "true" : "false"
        case .int(let i):
            return String(i)
        case .double(let d):
            return String(d)
        case .array(let arr):
            return "[\(arr.map { formatValue($0) }.joined(separator: ", "))]"
        case .dictionary(let dict):
            return "{\(dict.map { "\($0): \(formatValue($1))" }.joined(separator: ", "))}"
        case .null:
            return "null"
        }
    }
}

// MARK: - Supporting Views

private struct InputRow: View {
    let key: String
    let value: String

    var body: some View {
        HStack(alignment: .top, spacing: Theme.Spacing.xs) {
            Text(key)
                .font(.system(size: TypeScale.tiny, weight: .medium, design: .monospaced))
                .foregroundStyle(.tertiary)
                .frame(minWidth: 60, alignment: .trailing)

            Text(value)
                .font(.netclodeMonospacedSmall)
                .foregroundStyle(.secondary)
                .lineLimit(3)
                .truncationMode(.tail)
        }
    }
}

/// Specialized view for Edit tool that shows a proper diff
private struct EditToolDiffSection: View {
    let input: [String: AnyCodableValue]
    
    private var filePath: String? {
        input["file_path"]?.stringValue
    }
    
    private var oldString: String? {
        input["old_string"]?.stringValue
    }
    
    private var newString: String? {
        input["new_string"]?.stringValue
    }
    
    private var replaceAll: Bool {
        input["replace_all"]?.boolValue ?? false
    }
    
    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Spacing.xs) {
            // File path
            if let path = filePath {
                HStack(spacing: Theme.Spacing.xxs) {
                    Image(systemName: "doc.text")
                        .font(.system(size: TypeScale.tiny))
                        .foregroundStyle(.secondary)
                    Text(path)
                        .font(.netclodeMonospacedSmall)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
            }
            
            // Diff view
            if let oldString = oldString, let newString = newString {
                DiffView(oldString: oldString, newString: newString)
            } else if let oldString = oldString {
                // Only old string (deletion)
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(oldString.components(separatedBy: "\n"), id: \.self) { line in
                        HStack(spacing: 0) {
                            Text("-")
                                .foregroundStyle(DiffColors.deletionText)
                                .frame(width: 16)
                            Text(line)
                                .foregroundStyle(DiffColors.deletionText)
                            Spacer(minLength: 0)
                        }
                        .padding(.horizontal, 8)
                        .padding(.vertical, 2)
                        .background(DiffColors.deletionBackground)
                    }
                }
                .font(.system(size: 12, design: .monospaced))
                .clipShape(RoundedRectangle(cornerRadius: 6))
            } else if let newString = newString {
                // Only new string (addition)
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(newString.components(separatedBy: "\n"), id: \.self) { line in
                        HStack(spacing: 0) {
                            Text("+")
                                .foregroundStyle(DiffColors.additionText)
                                .frame(width: 16)
                            Text(line)
                                .foregroundStyle(DiffColors.additionText)
                            Spacer(minLength: 0)
                        }
                        .padding(.horizontal, 8)
                        .padding(.vertical, 2)
                        .background(DiffColors.additionBackground)
                    }
                }
                .font(.system(size: 12, design: .monospaced))
                .clipShape(RoundedRectangle(cornerRadius: 6))
            }
            
            // Replace all indicator
            if replaceAll {
                HStack(spacing: Theme.Spacing.xxs) {
                    Image(systemName: "arrow.2.squarepath")
                        .font(.system(size: TypeScale.tiny))
                    Text("Replace all occurrences")
                        .font(.netclodeCaption)
                }
                .foregroundStyle(.secondary)
            }
        }
    }
}

// MARK: - AnyCodableValue Extension

private extension AnyCodableValue {
    var stringValue: String? {
        if case .string(let s) = self { return s }
        return nil
    }
    
    var boolValue: Bool? {
        if case .bool(let b) = self { return b }
        return nil
    }
}

/// Compact row for nested tool events within a parent Task
private struct ChildToolEventRow: View {
    let grouped: GroupedEvent
    
    private var toolName: String {
        switch grouped.event {
        case .toolStart(let e): return e.tool
        case .toolEnd(let e): return e.tool
        default: return "Tool"
        }
    }
    
    private var summaryText: String {
        guard case .toolStart(let e) = grouped.event else { return "" }
        let input = e.input
        
        switch toolName.lowercased() {
        case "read":
            if let path = input["file_path"]?.description {
                return formatPath(path)
            }
        case "write", "edit":
            if let path = input["file_path"]?.description {
                return formatPath(path)
            }
        case "bash":
            if let cmd = input["command"]?.description {
                return String(cmd.prefix(40)) + (cmd.count > 40 ? "..." : "")
            }
        case "glob", "grep":
            if let pattern = input["pattern"]?.description {
                return pattern
            }
        default:
            break
        }
        
        // Fallback: first string value
        for (_, value) in input {
            if case .string(let s) = value, !s.isEmpty {
                return String(s.prefix(30)) + (s.count > 30 ? "..." : "")
            }
        }
        return ""
    }
    
    private var isSuccess: Bool {
        if case .toolEnd(let e) = grouped.endEvent {
            return e.isSuccess
        }
        return true
    }
    
    private var isRunning: Bool {
        grouped.endEvent == nil
    }

    private var durationText: String? {
        guard case .toolEnd(let e) = grouped.endEvent,
              let ms = e.durationMs, ms > 0 else { return nil }
        if ms < 1000 {
            return "\(ms)ms"
        } else if ms < 60000 {
            return String(format: "%.1fs", Double(ms) / 1000)
        } else {
            return String(format: "%.1fm", Double(ms) / 60000)
        }
    }
    
    private var badgeColor: Color {
        if !isSuccess { return Theme.Colors.error }
        switch toolName.lowercased() {
        case "read", "glob", "grep": return .blue
        case "write", "edit": return .orange
        case "bash": return .green
        case "task": return .purple
        case "webfetch", "websearch": return .cyan
        default: return Theme.Colors.brand
        }
    }
    
    private var toolIcon: String {
        switch toolName.lowercased() {
        case "read": return "doc.text"
        case "write": return "square.and.pencil"
        case "edit": return "pencil"
        case "bash": return "terminal"
        case "glob": return "folder.badge.gearshape"
        case "grep": return "magnifyingglass"
        case "task": return "arrow.triangle.branch"
        case "webfetch": return "globe"
        case "websearch": return "magnifyingglass.circle"
        default: return "wrench"
        }
    }
    
    private func formatPath(_ path: String) -> String {
        let components = path.split(separator: "/")
        if components.count <= 2 { return path }
        return ".../" + components.suffix(2).joined(separator: "/")
    }
    
    var body: some View {
        HStack(spacing: Theme.Spacing.xs) {
            // Compact tool badge
            HStack(spacing: 2) {
                Image(systemName: toolIcon)
                    .font(.system(size: 9, weight: .semibold))
                Text(toolName)
                    .font(.system(size: TypeScale.tiny, weight: .semibold, design: .monospaced))
            }
            .foregroundStyle(badgeColor)
            .padding(.horizontal, 6)
            .padding(.vertical, 2)
            .background(badgeColor.opacity(0.15))
            .clipShape(Capsule())
            
            // Summary
            Text(summaryText)
                .font(.system(size: TypeScale.tiny, design: .monospaced))
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .truncationMode(.middle)
            
            Spacer()

            // Duration (if completed)
            if let duration = durationText {
                Text(duration)
                    .font(.system(size: TypeScale.micro, weight: .medium, design: .monospaced))
                    .foregroundStyle(.tertiary)
            }
            
            // Status
            if isRunning {
                ProgressView()
                    .scaleEffect(0.5)
            } else if isSuccess {
                Image(systemName: "checkmark.circle.fill")
                    .font(.system(size: TypeScale.tiny))
                    .foregroundStyle(Theme.Colors.success)
            } else {
                Image(systemName: "xmark.circle.fill")
                    .font(.system(size: TypeScale.tiny))
                    .foregroundStyle(Theme.Colors.error)
            }
        }
        .frame(minHeight: 36)
        .contentShape(Rectangle())
    }
}

private struct ExpandableSection<Content: View>: View {
    let title: String
    var defaultExpanded: Bool = true
    @ViewBuilder let content: () -> Content

    @State private var isExpanded: Bool = true

    init(title: String, defaultExpanded: Bool = true, @ViewBuilder content: @escaping () -> Content) {
        self.title = title
        self.defaultExpanded = defaultExpanded
        self.content = content
        self._isExpanded = State(initialValue: defaultExpanded)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Spacing.xxs) {
            Button {
                withAnimation(.snappy(duration: 0.15)) {
                    isExpanded.toggle()
                }
            } label: {
                HStack(spacing: 4) {
                    Image(systemName: "chevron.right")
                        .font(.system(size: 8, weight: .bold))
                        .rotationEffect(.degrees(isExpanded ? 90 : 0))

                    Text(title)
                        .font(.system(size: TypeScale.micro, weight: .semibold))
                        .tracking(0.5)
                    
                    Spacer()
                }
                .foregroundStyle(.tertiary)
                .frame(minHeight: 32)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)

            if isExpanded {
                content()
                    .padding(.leading, Theme.Spacing.sm)
            }
        }
    }
}

// MARK: - Command Event Card

struct CommandEventCard: View {
    let startEvent: CommandStartEvent?
    let endEvent: CommandEndEvent?

    @State private var isExpanded = false

    private var isRunning: Bool {
        endEvent == nil
    }

    private var isSuccess: Bool {
        endEvent?.isSuccess ?? true
    }

    private var command: String {
        startEvent?.command ?? endEvent?.command ?? ""
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            // Header row
            Button {
                withAnimation(.snappy(duration: 0.2)) {
                    isExpanded.toggle()
                }
            } label: {
                HStack(spacing: Theme.Spacing.sm) {
                    // Bash badge
                    HStack(spacing: 4) {
                        Image(systemName: "terminal")
                            .font(.system(size: TypeScale.tiny, weight: .semibold))
                        Text("Bash")
                            .font(.system(size: TypeScale.caption, weight: .semibold, design: .monospaced))
                    }
                    .foregroundStyle(.green)
                    .padding(.horizontal, 8)
                    .padding(.vertical, 4)
                    .background(Color.green.opacity(0.15))
                    .clipShape(Capsule())

                    // Command preview
                    Text("$ " + command.prefix(40))
                        .font(.netclodeMonospacedSmall)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)

                    Spacer()

                    // Exit code if finished
                    if let end = endEvent {
                        Text("exit \(end.exitCode)")
                            .font(.system(size: TypeScale.tiny, weight: .medium, design: .monospaced))
                            .foregroundStyle(isSuccess ? Theme.Colors.success : Theme.Colors.error)
                    }

                    // Status indicator
                    if isRunning {
                        ProgressView()
                            .scaleEffect(0.6)
                    } else if isSuccess {
                        Image(systemName: "checkmark.circle.fill")
                            .font(.system(size: TypeScale.body))
                            .foregroundStyle(Theme.Colors.success)
                    } else {
                        Image(systemName: "xmark.circle.fill")
                            .font(.system(size: TypeScale.body))
                            .foregroundStyle(Theme.Colors.error)
                    }

                    // Expand chevron
                    Image(systemName: "chevron.right")
                        .font(.system(size: TypeScale.micro, weight: .semibold))
                        .foregroundStyle(.tertiary)
                        .rotationEffect(.degrees(isExpanded ? 90 : 0))
                }
                .padding(.horizontal, Theme.Spacing.sm)
                .padding(.vertical, Theme.Spacing.xs)
                .frame(minHeight: 44)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)

            // Expanded content
            if isExpanded {
                VStack(alignment: .leading, spacing: Theme.Spacing.xs) {
                    Divider()

                    // Full command
                    Text("$ " + command)
                        .font(.netclodeMonospacedSmall)
                        .foregroundStyle(.primary)
                        .textSelection(.enabled)

                    if let cwd = startEvent?.cwd {
                        Text("in \(cwd)")
                            .font(.system(size: TypeScale.tiny, design: .monospaced))
                            .foregroundStyle(.tertiary)
                    }

                    // Output
                    if let output = endEvent?.output, !output.isEmpty {
                        Divider()
                        ScrollView {
                            Text(output)
                                .font(.netclodeMonospacedSmall)
                                .foregroundStyle(.secondary)
                                .frame(maxWidth: .infinity, alignment: .leading)
                        }
                        .frame(maxHeight: 200)
                    }
                }
                .padding(.horizontal, Theme.Spacing.sm)
                .padding(.bottom, Theme.Spacing.sm)
            }
        }
        .background(Theme.Colors.codeBackground.opacity(0.5))
        .clipShape(RoundedRectangle(cornerRadius: Theme.Radius.sm))
    }
}

// MARK: - File Change Card

struct FileChangeCard: View {
    let event: FileChangeEvent

    var body: some View {
        HStack(spacing: Theme.Spacing.sm) {
            // File action badge
            HStack(spacing: 4) {
                Image(systemName: event.action.systemImage)
                    .font(.system(size: TypeScale.tiny, weight: .semibold))
                Text(event.action.displayName)
                    .font(.system(size: TypeScale.caption, weight: .semibold, design: .monospaced))
            }
            .foregroundStyle(actionColor)
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(actionColor.opacity(0.15))
            .clipShape(Capsule())

            // File path
            Text(event.fileName)
                .font(.netclodeMonospacedSmall)
                .foregroundStyle(.secondary)
                .lineLimit(1)

            Spacer()

            // Line changes
            if let added = event.linesAdded, added > 0 {
                Text("+\(added)")
                    .font(.system(size: TypeScale.caption, weight: .medium, design: .monospaced))
                    .foregroundStyle(Theme.Colors.success)
            }
            if let removed = event.linesRemoved, removed > 0 {
                Text("-\(removed)")
                    .font(.system(size: TypeScale.caption, weight: .medium, design: .monospaced))
                    .foregroundStyle(Theme.Colors.error)
            }
        }
        .padding(.horizontal, Theme.Spacing.sm)
        .padding(.vertical, Theme.Spacing.xs)
        .background(Theme.Colors.codeBackground.opacity(0.5))
        .clipShape(RoundedRectangle(cornerRadius: Theme.Radius.sm))
    }

    private var actionColor: Color {
        switch event.action {
        case .create: return Theme.Colors.success
        case .edit: return .orange
        case .delete: return Theme.Colors.error
        }
    }
}

// MARK: - Thinking Card

struct ThinkingCard: View {
    let event: ThinkingEvent

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Spacing.xs) {
            // Header row with icon and status
            HStack(spacing: Theme.Spacing.sm) {
                Image(systemName: "brain.head.profile")
                    .font(.system(size: TypeScale.caption))
                    .foregroundStyle(Theme.Colors.brandLight)
                    .opacity(event.partial ? 1.0 : 0.7)

                Text("Thinking")
                    .font(.netclodeCaption)
                    .fontWeight(.medium)
                    .foregroundStyle(Theme.Colors.brandLight)

                if event.partial {
                    // Pulsing indicator for streaming
                    Circle()
                        .fill(Theme.Colors.brandLight)
                        .frame(width: 6, height: 6)
                        .opacity(0.8)
                }

                Spacer()
            }

            // Content
            Text(event.content)
                .font(.netclodeSmall)
                .foregroundStyle(.secondary)
                .italic()
                .animation(.easeInOut(duration: 0.2), value: event.content)
        }
        .padding(.horizontal, Theme.Spacing.sm)
        .padding(.vertical, Theme.Spacing.xs)
        .background(Theme.Colors.brandLight.opacity(event.partial ? 0.15 : 0.1))
        .clipShape(RoundedRectangle(cornerRadius: Theme.Radius.sm))
        .animation(.easeInOut(duration: 0.2), value: event.partial)
    }
}

// MARK: - Port Exposed Card

struct PortExposedCard: View {
    let event: PortExposedEvent

    var body: some View {
        HStack(spacing: Theme.Spacing.sm) {
            HStack(spacing: 4) {
                Image(systemName: "network")
                    .font(.system(size: TypeScale.tiny, weight: .semibold))
                Text(verbatim: "Port \(event.port)")
                    .font(.system(size: TypeScale.caption, weight: .semibold, design: .monospaced))
            }
            .foregroundStyle(.cyan)
            .padding(.horizontal, 8)
            .padding(.vertical, 4)
            .background(Color.cyan.opacity(0.15))
            .clipShape(Capsule())

            if let process = event.process {
                Text(process)
                    .font(.netclodeCaption)
                    .foregroundStyle(.secondary)
            }

            Spacer()

            if let url = event.previewUrl {
                Menu {
                    if let link = URL(string: url) {
                        Link(destination: link) {
                            Label("Open in Safari", systemImage: "safari")
                        }
                    }
                    Button {
                        UIPasteboard.general.string = url
                    } label: {
                        Label("Copy URL", systemImage: "doc.on.doc")
                    }
                } label: {
                    HStack(spacing: 4) {
                        Text("Open")
                            .font(.system(size: TypeScale.caption, weight: .medium))
                        Image(systemName: "chevron.down")
                            .font(.system(size: 8))
                    }
                    .foregroundStyle(.cyan)
                }
            }
        }
        .padding(.horizontal, Theme.Spacing.sm)
        .padding(.vertical, Theme.Spacing.xs)
        .background(Theme.Colors.codeBackground.opacity(0.5))
        .clipShape(RoundedRectangle(cornerRadius: Theme.Radius.sm))
    }
}

// MARK: - Repo Clone Card

struct RepoCloneCard: View {
    let event: RepoCloneEvent

    private var statusColor: Color {
        switch event.stage {
        case .starting, .cloning:
            return .cyan
        case .done:
            return Theme.Colors.success
        case .error:
            return Theme.Colors.error
        }
    }

    private var statusIcon: String {
        switch event.stage {
        case .starting:
            return "arrow.down.circle"
        case .cloning:
            return "arrow.down.circle"
        case .done:
            return "checkmark.circle.fill"
        case .error:
            return "xmark.circle.fill"
        }
    }

    private var isInProgress: Bool {
        event.stage == .starting || event.stage == .cloning
    }

    /// Extracts "owner/repo" from various URL formats
    private var repoDisplayName: String {
        let repo = event.repo
        // Handle github.com/owner/repo, https://github.com/owner/repo, etc.
        if let range = repo.range(of: "github.com/") {
            let afterGithub = String(repo[range.upperBound...])
            // Remove .git suffix if present
            let cleaned = afterGithub.replacingOccurrences(of: ".git", with: "")
            return cleaned
        }
        // Fallback: just return the repo as-is
        return repo
    }

    var body: some View {
        HStack(spacing: Theme.Spacing.sm) {
            // GitHub icon
            Image(systemName: "arrow.triangle.branch")
                .font(.system(size: TypeScale.body, weight: .medium))
                .foregroundStyle(statusColor)
                .frame(width: 24, height: 24)
                .background(statusColor.opacity(0.15))
                .clipShape(RoundedRectangle(cornerRadius: 6))

            // Repo info
            VStack(alignment: .leading, spacing: 2) {
                Text(repoDisplayName)
                    .font(.system(size: TypeScale.small, weight: .medium, design: .monospaced))
                    .foregroundStyle(.primary)
                    .lineLimit(1)

                Text(event.message)
                    .font(.system(size: TypeScale.caption))
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }

            Spacer()

            // Status indicator
            if isInProgress {
                ProgressView()
                    .scaleEffect(0.7)
            } else {
                Image(systemName: statusIcon)
                    .font(.system(size: TypeScale.body + 1))
                    .foregroundStyle(statusColor)
            }
        }
        .padding(.horizontal, Theme.Spacing.sm)
        .padding(.vertical, Theme.Spacing.sm)
        .background(Theme.Colors.codeBackground.opacity(0.5))
        .clipShape(RoundedRectangle(cornerRadius: Theme.Radius.sm))
    }
}

// MARK: - System Event Card (Agent Disconnect/Reconnect)

struct SystemEventCard: View {
    let event: AgentEvent
    
    private var isDisconnect: Bool {
        if case .agentDisconnected = event { return true }
        return false
    }
    
    private var message: String {
        switch event {
        case .agentDisconnected(let e): return e.message
        case .agentReconnected(let e): return e.message
        default: return ""
        }
    }
    
    private var icon: String {
        isDisconnect ? "wifi.slash" : "wifi"
    }
    
    private var statusColor: Color {
        isDisconnect ? .orange : Theme.Colors.success
    }
    
    private var title: String {
        isDisconnect ? "Connection Lost" : "Reconnected"
    }
    
    var body: some View {
        HStack(spacing: Theme.Spacing.sm) {
            // Icon
            Image(systemName: icon)
                .font(.system(size: TypeScale.body, weight: .medium))
                .foregroundStyle(statusColor)
                .frame(width: 28, height: 28)
                .background(statusColor.opacity(0.15))
                .clipShape(RoundedRectangle(cornerRadius: 6))
            
            // Message
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(.system(size: TypeScale.small, weight: .semibold))
                    .foregroundStyle(statusColor)
                
                Text(message)
                    .font(.system(size: TypeScale.caption))
                    .foregroundStyle(.secondary)
                    .lineLimit(2)
            }
            
            Spacer()
        }
        .padding(.horizontal, Theme.Spacing.sm)
        .padding(.vertical, Theme.Spacing.sm)
        .background(statusColor.opacity(0.1))
        .clipShape(RoundedRectangle(cornerRadius: Theme.Radius.sm))
        .overlay(
            RoundedRectangle(cornerRadius: Theme.Radius.sm)
                .stroke(statusColor.opacity(0.3), lineWidth: 1)
        )
    }
}

// MARK: - Previews

#Preview("System Event Card") {
    VStack(spacing: Theme.Spacing.md) {
        SystemEventCard(event: .agentDisconnected(AgentDisconnectedEvent(
            id: UUID(),
            timestamp: Date(),
            message: "Agent connection lost. Send a message to continue when reconnected."
        )))
        
        SystemEventCard(event: .agentReconnected(AgentReconnectedEvent(
            id: UUID(),
            timestamp: Date(),
            message: "Agent reconnected. Send a message to continue."
        )))
    }
    .padding()
    .background(Theme.Colors.background)
}

#Preview("Tool Event - Running") {
    VStack(spacing: Theme.Spacing.md) {
        ToolEventCard(
            event: .toolStart(ToolStartEvent(
                id: UUID(),
                timestamp: Date(),
                tool: "Read",
                toolUseId: "123",
                parentToolUseId: nil,
                input: ["file_path": .string("/src/components/Button.swift")]
            )),
            endEvent: nil,
            children: []
        )

        ToolEventCard(
            event: .toolStart(ToolStartEvent(
                id: UUID(),
                timestamp: Date(),
                tool: "Bash",
                toolUseId: "124",
                parentToolUseId: nil,
                input: ["command": .string("npm run build && npm test")]
            )),
            endEvent: nil,
            children: []
        )
    }
    .padding()
    .background(Theme.Colors.background)
}

#Preview("Tool Event - Completed") {
    VStack(spacing: Theme.Spacing.md) {
        ToolEventCard(
            event: .toolStart(ToolStartEvent(
                id: UUID(),
                timestamp: Date(),
                tool: "Read",
                toolUseId: "123",
                parentToolUseId: nil,
                input: ["file_path": .string("/src/components/Button.swift")]
            )),
            endEvent: ToolEndEvent(
                id: UUID(),
                timestamp: Date(),
                tool: "Read",
                toolUseId: "123",
                parentToolUseId: nil,
                result: "import SwiftUI\n\nstruct Button: View {\n    var body: some View {\n        Text(\"Hello\")\n    }\n}",
                error: nil,
                durationMs: 23
            ),
            children: []
        )

        ToolEventCard(
            event: .toolStart(ToolStartEvent(
                id: UUID(),
                timestamp: Date(),
                tool: "Edit",
                toolUseId: "125",
                parentToolUseId: nil,
                input: [
                    "file_path": .string("/src/auth/AuthService.swift"),
                    "old_string": .string("func login()"),
                    "new_string": .string("func login(username: String)")
                ]
            )),
            endEvent: ToolEndEvent(
                id: UUID(),
                timestamp: Date(),
                tool: "Edit",
                toolUseId: "125",
                parentToolUseId: nil,
                result: nil,
                error: "File not found",
                durationMs: 156
            ),
            children: []
        )
    }
    .padding()
    .background(Theme.Colors.background)
}

#Preview("Command Card") {
    VStack(spacing: Theme.Spacing.md) {
        CommandEventCard(
            startEvent: CommandStartEvent(
                id: UUID(),
                timestamp: Date(),
                command: "npm run build",
                cwd: "/workspace/myproject"
            ),
            endEvent: nil
        )

        CommandEventCard(
            startEvent: CommandStartEvent(
                id: UUID(),
                timestamp: Date(),
                command: "npm test",
                cwd: "/workspace"
            ),
            endEvent: CommandEndEvent(
                id: UUID(),
                timestamp: Date(),
                command: "npm test",
                exitCode: 0,
                output: "PASS  src/Button.test.ts\nAll tests passed!"
            )
        )
    }
    .padding()
    .background(Theme.Colors.background)
}

#Preview("File Change Card") {
    VStack(spacing: Theme.Spacing.md) {
        FileChangeCard(event: FileChangeEvent(
            id: UUID(),
            timestamp: Date(),
            path: "/src/auth/AuthService.swift",
            action: .edit,
            linesAdded: 25,
            linesRemoved: 10
        ))

        FileChangeCard(event: FileChangeEvent(
            id: UUID(),
            timestamp: Date(),
            path: "/src/models/User.swift",
            action: .create,
            linesAdded: 45,
            linesRemoved: nil
        ))

        FileChangeCard(event: FileChangeEvent(
            id: UUID(),
            timestamp: Date(),
            path: "/src/deprecated/OldService.swift",
            action: .delete,
            linesAdded: nil,
            linesRemoved: 120
        ))
    }
    .padding()
    .background(Theme.Colors.background)
}

