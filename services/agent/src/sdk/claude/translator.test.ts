/**
 * Tests for Claude Code SDK event translator
 */

import { describe, it, expect, beforeEach } from "vitest";
import {
  createTranslatorState,
  resetTranslatorState,
  generateThinkingId,
  generateMessageId,
  translateTextBlock,
  translateToolUseBlock,
  translateToolResultBlock,
  translateToolBlockStart,
  translateThinkingBlockStart,
  translateTextBlockStart,
  translateTextDelta,
  translatePartialJsonDelta,
  translateThinkingDelta,
  translateThinkingBlockStop,
  translateToolInputBlockStop,
  parseToolInput,
  type TranslatorState,
} from "./translator.js";

describe("Claude Translator", () => {
  let state: TranslatorState;

  beforeEach(() => {
    state = createTranslatorState();
  });

  describe("createTranslatorState", () => {
    it("creates empty initial state", () => {
      expect(state.toolNameMap.size).toBe(0);
      expect(state.toolStartTimes.size).toBe(0);
      expect(state.blockIndexToToolId.size).toBe(0);
      expect(state.blockIndexToToolInput.size).toBe(0);
      expect(state.blockIndexToThinkingId.size).toBe(0);
      expect(state.blockIndexToThinkingContent.size).toBe(0);
      expect(state.streamedThinkingIds.size).toBe(0);
      expect(state.currentParentToolUseId).toBeNull();
      expect(state.currentTextMessageId).toBeNull();
      expect(state.textMessageIdCounter).toBe(0);
      expect(state.thinkingIdCounter).toBe(0);
      expect(state.textWasStreamed).toBe(false);
    });
  });

  describe("resetTranslatorState", () => {
    it("clears streaming state but preserves counters", () => {
      state.blockIndexToToolId.set(0, "tool_1");
      state.blockIndexToToolInput.set(0, '{"key":"value"}');
      state.blockIndexToThinkingId.set("0", "thinking_1");
      state.blockIndexToThinkingContent.set("0", "content");
      state.streamedThinkingIds.add("thinking_1");
      state.currentParentToolUseId = "parent_1";
      state.currentTextMessageId = "msg_1";
      state.textWasStreamed = true;
      state.textMessageIdCounter = 5;
      state.thinkingIdCounter = 3;

      resetTranslatorState(state);

      expect(state.blockIndexToToolId.size).toBe(0);
      expect(state.blockIndexToToolInput.size).toBe(0);
      expect(state.blockIndexToThinkingId.size).toBe(0);
      expect(state.blockIndexToThinkingContent.size).toBe(0);
      expect(state.streamedThinkingIds.size).toBe(0);
      expect(state.currentParentToolUseId).toBeNull();
      expect(state.currentTextMessageId).toBeNull();
      expect(state.textWasStreamed).toBe(false);
      // Counters preserved
      expect(state.textMessageIdCounter).toBe(5);
      expect(state.thinkingIdCounter).toBe(3);
    });
  });

  describe("generateThinkingId", () => {
    it("generates unique IDs with counter", () => {
      const id1 = generateThinkingId(state);
      const id2 = generateThinkingId(state);
      expect(id1).toMatch(/^thinking_\d+_1$/);
      expect(id2).toMatch(/^thinking_\d+_2$/);
    });

    it("increments state counter", () => {
      generateThinkingId(state);
      expect(state.thinkingIdCounter).toBe(1);
    });
  });

  describe("generateMessageId", () => {
    it("generates unique IDs with counter", () => {
      const id1 = generateMessageId(state);
      const id2 = generateMessageId(state);
      expect(id1).toMatch(/^msg_\d+_1$/);
      expect(id2).toMatch(/^msg_\d+_2$/);
    });
  });

  describe("translateTextBlock", () => {
    it("translates text block", () => {
      const result = translateTextBlock({ type: "text", text: "Hello world" }, state);
      expect(result).toEqual({
        type: "textDelta",
        content: "Hello world",
        partial: false,
      });
    });

    it("returns empty content if text was streamed", () => {
      state.textWasStreamed = true;
      const result = translateTextBlock({ type: "text", text: "Hello world" }, state);
      expect((result as { content: string }).content).toBe("");
    });

    it("resets textWasStreamed flag", () => {
      state.textWasStreamed = true;
      translateTextBlock({ type: "text", text: "test" }, state);
      expect(state.textWasStreamed).toBe(false);
    });
  });

  describe("translateToolUseBlock", () => {
    it("translates tool_use block", () => {
      const result = translateToolUseBlock(
        { type: "tool_use", id: "tool_1", name: "Read", input: { path: "/file.ts" } },
        state
      );
      expect(result).toEqual({
        type: "toolStart",
        tool: "Read",
        toolUseId: "tool_1",
        input: { path: "/file.ts" },
      });
    });

    it("tracks tool name and start time", () => {
      translateToolUseBlock(
        { type: "tool_use", id: "tool_1", name: "Read", input: {} },
        state
      );
      expect(state.toolNameMap.get("tool_1")).toBe("Read");
      expect(state.toolStartTimes.has("tool_1")).toBe(true);
    });

    it("returns null for already emitted tool", () => {
      state.toolNameMap.set("tool_1", "Read");
      const result = translateToolUseBlock(
        { type: "tool_use", id: "tool_1", name: "Read", input: {} },
        state
      );
      expect(result).toBeNull();
    });

    it("includes parent tool use ID if set", () => {
      state.currentParentToolUseId = "parent_1";
      const result = translateToolUseBlock(
        { type: "tool_use", id: "tool_1", name: "Read", input: {} },
        state
      );
      expect((result as { parentToolUseId: string }).parentToolUseId).toBe("parent_1");
    });

    it("omits empty input", () => {
      const result = translateToolUseBlock(
        { type: "tool_use", id: "tool_1", name: "Read", input: {} },
        state
      );
      expect(result).not.toHaveProperty("input");
    });
  });

  describe("translateToolResultBlock", () => {
    it("translates successful tool result", () => {
      state.toolNameMap.set("tool_1", "Read");
      state.toolStartTimes.set("tool_1", Date.now() - 100);

      const result = translateToolResultBlock(
        { type: "tool_result", tool_use_id: "tool_1", content: "file contents" },
        state
      );

      expect(result.type).toBe("toolEnd");
      expect((result as { tool: string }).tool).toBe("Read");
      expect((result as { result: string }).result).toBe("file contents");
      expect((result as { error: undefined }).error).toBeUndefined();
    });

    it("translates error result", () => {
      state.toolNameMap.set("tool_1", "Read");

      const result = translateToolResultBlock(
        { type: "tool_result", tool_use_id: "tool_1", content: "File not found", is_error: true },
        state
      );

      expect((result as { error: string }).error).toBe("File not found");
      expect((result as { result: undefined }).result).toBeUndefined();
    });

    it("calculates duration", () => {
      state.toolStartTimes.set("tool_1", Date.now() - 500);
      const result = translateToolResultBlock(
        { type: "tool_result", tool_use_id: "tool_1", content: "ok" },
        state
      );
      expect((result as { durationMs: number }).durationMs).toBeGreaterThanOrEqual(500);
    });

    it("cleans up tracking state", () => {
      state.toolNameMap.set("tool_1", "Read");
      state.toolStartTimes.set("tool_1", 1000);
      translateToolResultBlock(
        { type: "tool_result", tool_use_id: "tool_1", content: "ok" },
        state
      );
      expect(state.toolNameMap.has("tool_1")).toBe(false);
      expect(state.toolStartTimes.has("tool_1")).toBe(false);
    });
  });

  describe("translateToolBlockStart", () => {
    it("creates toolStart event and tracks state", () => {
      const result = translateToolBlockStart(0, "tool_1", "Bash", state);

      expect(result).toEqual({
        type: "toolStart",
        tool: "Bash",
        toolUseId: "tool_1",
      });
      expect(state.blockIndexToToolId.get(0)).toBe("tool_1");
      expect(state.toolNameMap.get("tool_1")).toBe("Bash");
      expect(state.toolStartTimes.has("tool_1")).toBe(true);
    });
  });

  describe("translateThinkingBlockStart", () => {
    it("generates thinking ID and tracks it", () => {
      const thinkingId = translateThinkingBlockStart(0, state);
      expect(thinkingId).toMatch(/^thinking_\d+_1$/);
      expect(state.blockIndexToThinkingId.get("0")).toBe(thinkingId);
    });
  });

  describe("translateTextBlockStart", () => {
    it("generates message ID", () => {
      const messageId = translateTextBlockStart(state);
      expect(messageId).toMatch(/^msg_\d+_1$/);
      expect(state.currentTextMessageId).toBe(messageId);
    });
  });

  describe("translateTextDelta", () => {
    it("translates text delta", () => {
      const result = translateTextDelta("Hello ", state);
      expect(result).toEqual({
        type: "textDelta",
        content: "Hello ",
        partial: true,
      });
      expect(state.textWasStreamed).toBe(true);
    });

    it("includes message ID if set", () => {
      state.currentTextMessageId = "msg_1";
      const result = translateTextDelta("test", state);
      expect((result as { messageId: string }).messageId).toBe("msg_1");
    });
  });

  describe("translatePartialJsonDelta", () => {
    it("translates partial JSON delta", () => {
      state.blockIndexToToolId.set(0, "tool_1");

      const result = translatePartialJsonDelta(0, '{"path":', state);

      expect(result).toEqual({
        type: "toolInput",
        toolUseId: "tool_1",
        inputDelta: '{"path":',
      });
    });

    it("accumulates partial JSON", () => {
      state.blockIndexToToolId.set(0, "tool_1");

      translatePartialJsonDelta(0, '{"path":', state);
      translatePartialJsonDelta(0, '"/file"}', state);

      expect(state.blockIndexToToolInput.get(0)).toBe('{"path":"/file"}');
    });

    it("returns null for unknown tool index", () => {
      const result = translatePartialJsonDelta(999, "{}", state);
      expect(result).toBeNull();
    });

    it("includes parent tool ID if set", () => {
      state.blockIndexToToolId.set(0, "tool_1");
      state.currentParentToolUseId = "parent_1";

      const result = translatePartialJsonDelta(0, "{}", state);
      expect((result as { parentToolUseId: string }).parentToolUseId).toBe("parent_1");
    });
  });

  describe("translateThinkingDelta", () => {
    it("translates thinking delta", () => {
      state.blockIndexToThinkingId.set("0", "thinking_1");

      const result = translateThinkingDelta(0, "Let me think...", state);

      expect(result).toEqual({
        type: "thinking",
        thinkingId: "thinking_1",
        content: "Let me think...",
        partial: true,
      });
    });

    it("accumulates thinking content", () => {
      state.blockIndexToThinkingId.set("0", "thinking_1");

      translateThinkingDelta(0, "First ", state);
      translateThinkingDelta(0, "Second", state);

      expect(state.blockIndexToThinkingContent.get("0")).toBe("First Second");
    });

    it("tracks streamed thinking IDs", () => {
      state.blockIndexToThinkingId.set("0", "thinking_1");
      translateThinkingDelta(0, "test", state);
      expect(state.streamedThinkingIds.has("thinking_1")).toBe(true);
    });

    it("returns null for unknown thinking index", () => {
      const result = translateThinkingDelta(999, "test", state);
      expect(result).toBeNull();
    });
  });

  describe("translateThinkingBlockStop", () => {
    it("translates thinking block completion", () => {
      state.blockIndexToThinkingId.set("0", "thinking_1");
      state.blockIndexToThinkingContent.set("0", "Full thought");
      state.streamedThinkingIds.add("thinking_1");

      const result = translateThinkingBlockStop(0, state);

      expect(result).toEqual({
        type: "thinking",
        thinkingId: "thinking_1",
        content: "Full thought",
        partial: false,
      });
    });

    it("cleans up tracking state", () => {
      state.blockIndexToThinkingId.set("0", "thinking_1");
      state.blockIndexToThinkingContent.set("0", "content");
      state.streamedThinkingIds.add("thinking_1");

      translateThinkingBlockStop(0, state);

      expect(state.blockIndexToThinkingId.has("0")).toBe(false);
      expect(state.blockIndexToThinkingContent.has("0")).toBe(false);
    });

    it("returns null if thinking was not streamed", () => {
      state.blockIndexToThinkingId.set("0", "thinking_1");
      // Not added to streamedThinkingIds
      const result = translateThinkingBlockStop(0, state);
      expect(result).toBeNull();
    });

    it("returns null for unknown index", () => {
      const result = translateThinkingBlockStop(999, state);
      expect(result).toBeNull();
    });
  });

  describe("translateToolInputBlockStop", () => {
    it("translates tool input completion", () => {
      state.blockIndexToToolId.set(0, "tool_1");
      state.blockIndexToToolInput.set(0, '{"path":"/file.ts"}');

      const result = translateToolInputBlockStop(0, state);

      expect(result).toEqual({
        type: "toolInputComplete",
        toolUseId: "tool_1",
        input: { path: "/file.ts" },
      });
    });

    it("handles invalid JSON with fallback", () => {
      state.blockIndexToToolId.set(0, "tool_1");
      state.blockIndexToToolInput.set(0, "invalid json");

      const result = translateToolInputBlockStop(0, state);

      expect(result).not.toBeNull();
      expect((result as unknown as { input: { _raw: string } }).input).toEqual({ _raw: "invalid json" });
    });

    it("cleans up tracking state", () => {
      state.blockIndexToToolId.set(0, "tool_1");
      state.blockIndexToToolInput.set(0, "{}");

      translateToolInputBlockStop(0, state);

      expect(state.blockIndexToToolId.has(0)).toBe(false);
      expect(state.blockIndexToToolInput.has(0)).toBe(false);
    });

    it("returns null for unknown index", () => {
      const result = translateToolInputBlockStop(999, state);
      expect(result).toBeNull();
    });

    it("includes parent tool ID if set", () => {
      state.blockIndexToToolId.set(0, "tool_1");
      state.currentParentToolUseId = "parent_1";

      const result = translateToolInputBlockStop(0, state);
      expect((result as { parentToolUseId: string }).parentToolUseId).toBe("parent_1");
    });
  });

  describe("parseToolInput", () => {
    it("parses valid JSON", () => {
      const result = parseToolInput('{"key":"value"}');
      expect(result).toEqual({ key: "value" });
    });

    it("returns empty object for undefined", () => {
      expect(parseToolInput(undefined)).toEqual({});
    });

    it("returns fallback for invalid JSON", () => {
      const result = parseToolInput("not json");
      expect(result).toEqual({ _raw: "not json" });
    });
  });
});
