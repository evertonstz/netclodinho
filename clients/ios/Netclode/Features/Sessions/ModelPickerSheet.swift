import SwiftUI

/// Renders model name with semibold purple effort level and dimmed provider suffix
/// e.g., "Codex Mini High (ChatGPT)" -> "Codex Mini " + bold purple "High" + dimmed " (ChatGPT)"
struct ModelNameText: View {
    let name: String
    let font: Font

    private static let effortLevels = ["xHigh", "High", "Med", "Low"]

    init(_ name: String, font: Font = .netclodeBody) {
        self.name = name
        self.font = font
    }

    var body: some View {
        let (baseName, effort, provider) = parseName(name)

        if let effort = effort {
            Text(baseName)
                .font(font)
                .foregroundStyle(.primary)
            + Text(effort)
                .font(font.weight(.semibold))
                .foregroundStyle(Theme.Colors.brand)
            + Text(provider ?? "")
                .font(font)
                .foregroundStyle(.tertiary)
        } else if let provider = provider {
            Text(baseName)
                .font(font)
                .foregroundStyle(.primary)
            + Text(provider)
                .font(font)
                .foregroundStyle(.tertiary)
        } else {
            Text(name)
                .font(font)
                .foregroundStyle(.primary)
        }
    }

    /// Parse "Codex Mini High (ChatGPT)" into ("Codex Mini ", "High", " (ChatGPT)")
    private func parseName(_ name: String) -> (String, String?, String?) {
        // Extract provider suffix like " (ChatGPT)"
        var baseName = name
        var provider: String? = nil

        if let providerRange = name.range(of: #"\s*\([^)]+\)$"#, options: .regularExpression) {
            provider = String(name[providerRange])
            baseName = String(name[..<providerRange.lowerBound])
        }

        // Check if baseName ends with an effort level
        for effort in Self.effortLevels {
            if baseName.hasSuffix(" \(effort)") {
                let effortStart = baseName.index(baseName.endIndex, offsetBy: -effort.count)
                return (String(baseName[..<effortStart]), effort, provider)
            }
        }

        return (baseName, nil, provider)
    }
}

/// Unified model for the picker (works across all providers)
struct PickerModel: Identifiable, Hashable {
    let id: String
    let name: String
    let provider: String?
    let supportsVision: Bool
    let supportsReasoning: Bool
    let inputCost: Double?
    let outputCost: Double?

    /// Create from CopilotModel
    static func from(_ model: CopilotModel) -> PickerModel {
        PickerModel(
            id: model.id,
            name: model.name,
            provider: model.provider,
            supportsVision: model.capabilities.contains("vision"),
            supportsReasoning: model.capabilities.contains("reasoning"),
            inputCost: nil,
            outputCost: nil
        )
    }

    /// Create from AIModel (models.dev)
    static func from(_ model: AIModel) -> PickerModel {
        PickerModel(
            id: model.fullModelId,
            name: model.name,
            provider: model.providerName,
            supportsVision: false,
            supportsReasoning: model.supportsReasoning,
            inputCost: model.inputCost,
            outputCost: model.outputCost
        )
    }
}

struct ModelPickerSheet: View {
    @Environment(\.dismiss) private var dismiss
    @Binding var selectedModelId: String
    let models: [PickerModel]
    let title: String
    let isLoading: Bool

    var body: some View {
        NavigationStack {
            Group {
                if isLoading && models.isEmpty {
                    VStack(spacing: Theme.Spacing.md) {
                        ProgressView()
                        Text("Loading models...")
                            .font(.netclodeCaption)
                            .foregroundStyle(.secondary)
                    }
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                } else if models.isEmpty {
                    ContentUnavailableView(
                        "No Models Available",
                        systemImage: "sparkles",
                        description: Text("Could not load models")
                    )
                } else {
                    ScrollView {
                        LazyVStack(spacing: Theme.Spacing.xs) {
                            ForEach(models) { model in
                                ModelRowGlass(
                                    model: model,
                                    isSelected: model.id == selectedModelId,
                                    onTap: {
                                        withAnimation(.smooth(duration: 0.3)) {
                                            selectedModelId = model.id
                                        }
                                        DispatchQueue.main.asyncAfter(deadline: .now() + 0.15) {
                                            dismiss()
                                        }
                                    }
                                )
                            }
                        }
                        .padding(.horizontal, Theme.Spacing.md)
                        .padding(.vertical, Theme.Spacing.sm)
                    }
                }
            }
            .background(Theme.Colors.background)
            .navigationTitle(title)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") {
                        dismiss()
                    }
                }
            }
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
    }
}

struct ModelRow: View {
    let model: PickerModel
    let isSelected: Bool

    var body: some View {
        HStack(spacing: Theme.Spacing.sm) {
            // Selection indicator
            Image(systemName: isSelected ? "checkmark.circle.fill" : "circle")
                .foregroundStyle(isSelected ? Theme.Colors.brand : .secondary)
                .font(.system(size: 20))
                .contentTransition(.symbolEffect(.replace))

            // Provider logo
            ProviderLogo(provider: model.provider, size: 20)
                .foregroundStyle(.secondary)

            // Model info
            VStack(alignment: .leading, spacing: 2) {
                ModelNameText(model.name)

                // Capabilities and cost row
                HStack(spacing: Theme.Spacing.sm) {
                    if model.supportsVision {
                        Label("Vision", systemImage: "eye")
                            .font(.netclodeCaption)
                            .foregroundStyle(.secondary)
                    }

                    if model.supportsReasoning {
                        Label("Reasoning", systemImage: "brain")
                            .font(.netclodeCaption)
                            .foregroundStyle(.secondary)
                    }

                    if let input = model.inputCost, let output = model.outputCost {
                        Text("$\(formatCost(input))/$\(formatCost(output))")
                            .font(.netclodeCaption)
                            .foregroundStyle(.tertiary)
                    }
                }
            }

            Spacer()
        }
        .padding(Theme.Spacing.sm)
        .contentShape(Rectangle())
    }

    private func formatCost(_ cost: Double) -> String {
        if cost < 1.0 {
            return String(format: "%.2f", cost)
        } else {
            return String(format: "%.0f", cost)
        }
    }
}

/// Model row with liquid glass effect
struct ModelRowGlass: View {
    let model: PickerModel
    let isSelected: Bool
    let onTap: () -> Void

    var body: some View {
        Button(action: onTap) {
            ModelRow(model: model, isSelected: isSelected)
        }
        .buttonStyle(.plain)
        .glassEffect(
            isSelected
                ? .regular.tint(Theme.Colors.brand.glassTint).interactive()
                : .regular.interactive(),
            in: RoundedRectangle(cornerRadius: Theme.Radius.md)
        )
    }
}

/// Full-width inline liquid glass picker
struct InlineModelPicker: View {
    @Binding var selectedModelId: String
    let models: [PickerModel]
    @Binding var isExpanded: Bool

    private var selectedModel: PickerModel? {
        models.first { $0.id == selectedModelId }
    }

    var body: some View {
        VStack(spacing: 0) {
            // Collapsed state - shows selected model
            Button {
                withAnimation(.smooth(duration: 0.25)) {
                    isExpanded.toggle()
                }
            } label: {
                HStack(spacing: Theme.Spacing.xs) {
                    if let model = selectedModel {
                        ProviderLogo(provider: model.provider, size: 16)
                            .frame(width: 20)
                            .foregroundStyle(.secondary)
                        ModelNameText(model.name)
                            .contentTransition(.numericText())
                    } else if !models.isEmpty {
                        // Show first model as fallback
                        ProviderLogo(provider: models.first?.provider, size: 16)
                            .frame(width: 20)
                            .foregroundStyle(.secondary)
                        ModelNameText(models.first?.name ?? "Select a model")
                            .contentTransition(.numericText())
                    } else {
                        Text("Select a model")
                            .font(.netclodeBody)
                            .foregroundStyle(.secondary)
                    }
                    Spacer()
                    Image(systemName: "chevron.up.chevron.down")
                        .font(.system(size: 12, weight: .medium))
                        .foregroundStyle(.secondary)
                        .rotationEffect(.degrees(isExpanded ? 180 : 0))
                }
                .padding(Theme.Spacing.sm)
                .frame(maxWidth: .infinity)
                .contentShape(Rectangle())
                .animation(.smooth(duration: 0.2), value: selectedModel?.id)
            }
            .buttonStyle(.plain)
            .glassEffect(
                isExpanded ? .regular.tint(Theme.Colors.brand.glassTint).interactive() : .regular.interactive(),
                in: RoundedRectangle(cornerRadius: Theme.Radius.md)
            )

            // Expanded state - shows all options
            if isExpanded {
                ScrollView {
                    LazyVStack(spacing: 2) {
                        ForEach(models) { model in
                            Button {
                                withAnimation(.smooth(duration: 0.2)) {
                                    selectedModelId = model.id
                                    isExpanded = false
                                }
                            } label: {
                                HStack(spacing: Theme.Spacing.xs) {
                                    Image(systemName: model.id == selectedModelId ? "checkmark.circle.fill" : "circle")
                                        .foregroundStyle(model.id == selectedModelId ? Theme.Colors.brand : .secondary)
                                        .font(.system(size: 16))
                                        .contentTransition(.symbolEffect(.replace))

                                    ProviderLogo(provider: model.provider, size: 16)
                                        .foregroundStyle(.secondary)

                                    ModelNameText(model.name)

                                    Spacer()
                                }
                                .padding(.horizontal, Theme.Spacing.sm)
                                .padding(.vertical, Theme.Spacing.xs)
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .contentShape(Rectangle())
                            }
                            .buttonStyle(.plain)
                        }
                    }
                    .padding(.vertical, Theme.Spacing.xs)
                }
                .frame(maxHeight: 280)
                .glassEffect(.regular, in: RoundedRectangle(cornerRadius: Theme.Radius.md))
                .transition(.asymmetric(
                    insertion: .opacity.combined(with: .scale(scale: 0.95, anchor: .top)),
                    removal: .opacity
                ))
                .padding(.top, Theme.Spacing.xs)
            }
        }
        .animation(.smooth(duration: 0.25), value: isExpanded)
    }

}

/// A button that shows the selected model and opens the picker
struct ModelPickerButton: View {
    let selectedModel: PickerModel?
    let placeholder: String
    let isLoading: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: Theme.Spacing.xs) {
                if isLoading {
                    ProgressView()
                        .scaleEffect(0.8)
                } else if let model = selectedModel {
                    ProviderLogo(provider: model.provider, size: 18)
                        .foregroundStyle(.secondary)

                    ModelNameText(model.name)
                } else {
                    Text(placeholder)
                        .font(.netclodeBody)
                        .foregroundStyle(.secondary)
                }

                Spacer()

                Image(systemName: "chevron.up.chevron.down")
                    .font(.system(size: 12, weight: .medium))
                    .foregroundStyle(.secondary)
            }
            .padding(Theme.Spacing.sm)
            .frame(maxWidth: .infinity)
            .glassEffect(.regular.interactive(), in: .rect(cornerRadius: Theme.Radius.md))
        }
        .buttonStyle(.plain)
    }

}

#Preview("Model Picker Sheet") {
    ModelPickerSheet(
        selectedModelId: .constant("claude-sonnet-4-20250514"),
        models: [
            PickerModel(id: "claude-sonnet-4-20250514", name: "Claude Sonnet 4", provider: "Anthropic", supportsVision: true, supportsReasoning: true, inputCost: 3.0, outputCost: 15.0),
            PickerModel(id: "gpt-4o", name: "GPT-4o", provider: "OpenAI", supportsVision: true, supportsReasoning: false, inputCost: nil, outputCost: nil),
            PickerModel(id: "gpt-4o-mini", name: "GPT-4o Mini", provider: "OpenAI", supportsVision: true, supportsReasoning: false, inputCost: nil, outputCost: nil),
            PickerModel(id: "o3-mini", name: "o3-mini", provider: "OpenAI", supportsVision: false, supportsReasoning: true, inputCost: nil, outputCost: nil),
        ],
        title: "Select Model",
        isLoading: false
    )
}

#Preview("Model Picker Button") {
    VStack(spacing: 20) {
        ModelPickerButton(
            selectedModel: PickerModel(id: "claude-sonnet-4", name: "Claude Sonnet 4", provider: "Anthropic", supportsVision: true, supportsReasoning: true, inputCost: nil, outputCost: nil),
            placeholder: "Select a model",
            isLoading: false,
            action: {}
        )

        ModelPickerButton(
            selectedModel: PickerModel(id: "gpt-4o-mini", name: "GPT-4o Mini", provider: "OpenAI", supportsVision: true, supportsReasoning: false, inputCost: nil, outputCost: nil),
            placeholder: "Select a model",
            isLoading: false,
            action: {}
        )

        ModelPickerButton(
            selectedModel: nil,
            placeholder: "Select a model",
            isLoading: true,
            action: {}
        )
    }
    .padding()
    .background(Theme.Colors.background)
}
