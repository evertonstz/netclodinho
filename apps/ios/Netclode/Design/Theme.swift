import SwiftUI

enum Theme {
    // MARK: - Warm, Cozy Color Palette

    enum Colors {
        // Base warm tones
        static let warmCream = Color(red: 0.98, green: 0.96, blue: 0.94)
        static let warmPeach = Color(red: 0.95, green: 0.75, blue: 0.65)
        static let warmApricot = Color(red: 0.95, green: 0.6, blue: 0.4)
        static let warmCoral = Color(red: 0.92, green: 0.5, blue: 0.45)

        // Cozy accent colors
        static let cozyLavender = Color(red: 0.7, green: 0.6, blue: 0.8)
        static let cozyPurple = Color(red: 0.6, green: 0.5, blue: 0.7)
        static let cozySage = Color(red: 0.6, green: 0.75, blue: 0.65)
        static let cozyTeal = Color(red: 0.5, green: 0.7, blue: 0.75)

        // Gentle neutrals
        static let gentleBlue = Color(red: 0.5, green: 0.65, blue: 0.8)
        static let gentleGray = Color(red: 0.6, green: 0.6, blue: 0.62)
        static let softCharcoal = Color(red: 0.25, green: 0.25, blue: 0.28)

        // UI colors
        static let userMessageTint = gentleBlue.opacity(0.3)
        static let assistantMessageTint = warmApricot.opacity(0.2)
        static let inputTint = cozyPurple.opacity(0.15)
        static let buttonTint = cozyPurple.opacity(0.4)
    }

    // MARK: - Status Colors

    enum StatusColor {
        case creating
        case ready
        case running
        case paused
        case error

        var color: Color {
            switch self {
            case .creating: Color.orange.opacity(0.8)
            case .ready: Color.green.opacity(0.8)
            case .running: Color.blue.opacity(0.8)
            case .paused: Color.gray.opacity(0.8)
            case .error: Color.red.opacity(0.8)
            }
        }

        var glassTint: Color {
            switch self {
            case .creating: Color.orange.opacity(0.15)
            case .ready: Color.green.opacity(0.15)
            case .running: Color.blue.opacity(0.15)
            case .paused: Color.gray.opacity(0.15)
            case .error: Color.red.opacity(0.15)
            }
        }
    }

    // MARK: - Spacing

    enum Spacing {
        static let xxs: CGFloat = 4
        static let xs: CGFloat = 8
        static let sm: CGFloat = 12
        static let md: CGFloat = 16
        static let lg: CGFloat = 24
        static let xl: CGFloat = 32
        static let xxl: CGFloat = 48
    }

    // MARK: - Corner Radius

    enum Radius {
        static let sm: CGFloat = 8
        static let md: CGFloat = 12
        static let lg: CGFloat = 16
        static let xl: CGFloat = 24
        static let pill: CGFloat = 9999
    }

    // MARK: - Shadows

    enum Shadow {
        static let soft = ShadowStyle(
            color: .black.opacity(0.08),
            radius: 8,
            x: 0,
            y: 4
        )

        static let medium = ShadowStyle(
            color: .black.opacity(0.12),
            radius: 12,
            x: 0,
            y: 6
        )

        static let warm = ShadowStyle(
            color: Colors.warmApricot.opacity(0.2),
            radius: 16,
            x: 0,
            y: 8
        )
    }

    struct ShadowStyle {
        let color: Color
        let radius: CGFloat
        let x: CGFloat
        let y: CGFloat
    }
}

// MARK: - Typography

extension Font {
    static let netclodeTitle = Font.system(.largeTitle, design: .rounded, weight: .bold)
    static let netclodeHeadline = Font.system(.headline, design: .rounded, weight: .semibold)
    static let netclodeSubheadline = Font.system(.subheadline, design: .rounded, weight: .medium)
    static let netclodeBody = Font.system(.body, design: .rounded)
    static let netclodeCaption = Font.system(.caption, design: .rounded)
    static let netclodeMonospaced = Font.system(.body, design: .monospaced)
    static let netclodeMonospacedSmall = Font.system(.caption, design: .monospaced)
}
