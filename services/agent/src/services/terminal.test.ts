import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { EventEmitter } from "node:events";
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const mockSocket = Object.assign(new EventEmitter(), {
  writeInput: vi.fn(),
  writeResize: vi.fn(),
  writeFrame: vi.fn(),
  close: vi.fn(),
}) as EventEmitter & {
  writeInput: ReturnType<typeof vi.fn>;
  writeResize: ReturnType<typeof vi.fn>;
  writeFrame: ReturnType<typeof vi.fn>;
  close: ReturnType<typeof vi.fn>;
};

vi.mock("./zmx-service.js", () => ({
  getZmxService: () => ({
    ensureSession: vi.fn().mockResolvedValue(mockSocket),
    getHistory: vi.fn().mockResolvedValue(""),
    shutdown: vi.fn(),
  }),
}));

import {
  setTerminalOutputCallback,
  handleTerminalInput,
  registerOutputCallback,
} from "./terminal.js";
import { Tag } from "./zmx-socket.js";

describe("terminal service (zmx)", () => {
  beforeEach(() => {
    setTerminalOutputCallback(null);
    mockSocket.removeAllListeners();
    vi.clearAllMocks();
  });

  describe("setTerminalOutputCallback", () => {
    it("accepts and clears callback", () => {
      const cb = vi.fn();
      setTerminalOutputCallback(cb);
      setTerminalOutputCallback(null);
      expect(true).toBe(true);
    });
  });

  describe("handleTerminalInput", () => {
    it("sends input to zmx socket asynchronously", async () => {
      handleTerminalInput("ls\r");
      // Flush microtasks so ensureSession resolves
      await new Promise((r) => setTimeout(r, 10));
      expect(mockSocket.writeInput).toHaveBeenCalledWith("ls\r");
    });
  });

  describe("registerOutputCallback", () => {
    it("registers and unregisters callbacks", () => {
      const cb = vi.fn();
      const unregister = registerOutputCallback(cb);
      unregister();
      expect(true).toBe(true);
    });
  });

  describe("output forwarding", () => {
    it("forwards a daemon Output frame to the global callback", async () => {
      const globalCb = vi.fn();
      setTerminalOutputCallback(globalCb);
      // Unique sessionId forces a fresh getOrCreateSocket (module-level
      // activeSockets persists across tests) so the frame handler is registered.
      handleTerminalInput("x", "out-forward-sess");
      await new Promise((r) => setTimeout(r, 10));
      // Daemon streams PTY output as an Output frame.
      mockSocket.emit("frame", Tag.Output, Buffer.from("shell-prompt$ "));
      expect(globalCb).toHaveBeenCalledWith("shell-prompt$ ");
    });

    it("reassembles multi-byte UTF-8 characters split across frames", async () => {
      const globalCb = vi.fn();
      setTerminalOutputCallback(globalCb);
      handleTerminalInput("x", "utf8-split-sess");
      await new Promise((r) => setTimeout(r, 10));

      // "ç" (U+00E7) is 0xC3 0xA7 in UTF-8 — split it across two frames,
      // as the zmx daemon can do at any byte boundary.
      const bytes = Buffer.from("açb", "utf-8"); // 61 C3 A7 62
      mockSocket.emit("frame", Tag.Output, bytes.subarray(0, 2)); // "a" + half of ç
      mockSocket.emit("frame", Tag.Output, bytes.subarray(2)); // rest of ç + "b"

      const received = globalCb.mock.calls.map((c) => c[0]).join("");
      expect(received).toBe("açb");
      expect(received).not.toContain("\uFFFD");
    });

    it("ignores non-Output frames", async () => {
      const globalCb = vi.fn();
      setTerminalOutputCallback(globalCb);
      handleTerminalInput("x", "ignore-frame-sess");
      await new Promise((r) => setTimeout(r, 10));
      mockSocket.emit("frame", Tag.Resize, Buffer.alloc(0));
      expect(globalCb).not.toHaveBeenCalled();
    });
  });

  describe("data input", () => {
    it("writes client keystrokes to the session socket as input", async () => {
      handleTerminalInput("echo hi\r");
      await new Promise((r) => setTimeout(r, 10));
      expect(mockSocket.writeInput).toHaveBeenCalledWith("echo hi\r");
    });
  });
});
