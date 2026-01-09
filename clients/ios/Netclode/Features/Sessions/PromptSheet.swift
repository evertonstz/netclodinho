import SwiftUI

struct PromptSheet: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(WebSocketService.self) private var webSocketService
    @Environment(SettingsStore.self) private var settingsStore
    @Environment(SessionStore.self) private var sessionStore

    @State private var promptText = ""
    @State private var isSubmitting = false
    @FocusState private var isFocused: Bool

    private var canSubmit: Bool {
        !promptText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && !isSubmitting
    }

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                // Text input area
                TextField(
                    "What do you want to build?",
                    text: $promptText,
                    axis: .vertical
                )
                .font(.netclodeBody)
                .lineLimit(3...12)
                .padding(Theme.Spacing.md)
                .glassEffect(.regular, in: RoundedRectangle(cornerRadius: Theme.Radius.lg))
                .padding(.horizontal, Theme.Spacing.md)
                .padding(.top, Theme.Spacing.md)
                .focused($isFocused)

                Spacer()
            }
            .background(Theme.Colors.background)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button {
                        if settingsStore.hapticFeedbackEnabled {
                            HapticFeedback.light()
                        }
                        dismiss()
                    } label: {
                        Image(systemName: "xmark")
                    }
                    .tint(.red)
                }

                ToolbarItem(placement: .principal) {
                    Text("New Session")
                        .font(.netclodeHeadline)
                }

                ToolbarSpacer(placement: .topBarTrailing)

                ToolbarItem(placement: .confirmationAction) {
                    Button {
                        submitPrompt()
                    } label: {
                        if isSubmitting {
                            ProgressView()
                                .tint(.white)
                        } else {
                            Image(systemName: "paperplane")
                                .symbolVariant(canSubmit ? .fill : .none)
                                .bold()
                        }
                    }
                    .buttonStyle(.glassProminent)
                    .buttonBorderShape(.circle)
                    .tint(Theme.Colors.brand)
                    .disabled(!canSubmit)
                    .keyboardShortcut(.return, modifiers: .command)
                }
            }
            .onAppear {
                isFocused = true
            }
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
        .interactiveDismissDisabled(isSubmitting)
    }

    private func submitPrompt() {
        let text = promptText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return }

        isSubmitting = true

        if settingsStore.hapticFeedbackEnabled {
            HapticFeedback.medium()
        }

        // Store prompt text - will be associated with session when sessionCreated arrives
        sessionStore.pendingPromptText = text
        
        // Create session
        webSocketService.send(.sessionCreate(name: nil, repo: nil, initialPrompt: text))

        dismiss()
    }
}

// MARK: - Preview

#Preview {
    Color.clear
        .sheet(isPresented: .constant(true)) {
            PromptSheet()
                .environment(WebSocketService())
                .environment(SettingsStore())
        }
}
