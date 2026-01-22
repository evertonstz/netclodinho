import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

// Mock node-pty before importing terminal module
vi.mock("node-pty", () => ({
  default: {
    spawn: vi.fn(() => ({
      onData: vi.fn(),
      onExit: vi.fn(),
      write: vi.fn(),
      resize: vi.fn(),
      kill: vi.fn(),
    })),
  },
  spawn: vi.fn(() => ({
    onData: vi.fn(),
    onExit: vi.fn(),
    write: vi.fn(),
    resize: vi.fn(),
    kill: vi.fn(),
  })),
}));

import {
  setTerminalOutputCallback,
  handleTerminalInput,
  registerOutputCallback,
} from "./terminal.js";

describe("terminal service", () => {
  beforeEach(() => {
    // Reset terminal output callback
    setTerminalOutputCallback(null);
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  describe("setTerminalOutputCallback", () => {
    it("accepts a callback function", () => {
      const callback = vi.fn();
      expect(() => setTerminalOutputCallback(callback)).not.toThrow();
    });

    it("accepts null to clear callback", () => {
      const callback = vi.fn();
      setTerminalOutputCallback(callback);
      expect(() => setTerminalOutputCallback(null)).not.toThrow();
    });
  });

  describe("handleTerminalInput", () => {
    it("does not throw for valid input", () => {
      expect(() => handleTerminalInput("ls -la\n")).not.toThrow();
    });

    it("handles empty string input", () => {
      expect(() => handleTerminalInput("")).not.toThrow();
    });

    it("handles special characters", () => {
      expect(() => handleTerminalInput("\x03")).not.toThrow(); // Ctrl+C
      expect(() => handleTerminalInput("\x04")).not.toThrow(); // Ctrl+D
      expect(() => handleTerminalInput("\t")).not.toThrow(); // Tab
    });
  });

  describe("registerOutputCallback", () => {
    it("returns an unregister function", () => {
      const callback = vi.fn();
      const unregister = registerOutputCallback(callback);
      expect(typeof unregister).toBe("function");
    });

    it("unregister function does not throw", () => {
      const callback = vi.fn();
      const unregister = registerOutputCallback(callback);
      expect(() => unregister()).not.toThrow();
    });

    it("can register multiple callbacks", () => {
      const callback1 = vi.fn();
      const callback2 = vi.fn();
      const unregister1 = registerOutputCallback(callback1);
      const unregister2 = registerOutputCallback(callback2);
      expect(typeof unregister1).toBe("function");
      expect(typeof unregister2).toBe("function");
    });
  });
});
