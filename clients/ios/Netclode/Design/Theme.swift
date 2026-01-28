#if canImport(UIKit)
import UIKit
#elseif canImport(AppKit)
import AppKit
#endif
import SwiftUI

enum Theme {
    // MARK: - Adaptive Color Palette

    enum Colors {
        // Primary backgrounds
        #if canImport(UIKit)
        static let background = Color(.systemBackground)
        static let secondaryBackground = Color(.secondarySystemBackground)
        static let tertiaryBackground = Color(.tertiarySystemBackground)
        #elseif canImport(AppKit)
        static let background = Color(.windowBackgroundColor)
        static let secondaryBackground = Color(.controlBackgroundColor)
        static let tertiaryBackground = Color(.underPageBackgroundColor)
        #endif

        // Primary text
        #if canImport(UIKit)
        static let primaryText = Color(.label)
        static let secondaryText = Color(.secondaryLabel)
        #elseif canImport(AppKit)
        static let primaryText = Color(.labelColor)
        static let secondaryText = Color(.secondaryLabelColor)
        #endif

        // Brand colors (consistent across themes)
        static let brand = Color(red: 0.6, green: 0.5, blue: 0.7) // Cozy purple
        static let brandLight = Color(red: 0.7, green: 0.6, blue: 0.8) // Cozy lavender

        // Message bubbles - adaptive
        static let userBubble = Color.adaptive(light: Color(red: 0.2, green: 0.4, blue: 0.6),
                                               dark: Color(red: 0.25, green: 0.45, blue: 0.65))
        static let userBubbleText = Color.white

        static let assistantBubble = Color.adaptive(light: Color(red: 0.95, green: 0.93, blue: 0.90),
                                                    dark: Color(red: 0.22, green: 0.22, blue: 0.24))
        static let assistantBubbleText = Color.adaptive(light: Color(red: 0.15, green: 0.15, blue: 0.15),
                                                        dark: Color(red: 0.95, green: 0.95, blue: 0.95))

        // Status colors
        static let success = Color.green
        static let warning = Color.orange
        static let error = Color.red
        static let info = Color.blue

        // Accent colors
        static let accent = brand

        // Code blocks
        static let codeBackground = Color.adaptive(light: Color(red: 0.95, green: 0.95, blue: 0.97),
                                                   dark: Color(red: 0.12, green: 0.12, blue: 0.14))
        static let codeText = Color.adaptive(light: Color(red: 0.2, green: 0.2, blue: 0.25),
                                             dark: Color(red: 0.9, green: 0.9, blue: 0.92))

        // Glass tints
        static let glassTint = Color.adaptive(light: Color.white.opacity(0.6),
                                              dark: Color.white.opacity(0.1))

        // Input field
        static let inputBackground = Color.adaptive(light: Color(red: 0.96, green: 0.96, blue: 0.97),
                                                    dark: Color(red: 0.15, green: 0.15, blue: 0.17))
    }

    // MARK: - Status Colors

    enum StatusColor {
        case creating
        case resuming
        case ready
        case running
        case paused
        case error
        case interrupted

        var color: Color {
            switch self {
            case .creating: Color.orange
            case .resuming: Color.cyan
            case .ready: Color.green
            case .running: Color.blue
            case .paused: Color.gray
            case .error: Color.red
            case .interrupted: Color.orange
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
    /// Creates an adaptive color that changes based on light/dark mode
    static func adaptive(light: Color, dark: Color) -> Color {
        #if canImport(UIKit)
        Color(uiColor: UIColor { traitCollection in
            switch traitCollection.userInterfaceStyle {
            case .dark:
                return UIColor(dark)
            default:
                return UIColor(light)
            }
        })
        #elseif canImport(AppKit)
        Color(nsColor: NSColor(name: nil) { appearance in
            if appearance.bestMatch(from: [.aqua, .darkAqua]) == .darkAqua {
                return NSColor(dark)
            } else {
                return NSColor(light)
            }
        })
        #endif
    }

    /// Returns a glass-compatible tint version of this color
    var glassTint: Color {
        self.opacity(0.5)
    }
}

// MARK: - Typography

enum TypeScale {
    static let body: CGFloat = 15
    static let small: CGFloat = 13
    static let caption: CGFloat = 12
    static let tiny: CGFloat = 11
    static let micro: CGFloat = 10
}

extension Font {
    static let netclodeTitle = Font.system(.largeTitle, design: .rounded, weight: .bold)
    static let netclodeHeadline = Font.system(.headline, design: .rounded, weight: .semibold)
    static let netclodeSubheadline = Font.system(.subheadline, design: .rounded, weight: .medium)
    static let netclodeBody = Font.system(size: TypeScale.body, weight: .regular, design: .rounded)
    static let netclodeCaption = Font.system(size: TypeScale.caption, weight: .regular, design: .rounded)
    static let netclodeSmall = Font.system(size: TypeScale.small, weight: .regular, design: .rounded)
    static let netclodeMonospaced = Font.system(size: TypeScale.body, weight: .regular, design: .monospaced)
    static let netclodeMonospacedSmall = Font.system(size: TypeScale.small, weight: .regular, design: .monospaced)
    static let netclodeMonospacedCaption = Font.system(size: TypeScale.caption, weight: .regular, design: .monospaced)
}
