import SwiftUI

/// A styled SDK picker with logos for each provider
struct SdkPicker: View {
    @Binding var selection: SdkType

    var body: some View {
        HStack(spacing: Theme.Spacing.xs) {
            ForEach(SdkType.allCases, id: \.self) { sdk in
                SdkPickerOption(
                    sdk: sdk,
                    isSelected: selection == sdk,
                    onTap: {
                        withAnimation(.smooth(duration: 0.2)) {
                            selection = sdk
                        }
                    }
                )
            }
        }
    }
}

/// Individual option in the SDK picker
private struct SdkPickerOption: View {
    let sdk: SdkType
    let isSelected: Bool
    let onTap: () -> Void

    var body: some View {
        Button(action: onTap) {
            VStack(spacing: Theme.Spacing.xxs) {
                // Logo
                sdk.logo
                    .resizable()
                    .scaledToFit()
                    .frame(width: 20, height: 20)
                    .foregroundStyle(isSelected ? Theme.Colors.brand : .secondary)

                // Name
                Text(sdk.shortName)
                    .font(.system(size: TypeScale.tiny, weight: isSelected ? .semibold : .regular, design: .rounded))
                    .foregroundStyle(isSelected ? .primary : .secondary)
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, Theme.Spacing.sm)
            .contentShape(Rectangle())
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

// MARK: - SdkType Logo Extension

extension SdkType {
    /// Logo image for the SDK
    var logo: Image {
        switch self {
        case .claude:
            return Image("anthropic-logo")
        case .opencode:
            return Image("opencode-logo")
        case .copilot:
            return Image("github-mark")
        }
    }

    /// Short display name for compact UI
    var shortName: String {
        switch self {
        case .claude: return "Claude"
        case .opencode: return "OpenCode"
        case .copilot: return "Copilot"
        }
    }
}

// MARK: - Previews

#Preview("SDK Picker") {
    struct PreviewWrapper: View {
        @State private var selection: SdkType = .claude

        var body: some View {
            VStack(spacing: 20) {
                SdkPicker(selection: $selection)

                Text("Selected: \(selection.displayName)")
                    .font(.netclodeCaption)
                    .foregroundStyle(.secondary)
            }
            .padding()
            .background(Theme.Colors.background)
        }
    }

    return PreviewWrapper()
}

#Preview("SDK Picker - All States") {
    VStack(spacing: 20) {
        ForEach(SdkType.allCases, id: \.self) { sdk in
            SdkPicker(selection: .constant(sdk))
        }
    }
    .padding()
    .background(Theme.Colors.background)
}
