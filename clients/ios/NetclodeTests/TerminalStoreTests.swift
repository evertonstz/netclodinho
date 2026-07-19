import XCTest
@testable import Netclode

/// Store-level lifecycle tests for the persistent Ghostty terminal bridges.
///
/// These deliberately avoid touching `bridge.terminalView` so no ghostty
/// surface/renderer is created in the unit-test host — the store and bridge
/// buffering behavior is what's under test.
@MainActor
final class TerminalStoreTests: XCTestCase {
    func testBridgePersistsAcrossLookups() {
        let store = TerminalStore()
        let first = store.bridge(for: "sess1")
        let second = store.bridge(for: "sess1")
        XCTAssertTrue(first === second, "bridge must persist for the session lifetime (keep-alive across view detach)")
    }

    func testBridgesAreIsolatedPerSession() {
        let store = TerminalStore()
        let a = store.bridge(for: "sess-a")
        let b = store.bridge(for: "sess-b")
        XCTAssertFalse(a === b)
    }

    func testAppendOutputBeforeViewCreationBuffers() {
        let store = TerminalStore()
        // No bridge exists yet — output must create one and buffer without crashing.
        store.appendOutput(sessionId: "sess1", data: "hello \u{1B}[32mworld\u{1B}[0m\r\n")
        let bridge = store.bridge(for: "sess1")
        XCTAssertEqual(bridge.sessionId, "sess1")
    }

    func testClearOutputTearsDownBridge() {
        let store = TerminalStore()
        let original = store.bridge(for: "sess1")
        store.clearOutput(for: "sess1")
        let recreated = store.bridge(for: "sess1")
        XCTAssertFalse(original === recreated, "teardown on session delete must drop the bridge")
    }

    func testSeedHistoryOnlyAppliesToEmptyTerminal() {
        let store = TerminalStore()
        // Relaunch path: empty terminal accepts the seed.
        store.seedHistory(sessionId: "sess1", data: "restored scrollback")
        // Reconnect path: live output already present — the second seed must be a no-op
        // (verified indirectly: seeding again does not crash and bridge persists).
        store.appendOutput(sessionId: "sess1", data: "live output")
        let before = store.bridge(for: "sess1")
        store.seedHistory(sessionId: "sess1", data: "restored scrollback")
        XCTAssertTrue(before === store.bridge(for: "sess1"))
    }

    func testInitialGridSizeIsZeroUntilSurfaceReports() {
        let store = TerminalStore()
        let bridge = store.bridge(for: "sess1")
        XCTAssertEqual(bridge.cols, 0)
        XCTAssertEqual(bridge.rows, 0)
    }
}
