import SwiftUI

/// A text field with autocomplete dropdown for GitHub repository selection.
struct RepoAutocomplete: View {
    @Binding var text: String
    @Environment(GitHubStore.self) private var githubStore
    @Environment(WebSocketService.self) private var webSocketService
    @Environment(SettingsStore.self) private var settingsStore
    
    @State private var isDropdownVisible = false
    @FocusState private var isFocused: Bool
    
    private var filteredRepos: [GitHubRepo] {
        githubStore.filteredRepos(query: text)
    }
    
    private var shouldShowDropdown: Bool {
        isFocused && !filteredRepos.isEmpty
    }
    
    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Spacing.xs) {
            // Text field with refresh button
            HStack(spacing: Theme.Spacing.sm) {
                TextField(
                    "owner/repo",
                    text: $text
                )
                .font(.netclodeBody)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .keyboardType(.URL)
                .focused($isFocused)
                .onChange(of: isFocused) { _, focused in
                    if focused {
                        // Fetch repos when focused (if cache is stale)
                        githubStore.fetchIfNeeded(webSocketService: webSocketService)
                    }
                    withAnimation(.easeInOut(duration: 0.15)) {
                        isDropdownVisible = focused
                    }
                }
                
                // Refresh button
                if githubStore.isLoading {
                    ProgressView()
                        .scaleEffect(0.8)
                        .frame(width: 20, height: 20)
                } else {
                    Button {
                        if settingsStore.hapticFeedbackEnabled {
                            HapticFeedback.light()
                        }
                        githubStore.fetchIfNeeded(webSocketService: webSocketService, force: true)
                    } label: {
                        Image(systemName: "arrow.clockwise")
                            .font(.system(size: 14))
                            .foregroundStyle(githubStore.isCacheStale ? Theme.Colors.brand : .secondary)
                    }
                    .buttonStyle(.plain)
                }
            }
            .padding(Theme.Spacing.md)
            .glassEffect(.regular, in: RoundedRectangle(cornerRadius: Theme.Radius.lg))
            
            // Dropdown list (appears below the text field)
            if shouldShowDropdown {
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 0) {
                        ForEach(filteredRepos.prefix(10)) { repo in
                            RepoDropdownRow(repo: repo) {
                                selectRepo(repo)
                            }
                        }
                        
                        if filteredRepos.count > 10 {
                            Text("\(filteredRepos.count - 10) more...")
                                .font(.netclodeCaption)
                                .foregroundStyle(.secondary)
                                .padding(.horizontal, Theme.Spacing.md)
                                .padding(.vertical, Theme.Spacing.sm)
                        }
                    }
                }
                .frame(maxHeight: 200)
                .glassEffect(.regular, in: RoundedRectangle(cornerRadius: Theme.Radius.md))
                .zIndex(100)
            } else if githubStore.isLoading && isFocused {
                // Show loading indicator when fetching repos
                HStack {
                    Spacer()
                    ProgressView()
                        .padding(Theme.Spacing.md)
                    Spacer()
                }
                .glassEffect(.regular, in: RoundedRectangle(cornerRadius: Theme.Radius.md))
            } else if let error = githubStore.errorMessage, isFocused {
                // Show error message
                Text(error)
                    .font(.netclodeCaption)
                    .foregroundStyle(.red)
                    .padding(Theme.Spacing.sm)
            } else if githubStore.repos.isEmpty && isFocused && !githubStore.isLoading {
                // Show hint when no repos available
                Text("No repositories available. Check GitHub App installation.")
                    .font(.netclodeCaption)
                    .foregroundStyle(.secondary)
                    .padding(Theme.Spacing.sm)
            }
        }
    }
    
    private func selectRepo(_ repo: GitHubRepo) {
        if settingsStore.hapticFeedbackEnabled {
            HapticFeedback.light()
        }
        text = repo.fullName
        isFocused = false
    }
}

/// A single row in the repo dropdown
private struct RepoDropdownRow: View {
    let repo: GitHubRepo
    let onSelect: () -> Void
    
    var body: some View {
        Button(action: onSelect) {
            HStack(spacing: Theme.Spacing.sm) {
                // Private/public indicator
                Image(systemName: repo.isPrivate ? "lock.fill" : "globe")
                    .font(.system(size: 12))
                    .foregroundStyle(repo.isPrivate ? Theme.Colors.warning : .secondary)
                    .frame(width: 16)
                
                VStack(alignment: .leading, spacing: 2) {
                    Text(repo.fullName)
                        .font(.netclodeBody)
                        .foregroundStyle(.primary)
                        .lineLimit(1)
                    
                    if let description = repo.description, !description.isEmpty {
                        Text(description)
                            .font(.netclodeCaption)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                    }
                }
                
                Spacer()
            }
            .padding(.horizontal, Theme.Spacing.md)
            .padding(.vertical, Theme.Spacing.sm)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }
}

// MARK: - Preview

#Preview {
    VStack {
        RepoAutocomplete(text: .constant(""))
            .padding()
    }
    .environment(GitHubStore())
    .environment(WebSocketService())
    .environment(SettingsStore())
}
