import SwiftUI

struct ChatInputBar: View {
    @Binding var text: String
    let isProcessing: Bool
    let sessionStatus: SessionStatus?
    var isFocused: FocusState<Bool>.Binding
    let onSend: () -> Void
    let onInterrupt: () -> Void

    private let inputHeight: CGFloat = 44
    private let maxHeight: CGFloat = 100

    private var canSend: Bool {
        !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && sessionStatus != .error
    }

    var body: some View {
        HStack(alignment: .bottom, spacing: Theme.Spacing.xs) {
            // Text input
            ZStack(alignment: .leading) {
                if text.isEmpty {
                    Text("Reply...")
                        .foregroundStyle(.tertiary)
                        .padding(.horizontal, Theme.Spacing.sm)
                }

                TextEditor(text: $text)
                    .focused(isFocused)
                    .scrollContentBackground(.hidden)
                    .frame(minHeight: inputHeight, maxHeight: maxHeight)
                    .fixedSize(horizontal: false, vertical: true)
                    .padding(.horizontal, Theme.Spacing.xs)
            }
            .font(.netclodeBody)
            .glassEffect(.regular, in: RoundedRectangle(cornerRadius: inputHeight / 2))

            // Send/Stop button
            Group {
                if isProcessing {
                    // Stop button
                    Button {
                        onInterrupt()
                    } label: {
                        Image(systemName: "stop.fill")
                            .font(.system(size: TypeScale.body, weight: .semibold))
                            .foregroundStyle(.white)
                            .frame(width: inputHeight, height: inputHeight)
                            .glassEffect(
                                .regular.interactive().tint(Theme.Colors.error.glassTint),
                                in: Circle()
                            )
                    }
                    .transition(.scale.combined(with: .opacity))
                } else {
                    // Send button
                    Button {
                        onSend()
                    } label: {
                        Image(systemName: "arrow.up")
                            .font(.system(size: TypeScale.body, weight: .semibold))
                            .foregroundStyle(canSend ? .white : .secondary)
                            .frame(width: inputHeight, height: inputHeight)
                            .glassEffect(
                                canSend
                                    ? .regular.interactive().tint(Theme.Colors.brand.glassTint)
                                    : .regular.interactive(),
                                in: Circle()
                            )
                            .overlay(alignment: .topTrailing) {
                                if let status = sessionStatus {
                                    Circle()
                                        .fill(status.tintColor.color)
                                        .frame(width: 10, height: 10)
                                        .overlay(
                                            Circle()
                                                .strokeBorder(.black.opacity(0.2), lineWidth: 1)
                                        )
                                        .offset(x: 2, y: -2)
                                        .pulsing(status == .running)
                                }
                            }
                    }
                    .disabled(!canSend)
                    .transition(.scale.combined(with: .opacity))
                }
            }
            .animation(.snappy, value: isProcessing)
        }
        .padding(.horizontal, Theme.Spacing.sm)
        .padding(.vertical, Theme.Spacing.xs)
    }
}

// MARK: - Streaming Indicator

struct StreamingIndicator: View {
    @State private var animatingDot = 0
    @State private var animationTimer: Timer?

    var body: some View {
        HStack(alignment: .top, spacing: Theme.Spacing.sm) {
            // Avatar
            Image(systemName: "brain.head.profile")
                .font(.system(size: TypeScale.body, weight: .medium))
                .foregroundStyle(.white)
                .frame(width: 28, height: 28)
                .glassEffect(
                    .regular.tint(Theme.Colors.brand.glassTint),
                    in: Circle()
                )

            // Typing indicator
            HStack(spacing: 4) {
                ForEach(0..<3, id: \.self) { index in
                    Circle()
                        .fill(Theme.Colors.brand)
                        .frame(width: 8, height: 8)
                        .offset(y: animatingDot == index ? -4 : 0)
                }
            }
            .padding(.horizontal, Theme.Spacing.md)
            .padding(.vertical, Theme.Spacing.sm)
            .glassEffect(.regular, in: RoundedRectangle(cornerRadius: Theme.Radius.lg))

            Spacer()
        }
        .onAppear {
            startAnimation()
        }
        .onDisappear {
            stopAnimation()
        }
    }

    private func startAnimation() {
        // Invalidate any existing timer first
        animationTimer?.invalidate()
        animationTimer = Timer.scheduledTimer(withTimeInterval: 0.3, repeats: true) { _ in
            MainActor.assumeIsolated {
                withAnimation(.bouncy) {
                    animatingDot = (animatingDot + 1) % 3
                }
            }
        }
    }

    private func stopAnimation() {
        animationTimer?.invalidate()
        animationTimer = nil
    }
}

// MARK: - Preview

#Preview {
    VStack {
        Spacer()

        ChatInputBar(
            text: .constant(""),
            isProcessing: false,
            sessionStatus: .ready,
            isFocused: FocusState<Bool>().projectedValue,
            onSend: {},
            onInterrupt: {}
        )
    }
    .background(Theme.Colors.background)
}

#Preview("Processing") {
    VStack {
        StreamingIndicator()
            .padding()

        Spacer()

        ChatInputBar(
            text: .constant("Hello"),
            isProcessing: true,
            sessionStatus: .running,
            isFocused: FocusState<Bool>().projectedValue,
            onSend: {},
            onInterrupt: {}
        )
    }
    .background(Theme.Colors.background)
}
