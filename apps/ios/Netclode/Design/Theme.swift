import SwiftUI

enum Theme {
    // MARK: - Adaptive Color Palette

    enum Colors {
        // Primary backgrounds
        static let background = Color(.systemBackground)
        static let secondaryBackground = Color(.secondarySystemBackground)
        static let tertiaryBackground = Color(.tertiarySystemBackground)

        // Primary text
        static let primaryText = Color(.label)
        static let secondaryText = Color(.secondaryLabel)

        // Brand colors (consistent across themes)
        static let brand = Color(red: 0.6, green: 0.5, blue: 0.7) // Cozy purple
        static let brandLight = Color(red: 0.7, green: 0.6, blue: 0.8) // Cozy lavender

        // Message bubbles - adaptive
        static let userBubble = Color(light: Color(red: 0.2, green: 0.4, blue: 0.6),
                                      dark: Color(red: 0.25, green: 0.45, blue: 0.65))
        static let userBubbleText = Color.white

        static let assistantBubble = Color(light: Color(red: 0.95, green: 0.93, blue: 0.90),
                                           dark: Color(red: 0.22, green: 0.22, blue: 0.24))
        static let assistantBubbleText = Color(light: Color(red: 0.15, green: 0.15, blue: 0.15),
                                               dark: Color(red: 0.95, green: 0.95, blue: 0.95))

        // Status colors
        static let success = Color.green
        static let warning = Color.orange
        static let error = Color.red
        static let info = Color.blue

        // Accent colors
        static let accent = brand

        // Code blocks
        static let codeBackground = Color(light: Color(red: 0.95, green: 0.95, blue: 0.97),
                                          dark: Color(red: 0.12, green: 0.12, blue: 0.14))
        static let codeText = Color(light: Color(red: 0.2, green: 0.2, blue: 0.25),
                                    dark: Color(red: 0.9, green: 0.9, blue: 0.92))

        // Glass tints
        static let glassTint = Color(light: Color.white.opacity(0.6),
                                     dark: Color.white.opacity(0.1))

        // Input field
        static let inputBackground = Color(light: Color(red: 0.96, green: 0.96, blue: 0.97),
                                           dark: Color(red: 0.15, green: 0.15, blue: 0.17))
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
            case .creating: Color.orange
            case .ready: Color.green
            case .running: Color.blue
            case .paused: Color.gray
            case .error: Color.red
            }
        }

        var glassTint: Color {
            color.opacity(0.15)
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
    }

    struct ShadowStyle {
        let color: Color
        let radius: CGFloat
        let x: CGFloat
        let y: CGFloat
    }
}

// MARK: - Adaptive Color Extension

extension Color {
    init(light: Color, dark: Color) {
        self.init(uiColor: UIColor { traitCollection in
            switch traitCollection.userInterfaceStyle {
            case .dark:
                return UIColor(dark)
            default:
                return UIColor(light)
            }
        })
    }

    /// Returns a glass-compatible tint version of this color
    var glassTint: Color {
        self.opacity(0.25)
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
