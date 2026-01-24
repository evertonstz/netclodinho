import SwiftUI

struct PromptSheet: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(ConnectService.self) private var connectService
    @Environment(SettingsStore.self) private var settingsStore
    @Environment(SessionStore.self) private var sessionStore
    @Environment(GitHubStore.self) private var githubStore
    @Environment(ModelsStore.self) private var modelsStore
    @Environment(CopilotStore.self) private var copilotStore

    @State private var promptText = ""
    @State private var repoURL = ""
    @State private var repoAccess: RepoAccess = .write
    @State private var selectedSdkType: SdkType = .claude
    @State private var selectedModelId: String = ModelsStore.defaultModelId
    @State private var selectedCopilotBackend: CopilotBackend = .anthropic
    @State private var selectedCopilotModelId: String = CopilotStore.defaultAnthropicModelId
    @State private var isSubmitting = false
    @State private var canSubmit = false
    @FocusState private var isFocused: Bool

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                // Text input area
                TextField(
                    "What do you want to build?",
                    text: $promptText,
                    axis: .vertical
                )
                .font(.netclodeBody)
                .tint(Theme.Colors.brand)
                .lineLimit(3...12)
                .padding(Theme.Spacing.md)
                .glassEffect(.regular, in: RoundedRectangle(cornerRadius: Theme.Radius.lg))
                .padding(.horizontal, Theme.Spacing.md)
                .padding(.top, Theme.Spacing.md)
                .focused($isFocused)

                // SDK and Model section
                VStack(alignment: .leading, spacing: Theme.Spacing.sm) {
                    HStack(spacing: Theme.Spacing.xs) {
                        Image(systemName: "cpu")
                            .foregroundStyle(.secondary)
                        Text("Agent SDK")
                            .font(.netclodeCaption)
                            .foregroundStyle(.secondary)
                    }

                    Picker("SDK", selection: $selectedSdkType) {
                        ForEach(SdkType.allCases, id: \.self) { sdk in
                            Text(sdk.displayName).tag(sdk)
                        }
                    }
                    .pickerStyle(.segmented)

                    // Model picker (shown for OpenCode and Copilot with model support)
                    if selectedSdkType == .opencode {
                        HStack(spacing: Theme.Spacing.xs) {
                            Image(systemName: "sparkles")
                                .foregroundStyle(.secondary)
                            Text("Model")
                                .font(.netclodeCaption)
                                .foregroundStyle(.secondary)
                        }
                        .padding(.top, Theme.Spacing.xs)

                        Picker("Model", selection: $selectedModelId) {
                            ForEach(modelsStore.anthropicModels) { model in
                                Text(model.name).tag(model.fullModelId)
                            }
                        }
                        .pickerStyle(.menu)
                        .glassEffect(.regular, in: RoundedRectangle(cornerRadius: Theme.Radius.md))
                    }

                    // Backend and model picker (only shown for Copilot)
                    if selectedSdkType == .copilot {
                        HStack(spacing: Theme.Spacing.xs) {
                            Image(systemName: "server.rack")
                                .foregroundStyle(.secondary)
                            Text("Backend")
                                .font(.netclodeCaption)
                                .foregroundStyle(.secondary)
                        }
                        .padding(.top, Theme.Spacing.xs)

                        Picker("Backend", selection: $selectedCopilotBackend) {
                            ForEach(CopilotBackend.allCases, id: \.self) { backend in
                                Text(backend.displayName).tag(backend)
                            }
                        }
                        .pickerStyle(.segmented)
                        .onChange(of: selectedCopilotBackend) { _, newBackend in
                            // Reset model selection when backend changes
                            selectedCopilotModelId = copilotStore.defaultModelId(for: newBackend)
                            // Fetch models for the new backend
                            fetchCopilotModels(for: newBackend)
                        }

                        // Model picker for Copilot
                        HStack(spacing: Theme.Spacing.xs) {
                            Image(systemName: "sparkles")
                                .foregroundStyle(.secondary)
                            Text("Model")
                                .font(.netclodeCaption)
                                .foregroundStyle(.secondary)
                            
                            if copilotStore.isLoadingModels {
                                ProgressView()
                                    .scaleEffect(0.7)
                            }
                        }
                        .padding(.top, Theme.Spacing.xs)

                        let models = copilotStore.models(for: selectedCopilotBackend)
                        if models.isEmpty && !copilotStore.isLoadingModels {
                            // Show default model when models haven't loaded
                            Text(copilotStore.defaultModelId(for: selectedCopilotBackend))
                                .font(.netclodeCaption)
                                .foregroundStyle(.secondary)
                                .padding(.vertical, Theme.Spacing.xs)
                        } else {
                            Picker("Model", selection: $selectedCopilotModelId) {
                                ForEach(models) { model in
                                    HStack {
                                        Text(model.name)
                                        if let multiplier = model.billingMultiplier, multiplier != 1.0 {
                                            Text(multiplier < 1.0 ? "(\(String(format: "%.2fx", multiplier)))" : "(\(String(format: "%.0fx", multiplier)))")
                                                .foregroundStyle(.secondary)
                                        }
                                    }
                                    .tag(model.id)
                                }
                            }
                            .pickerStyle(.menu)
                            .glassEffect(.regular, in: RoundedRectangle(cornerRadius: Theme.Radius.md))
                        }
                    }
                }
                .padding(.horizontal, Theme.Spacing.md)
                .padding(.top, Theme.Spacing.md)

                // Repository section
                VStack(alignment: .leading, spacing: Theme.Spacing.sm) {
                    HStack(spacing: Theme.Spacing.xs) {
                        Image("github-mark")
                            .resizable()
                            .scaledToFit()
                            .frame(width: 14, height: 14)
                            .foregroundStyle(.secondary)
                        Text("Repository (optional)")
                            .font(.netclodeCaption)
                            .foregroundStyle(.secondary)
                    }
                    
                    RepoAutocomplete(text: $repoURL)
                    
                    if !repoURL.isEmpty {
                        Picker("Access", selection: $repoAccess) {
                            Text("Read & Write").tag(RepoAccess.write)
                            Text("Read Only").tag(RepoAccess.read)
                        }
                        .pickerStyle(.segmented)
                    }
                }
                .padding(.horizontal, Theme.Spacing.md)
                .padding(.top, Theme.Spacing.md)

                Spacer()
            }
            .background(Theme.Colors.background)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button {
                        if settingsStore.hapticFeedbackEnabled {
                            HapticFeedback.light()
                        }
                        dismiss()
                    } label: {
                        Image(systemName: "xmark")
                    }
                    .tint(.red)
                    .accessibilityLabel("Cancel")
                }

                ToolbarItem(placement: .principal) {
                    Text("New Session")
                        .font(.netclodeHeadline)
                }

                ToolbarSpacer(placement: .topBarTrailing)

                ToolbarItem(placement: .confirmationAction) {
                    Button {
                        submitPrompt()
                    } label: {
                        if isSubmitting {
                            ProgressView()
                                .tint(.white)
                        } else {
                            Image(systemName: "paperplane")
                                .symbolVariant(canSubmit ? .fill : .none)
                                .bold()
                        }
                    }
                    .buttonStyle(.glassProminent)
                    .buttonBorderShape(.circle)
                    .tint(Theme.Colors.brand)
                    .disabled(!canSubmit)
                    .keyboardShortcut(.return, modifiers: .command)
                    .accessibilityLabel("Send")
                }
            }
            .onAppear {
                isFocused = true
                // Fetch Copilot models if Copilot SDK is selected
                if selectedSdkType == .copilot {
                    fetchCopilotModels(for: selectedCopilotBackend)
                }
            }
            .onChange(of: selectedSdkType) { _, newSdkType in
                // Fetch models when switching to Copilot
                if newSdkType == .copilot {
                    fetchCopilotModels(for: selectedCopilotBackend)
                }
            }
            .onChange(of: promptText) { _, newValue in
                canSubmit = !newValue.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && !isSubmitting
            }
            .onChange(of: isSubmitting) { _, newValue in
                canSubmit = !promptText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && !newValue
            }
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
        .interactiveDismissDisabled(isSubmitting)
    }

    private func submitPrompt() {
        let text = promptText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return }

        isSubmitting = true

        if settingsStore.hapticFeedbackEnabled {
            HapticFeedback.medium()
        }

        // Store prompt text - will be associated with session when sessionCreated arrives
        sessionStore.pendingPromptText = text
        
        // Parse repo URL if provided
        let repo = repoURL.trimmingCharacters(in: .whitespacesAndNewlines)
        let repoParam = repo.isEmpty ? nil : repo
        let accessParam = repoParam != nil ? repoAccess : nil

        // SDK, model, and backend params
        let sdkParam = selectedSdkType
        let modelParam: String?
        let copilotBackendParam: CopilotBackend?
        
        switch selectedSdkType {
        case .opencode:
            modelParam = selectedModelId
            copilotBackendParam = nil
        case .copilot:
            modelParam = selectedCopilotModelId
            copilotBackendParam = selectedCopilotBackend
        case .claude:
            modelParam = nil
            copilotBackendParam = nil
        }
        
        // Create session
        connectService.send(.sessionCreate(
            name: nil,
            repo: repoParam,
            repoAccess: accessParam,
            initialPrompt: text,
            sdkType: sdkParam,
            model: modelParam,
            copilotBackend: copilotBackendParam
        ))

        dismiss()
    }

    private func fetchCopilotModels(for backend: CopilotBackend) {
        copilotStore.setLoadingModels(true)
        connectService.send(.listModels(sdkType: .copilot, copilotBackend: backend))
    }
}

// MARK: - Preview

#Preview {
    Color.clear
        .sheet(isPresented: .constant(true)) {
            PromptSheet()
                .environment(ConnectService())
                .environment(SettingsStore())
                .environment(SessionStore())
                .environment(GitHubStore())
                .environment(ModelsStore())
                .environment(CopilotStore())
        }
}
