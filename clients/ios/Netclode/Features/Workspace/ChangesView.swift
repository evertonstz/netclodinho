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
            // Header
            HStack {
                Text("Changes")
                    .font(.netclodeHeadline)
                
                Spacer()
                
                if isLoadingStatus {
                    ProgressView()
                        .scaleEffect(0.7)
                } else {
                    Button {
                        requestGitStatus()
                    } label: {
                        Image(systemName: "arrow.clockwise")
                            .font(.system(size: TypeScale.body))
                    }
                    .buttonStyle(.plain)
                    .foregroundStyle(.secondary)
                }
            }
            .padding(.horizontal, Theme.Spacing.md)
            .padding(.vertical, Theme.Spacing.sm)
            
            Divider()
            
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
                // File list with inline diffs
                ScrollView {
                    LazyVStack(spacing: 0) {
                        ForEach(files) { file in
                            FileChangeDisclosure(
                                file: file,
                                isExpanded: expandedFile == file.path,
                                isLoadingDiff: isLoadingDiff && expandedFile == file.path,
                                diffContent: expandedFile == file.path ? diffContent : nil
                            ) {
                                toggleFile(file.path)
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

// MARK: - File Change Disclosure

private struct FileChangeDisclosure: View {
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
                    // Expand/collapse chevron
                    Image(systemName: "chevron.right")
                        .font(.system(size: TypeScale.micro, weight: .semibold))
                        .foregroundStyle(.tertiary)
                        .rotationEffect(.degrees(isExpanded ? 90 : 0))
                        .animation(.snappy(duration: 0.2), value: isExpanded)
                    
                    // Status badge
                    Text(file.status.shortLabel)
                        .font(.system(size: TypeScale.micro, weight: .semibold, design: .monospaced))
                        .foregroundStyle(statusColor)
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(statusColor.opacity(0.15))
                        .clipShape(RoundedRectangle(cornerRadius: 4))
                    
                    // File info
                    VStack(alignment: .leading, spacing: 1) {
                        Text(file.fileName)
                            .font(.netclodeMonospacedSmall)
                            .foregroundStyle(.primary)
                            .lineLimit(1)
                        
                        if !file.directory.isEmpty {
                            Text(file.directory)
                                .font(.system(size: TypeScale.tiny, design: .monospaced))
                                .foregroundStyle(.tertiary)
                                .lineLimit(1)
                        }
                    }
                    
                    Spacer()
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
                    } else if let diff = diffContent, !diff.isEmpty {
                        UnifiedDiffView(diffContent: diff, showFileHeaders: false)
                            .padding(.horizontal, Theme.Spacing.sm)
                            .padding(.bottom, Theme.Spacing.sm)
                    } else {
                        Text("No diff available")
                            .font(.netclodeCaption)
                            .foregroundStyle(.secondary)
                            .frame(maxWidth: .infinity)
                            .padding(Theme.Spacing.md)
                    }
                }
                .background(Theme.Colors.secondaryBackground)
            }
            
            Divider()
                .padding(.leading, Theme.Spacing.md)
        }
    }
    
    private var statusColor: Color {
        switch file.status {
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

// MARK: - Preview

#Preview("Changes View") {
    let gitStore = GitStore()
    
    // Simulate some files
    Task { @MainActor in
        gitStore.setFiles([
            GitFileChange(path: "src/App.swift", status: .modified, staged: false),
            GitFileChange(path: "src/Models/User.swift", status: .added, staged: false),
            GitFileChange(path: "README.md", status: .modified, staged: true),
            GitFileChange(path: "old-file.txt", status: .deleted, staged: false),
            GitFileChange(path: "new-feature.swift", status: .untracked, staged: false),
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
