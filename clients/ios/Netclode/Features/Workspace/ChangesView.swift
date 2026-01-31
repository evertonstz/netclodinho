import SwiftUI

/// View for displaying git working directory changes with inline diffs
struct ChangesView: View {
    let sessionId: String
    
    @Environment(GitStore.self) private var gitStore
    @Environment(ConnectService.self) private var connectService
    
    @State private var isRefreshing = false
    
    private var files: [GitFileChange] {
        gitStore.files(for: sessionId)
    }
    
    private var stagedFiles: [GitFileChange] {
        files.filter { $0.staged }
    }
    
    private var unstagedFiles: [GitFileChange] {
        files.filter { !$0.staged }
    }
    
    private var expandedFile: String? {
        gitStore.selectedFile(for: sessionId)
    }
    
    private var diffContent: String? {
        gitStore.diff(for: sessionId)
    }
    
    private var isLoadingStatus: Bool {
        gitStore.isLoadingStatus(for: sessionId)
    }
    
    private var isLoadingDiff: Bool {
        gitStore.isLoadingDiff(for: sessionId)
    }
    
    private var error: String? {
        gitStore.error(for: sessionId)
    }
    
    var body: some View {
        VStack(spacing: 0) {
            if let error = error {
                // Error state
                VStack(spacing: Theme.Spacing.sm) {
                    Image(systemName: "exclamationmark.triangle")
                        .font(.system(size: 32))
                        .foregroundStyle(Theme.Colors.warning)
                    Text(error)
                        .font(.netclodeCaption)
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                    
                    Button("Retry") {
                        requestGitStatus()
                    }
                    .buttonStyle(.bordered)
                }
                .padding(Theme.Spacing.lg)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if files.isEmpty && !isLoadingStatus {
                // Empty state
                VStack(spacing: Theme.Spacing.sm) {
                    Image(systemName: "checkmark.circle")
                        .font(.system(size: 32))
                        .foregroundStyle(Theme.Colors.success)
                    Text("No changes")
                        .font(.netclodeBody)
                        .foregroundStyle(.secondary)
                    Text("Working directory is clean")
                        .font(.netclodeCaption)
                        .foregroundStyle(.tertiary)
                }
                .padding(Theme.Spacing.lg)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                // File list with sections
                ScrollView {
                    LazyVStack(spacing: 0) {
                        // Staged section
                        if !stagedFiles.isEmpty {
                            SectionHeader(
                                title: "STAGED",
                                count: stagedFiles.count,
                                isLoading: isLoadingStatus
                            ) {
                                requestGitStatus()
                            }
                            
                            ForEach(stagedFiles) { file in
                                FileChangeRow(
                                    file: file,
                                    isExpanded: expandedFile == file.path,
                                    isLoadingDiff: isLoadingDiff && expandedFile == file.path,
                                    diffContent: expandedFile == file.path ? diffContent : nil
                                ) {
                                    toggleFile(file.path)
                                }
                            }
                        }
                        
                        // Unstaged section
                        if !unstagedFiles.isEmpty {
                            SectionHeader(
                                title: "UNSTAGED",
                                count: unstagedFiles.count,
                                isLoading: isLoadingStatus && stagedFiles.isEmpty
                            ) {
                                requestGitStatus()
                            }
                            
                            ForEach(unstagedFiles) { file in
                                FileChangeRow(
                                    file: file,
                                    isExpanded: expandedFile == file.path,
                                    isLoadingDiff: isLoadingDiff && expandedFile == file.path,
                                    diffContent: expandedFile == file.path ? diffContent : nil
                                ) {
                                    toggleFile(file.path)
                                }
                            }
                        }
                    }
                    .padding(.bottom, Theme.Spacing.lg)
                }
            }
        }
        .background(Theme.Colors.background)
        .onAppear {
            requestGitStatus()
        }
        .onChange(of: isLoadingDiff) { _, isLoading in
            // When isLoadingDiff becomes true and we have a selected file but no diff,
            // this means the files were refreshed and we need to re-fetch the diff
            if isLoading, let selectedPath = expandedFile, diffContent == nil {
                connectService.send(.gitDiff(sessionId: sessionId, file: selectedPath))
            }
        }
    }
    
    // MARK: - Actions
    
    private func requestGitStatus() {
        print("[ChangesView] requestGitStatus for session: \(sessionId)")
        gitStore.setLoadingStatus(true, for: sessionId)
        connectService.send(.gitStatus(sessionId: sessionId))
    }
    
    private func toggleFile(_ path: String) {
        if expandedFile == path {
            // Collapse
            gitStore.selectFile(nil, for: sessionId)
        } else {
            // Expand and fetch diff
            gitStore.selectFile(path, for: sessionId)
            gitStore.setLoadingDiff(true, for: sessionId)
            connectService.send(.gitDiff(sessionId: sessionId, file: path))
        }
    }
}

// MARK: - Section Header

private struct SectionHeader: View {
    let title: String
    let count: Int
    let isLoading: Bool
    let onRefresh: () -> Void
    
    var body: some View {
        HStack {
            Text("\(title) (\(count))")
                .font(.system(size: TypeScale.small, weight: .semibold, design: .rounded))
                .foregroundStyle(.secondary)
            
            Spacer()
            
            if isLoading {
                ProgressView()
                    .scaleEffect(0.6)
            } else {
                Button {
                    onRefresh()
                } label: {
                    Image(systemName: "arrow.clockwise")
                        .font(.system(size: TypeScale.small))
                }
                .buttonStyle(.plain)
                .foregroundStyle(.tertiary)
            }
        }
        .padding(.horizontal, Theme.Spacing.md)
        .padding(.vertical, Theme.Spacing.sm)
        .background(Theme.Colors.secondaryBackground.opacity(0.5))
    }
}

// MARK: - File Change Row

private struct FileChangeRow: View {
    let file: GitFileChange
    let isExpanded: Bool
    let isLoadingDiff: Bool
    let diffContent: String?
    let onTap: () -> Void
    
    var body: some View {
        VStack(spacing: 0) {
            // File row header
            Button(action: onTap) {
                HStack(spacing: Theme.Spacing.sm) {
                    // Status badge with icon
                    StatusBadge(status: file.status)
                    
                    // File info
                    VStack(alignment: .leading, spacing: 1) {
                        Text(file.fileName)
                            .font(.netclodeMonospacedSmall)
                            .foregroundStyle(.primary)
                            .lineLimit(1)
                        
                        if !file.directory.isEmpty {
                            Text(file.displayDirectory)
                                .font(.system(size: TypeScale.tiny, design: .monospaced))
                                .foregroundStyle(.tertiary)
                                .lineLimit(1)
                        }
                    }
                    
                    Spacer()
                    
                    // Diff stats
                    if file.hasDiffStats {
                        DiffStatsView(
                            linesAdded: file.linesAdded ?? 0,
                            linesRemoved: file.linesRemoved ?? 0
                        )
                    }
                    
                    // Expand/collapse chevron
                    Image(systemName: "chevron.right")
                        .font(.system(size: TypeScale.micro, weight: .semibold))
                        .foregroundStyle(.tertiary)
                        .rotationEffect(.degrees(isExpanded ? 90 : 0))
                        .animation(.snappy(duration: 0.2), value: isExpanded)
                }
                .padding(.horizontal, Theme.Spacing.md)
                .padding(.vertical, Theme.Spacing.sm)
                .contentShape(Rectangle())
                .background(isExpanded ? Theme.Colors.brand.opacity(0.05) : Color.clear)
            }
            .buttonStyle(.plain)
            
            // Inline diff content (when expanded)
            if isExpanded {
                VStack(spacing: 0) {
                    if isLoadingDiff {
                        HStack(spacing: Theme.Spacing.sm) {
                            ProgressView()
                                .scaleEffect(0.7)
                            Text("Loading diff...")
                                .font(.netclodeCaption)
                                .foregroundStyle(.secondary)
                        }
                        .frame(maxWidth: .infinity)
                        .padding(Theme.Spacing.md)
                        .transition(.opacity)
                    } else if let diff = diffContent, !diff.isEmpty {
                        UnifiedDiffView(diffContent: diff, showFileHeaders: false)
                            .padding(.bottom, Theme.Spacing.sm)
                            .transition(.opacity.animation(.easeOut(duration: 0.2)))
                    } else {
                        Text("No diff available")
                            .font(.netclodeCaption)
                            .foregroundStyle(.secondary)
                            .frame(maxWidth: .infinity)
                            .padding(Theme.Spacing.md)
                            .transition(.opacity)
                    }
                }
                .background(Theme.Colors.secondaryBackground)
                .animation(.easeOut(duration: 0.2), value: isLoadingDiff)
            }
            
            Divider()
                .padding(.leading, Theme.Spacing.md)
        }
    }
}

// MARK: - Status Badge

private struct StatusBadge: View {
    let status: GitFileStatus
    
    var body: some View {
        Text(status.shortLabel)
            .font(.system(size: TypeScale.micro, weight: .bold, design: .monospaced))
            .foregroundStyle(statusColor)
            .frame(width: 22, height: 22)
            .background(statusColor.opacity(0.2))
            .clipShape(RoundedRectangle(cornerRadius: 5))
    }
    
    private var statusColor: Color {
        switch status {
        case .modified: return .orange
        case .added: return Theme.Colors.success
        case .deleted: return Theme.Colors.error
        case .renamed: return .purple
        case .copied: return .blue
        case .untracked: return .gray
        case .ignored: return .gray.opacity(0.5)
        case .unmerged: return Theme.Colors.warning
        }
    }
}

// MARK: - Diff Stats View

private struct DiffStatsView: View {
    let linesAdded: Int
    let linesRemoved: Int
    
    var body: some View {
        HStack(spacing: 4) {
            Text("+\(linesAdded)")
                .foregroundStyle(Theme.Colors.success)
            Text("/")
                .foregroundStyle(.tertiary)
            Text("-\(linesRemoved)")
                .foregroundStyle(Theme.Colors.error)
        }
        .font(.system(size: TypeScale.tiny, weight: .medium, design: .monospaced))
        .padding(.horizontal, 8)
        .padding(.vertical, 4)
        .background(Theme.Colors.secondaryBackground.opacity(0.8))
        .clipShape(RoundedRectangle(cornerRadius: 6))
    }
}

// MARK: - Preview

#Preview("Changes View") {
    let gitStore = GitStore()
    
    // Simulate some files with diff stats
    Task { @MainActor in
        gitStore.setFiles([
            GitFileChange(path: "repo-one/src/App.swift", status: .modified, staged: false, linesAdded: 15, linesRemoved: 8, repo: "repo-one"),
            GitFileChange(path: "repo-one/src/Models/User.swift", status: .added, staged: false, linesAdded: 104, linesRemoved: 0, repo: "repo-one"),
            GitFileChange(path: "repo-two/README.md", status: .modified, staged: true, linesAdded: 28, linesRemoved: 5, repo: "repo-two"),
            GitFileChange(path: "repo-two/old-file.txt", status: .deleted, staged: false, linesAdded: 0, linesRemoved: 2131, repo: "repo-two"),
            GitFileChange(path: "repo-two/new-feature.swift", status: .untracked, staged: false, linesAdded: 208, linesRemoved: 0, repo: "repo-two"),
        ], for: "preview-session")
    }
    
    return ChangesView(sessionId: "preview-session")
        .environment(gitStore)
        .environment(ConnectService())
}

#Preview("Changes View - Empty") {
    ChangesView(sessionId: "empty-session")
        .environment(GitStore())
        .environment(ConnectService())
}
