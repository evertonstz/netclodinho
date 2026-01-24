import Foundation

/// Represents an AI model from models.dev
struct AIModel: Identifiable, Codable, Hashable, Sendable {
    let id: String
    let name: String
    let providerId: String
    let providerName: String
    let contextLimit: Int?
    let outputLimit: Int?
    let inputCost: Double?
    let outputCost: Double?
    let supportsReasoning: Bool
    let supportsToolCalls: Bool

    /// Display name including provider
    var displayName: String {
        name
    }

    /// Full model ID for API calls (e.g., "anthropic/claude-sonnet-4-0")
    var fullModelId: String {
        "\(providerId)/\(id)"
    }

    /// Cost display string
    var costDisplay: String? {
        guard let input = inputCost, let output = outputCost else { return nil }
        return "$\(String(format: "%.2f", input))/$\(String(format: "%.2f", output)) per 1M tokens"
    }
}

/// Store for managing AI models from models.dev API
@MainActor
@Observable
final class ModelsStore {
    /// Models grouped by provider
    private(set) var modelsByProvider: [String: [AIModel]] = [:]

    /// All models flattened
    var allModels: [AIModel] {
        modelsByProvider.values.flatMap { $0 }.sorted { $0.name < $1.name }
    }

    /// Anthropic models only (for Phase 1)
    var anthropicModels: [AIModel] {
        modelsByProvider["anthropic"] ?? []
    }

    /// Whether a fetch is in progress
    private(set) var isLoading = false

    /// Last fetch date
    private(set) var lastFetched: Date?

    /// Error message if fetch failed
    private(set) var errorMessage: String?

    /// Cache TTL (1 hour)
    private let cacheTTL: TimeInterval = 3600

    /// Default model ID
    static let defaultModelId = "anthropic/claude-sonnet-4-0"

    /// UserDefaults keys
    private enum StorageKeys {
        static let models = "ai_models_cache"
        static let lastFetched = "ai_models_last_fetched"
    }

    /// Whether cache is stale
    var isCacheStale: Bool {
        guard let lastFetched else { return true }
        return Date().timeIntervalSince(lastFetched) > cacheTTL
    }

    init() {
        Task.detached(priority: .userInitiated) { [weak self] in
            await self?.loadFromStorage()
        }
    }

    // MARK: - Persistence

    private func loadFromStorage() async {
        let modelsKey = StorageKeys.models
        let lastFetchedKey = StorageKeys.lastFetched

        let result = await Task.detached(priority: .userInitiated) { () -> ([String: [AIModel]]?, Date?) in
            let models: [String: [AIModel]]?
            if let data = UserDefaults.standard.data(forKey: modelsKey) {
                models = try? JSONDecoder().decode([String: [AIModel]].self, from: data)
            } else {
                models = nil
            }

            let lastFetched = UserDefaults.standard.object(forKey: lastFetchedKey) as? Date
            return (models, lastFetched)
        }.value

        if let models = result.0 {
            self.modelsByProvider = models
        }
        if let lastFetched = result.1 {
            self.lastFetched = lastFetched
        }

        // Fetch if cache is stale
        if isCacheStale {
            await fetchModels()
        }
    }

    private func saveToStorage() {
        let modelsToSave = modelsByProvider
        let lastFetchedToSave = lastFetched
        let modelsKey = StorageKeys.models
        let lastFetchedKey = StorageKeys.lastFetched

        Task.detached(priority: .utility) {
            if let encoded = try? JSONEncoder().encode(modelsToSave) {
                UserDefaults.standard.set(encoded, forKey: modelsKey)
            }
            if let lastFetched = lastFetchedToSave {
                UserDefaults.standard.set(lastFetched, forKey: lastFetchedKey)
            }
        }
    }

    // MARK: - Fetching

    /// Fetch models from models.dev API
    func fetchModels() async {
        guard !isLoading else { return }

        isLoading = true
        errorMessage = nil

        do {
            let url = URL(string: "https://models.dev/api.json")!
            let (data, _) = try await URLSession.shared.data(from: url)

            let response = try JSONDecoder().decode(ModelsDevResponse.self, from: data)
            var newModels: [String: [AIModel]] = [:]

            // For Phase 1, only include Anthropic
            // Later we can expand to other providers
            let targetProviders = ["anthropic"]

            for providerId in targetProviders {
                guard let provider = response.providers[providerId] else { continue }

                let models = provider.models.values.compactMap { model -> AIModel? in
                    // Filter for models that support tool calls (required for coding)
                    guard model.toolCall == true else { return nil }

                    return AIModel(
                        id: model.id,
                        name: model.name,
                        providerId: providerId,
                        providerName: provider.name,
                        contextLimit: model.limit?.context,
                        outputLimit: model.limit?.output,
                        inputCost: model.cost?.input,
                        outputCost: model.cost?.output,
                        supportsReasoning: model.reasoning ?? false,
                        supportsToolCalls: model.toolCall ?? false
                    )
                }.sorted { $0.name < $1.name }

                if !models.isEmpty {
                    newModels[providerId] = models
                }
            }

            self.modelsByProvider = newModels
            self.lastFetched = Date()
            self.isLoading = false
            saveToStorage()

        } catch {
            self.errorMessage = error.localizedDescription
            self.isLoading = false
        }
    }

    /// Force refresh
    func refresh() async {
        await fetchModels()
    }

    /// Find model by full ID (e.g., "anthropic/claude-sonnet-4-0")
    func model(forFullId fullId: String) -> AIModel? {
        let parts = fullId.split(separator: "/", maxSplits: 1)
        guard parts.count == 2 else { return nil }
        let providerId = String(parts[0])
        let modelId = String(parts[1])
        return modelsByProvider[providerId]?.first { $0.id == modelId }
    }
}

// MARK: - API Response Types

private struct ModelsDevResponse: Decodable {
    let providers: [String: ProviderData]

    init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        providers = try container.decode([String: ProviderData].self)
    }
}

private struct ProviderData: Decodable {
    let id: String
    let name: String
    let models: [String: ModelData]

    enum CodingKeys: String, CodingKey {
        case id, name, models
    }
}

private struct ModelData: Decodable {
    let id: String
    let name: String
    let reasoning: Bool?
    let toolCall: Bool?
    let cost: ModelCost?
    let limit: ModelLimit?

    enum CodingKeys: String, CodingKey {
        case id, name, reasoning
        case toolCall = "tool_call"
        case cost, limit
    }
}

private struct ModelCost: Decodable {
    let input: Double?
    let output: Double?
}

private struct ModelLimit: Decodable {
    let context: Int?
    let output: Int?
}
