import SwiftUI

struct ChatInputBar: View {
    @Binding var text: String
    let isProcessing: Bool
    var isFocused: FocusState<Bool>.Binding
    let onSend: () -> Void
    let onInterrupt: () -> Void
    
    /// Whether the connection is usable (if false, messages will be queued)
    var isConnected: Bool = true
    /// Whether there's already a queued message (only one allowed)
    var hasQueuedMessage: Bool = false

    private let inputHeight: CGFloat = 44
    private let maxHeight: CGFloat = 100

    private var canSend: Bool {
        let hasText = !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        // Can't send if offline and already have a queued message
        if !isConnected && hasQueuedMessage {
            return false
        }
        return hasText
    }
    
    /// Whether the message will be queued (not sent immediately)
    private var willQueue: Bool {
        !isConnected
    }

    var body: some View {
        HStack(alignment: .bottom, spacing: Theme.Spacing.xs) {
            // Text input
            ZStack(alignment: .leading) {
                if text.isEmpty {
                    Text(willQueue ? "Reply (queue message)..." : "Reply...")
                        .foregroundStyle(.secondary)
                        .padding(.leading, 5)
                        .allowsHitTesting(false)
                }
                
                TextEditor(text: $text)
                    .focused(isFocused)
                    .scrollContentBackground(.hidden)
                    .tint(Theme.Colors.brand)
                    .frame(minHeight: 28, maxHeight: maxHeight)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .font(.netclodeBody)
            .padding(.horizontal, Theme.Spacing.md)
            .frame(minHeight: inputHeight)
            .adaptiveGlassInteractive(in: Capsule())

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
                            .adaptiveGlassInteractive(tint: Theme.Colors.error, in: Circle())
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
                            .adaptiveGlassInteractive(
                                tint: canSend ? (willQueue ? Color.orange : Theme.Colors.brand) : nil,
                                in: Circle()
                            )
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
                .adaptiveGlass(tint: Theme.Colors.brand, in: Circle())

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
            .adaptiveGlass(in: RoundedRectangle(cornerRadius: Theme.Radius.lg))

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
            isFocused: FocusState<Bool>().projectedValue,
            onSend: {},
            onInterrupt: {}
        )
    }
    .background(Theme.Colors.background)
}
