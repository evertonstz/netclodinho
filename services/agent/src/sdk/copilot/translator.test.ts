/**
 * Tests for Copilot event translator
 */

import { describe, it, expect, beforeEach } from "vitest";
import {
  createTranslatorState,
  resetTranslatorState,
  translateEvent,
  translateMessageDelta,
  translateMessage,
  translateReasoningDelta,
  translateReasoning,
  translateToolStart,
  translateToolComplete,
  translateUsage,
  translateSessionError,
  translateSessionIdle,
  type TranslatorState,
} from "./translator.js";

describe("Copilot Translator", () => {
  let state: TranslatorState;

  beforeEach(() => {
    state = createTranslatorState();
  });

  describe("createTranslatorState", () => {
    it("creates empty initial state", () => {
      expect(state.toolNameMap.size).toBe(0);
      expect(state.toolStartTimes.size).toBe(0);
      expect(state.currentThinkingId).toBeNull();
      expect(state.thinkingIdCounter).toBe(0);
      expect(state.currentTextMessageId).toBeNull();
      expect(state.textMessageIdCounter).toBe(0);
      expect(state.lastUsage).toBeNull();
    });
  });

  describe("resetTranslatorState", () => {
    it("clears all state except counters", () => {
      state.toolNameMap.set("tool_1", "Read");
      state.toolStartTimes.set("tool_1", 1000);
      state.currentThinkingId = "thinking_1";
      state.thinkingIdCounter = 5;
      state.currentTextMessageId = "msg_1";
      state.textMessageIdCounter = 10;
      state.lastUsage = { inputTokens: 100, outputTokens: 50 };

      resetTranslatorState(state);

      expect(state.toolNameMap.size).toBe(0);
      expect(state.toolStartTimes.size).toBe(0);
      expect(state.currentThinkingId).toBeNull();
      expect(state.currentTextMessageId).toBeNull();
      expect(state.lastUsage).toBeNull();
      // Counters are preserved
      expect(state.thinkingIdCounter).toBe(5);
      expect(state.textMessageIdCounter).toBe(10);
    });
  });

  describe("translateMessageDelta", () => {
    it("translates delta content", () => {
      const result = translateMessageDelta({ deltaContent: "Hello" }, state);
      expect(result?.type).toBe("textDelta");
      expect((result as { content: string }).content).toBe("Hello");
      expect((result as { partial: boolean }).partial).toBe(true);
    });

    it("generates message ID", () => {
      const result = translateMessageDelta({ deltaContent: "Hi" }, state);
      expect((result as { messageId: string }).messageId).toMatch(/^msg_\d+_1$/);
    });

    it("reuses message ID for subsequent deltas", () => {
      const result1 = translateMessageDelta({ deltaContent: "Hello" }, state);
      const result2 = translateMessageDelta({ deltaContent: " world" }, state);
      expect((result1 as { messageId: string }).messageId).toBe(
        (result2 as { messageId: string }).messageId
      );
    });

    it("falls back to content field", () => {
      const result = translateMessageDelta({ content: "fallback" }, state);
      expect((result as { content: string }).content).toBe("fallback");
    });

    it("returns null for empty content", () => {
      expect(translateMessageDelta({}, state)).toBeNull();
      expect(translateMessageDelta({ deltaContent: "" }, state)).toBeNull();
    });
  });

  describe("translateMessage", () => {
    it("translates final message", () => {
      const result = translateMessage({ content: "Final response" }, state);
      expect(result).toEqual({
        type: "textDelta",
        content: "Final response",
        partial: false,
        messageId: expect.stringMatching(/^msg_\d+_1$/),
      });
    });

    it("uses existing message ID if available", () => {
      state.currentTextMessageId = "existing_msg";
      const result = translateMessage({ content: "test" }, state);
      expect((result as { messageId: string }).messageId).toBe("existing_msg");
    });

    it("resets message ID after translation", () => {
      state.currentTextMessageId = "msg_1";
      translateMessage({ content: "test" }, state);
      expect(state.currentTextMessageId).toBeNull();
    });

    it("handles empty content", () => {
      const result = translateMessage({}, state);
      expect((result as { content: string }).content).toBe("");
    });
  });

  describe("translateReasoningDelta", () => {
    it("translates reasoning delta", () => {
      const result = translateReasoningDelta({ deltaContent: "Thinking..." }, state);
      expect(result?.type).toBe("thinking");
      expect((result as { content: string }).content).toBe("Thinking...");
      expect((result as { partial: boolean }).partial).toBe(true);
    });

    it("generates thinking ID", () => {
      const result = translateReasoningDelta({ deltaContent: "hmm" }, state);
      expect((result as { thinkingId: string }).thinkingId).toMatch(/^thinking_\d+_1$/);
    });

    it("reuses thinking ID for subsequent deltas", () => {
      const result1 = translateReasoningDelta({ deltaContent: "First" }, state);
      const result2 = translateReasoningDelta({ deltaContent: "Second" }, state);
      expect((result1 as { thinkingId: string }).thinkingId).toBe(
        (result2 as { thinkingId: string }).thinkingId
      );
    });

    it("returns null for empty content", () => {
      expect(translateReasoningDelta({}, state)).toBeNull();
      expect(translateReasoningDelta({ deltaContent: "" }, state)).toBeNull();
    });
  });

  describe("translateReasoning", () => {
    it("marks reasoning as complete", () => {
      state.currentThinkingId = "thinking_1";
      const result = translateReasoning(state);
      expect(result).toEqual({
        type: "thinking",
        thinkingId: "thinking_1",
        content: "",
        partial: false,
      });
    });

    it("resets thinking ID", () => {
      state.currentThinkingId = "thinking_1";
      translateReasoning(state);
      expect(state.currentThinkingId).toBeNull();
    });

    it("resets text message ID", () => {
      state.currentTextMessageId = "msg_1";
      translateReasoning(state);
      expect(state.currentTextMessageId).toBeNull();
    });

    it("generates thinking ID if none exists", () => {
      const result = translateReasoning(state);
      expect((result as { thinkingId: string }).thinkingId).toMatch(/^thinking_\d+_1$/);
    });
  });

  describe("translateToolStart", () => {
    it("translates tool start", () => {
      const result = translateToolStart(
        { toolName: "Read", toolCallId: "call_1", arguments: { path: "/file.ts" } },
        undefined,
        state
      );
      expect(result).toEqual({
        type: "toolStart",
        tool: "Read",
        toolUseId: "call_1",
        input: { path: "/file.ts" },
      });
    });

    it("tracks tool name", () => {
      translateToolStart({ toolName: "Bash", toolCallId: "call_1" }, undefined, state);
      expect(state.toolNameMap.get("call_1")).toBe("Bash");
    });

    it("tracks start time", () => {
      const before = Date.now();
      translateToolStart({ toolCallId: "call_1" }, undefined, state);
      const after = Date.now();
      const startTime = state.toolStartTimes.get("call_1");
      expect(startTime).toBeGreaterThanOrEqual(before);
      expect(startTime).toBeLessThanOrEqual(after);
    });

    it("uses event ID as fallback", () => {
      const result = translateToolStart({ toolName: "Read" }, "event_id", state);
      expect((result as { toolUseId: string }).toolUseId).toBe("event_id");
    });

    it("defaults to unknown tool name", () => {
      const result = translateToolStart({}, undefined, state);
      expect((result as { tool: string }).tool).toBe("unknown");
    });
  });

  describe("translateToolComplete", () => {
    it("translates successful completion", () => {
      state.toolNameMap.set("call_1", "Read");
      state.toolStartTimes.set("call_1", Date.now() - 100);

      const result = translateToolComplete(
        { toolCallId: "call_1", result: "file contents", resultType: "success" },
        undefined,
        state
      );

      expect(result.type).toBe("toolEnd");
      expect((result as { tool: string }).tool).toBe("Read");
      expect((result as { result: string }).result).toBe("file contents");
      expect((result as { error: undefined }).error).toBeUndefined();
    });

    it("translates failure", () => {
      state.toolNameMap.set("call_1", "Read");

      const result = translateToolComplete(
        { toolCallId: "call_1", error: "Not found", resultType: "failure" },
        undefined,
        state
      );

      expect((result as { error: string }).error).toBe("Not found");
      expect((result as { result: undefined }).result).toBeUndefined();
    });

    it("handles rejected result type", () => {
      const result = translateToolComplete(
        { toolCallId: "call_1", result: "Permission denied", resultType: "rejected" },
        undefined,
        state
      );
      expect((result as { error: string }).error).toBe("Permission denied");
    });

    it("handles denied result type", () => {
      const result = translateToolComplete(
        { toolCallId: "call_1", error: "Access denied", resultType: "denied" },
        undefined,
        state
      );
      expect((result as { error: string }).error).toBe("Access denied");
    });

    it("calculates duration", () => {
      state.toolStartTimes.set("call_1", Date.now() - 500);
      const result = translateToolComplete({ toolCallId: "call_1" }, undefined, state);
      expect((result as { durationMs: number }).durationMs).toBeGreaterThanOrEqual(500);
    });

    it("cleans up tracking state", () => {
      state.toolNameMap.set("call_1", "Read");
      state.toolStartTimes.set("call_1", 1000);
      translateToolComplete({ toolCallId: "call_1" }, undefined, state);
      expect(state.toolNameMap.has("call_1")).toBe(false);
      expect(state.toolStartTimes.has("call_1")).toBe(false);
    });

    it("resets text message ID", () => {
      state.currentTextMessageId = "msg_1";
      translateToolComplete({ toolCallId: "call_1" }, undefined, state);
      expect(state.currentTextMessageId).toBeNull();
    });
  });

  describe("translateUsage", () => {
    it("stores usage data", () => {
      translateUsage({ inputTokens: 100, outputTokens: 50 }, state);
      expect(state.lastUsage).toEqual({ inputTokens: 100, outputTokens: 50 });
    });

    it("returns null", () => {
      const result = translateUsage({ inputTokens: 100 }, state);
      expect(result).toBeNull();
    });

    it("handles missing values", () => {
      translateUsage({}, state);
      expect(state.lastUsage).toEqual({ inputTokens: 0, outputTokens: 0 });
    });
  });

  describe("translateSessionError", () => {
    it("translates error with message", () => {
      const result = translateSessionError({ message: "Connection lost" });
      expect(result).toEqual({
        type: "error",
        message: "Connection lost",
        retryable: false,
      });
    });

    it("falls back to errorType", () => {
      const result = translateSessionError({ errorType: "timeout" });
      expect((result as { message: string }).message).toBe("timeout");
    });

    it("handles empty data", () => {
      const result = translateSessionError({});
      expect((result as { message: string }).message).toBe("Unknown session error");
    });
  });

  describe("translateSessionIdle", () => {
    it("returns result with usage", () => {
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
    it("handles assistant.message_delta", () => {
      const result = translateEvent(
        { type: "assistant.message_delta", data: { deltaContent: "Hi" } },
        state
      );
      expect(result?.type).toBe("textDelta");
    });

    it("handles assistant.message", () => {
      const result = translateEvent(
        { type: "assistant.message", data: { content: "Done" } },
        state
      );
      expect((result as { partial: boolean }).partial).toBe(false);
    });

    it("handles assistant.reasoning_delta", () => {
      const result = translateEvent(
        { type: "assistant.reasoning_delta", data: { deltaContent: "Thinking" } },
        state
      );
      expect(result?.type).toBe("thinking");
    });

    it("handles assistant.reasoning", () => {
      state.currentThinkingId = "t1";
      const result = translateEvent({ type: "assistant.reasoning" }, state);
      expect((result as { partial: boolean }).partial).toBe(false);
    });

    it("handles tool.execution_start", () => {
      const result = translateEvent(
        { type: "tool.execution_start", id: "e1", data: { toolName: "Read" } },
        state
      );
      expect(result?.type).toBe("toolStart");
    });

    it("handles tool.execution_complete", () => {
      state.toolNameMap.set("call_1", "Read");
      const result = translateEvent(
        { type: "tool.execution_complete", data: { toolCallId: "call_1" } },
        state
      );
      expect(result?.type).toBe("toolEnd");
    });

    it("handles assistant.usage", () => {
      const result = translateEvent(
        { type: "assistant.usage", data: { inputTokens: 100 } },
        state
      );
      expect(result).toBeNull();
      expect(state.lastUsage?.inputTokens).toBe(100);
    });

    it("handles session.error", () => {
      const result = translateEvent(
        { type: "session.error", data: { message: "Error" } },
        state
      );
      expect(result?.type).toBe("error");
    });

    it("returns null for unknown events", () => {
      const result = translateEvent({ type: "unknown.event" }, state);
      expect(result).toBeNull();
    });
  });
});
