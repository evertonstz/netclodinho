import SwiftUI

struct GlassTextField: View {
    let placeholder: String
    @Binding var text: String
    let icon: String?
    let axis: Axis

    init(
        _ placeholder: String,
        text: Binding<String>,
        icon: String? = nil,
        axis: Axis = .horizontal
    ) {
        self.placeholder = placeholder
        self._text = text
        self.icon = icon
        self.axis = axis
    }

    var body: some View {
        HStack(spacing: Theme.Spacing.sm) {
            if let icon {
                Image(systemName: icon)
                    .foregroundStyle(.secondary)
            }

            TextField(placeholder, text: $text, axis: axis)
                .textFieldStyle(.plain)
        }
        .font(.netclodeBody)
        .padding(.horizontal, Theme.Spacing.md)
        .padding(.vertical, Theme.Spacing.sm)
        .glassEffect(
            .regular.interactive().tint(Theme.Colors.inputTint),
            in: RoundedRectangle(cornerRadius: Theme.Radius.md)
        )
    }
}

// MARK: - Secure Glass Text Field

struct GlassSecureField: View {
    let placeholder: String
    @Binding var text: String
    let icon: String?

    init(
        _ placeholder: String,
        text: Binding<String>,
        icon: String? = "lock.fill"
    ) {
        self.placeholder = placeholder
        self._text = text
        self.icon = icon
    }

    var body: some View {
        HStack(spacing: Theme.Spacing.sm) {
            if let icon {
                Image(systemName: icon)
                    .foregroundStyle(.secondary)
            }

            SecureField(placeholder, text: $text)
                .textFieldStyle(.plain)
        }
        .font(.netclodeBody)
        .padding(.horizontal, Theme.Spacing.md)
        .padding(.vertical, Theme.Spacing.sm)
        .glassEffect(
            .regular.interactive().tint(Theme.Colors.inputTint),
            in: RoundedRectangle(cornerRadius: Theme.Radius.md)
        )
    }
}

// MARK: - Multi-line Glass Text Editor

struct GlassTextEditor: View {
    let placeholder: String
    @Binding var text: String
    let minHeight: CGFloat

    init(
        _ placeholder: String = "",
        text: Binding<String>,
        minHeight: CGFloat = 100
    ) {
        self.placeholder = placeholder
        self._text = text
        self.minHeight = minHeight
    }

    var body: some View {
        ZStack(alignment: .topLeading) {
            if text.isEmpty {
                Text(placeholder)
                    .foregroundStyle(.tertiary)
                    .padding(.horizontal, Theme.Spacing.xxs)
                    .padding(.vertical, Theme.Spacing.xs)
            }

            TextEditor(text: $text)
                .scrollContentBackground(.hidden)
                .background(.clear)
                .frame(minHeight: minHeight)
        }
        .font(.netclodeBody)
        .padding(Theme.Spacing.sm)
        .glassEffect(
            .regular.interactive().tint(Theme.Colors.inputTint),
            in: RoundedRectangle(cornerRadius: Theme.Radius.md)
        )
    }
}

// MARK: - Preview

#Preview {
    ZStack {
        WarmGradientBackground()

        VStack(spacing: 20) {
            GlassTextField("Server URL", text: .constant(""), icon: "server.rack")

            GlassTextField("Session name", text: .constant("My Project"))

            GlassSecureField("API Key", text: .constant(""))

            GlassTextEditor("Enter your prompt...", text: .constant(""))
        }
        .padding()
    }
}
