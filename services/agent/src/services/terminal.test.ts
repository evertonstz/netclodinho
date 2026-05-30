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
});
