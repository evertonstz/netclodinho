import Foundation
import GhosttyTerminal
import UIKit

/// Persistent per-session Ghostty terminal.
///
/// Owns the pieces that must outlive any SwiftUI view:
/// - `InMemoryTerminalSession` — libghostty's host-managed byte-stream
///   backend. Remote PTY output is fed in via `feed(_:)`; user keystrokes and
///   viewport changes come back through the `write`/`resize` callbacks and are
///   forwarded to the control plane as `terminalInput`/`terminalResize`.
/// - `TerminalController` — ghostty app/config/theme lifecycle.
/// - `UITerminalView` — the rendering view, created once and re-parented by
///   SwiftUI on every tab entry. The (forked) wrapper preserves the ghostty
///   surface across window detach/reattach, so terminal state survives all
///   view-hierarchy changes; the capped buffer replay via
///   `terminalDidAttachSurface` only runs for genuinely new surfaces
///   (first open, app relaunch restore).
///
/// This class is the single seam over GhosttyTerminal — no other file in the
/// app imports the wrapper.
@MainActor
final class GhosttyTerminalBridge: NSObject {
    let sessionId: String
    weak var connectService: ConnectService?

    /// Last known grid size, used by views to trigger PTY spawn on (re)entry.
    private(set) var cols: Int = 0
    private(set) var rows: Int = 0

    /// Capped raw-output buffer replayed into freshly (re)built surfaces
    /// (tab re-entry, app relaunch restore).
    private var scrollback: [UInt8] = []
    private let maxScrollbackBytes = 512 * 1024

    /// Replay must never be one giant surface write: the wrapper's teardown
    /// (`clearSurface`) blocks the MAIN thread until the in-flight write
    /// finishes, and its generation counter can only discard *queued* writes.
    /// Small chunks bound that wait and let a mid-replay surface teardown
    /// (SwiftUI re-parenting on session close/reopen) drop the remainder
    /// instead of deadlocking.
    private let replayChunkBytes = 16 * 1024
    private var surfaceAttached = false

    /// One ghostty app/config for every surface, created at app boot
    /// (`prewarm()`), matching vvterm and Ghostty.app: both initialize the
    /// runtime at launch and share a single app object across all terminals.
    /// This moves the one-time runtime cost (ghostty_init, config parse, font
    /// discovery) off the first terminal open, and avoids one ghostty app per
    /// session.
    static let sharedController = TerminalController()

    /// Kick the ghostty runtime initialization at app startup.
    static func prewarm() {
        _ = sharedController
    }

    private var session: InMemoryTerminalSession!
    private var _terminalView: UITerminalView?

    init(sessionId: String, connectService: ConnectService?) {
        self.sessionId = sessionId
        self.connectService = connectService
        super.init()

        session = InMemoryTerminalSession(
            write: { [weak self] data in
                Task { @MainActor in self?.sendInput(data) }
            },
            resize: { [weak self] viewport in
                Task { @MainActor in self?.sendResize(viewport) }
            }
        )
    }

    /// The persistent terminal view. Created lazily on first presentation and
    /// reused for the lifetime of the session.
    var terminalView: UITerminalView {
        if let view = _terminalView {
            return view
        }
        let view = UITerminalView(frame: .zero)
        view.configuration = TerminalSurfaceOptions(backend: .inMemory(session))
        view.controller = Self.sharedController
        view.delegate = self
        _terminalView = view
        return view
    }

    // MARK: - Remote PTY output (agent → terminal)

    func feed(_ bytes: [UInt8]) {
        scrollback.append(contentsOf: bytes)
        if scrollback.count > maxScrollbackBytes {
            scrollback.removeFirst(scrollback.count - maxScrollbackBytes)
        }
        session.receive(Data(bytes))
    }

    /// Seed scrollback from persisted history, only when the terminal has no
    /// content yet (app relaunch). Reconnect refreshes hit the guard and skip.
    func seedIfEmpty(_ bytes: [UInt8]) {
        guard scrollback.isEmpty, !bytes.isEmpty else { return }
        feed(bytes)
    }

    /// Re-send the last known size unconditionally, bypassing the dedup in
    /// `sendResize`. Needed after reconnects: resizes fired while the stream
    /// was down are silently dropped, but `cols`/`rows` were already updated,
    /// so the dedup would suppress an identical re-send and leave the remote
    /// PTY wrapping at a stale width.
    func resyncSize() {
        guard cols > 0, rows > 0 else { return }
        connectService?.send(.terminalResize(
            sessionId: sessionId,
            cols: cols,
            rows: rows
        ))
    }

    // MARK: - Teardown (session delete)

    func teardown() {
        _terminalView?.removeFromSuperview()
        _terminalView = nil
        scrollback.removeAll()
    }

    // MARK: - Terminal → agent

    private func sendInput(_ data: Data) {
        guard !data.isEmpty else { return }
        connectService?.send(.terminalInput(
            sessionId: sessionId,
            data: String(decoding: data, as: UTF8.self)
        ))
    }

    private func sendResize(_ viewport: InMemoryTerminalViewport) {
        let newCols = Int(viewport.columns)
        let newRows = Int(viewport.rows)
        guard newCols > 0, newRows > 0 else { return }
        guard newCols != cols || newRows != rows else { return }
        cols = newCols
        rows = newRows
        connectService?.send(.terminalResize(
            sessionId: sessionId,
            cols: newCols,
            rows: newRows
        ))
    }
}

// MARK: - Surface lifecycle

extension GhosttyTerminalBridge: TerminalSurfaceLifecycleDelegate {
    /// Fires whenever the ghostty surface is (re)built — first presentation,
    /// tab re-entry, window re-attach. Replaying the buffered raw stream
    /// reconstructs the visible screen; ghostty handles reflow on subsequent
    /// resizes.
    ///
    /// The replay is deferred one runloop hop and re-checked against
    /// `surfaceAttached`: SwiftUI re-parenting can attach/detach several times
    /// in quick succession, and replaying into a surface that is about to be
    /// torn down is wasted work at best and a teardown stall at worst.
    func terminalDidAttachSurface(_ surface: TerminalSurface) {
        surfaceAttached = true
        guard !scrollback.isEmpty else { return }
        DispatchQueue.main.async { [weak self] in
            guard let self, surfaceAttached, !scrollback.isEmpty else { return }
            var offset = 0
            let bytes = scrollback
            while offset < bytes.count {
                let end = min(offset + replayChunkBytes, bytes.count)
                session.receive(Data(bytes[offset..<end]))
                offset = end
            }
        }
    }

    func terminalDidDetachSurface() {
        // Surface state is gone; scrollback buffer stays for the next attach.
        surfaceAttached = false
    }
}
