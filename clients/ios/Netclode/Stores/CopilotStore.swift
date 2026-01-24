import Foundation

/// Store for managing Copilot SDK models and status
@MainActor
@Observable
final class CopilotStore {
    /// Available models for GitHub backend
    private(set) var githubModels: [CopilotModel] = []
    
    /// Available models for Anthropic backend
    private(set) var anthropicModels: [CopilotModel] = []
    
    /// Copilot authentication and quota status
    private(set) var status: CopilotStatus?
    
    /// Whether a fetch is in progress
    private(set) var isLoadingModels = false
    private(set) var isLoadingStatus = false
    
    /// Error messages
    private(set) var modelsError: String?
    private(set) var statusError: String?

    /// Default model IDs
    static let defaultGitHubModelId = "gpt-4o"
    static let defaultAnthropicModelId = "claude-sonnet-4-20250514"

    /// Get models for a specific backend
    func models(for backend: CopilotBackend) -> [CopilotModel] {
        switch backend {
        case .github:
            return githubModels
        case .anthropic:
            return anthropicModels
        }
    }

    /// Get default model ID for a backend
    func defaultModelId(for backend: CopilotBackend) -> String {
        switch backend {
        case .github:
            return Self.defaultGitHubModelId
        case .anthropic:
            return Self.defaultAnthropicModelId
        }
    }

    /// Update models from server response
    func updateModels(_ models: [CopilotModel], for backend: CopilotBackend) {
        switch backend {
        case .github:
            githubModels = models
        case .anthropic:
            anthropicModels = models
        }
        isLoadingModels = false
        modelsError = nil
    }

    /// Update status from server response
    func updateStatus(_ status: CopilotStatus) {
        self.status = status
        isLoadingStatus = false
        statusError = nil
    }

    /// Mark models as loading
    func setLoadingModels(_ loading: Bool) {
        isLoadingModels = loading
    }

    /// Mark status as loading
    func setLoadingStatus(_ loading: Bool) {
        isLoadingStatus = loading
    }

    /// Set models error
    func setModelsError(_ error: String) {
        modelsError = error
        isLoadingModels = false
    }

    /// Set status error
    func setStatusError(_ error: String) {
        statusError = error
        isLoadingStatus = false
    }
}
