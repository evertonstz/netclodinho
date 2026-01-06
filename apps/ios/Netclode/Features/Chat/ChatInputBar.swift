import SwiftUI

struct ChatInputBar: View {
    @Binding var text: String
    let isProcessing: Bool
    var isFocused: FocusState<Bool>.Binding
    let onSend: () -> Void
    let onInterrupt: () -> Void

    @State private var textEditorHeight: CGFloat = 40

    private let minHeight: CGFloat = 40
    private let maxHeight: CGFloat = 120

    var body: some View {
        HStack(alignment: .bottom, spacing: Theme.Spacing.sm) {
            // Text input
            ZStack(alignment: .leading) {
                if text.isEmpty {
                    Text("Ask Claude anything...")
                        .foregroundStyle(.tertiary)
                        .padding(.horizontal, Theme.Spacing.sm)
                        .padding(.vertical, Theme.Spacing.sm)
                }

                TextEditor(text: $text)
                    .focused(isFocused)
                    .scrollContentBackground(.hidden)
                    .frame(minHeight: minHeight, maxHeight: maxHeight)
                    .fixedSize(horizontal: false, vertical: true)
                    .padding(.horizontal, Theme.Spacing.xs)
                    .padding(.vertical, Theme.Spacing.xxs)
            }
            .font(.netclodeBody)
            .glassEffect(
                .regular.interactive().tint(Theme.Colors.inputTint),
                in: RoundedRectangle(cornerRadius: Theme.Radius.lg)
            )

            // Send/Stop button
            Group {
                if isProcessing {
                    // Stop button
                    Button {
                        onInterrupt()
                    } label: {
                        Image(systemName: "stop.fill")
                            .font(.system(size: 16, weight: .semibold))
                            .foregroundStyle(.white)
                            .frame(width: 44, height: 44)
                            .background(Theme.Colors.warmCoral)
                            .clipShape(Circle())
                    }
                    .transition(.scale.combined(with: .opacity))
                } else {
                    // Send button
                    Button {
                        onSend()
                    } label: {
                        Image(systemName: "arrow.up")
                            .font(.system(size: 16, weight: .semibold))
                            .foregroundStyle(.white)
                            .frame(width: 44, height: 44)
                            .background(
                                text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
                                    ? Theme.Colors.gentleGray
                                    : Theme.Colors.cozyPurple
                            )
                            .clipShape(Circle())
                    }
                    .disabled(text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                    .transition(.scale.combined(with: .opacity))
                }
            }
            .animation(.snappy, value: isProcessing)
        }
        .padding(.horizontal, Theme.Spacing.md)
        .padding(.vertical, Theme.Spacing.sm)
        .background {
            Rectangle()
                .fill(.ultraThinMaterial)
                .ignoresSafeArea()
        }
    }
}

// MARK: - Streaming Indicator

struct StreamingIndicator: View {
    @State private var animatingDot = 0

    var body: some View {
        HStack(alignment: .top, spacing: Theme.Spacing.sm) {
            // Avatar
            ZStack {
                Circle()
                    .fill(Theme.Colors.warmApricot)
                    .frame(width: 24, height: 24)

                Image(systemName: "brain.head.profile")
                    .font(.system(size: 12))
                    .foregroundStyle(.white)
            }

            // Typing indicator
            HStack(spacing: 4) {
                ForEach(0..<3, id: \.self) { index in
                    Circle()
                        .fill(Theme.Colors.cozyPurple)
                        .frame(width: 8, height: 8)
                        .offset(y: animatingDot == index ? -4 : 0)
                }
            }
            .padding(.horizontal, Theme.Spacing.md)
            .padding(.vertical, Theme.Spacing.sm)
            .glassEffect(
                .regular.tint(Theme.Colors.assistantMessageTint),
                in: RoundedRectangle(cornerRadius: Theme.Radius.md)
            )

            Spacer()
        }
        .onAppear {
            startAnimation()
        }
    }

    private func startAnimation() {
        Timer.scheduledTimer(withTimeInterval: 0.3, repeats: true) { _ in
            withAnimation(.bouncy) {
                animatingDot = (animatingDot + 1) % 3
            }
        }
    }
}

// MARK: - Preview

#Preview {
    VStack {
        Spacer()

        ChatInputBar(
            text: .constant(""),
            isProcessing: false,
            isFocused: FocusState<Bool>().projectedValue,
            onSend: {},
            onInterrupt: {}
        )
    }
    .background(WarmGradientBackground())
}

#Preview("Processing") {
    VStack {
        StreamingIndicator()
            .padding()

        Spacer()

        ChatInputBar(
            text: .constant("Hello"),
            isProcessing: true,
            isFocused: FocusState<Bool>().projectedValue,
            onSend: {},
            onInterrupt: {}
        )
    }
    .background(WarmGradientBackground())
}
