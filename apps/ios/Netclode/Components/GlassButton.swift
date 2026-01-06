import SwiftUI

struct GlassButton: View {
    let title: String
    let icon: String?
    let tint: Color?
    let isLoading: Bool
    let action: () -> Void

    @Environment(SettingsStore.self) private var settingsStore

    init(
        _ title: String,
        icon: String? = nil,
        tint: Color? = nil,
        isLoading: Bool = false,
        action: @escaping () -> Void
    ) {
        self.title = title
        self.icon = icon
        self.tint = tint
        self.isLoading = isLoading
        self.action = action
    }

    var body: some View {
        Button {
            if settingsStore.hapticFeedbackEnabled {
                HapticFeedback.light()
            }
            action()
        } label: {
            HStack(spacing: Theme.Spacing.xs) {
                if isLoading {
                    ProgressView()
                        .tint(.primary)
                } else if let icon {
                    Image(systemName: icon)
                }
                Text(title)
            }
            .font(.netclodeSubheadline)
            .foregroundStyle(.primary)
            .padding(.horizontal, Theme.Spacing.md)
            .padding(.vertical, Theme.Spacing.sm)
        }
        .glassEffect(
            .regular.interactive().tint(tint ?? Theme.Colors.buttonTint),
            in: .capsule
        )
        .disabled(isLoading)
    }
}

// MARK: - Icon-Only Glass Button

struct GlassIconButton: View {
    let icon: String
    let tint: Color?
    let size: CGFloat
    let action: () -> Void

    @Environment(SettingsStore.self) private var settingsStore

    init(
        icon: String,
        tint: Color? = nil,
        size: CGFloat = 44,
        action: @escaping () -> Void
    ) {
        self.icon = icon
        self.tint = tint
        self.size = size
        self.action = action
    }

    var body: some View {
        Button {
            if settingsStore.hapticFeedbackEnabled {
                HapticFeedback.light()
            }
            action()
        } label: {
            Image(systemName: icon)
                .font(.system(size: size * 0.4))
                .foregroundStyle(.primary)
                .frame(width: size, height: size)
        }
        .glassEffect(
            .regular.interactive().tint(tint ?? Theme.Colors.buttonTint),
            in: Circle()
        )
    }
}

// MARK: - Floating Action Button

struct FloatingActionButton: View {
    let icon: String
    let tint: Color?
    let action: () -> Void

    @Environment(SettingsStore.self) private var settingsStore

    init(
        icon: String = "plus",
        tint: Color? = nil,
        action: @escaping () -> Void
    ) {
        self.icon = icon
        self.tint = tint
        self.action = action
    }

    var body: some View {
        Button {
            if settingsStore.hapticFeedbackEnabled {
                HapticFeedback.medium()
            }
            action()
        } label: {
            Image(systemName: icon)
                .font(.system(size: 24, weight: .medium))
                .foregroundStyle(.primary)
                .frame(width: 56, height: 56)
        }
        .glassEffect(
            .regular.interactive().tint(tint ?? Theme.Colors.warmApricot.opacity(0.4)),
            in: Circle()
        )
        .shadow(
            color: Theme.Shadow.warm.color,
            radius: Theme.Shadow.warm.radius,
            x: Theme.Shadow.warm.x,
            y: Theme.Shadow.warm.y
        )
    }
}

// MARK: - Preview

#Preview {
    ZStack {
        WarmGradientBackground()

        VStack(spacing: 20) {
            GlassButton("Primary Action", icon: "arrow.right") {
                print("Tapped")
            }

            GlassButton("Loading...", isLoading: true) {
                print("Tapped")
            }

            HStack(spacing: 16) {
                GlassIconButton(icon: "play.fill") {
                    print("Play")
                }

                GlassIconButton(icon: "pause.fill") {
                    print("Pause")
                }

                GlassIconButton(icon: "trash", tint: .red.opacity(0.3)) {
                    print("Delete")
                }
            }

            Spacer()

            HStack {
                Spacer()
                FloatingActionButton(icon: "plus") {
                    print("Add")
                }
            }
            .padding()
        }
        .padding()
    }
    .environment(SettingsStore())
}
