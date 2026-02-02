import SwiftUI

/// Displays a provider logo based on provider name or model name
struct ProviderLogo: View {
    let provider: String?
    var modelName: String? = nil  // Used to infer logo when provider is generic (e.g., "Copilot")
    var size: CGFloat = 16

    var body: some View {
        if let image = providerImage {
            image
                .resizable()
                .scaledToFit()
                .frame(width: size, height: size)
        } else {
            // Fallback: sparkles icon for unknown providers
            Image(systemName: "sparkles")
                .font(.system(size: size * 0.75))
                .frame(width: size, height: size)
        }
    }

    private var providerImage: Image? {
        // First try to infer from model name (more specific)
        if let modelName = modelName?.lowercased() {
            if modelName.contains("claude") || modelName.contains("sonnet") || modelName.contains("haiku") || modelName.contains("opus") {
                return Image("anthropic-logo")
            } else if modelName.contains("gpt") || modelName.contains("o1") || modelName.contains("o3") || modelName.contains("codex") {
                return Image("openai-logo")
            } else if modelName.contains("gemini") {
                return Image("google-logo")
            } else if modelName.contains("grok") {
                return Image("xai-logo")
            } else if modelName.contains("mistral") || modelName.contains("codestral") || modelName.contains("devstral") || modelName.contains("ministral") || modelName.contains("magistral") || modelName.contains("mixtral") || modelName.contains("pixtral") {
                return Image("mistral-logo")
            } else if modelName.contains("llama") || modelName.contains("qwen") || modelName.contains("deepseek") || modelName.contains("phi") {
                return Image("ollama-logo")
            }
        }
        
        guard let provider = provider?.lowercased() else { return nil }

        switch provider {
        case "anthropic":
            return Image("anthropic-logo")
        case "openai", "chatgpt":
            return Image("openai-logo")
        case "google", "google ai":
            return Image("google-logo")
        case "xai":
            return Image("xai-logo")
        case "mistral":
            return Image("mistral-logo")
        case "github", "github copilot", "copilot":
            return Image("github-mark")
        case "ollama":
            return Image("ollama-logo")
        case "opencode":
            return Image("opencode-logo")
        default:
            // Check for partial matches in provider
            if provider.contains("anthropic") || provider.contains("claude") {
                return Image("anthropic-logo")
            } else if provider.contains("openai") || provider.contains("gpt") || provider.contains("o1") || provider.contains("o3") {
                return Image("openai-logo")
            } else if provider.contains("google") || provider.contains("gemini") {
                return Image("google-logo")
            } else if provider.contains("xai") || provider.contains("grok") {
                return Image("xai-logo")
            } else if provider.contains("mistral") {
                return Image("mistral-logo")
            } else if provider.contains("github") || provider.contains("copilot") {
                return Image("github-mark")
            } else if provider.contains("ollama") {
                return Image("ollama-logo")
            } else if provider.contains("opencode") {
                return Image("opencode-logo")
            }
            return nil
        }
    }
}

// MARK: - Previews

#Preview("Provider Logos") {
    VStack(spacing: 20) {
        HStack(spacing: 16) {
            VStack {
                ProviderLogo(provider: "Anthropic", size: 24)
                Text("Anthropic").font(.caption)
            }
            VStack {
                ProviderLogo(provider: "OpenAI", size: 24)
                Text("OpenAI").font(.caption)
            }
            VStack {
                ProviderLogo(provider: "Google", size: 24)
                Text("Google").font(.caption)
            }
            VStack {
                ProviderLogo(provider: "xAI", size: 24)
                Text("xAI").font(.caption)
            }
            VStack {
                ProviderLogo(provider: "GitHub", size: 24)
                Text("GitHub").font(.caption)
            }
            VStack {
                ProviderLogo(provider: "Mistral", size: 24)
                Text("Mistral").font(.caption)
            }
            VStack {
                ProviderLogo(provider: "Ollama", size: 24)
                Text("Ollama").font(.caption)
            }
            VStack {
                ProviderLogo(provider: "OpenCode", size: 24)
                Text("OpenCode").font(.caption)
            }
        }

        HStack(spacing: 16) {
            ProviderLogo(provider: "Anthropic", size: 16)
            ProviderLogo(provider: "OpenAI", size: 16)
            ProviderLogo(provider: "Google", size: 16)
            ProviderLogo(provider: "xAI", size: 16)
            ProviderLogo(provider: "GitHub", size: 16)
            ProviderLogo(provider: "Mistral", size: 16)
            ProviderLogo(provider: "Ollama", size: 16)
            ProviderLogo(provider: "OpenCode", size: 16)
        }
        .foregroundStyle(.secondary)
    }
    .padding()
}
