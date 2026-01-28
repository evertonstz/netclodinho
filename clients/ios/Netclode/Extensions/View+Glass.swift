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

    /// Apply a cross-platform code/card background
    /// Uses material on macCatalyst for blur effect, semi-transparent color on iOS
    @ViewBuilder
    func codeCardBackground() -> some View {
        #if targetEnvironment(macCatalyst)
        self.background(.ultraThinMaterial)
        #else
        self.background(Theme.Colors.codeBackground.opacity(0.5))
        #endif
    }

    /// Apply a cross-platform glass effect for interactive elements (buttons, inputs)
    /// Uses .glassEffect on iOS, material + shape on macCatalyst
    @ViewBuilder
    func adaptiveGlass<S: Shape>(tint: Color? = nil, in shape: S) -> some View {
        #if targetEnvironment(macCatalyst)
        self
            .background(.regularMaterial, in: shape)
            .ifLet(tint) { view, tintColor in
                view.overlay(shape.fill(tintColor.opacity(0.2)))
            }
        #else
        if let tint {
            self.glassEffect(.regular.tint(tint.glassTint), in: shape)
        } else {
            self.glassEffect(.regular, in: shape)
        }
        #endif
    }

    /// Apply a cross-platform interactive glass effect
    @ViewBuilder
    func adaptiveGlassInteractive<S: Shape>(tint: Color? = nil, in shape: S) -> some View {
        #if targetEnvironment(macCatalyst)
        self
            .background(.regularMaterial, in: shape)
            .ifLet(tint) { view, tintColor in
                view.overlay(shape.fill(tintColor.opacity(0.2)))
            }
        #else
        if let tint {
            self.glassEffect(.regular.interactive().tint(tint.glassTint), in: shape)
        } else {
            self.glassEffect(.regular.interactive(), in: shape)
        }
        #endif
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
