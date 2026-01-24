/**
 * Tests for SDK factory
 */

import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { parseSdkType } from "./factory.js";

// Note: createSDKAdapter and shutdownAllAdapters are harder to unit test
// as they instantiate real adapters. Integration tests would be more appropriate.

describe("SDK Factory", () => {
  describe("parseSdkType", () => {
    it("returns 'claude' for undefined", () => {
      expect(parseSdkType(undefined)).toBe("claude");
    });

    it("returns 'claude' for empty string", () => {
      expect(parseSdkType("")).toBe("claude");
    });

    it("returns 'claude' for SDK_TYPE_CLAUDE", () => {
      expect(parseSdkType("SDK_TYPE_CLAUDE")).toBe("claude");
    });

    it("returns 'claude' for CLAUDE", () => {
      expect(parseSdkType("CLAUDE")).toBe("claude");
    });

    it("returns 'claude' for claude", () => {
      expect(parseSdkType("claude")).toBe("claude");
    });

    it("returns 'opencode' for SDK_TYPE_OPENCODE", () => {
      expect(parseSdkType("SDK_TYPE_OPENCODE")).toBe("opencode");
    });

    it("returns 'opencode' for OPENCODE", () => {
      expect(parseSdkType("OPENCODE")).toBe("opencode");
    });

    it("returns 'opencode' for opencode", () => {
      expect(parseSdkType("opencode")).toBe("opencode");
    });

    it("returns 'claude' for unknown values (default)", () => {
      expect(parseSdkType("unknown")).toBe("claude");
      expect(parseSdkType("GPT")).toBe("claude");
      expect(parseSdkType("gemini")).toBe("claude");
    });
  });
});
