import SwiftUI

extension Animation {
    /// Smooth spring for general UI transitions
    static let glassSpring = Animation.spring(
        response: 0.4,
        dampingFraction: 0.8,
        blendDuration: 0.1
    )

    /// Bouncy spring for playful interactions
    static let bouncy = Animation.bouncy(duration: 0.5, extraBounce: 0.2)

    /// Quick spring for responsive feedback
    static let snappy = Animation.spring(
        response: 0.25,
        dampingFraction: 0.85,
        blendDuration: 0
    )

    /// Smooth ease for subtle transitions
    static let smoothAppear = Animation.easeOut(duration: 0.3)

    /// Slow ease for dramatic reveals
    static let slowReveal = Animation.easeInOut(duration: 0.5)
}

// MARK: - Transition Extensions

extension AnyTransition {
    /// Glass-style fade with scale
    static var glassAppear: AnyTransition {
        .opacity.combined(with: .scale(scale: 0.95))
    }

    /// Slide from bottom with fade
    static var slideUp: AnyTransition {
        .move(edge: .bottom).combined(with: .opacity)
    }

    /// Gentle scale transition
    static var gentleScale: AnyTransition {
        .scale(scale: 0.98).combined(with: .opacity)
    }
}

// MARK: - View Modifiers for Animations

extension View {
    /// Apply staggered animation delay based on index
    func staggeredAnimation(index: Int, baseDelay: Double = 0.05) -> some View {
        self.animation(
            .glassSpring.delay(Double(index) * baseDelay),
            value: index
        )
    }

    /// Pulse animation for loading states
    func pulsing() -> some View {
        modifier(PulsingModifier())
    }
}

struct PulsingModifier: ViewModifier {
    @State private var isPulsing = false

    func body(content: Content) -> some View {
        content
            .opacity(isPulsing ? 0.5 : 1.0)
            .animation(
                .easeInOut(duration: 0.8)
                .repeatForever(autoreverses: true),
                value: isPulsing
            )
            .onAppear {
                isPulsing = true
            }
    }
}

// MARK: - Haptic Feedback

enum HapticFeedback {
    static func light() {
        UIImpactFeedbackGenerator(style: .light).impactOccurred()
    }

    static func medium() {
        UIImpactFeedbackGenerator(style: .medium).impactOccurred()
    }

    static func heavy() {
        UIImpactFeedbackGenerator(style: .heavy).impactOccurred()
    }

    static func success() {
        UINotificationFeedbackGenerator().notificationOccurred(.success)
    }

    static func warning() {
        UINotificationFeedbackGenerator().notificationOccurred(.warning)
    }

    static func error() {
        UINotificationFeedbackGenerator().notificationOccurred(.error)
    }

    static func selection() {
        UISelectionFeedbackGenerator().selectionChanged()
    }
}
