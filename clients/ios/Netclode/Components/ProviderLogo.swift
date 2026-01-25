import SwiftUI

/// Displays a provider logo based on provider name
struct ProviderLogo: View {
    let provider: String?
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
        guard let provider = provider?.lowercased() else { return nil }

        switch provider {
        case "anthropic":
            return Image("anthropic-logo")
        case "openai":
            return Image("openai-logo")
        case "google", "google ai":
            return Image("google-logo")
        case "xai":
            return Image("xai-logo")
        case "github", "github copilot":
            return Image("github-mark")
        default:
            // Check for partial matches
            if provider.contains("anthropic") || provider.contains("claude") {
                return Image("anthropic-logo")
            } else if provider.contains("openai") || provider.contains("gpt") || provider.contains("o1") || provider.contains("o3") {
                return Image("openai-logo")
            } else if provider.contains("google") || provider.contains("gemini") {
                return Image("google-logo")
            } else if provider.contains("xai") || provider.contains("grok") {
                return Image("xai-logo")
            } else if provider.contains("github") || provider.contains("copilot") {
                return Image("github-mark")
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
        }

        HStack(spacing: 16) {
            ProviderLogo(provider: "Anthropic", size: 16)
            ProviderLogo(provider: "OpenAI", size: 16)
            ProviderLogo(provider: "Google", size: 16)
            ProviderLogo(provider: "xAI", size: 16)
            ProviderLogo(provider: "GitHub", size: 16)
        }
        .foregroundStyle(.secondary)
    }
    .padding()
}
