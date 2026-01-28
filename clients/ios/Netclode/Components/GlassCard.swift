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
            .adaptiveGlass(tint: tint, in: RoundedRectangle(cornerRadius: cornerRadius))
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
            .adaptiveGlassInteractive(tint: tint, in: RoundedRectangle(cornerRadius: cornerRadius))
    }
}

// MARK: - Specialized Glass Cards (legacy support)

struct UserMessageCard<Content: View>: View {
    @ViewBuilder let content: () -> Content

    var body: some View {
        content()
            .padding(Theme.Spacing.md)
            .adaptiveGlass(tint: Theme.Colors.userBubble, in: RoundedRectangle(cornerRadius: Theme.Radius.lg))
    }
}

struct AssistantMessageCard<Content: View>: View {
    @ViewBuilder let content: () -> Content

    var body: some View {
        content()
            .padding(Theme.Spacing.md)
            .adaptiveGlass(in: RoundedRectangle(cornerRadius: Theme.Radius.lg))
    }
}

struct StatusCard<Content: View>: View {
    let status: SessionStatus
    @ViewBuilder let content: () -> Content

    var body: some View {
        GlassCard {
            content()
        }
    }
}

// MARK: - Preview

#Preview {
    VStack(spacing: 20) {
        GlassCard {
            Text("Regular Glass Card")
                .font(.netclodeHeadline)
        }

        GlassCard {
            Text("Another Glass Card")
                .font(.netclodeHeadline)
        }

        UserMessageCard {
            Text("User Message Style")
                .font(.netclodeBody)
                .foregroundStyle(Theme.Colors.userBubbleText)
        }

        AssistantMessageCard {
            Text("Assistant Message Style")
                .font(.netclodeBody)
                .foregroundStyle(Theme.Colors.assistantBubbleText)
        }
    }
    .padding()
    .background(Theme.Colors.background)
}
