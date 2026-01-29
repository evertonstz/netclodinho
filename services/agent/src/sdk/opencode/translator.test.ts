/**
 * Tests for OpenCode event translator
 */

import { describe, it, expect, beforeEach } from "vitest";
import {
  createTranslatorState,
  resetTranslatorState,
  translateEvent,
  translateMessagePartUpdated,
  translateMessageUpdated,
  translateSessionIdle,
  type TranslatorState,
} from "./translator.js";

describe("OpenCode Translator", () => {
  let state: TranslatorState;

  beforeEach(() => {
    state = createTranslatorState();
  });

  describe("createTranslatorState", () => {
    it("creates empty initial state", () => {
      expect(state.assistantMessageIds.size).toBe(0);
      expect(state.toolStartTimes.size).toBe(0);
      expect(state.toolStartEmitted.size).toBe(0);
      expect(state.currentTextPartId).toBeNull();
      expect(state.currentTextMessageId).toBeNull();
      expect(state.textMessageIdCounter).toBe(0);
      expect(state.lastUsage).toBeNull();
    });
  });

  describe("resetTranslatorState", () => {
    it("clears all state", () => {
      state.assistantMessageIds.add("msg_1");
      state.toolStartTimes.set("tool_1", 1000);
      state.toolStartEmitted.add("tool_1");
      state.currentTextPartId = "part_1";
      state.currentTextMessageId = "msg_1";
      state.lastUsage = { inputTokens: 100, outputTokens: 50 };

      resetTranslatorState(state);

      expect(state.assistantMessageIds.size).toBe(0);
      expect(state.toolStartTimes.size).toBe(0);
      expect(state.toolStartEmitted.size).toBe(0);
      expect(state.currentTextPartId).toBeNull();
      expect(state.currentTextMessageId).toBeNull();
      expect(state.lastUsage).toBeNull();
    });

    it("preserves textMessageIdCounter", () => {
      state.textMessageIdCounter = 5;
      resetTranslatorState(state);
      expect(state.textMessageIdCounter).toBe(5);
    });
  });

  describe("translateMessagePartUpdated - text", () => {
    it("translates text delta", () => {
      const result = translateMessagePartUpdated(
        { type: "text", id: "part_1" },
        "Hello ",
        state
      );
      expect(result?.type).toBe("textDelta");
      expect((result as { content: string }).content).toBe("Hello ");
      expect((result as { partial: boolean }).partial).toBe(true);
    });

    it("generates message ID for text part", () => {
      const result = translateMessagePartUpdated(
        { type: "text", id: "part_1" },
        "Hello",
        state
      );
      expect((result as { messageId: string }).messageId).toMatch(/^msg_\d+_\d+$/);
    });

    it("reuses message ID for same part", () => {
      const result1 = translateMessagePartUpdated(
        { type: "text", id: "part_1" },
        "Hello",
        state
      );
      const result2 = translateMessagePartUpdated(
        { type: "text", id: "part_1" },
        " world",
        state
      );
      expect((result1 as { messageId: string }).messageId).toBe(
        (result2 as { messageId: string }).messageId
      );
    });

    it("generates new message ID for different part", () => {
      const result1 = translateMessagePartUpdated(
        { type: "text", id: "part_1" },
        "Hello",
        state
      );
      const result2 = translateMessagePartUpdated(
        { type: "text", id: "part_2" },
        "world",
        state
      );
      expect((result1 as { messageId: string }).messageId).not.toBe(
        (result2 as { messageId: string }).messageId
      );
    });

    it("returns null for text without delta", () => {
      const result = translateMessagePartUpdated(
        { type: "text", id: "part_1", text: "accumulated" },
        undefined,
        state
      );
      expect(result).toBeNull();
    });

    it("filters non-assistant messages", () => {
      const result = translateMessagePartUpdated(
        { type: "text", id: "part_1", messageID: "user_msg" },
        "Hello",
        state
      );
      expect(result).toBeNull();
    });

    it("allows assistant messages", () => {
      state.assistantMessageIds.add("assistant_msg");
      const result = translateMessagePartUpdated(
        { type: "text", id: "part_1", messageID: "assistant_msg" },
        "Hello",
        state
      );
      expect(result?.type).toBe("textDelta");
    });
  });

  describe("translateMessagePartUpdated - reasoning", () => {
    it("translates reasoning delta", () => {
      const result = translateMessagePartUpdated(
        { type: "reasoning", id: "reason_1" },
        "Thinking...",
        state
      );
      expect(result).toEqual({
        type: "thinking",
        thinkingId: "reason_1",
        content: "Thinking...",
        partial: true,
      });
    });

    it("translates reasoning completion", () => {
      const result = translateMessagePartUpdated(
        { type: "reasoning", id: "reason_1", text: "Full reasoning" },
        undefined,
        state
      );
      expect(result).toEqual({
        type: "thinking",
        thinkingId: "reason_1",
        content: "Full reasoning",
        partial: false,
      });
    });

    it("generates thinking ID if not provided", () => {
      const result = translateMessagePartUpdated(
        { type: "reasoning" },
        "thinking",
        state
      );
      expect((result as { thinkingId: string }).thinkingId).toMatch(/^thinking_\d+$/);
    });
  });

  describe("translateMessagePartUpdated - tool", () => {
    it("ignores pending status", () => {
      const result = translateMessagePartUpdated(
        {
          type: "tool",
          tool: "read",
          callID: "call_1",
          state: { status: "pending" },
        },
        undefined,
        state
      );
      expect(result).toBeNull();
      expect(state.toolStartTimes.has("call_1")).toBe(true);
    });

    it("emits toolStart on running status", () => {
      const result = translateMessagePartUpdated(
        {
          type: "tool",
          tool: "read",
          callID: "call_1",
          state: { status: "running", input: { file_path: "/test.ts" } },
        },
        undefined,
        state
      );
      expect(result).toEqual({
        type: "toolStart",
        tool: "Read",
        toolUseId: "call_1",
        input: { file_path: "/test.ts" },
      });
    });

    it("normalizes tool names", () => {
      const result = translateMessagePartUpdated(
        {
          type: "tool",
          tool: "webfetch",
          callID: "call_1",
          state: { status: "running" },
        },
        undefined,
        state
      );
      expect((result as { tool: string }).tool).toBe("WebFetch");
    });

    it("emits toolEnd on completed status", () => {
      // First emit toolStart
      translateMessagePartUpdated(
        {
          type: "tool",
          tool: "read",
          callID: "call_1",
          state: { status: "running" },
        },
        undefined,
        state
      );

      // Then complete
      const result = translateMessagePartUpdated(
        {
          type: "tool",
          tool: "read",
          callID: "call_1",
          state: { status: "completed", output: "file contents" },
        },
        undefined,
        state
      );
      expect(result?.type).toBe("toolEnd");
      expect((result as { result: string }).result).toBe("file contents");
    });

    it("emits toolEnd with error on error status", () => {
      // First emit toolStart
      translateMessagePartUpdated(
        {
          type: "tool",
          tool: "read",
          callID: "call_1",
          state: { status: "running" },
        },
        undefined,
        state
      );

      // Then error
      const result = translateMessagePartUpdated(
        {
          type: "tool",
          tool: "read",
          callID: "call_1",
          state: { status: "error", error: "File not found" },
        },
        undefined,
        state
      );
      expect(result?.type).toBe("toolEnd");
      expect((result as { error: string }).error).toBe("File not found");
    });

    it("emits toolStart for fast-completing tools", () => {
      // Tool goes directly to completed without running status
      const result = translateMessagePartUpdated(
        {
          type: "tool",
          tool: "read",
          callID: "call_1",
          state: { status: "completed", output: "result" },
        },
        undefined,
        state
      );
      // Should emit toolStart first
      expect(result?.type).toBe("toolStart");
    });

    it("parses raw JSON input", () => {
      const result = translateMessagePartUpdated(
        {
          type: "tool",
          tool: "read",
          callID: "call_1",
          state: { status: "running", raw: '{"filePath":"/test.ts"}' },
        },
        undefined,
        state
      );
      expect((result as { input: Record<string, unknown> }).input).toEqual({
        file_path: "/test.ts",
      });
    });
  });

  describe("translateMessageUpdated", () => {
    it("tracks assistant message IDs", () => {
      translateMessageUpdated({ role: "assistant", id: "msg_1" }, state);
      expect(state.assistantMessageIds.has("msg_1")).toBe(true);
    });

    it("ignores non-assistant messages", () => {
      translateMessageUpdated({ role: "user", id: "msg_1" }, state);
      expect(state.assistantMessageIds.has("msg_1")).toBe(false);
    });

    it("accumulates usage tokens", () => {
      translateMessageUpdated(
        {
          role: "assistant",
          time: { completed: true },
          tokens: { input: 100, output: 50 },
        },
        state
      );
      expect(state.lastUsage).toEqual({ inputTokens: 100, outputTokens: 50 });

      translateMessageUpdated(
        {
          role: "assistant",
          time: { completed: true },
          tokens: { input: 200, output: 100 },
        },
        state
      );
      expect(state.lastUsage).toEqual({ inputTokens: 300, outputTokens: 150 });
    });

    it("returns error event for errors", () => {
      const result = translateMessageUpdated(
        {
          error: { data: { message: "Rate limited" } },
        },
        state
      );
      expect(result).toEqual({
        type: "error",
        message: "Rate limited",
        retryable: false,
      });
    });

    it("returns null for normal updates", () => {
      const result = translateMessageUpdated({ role: "assistant" }, state);
      expect(result).toBeNull();
    });
  });

  describe("translateSessionIdle", () => {
    it("returns result with accumulated usage", () => {
      state.lastUsage = { inputTokens: 500, outputTokens: 200 };
      const result = translateSessionIdle(state);
      expect(result).toEqual({
        type: "result",
        inputTokens: 500,
        outputTokens: 200,
        totalTurns: 1,
      });
    });

    it("returns zero usage when no data", () => {
      const result = translateSessionIdle(state);
      expect(result).toEqual({
        type: "result",
        inputTokens: 0,
        outputTokens: 0,
        totalTurns: 1,
      });
    });
  });

  describe("translateEvent", () => {
    it("handles message.part.updated", () => {
      const result = translateEvent(
        {
          type: "message.part.updated",
          properties: { part: { type: "text", id: "p1" }, delta: "Hi" },
        },
        state
      );
      expect(result?.type).toBe("textDelta");
    });

    it("handles message.updated", () => {
      const result = translateEvent(
        {
          type: "message.updated",
          properties: { info: { error: { data: { message: "Error" } } } },
        },
        state
      );
      expect(result?.type).toBe("error");
    });

    it("handles session.idle", () => {
      const result = translateEvent({ type: "session.idle" }, state);
      expect(result?.type).toBe("result");
    });

    it("returns null for unknown events", () => {
      const result = translateEvent({ type: "unknown.event" }, state);
      expect(result).toBeNull();
    });

    it("returns null for missing properties", () => {
      const result = translateEvent({ type: "message.part.updated" }, state);
      expect(result).toBeNull();
    });
  });
});
