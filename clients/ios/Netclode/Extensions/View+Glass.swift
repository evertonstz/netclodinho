import SwiftUI

// MARK: - Card Style Extensions

extension View {
    /// Apply a standard card style
    func cardStyle(background: Color = Theme.Colors.secondaryBackground) -> some View {
        self
            .padding(Theme.Spacing.md)
            .background(background)
            .clipShape(RoundedRectangle(cornerRadius: Theme.Radius.lg))
    }

    /// Apply themed shadow
    func themedShadow(_ style: Theme.ShadowStyle = Theme.Shadow.soft) -> some View {
        self.shadow(
            color: style.color,
            radius: style.radius,
            x: style.x,
            y: style.y
        )
    }
}

// MARK: - Conditional Modifiers

extension View {
    /// Apply a modifier only if a condition is true
    @ViewBuilder
    func `if`<Content: View>(_ condition: Bool, transform: (Self) -> Content) -> some View {
        if condition {
            transform(self)
        } else {
            self
        }
    }

    /// Apply a modifier only if a value is non-nil
    @ViewBuilder
    func ifLet<T, Content: View>(_ value: T?, transform: (Self, T) -> Content) -> some View {
        if let value {
            transform(self, value)
        } else {
            self
        }
    }
}

// MARK: - Animation Extensions

extension View {
    /// Apply glass spring animation to value changes
    func glassAnimation<V: Equatable>(value: V) -> some View {
        self.animation(.glassSpring, value: value)
    }

    /// Apply bouncy animation to value changes
    func bouncyAnimation<V: Equatable>(value: V) -> some View {
        self.animation(.bouncy, value: value)
    }
}

// MARK: - Preview Helpers

extension View {
    /// Wrap view in standard preview environment
    func previewEnvironment() -> some View {
        self
            .environment(SessionStore())
            .environment(ChatStore())
            .environment(EventStore())
            .environment(TerminalStore())
            .environment(SettingsStore())
            .environment(ConnectService())
            .environment(MessageRouter.preview)
    }
}
