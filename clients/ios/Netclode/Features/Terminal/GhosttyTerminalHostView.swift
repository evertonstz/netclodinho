import GhosttyTerminal
import SwiftUI

/// Presents the store-owned persistent terminal view.
///
/// SwiftUI may create and destroy this representable freely (tab switches,
/// navigation); it only re-parents the bridge's long-lived `UITerminalView`
/// and never owns terminal state.
struct GhosttyTerminalHostView: UIViewRepresentable {
    let bridge: GhosttyTerminalBridge

    func makeUIView(context: Context) -> UITerminalView {
        let view = bridge.terminalView
        DispatchQueue.main.async {
            view.becomeFirstResponder()
        }
        return view
    }

    func updateUIView(_ uiView: UITerminalView, context: Context) {}
}
