import SwiftUI

struct GlassCard<Content: View>: View {
    let tint: Color?
    let cornerRadius: CGFloat
    let padding: CGFloat
    @ViewBuilder let content: () -> Content

    init(
        tint: Color? = nil,
        cornerRadius: CGFloat = Theme.Radius.lg,
        padding: CGFloat = Theme.Spacing.md,
        @ViewBuilder content: @escaping () -> Content
    ) {
        self.tint = tint
        self.cornerRadius = cornerRadius
        self.padding = padding
        self.content = content
    }

    var body: some View {
        content()
            .padding(padding)
            .glassEffect(
                .regular.tint(tint ?? .clear),
                in: RoundedRectangle(cornerRadius: cornerRadius)
            )
    }
}

// MARK: - Glass Card Variants

struct GlassCardInteractive<Content: View>: View {
    let tint: Color?
    let cornerRadius: CGFloat
    let padding: CGFloat
    @ViewBuilder let content: () -> Content

    init(
        tint: Color? = nil,
        cornerRadius: CGFloat = Theme.Radius.lg,
        padding: CGFloat = Theme.Spacing.md,
        @ViewBuilder content: @escaping () -> Content
    ) {
        self.tint = tint
        self.cornerRadius = cornerRadius
        self.padding = padding
        self.content = content
    }

    var body: some View {
        content()
            .padding(padding)
            .glassEffect(
                .regular.interactive().tint(tint ?? .clear),
                in: RoundedRectangle(cornerRadius: cornerRadius)
            )
    }
}

// MARK: - Specialized Glass Cards

struct UserMessageCard<Content: View>: View {
    @ViewBuilder let content: () -> Content

    var body: some View {
        GlassCard(tint: Theme.Colors.userMessageTint) {
            content()
        }
    }
}

struct AssistantMessageCard<Content: View>: View {
    @ViewBuilder let content: () -> Content

    var body: some View {
        GlassCard(tint: Theme.Colors.assistantMessageTint) {
            content()
        }
    }
}

struct StatusCard<Content: View>: View {
    let status: SessionStatus
    @ViewBuilder let content: () -> Content

    var body: some View {
        GlassCard(tint: status.tintColor.glassTint) {
            content()
        }
    }
}

// MARK: - Preview

#Preview {
    ZStack {
        WarmGradientBackground()

        VStack(spacing: 20) {
            GlassCard {
                Text("Regular Glass Card")
                    .font(.netclodeHeadline)
            }

            GlassCard(tint: Theme.Colors.cozyPurple.opacity(0.3)) {
                Text("Tinted Glass Card")
                    .font(.netclodeHeadline)
            }

            UserMessageCard {
                Text("User Message Style")
                    .font(.netclodeBody)
            }

            AssistantMessageCard {
                Text("Assistant Message Style")
                    .font(.netclodeBody)
            }
        }
        .padding()
    }
}
