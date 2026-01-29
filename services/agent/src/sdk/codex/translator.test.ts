/**
 * Tests for Codex event translator
 */

import { describe, it, expect, beforeEach } from "vitest";
import {
  createTranslatorState,
  resetTranslatorState,
  translateEvent,
  translateItemStarted,
  translateItemCompleted,
  translateTurnFailed,
  translateError,
  storeUsage,
  createResultEvent,
  type TranslatorState,
  type CodexItem,
} from "./translator.js";

describe("Codex Translator", () => {
  let state: TranslatorState;

  beforeEach(() => {
    state = createTranslatorState();
  });

  describe("createTranslatorState", () => {
    it("creates empty initial state", () => {
      expect(state.toolStartTimes.size).toBe(0);
      expect(state.currentThinkingId).toBeNull();
      expect(state.thinkingIdCounter).toBe(0);
      expect(state.lastUsage).toBeNull();
    });
  });

  describe("resetTranslatorState", () => {
    it("clears state except counter", () => {
      state.toolStartTimes.set("tool_1", 1000);
      state.currentThinkingId = "thinking_1";
      state.thinkingIdCounter = 5;
      state.lastUsage = { inputTokens: 100, outputTokens: 50 };

      resetTranslatorState(state);

      expect(state.toolStartTimes.size).toBe(0);
      expect(state.currentThinkingId).toBeNull();
      expect(state.lastUsage).toBeNull();
      expect(state.thinkingIdCounter).toBe(5); // Preserved
    });
  });

  describe("translateItemStarted", () => {
    it("translates command_execution", () => {
      const item: CodexItem = { type: "command_execution", id: "cmd_1" };
      const result = translateItemStarted(item, state);
      expect(result).toEqual([{
        type: "toolStart",
        tool: "Bash",
        toolUseId: "cmd_1",
      }]);
      expect(state.toolStartTimes.has("cmd_1")).toBe(true);
    });

    it("translates file_change with add", () => {
      const item: CodexItem = {
        type: "file_change",
        id: "file_1",
        changes: [{ kind: "add", path: "/new.ts" }],
      };
      const result = translateItemStarted(item, state);
      expect(result[0].type).toBe("toolStart");
      expect((result[0] as { tool: string }).tool).toBe("Write");
    });

    it("translates file_change with modify", () => {
      const item: CodexItem = {
        type: "file_change",
        id: "file_1",
        changes: [{ kind: "modify", path: "/existing.ts" }],
      };
      const result = translateItemStarted(item, state);
      expect(result[0].type).toBe("toolStart");
      expect((result[0] as { tool: string }).tool).toBe("Edit");
    });

    it("translates mcp_tool_call", () => {
      const item: CodexItem = {
        type: "mcp_tool_call",
        id: "mcp_1",
        tool: "custom_tool",
      };
      const result = translateItemStarted(item, state);
      expect(result).toEqual([{
        type: "toolStart",
        tool: "custom_tool",
        toolUseId: "mcp_1",
      }]);
    });

    it("translates reasoning", () => {
      const item: CodexItem = {
        type: "reasoning",
        id: "reason_1",
        text: "Thinking...",
      };
      const result = translateItemStarted(item, state);
      expect(result[0].type).toBe("thinking");
      expect((result[0] as { content: string }).content).toBe("Thinking...");
      expect((result[0] as { partial: boolean }).partial).toBe(true);
      expect(state.currentThinkingId).not.toBeNull();
    });

    it("translates web_search", () => {
      const item: CodexItem = { type: "web_search", id: "search_1" };
      const result = translateItemStarted(item, state);
      expect(result[0].type).toBe("toolStart");
      expect((result[0] as { tool: string }).tool).toBe("WebSearch");
    });

    it("returns empty for agent_message", () => {
      const item: CodexItem = { type: "agent_message", id: "msg_1" };
      expect(translateItemStarted(item, state)).toEqual([]);
    });

    it("returns empty for todo_list", () => {
      const item: CodexItem = { type: "todo_list", id: "todo_1" };
      expect(translateItemStarted(item, state)).toEqual([]);
    });

    it("returns empty for error", () => {
      const item: CodexItem = { type: "error", id: "err_1" };
      expect(translateItemStarted(item, state)).toEqual([]);
    });
  });

  describe("translateItemCompleted", () => {
    it("translates command_execution with output", () => {
      state.toolStartTimes.set("cmd_1", Date.now() - 100);
      const item: CodexItem = {
        type: "command_execution",
        id: "cmd_1",
        command: "ls -la",
        aggregated_output: "file1.ts\nfile2.ts",
        status: "success",
      };
      const result = translateItemCompleted(item, state);
      expect(result).toHaveLength(2);
      expect(result[0]).toEqual({
        type: "toolInputComplete",
        toolUseId: "cmd_1",
        input: { command: "ls -la" },
      });
      expect(result[1].type).toBe("toolEnd");
      expect((result[1] as { result: string }).result).toBe("file1.ts\nfile2.ts");
    });

    it("translates failed command_execution", () => {
      const item: CodexItem = {
        type: "command_execution",
        id: "cmd_1",
        command: "bad-cmd",
        status: "failed",
      };
      const result = translateItemCompleted(item, state);
      expect((result[1] as { error: string }).error).toBe("Command failed");
    });

    it("translates file_change", () => {
      const item: CodexItem = {
        type: "file_change",
        id: "file_1",
        changes: [
          { kind: "add", path: "/new.ts" },
          { kind: "modify", path: "/existing.ts" },
        ],
      };
      const result = translateItemCompleted(item, state);
      expect(result[0].type).toBe("toolEnd");
      expect((result[0] as { tool: string }).tool).toBe("Write");
      expect((result[0] as { result: string }).result).toBe("add: /new.ts, modify: /existing.ts");
    });

    it("translates mcp_tool_call with result", () => {
      const item: CodexItem = {
        type: "mcp_tool_call",
        id: "mcp_1",
        tool: "custom_tool",
        result: { data: "value" },
      };
      const result = translateItemCompleted(item, state);
      expect((result[0] as { result: string }).result).toBe('{"data":"value"}');
    });

    it("translates mcp_tool_call with error", () => {
      const item: CodexItem = {
        type: "mcp_tool_call",
        id: "mcp_1",
        tool: "custom_tool",
        error: { message: "Tool failed" },
      };
      const result = translateItemCompleted(item, state);
      expect((result[0] as { error: string }).error).toBe("Tool failed");
    });

    it("translates agent_message", () => {
      const item: CodexItem = {
        type: "agent_message",
        id: "msg_1",
        text: "Here is the response",
      };
      const result = translateItemCompleted(item, state);
      expect(result).toEqual([{
        type: "textDelta",
        content: "Here is the response",
        partial: false,
      }]);
    });

    it("translates reasoning completion", () => {
      state.currentThinkingId = "thinking_1";
      const item: CodexItem = {
        type: "reasoning",
        id: "reason_1",
        text: "Final thought",
      };
      const result = translateItemCompleted(item, state);
      expect(result).toEqual([{
        type: "thinking",
        thinkingId: "thinking_1",
        content: "Final thought",
        partial: false,
      }]);
      expect(state.currentThinkingId).toBeNull();
    });

    it("translates web_search completion", () => {
      const item: CodexItem = {
        type: "web_search",
        id: "search_1",
        query: "TypeScript tutorials",
      };
      const result = translateItemCompleted(item, state);
      expect((result[0] as { result: string }).result).toBe("Search: TypeScript tutorials");
    });

    it("translates error item", () => {
      const item: CodexItem = {
        type: "error",
        id: "err_1",
        message: "Something went wrong",
      };
      const result = translateItemCompleted(item, state);
      expect(result).toEqual([{
        type: "error",
        message: "Something went wrong",
        retryable: false,
      }]);
    });

    it("calculates duration", () => {
      state.toolStartTimes.set("cmd_1", Date.now() - 500);
      const item: CodexItem = { type: "command_execution", id: "cmd_1" };
      const result = translateItemCompleted(item, state);
      expect((result[1] as { durationMs: number }).durationMs).toBeGreaterThanOrEqual(500);
    });

    it("cleans up start time tracking", () => {
      state.toolStartTimes.set("cmd_1", 1000);
      const item: CodexItem = { type: "command_execution", id: "cmd_1" };
      translateItemCompleted(item, state);
      expect(state.toolStartTimes.has("cmd_1")).toBe(false);
    });
  });

  describe("translateTurnFailed", () => {
    it("translates turn failure", () => {
      const result = translateTurnFailed({ message: "API error" });
      expect(result).toEqual([{
        type: "error",
        message: "API error",
        retryable: false,
      }]);
    });

    it("handles empty message", () => {
      const result = translateTurnFailed({ message: "" });
      expect((result[0] as { message: string }).message).toBe("Turn failed");
    });
  });

  describe("translateError", () => {
    it("translates error message", () => {
      const result = translateError("Connection lost");
      expect(result).toEqual([{
        type: "error",
        message: "Connection lost",
        retryable: false,
      }]);
    });

    it("handles undefined message", () => {
      const result = translateError(undefined);
      expect((result[0] as { message: string }).message).toBe("Unknown error");
    });
  });

  describe("storeUsage", () => {
    it("stores usage data", () => {
      storeUsage({ input_tokens: 100, output_tokens: 50 }, state);
      expect(state.lastUsage).toEqual({ inputTokens: 100, outputTokens: 50 });
    });
  });

  describe("createResultEvent", () => {
    it("creates result with usage", () => {
      state.lastUsage = { inputTokens: 500, outputTokens: 200 };
      const result = createResultEvent(state);
      expect(result).toEqual({
        type: "result",
        inputTokens: 500,
        outputTokens: 200,
        totalTurns: 1,
      });
    });

    it("creates result with zero usage", () => {
      const result = createResultEvent(state);
      expect(result).toEqual({
        type: "result",
        inputTokens: 0,
        outputTokens: 0,
        totalTurns: 1,
      });
    });
  });

  describe("translateEvent", () => {
    it("handles item.started", () => {
      const result = translateEvent(
        { type: "item.started", item: { type: "command_execution", id: "c1" } },
        state
      );
      expect(result[0].type).toBe("toolStart");
    });

    it("handles item.completed", () => {
      const result = translateEvent(
        { type: "item.completed", item: { type: "agent_message", id: "m1", text: "Hi" } },
        state
      );
      expect(result[0].type).toBe("textDelta");
    });

    it("handles item.updated", () => {
      const result = translateEvent({ type: "item.updated" }, state);
      expect(result).toEqual([]);
    });

    it("handles turn.started", () => {
      const result = translateEvent({ type: "turn.started" }, state);
      expect(result).toEqual([]);
    });

    it("handles turn.completed with usage", () => {
      translateEvent(
        { type: "turn.completed", usage: { input_tokens: 100, output_tokens: 50 } },
        state
      );
      expect(state.lastUsage).toEqual({ inputTokens: 100, outputTokens: 50 });
    });

    it("handles turn.failed", () => {
      const result = translateEvent(
        { type: "turn.failed", error: { message: "Failed" } },
        state
      );
      expect(result[0].type).toBe("error");
    });

    it("handles error", () => {
      const result = translateEvent({ type: "error", message: "Oops" }, state);
      expect(result[0].type).toBe("error");
    });

    it("handles unknown events", () => {
      const result = translateEvent({ type: "unknown.event" }, state);
      expect(result).toEqual([]);
    });

    it("handles missing item", () => {
      const result = translateEvent({ type: "item.started" }, state);
      expect(result).toEqual([]);
    });
  });
});
