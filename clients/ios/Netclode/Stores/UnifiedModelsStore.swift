import Foundation

/// State for a single SDK type's models
struct SdkModelState: Sendable {
    var models: [CopilotModel] = []
    var isLoading: Bool = false
    var errorMessage: String? = nil
}

/// Unified store for all SDK model types - fetches via control-plane, no client caching
@MainActor
@Observable
final class UnifiedModelsStore {
    /// Models state per SDK type
    private(set) var claudeState = SdkModelState()
    private(set) var opencodeState = SdkModelState()
    private(set) var copilotState = SdkModelState()
    private(set) var codexState = SdkModelState()

    /// Copilot-specific status (auth & quota)
    private(set) var copilotStatus: CopilotStatus?
    private(set) var isLoadingCopilotStatus = false

    /// Sandbox resource limits
    private(set) var resourceLimits: ResourceLimits?
    private(set) var isLoadingResourceLimits = false

    /// Default model IDs per SDK
    static let defaultClaudeModelId = "claude-sonnet-4-5"
    static let defaultOpenCodeModelId = "anthropic/claude-sonnet-4-5"
    static let defaultCopilotModelId = "claude-sonnet-4.5"
    static let defaultCodexModelId = "gpt-5.2-codex:oauth:high"

    // MARK: - Accessors

    /// Get models for an SDK type
    func models(for sdkType: SdkType) -> [CopilotModel] {
        state(for: sdkType).models
    }

    /// Whether models are loading for an SDK type
    func isLoading(for sdkType: SdkType) -> Bool {
        state(for: sdkType).isLoading
    }

    /// Error message for an SDK type
    func errorMessage(for sdkType: SdkType) -> String? {
        state(for: sdkType).errorMessage
    }

    private func state(for sdkType: SdkType) -> SdkModelState {
        switch sdkType {
        case .claude: return claudeState
        case .opencode: return opencodeState
        case .copilot: return copilotState
        case .codex: return codexState
        }
    }

    // MARK: - Updates from server

    /// Update models from server response
    func updateModels(_ models: [CopilotModel], sdkType: SdkType) {
        switch sdkType {
        case .claude:
            claudeState.models = models
            claudeState.isLoading = false
            claudeState.errorMessage = nil
        case .opencode:
            opencodeState.models = models
            opencodeState.isLoading = false
            opencodeState.errorMessage = nil
        case .copilot:
            copilotState.models = models
            copilotState.isLoading = false
            copilotState.errorMessage = nil
        case .codex:
            codexState.models = models
            codexState.isLoading = false
            codexState.errorMessage = nil
        }
    }

    /// Set loading state for SDK type
    func setLoading(_ loading: Bool, for sdkType: SdkType) {
        switch sdkType {
        case .claude: claudeState.isLoading = loading
        case .opencode: opencodeState.isLoading = loading
        case .copilot: copilotState.isLoading = loading
        case .codex: codexState.isLoading = loading
        }
    }

    /// Set error for SDK type
    func setError(_ error: String, for sdkType: SdkType) {
        switch sdkType {
        case .claude:
            claudeState.errorMessage = error
            claudeState.isLoading = false
        case .opencode:
            opencodeState.errorMessage = error
            opencodeState.isLoading = false
        case .copilot:
            copilotState.errorMessage = error
            copilotState.isLoading = false
        case .codex:
            codexState.errorMessage = error
            codexState.isLoading = false
        }
    }

    /// Update Copilot status
    func updateCopilotStatus(_ status: CopilotStatus) {
        self.copilotStatus = status
        self.isLoadingCopilotStatus = false
    }

    /// Set Copilot status loading
    func setLoadingCopilotStatus(_ loading: Bool) {
        isLoadingCopilotStatus = loading
    }

    /// Update resource limits from server response
    func updateResourceLimits(_ limits: ResourceLimits) {
        self.resourceLimits = limits
        self.isLoadingResourceLimits = false
    }

    /// Set resource limits loading
    func setLoadingResourceLimits(_ loading: Bool) {
        isLoadingResourceLimits = loading
    }

    // MARK: - Lookups

    /// Find model by ID for a given SDK type
    func model(id: String, sdkType: SdkType) -> CopilotModel? {
        models(for: sdkType).first { $0.id == id }
    }

    /// Get default model ID for SDK type
    static func defaultModelId(for sdkType: SdkType) -> String {
        switch sdkType {
        case .claude: return defaultClaudeModelId
        case .opencode: return defaultOpenCodeModelId
        case .copilot: return defaultCopilotModelId
        case .codex: return defaultCodexModelId
        }
    }
}
