import Foundation

/// Store for managing Copilot SDK models and status
@MainActor
@Observable
final class CopilotStore {
    /// Available models (combined GitHub Copilot + Anthropic BYOK)
    private(set) var models: [CopilotModel] = []
    
    /// Copilot authentication and quota status
    private(set) var status: CopilotStatus?
    
    /// Whether a fetch is in progress
    private(set) var isLoadingModels = false
    private(set) var isLoadingStatus = false
    
    /// Error messages
    private(set) var modelsError: String?
    private(set) var statusError: String?

    /// Default model ID (Claude Sonnet 4.5 via Copilot)
    static let defaultModelId = "claude-sonnet-4.5"

    /// Update models from server response
    func updateModels(_ models: [CopilotModel]) {
        self.models = models
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
