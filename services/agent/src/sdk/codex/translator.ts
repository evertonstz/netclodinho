/**
 * Codex event translator
 *
 * Translates Codex SDK ThreadEvents to our common PromptEvent format.
 */

import type { JsonObject } from "@bufbuild/protobuf";
import type { PromptEvent } from "../types.js";

/**
 * Codex thread item structure
 */
export interface CodexItem {
  type: string;
  id: string;
  command?: string;
  changes?: Array<{ kind: string; path: string }>;
  tool?: string;
  text?: string;
  query?: string;
  status?: string;
  aggregated_output?: string;
  result?: unknown;
  error?: { message: string };
  message?: string;
}

/**
 * Codex thread event structure
 */
export interface CodexEvent {
  type: string;
  item?: CodexItem;
  error?: { message: string };
  message?: string;
  usage?: { input_tokens: number; output_tokens: number };
}

/**
 * State tracked during event translation
 */
export interface TranslatorState {
  toolStartTimes: Map<string, number>;
  currentThinkingId: string | null;
  thinkingIdCounter: number;
  lastUsage: { inputTokens: number; outputTokens: number } | null;
}

/**
 * Create initial translator state
 */
export function createTranslatorState(): TranslatorState {
  return {
    toolStartTimes: new Map(),
    currentThinkingId: null,
    thinkingIdCounter: 0,
    lastUsage: null,
  };
}

/**
 * Reset translator state for a new prompt
 */
export function resetTranslatorState(state: TranslatorState): void {
  state.toolStartTimes.clear();
  state.currentThinkingId = null;
  state.lastUsage = null;
}

/**
 * Translate item.started event
 */
export function translateItemStarted(
  item: CodexItem,
  state: TranslatorState
): PromptEvent[] {
  switch (item.type) {
    case "command_execution":
      state.toolStartTimes.set(item.id, Date.now());
      return [{
        type: "toolStart",
        tool: "Bash",
        toolUseId: item.id,
      }];

    case "file_change": {
      state.toolStartTimes.set(item.id, Date.now());
      const firstChange = item.changes?.[0];
      const toolName = firstChange?.kind === "add" ? "Write" : "Edit";
      return [{
        type: "toolStart",
        tool: toolName,
        toolUseId: item.id,
      }];
    }

    case "mcp_tool_call":
      state.toolStartTimes.set(item.id, Date.now());
      return [{
        type: "toolStart",
        tool: item.tool || "unknown",
        toolUseId: item.id,
      }];

    case "reasoning":
      state.currentThinkingId = `thinking_${Date.now()}_${++state.thinkingIdCounter}`;
      return [{
        type: "thinking",
        thinkingId: state.currentThinkingId,
        content: item.text || "",
        partial: true,
      }];

    case "web_search":
      state.toolStartTimes.set(item.id, Date.now());
      return [{
        type: "toolStart",
        tool: "WebSearch",
        toolUseId: item.id,
      }];

    case "agent_message":
    case "todo_list":
    case "error":
      return [];

    default:
      return [];
  }
}

/**
 * Translate item.completed event
 */
export function translateItemCompleted(
  item: CodexItem,
  state: TranslatorState
): PromptEvent[] {
  const startTime = state.toolStartTimes.get(item.id);
  const durationMs = startTime ? Date.now() - startTime : undefined;
  state.toolStartTimes.delete(item.id);

  switch (item.type) {
    case "command_execution":
      return [
        {
          type: "toolInputComplete",
          toolUseId: item.id,
          input: { command: item.command || "" } as JsonObject,
        },
        {
          type: "toolEnd",
          tool: "Bash",
          toolUseId: item.id,
          result: item.aggregated_output || "",
          error: item.status === "failed" ? "Command failed" : undefined,
          ...(durationMs !== undefined && { durationMs }),
        },
      ];

    case "file_change": {
      const changesSummary = item.changes?.map((c) => `${c.kind}: ${c.path}`).join(", ") || "";
      const firstChange = item.changes?.[0];
      const toolName = firstChange?.kind === "add" ? "Write" : "Edit";
      return [{
        type: "toolEnd",
        tool: toolName,
        toolUseId: item.id,
        result: changesSummary,
        error: item.status === "failed" ? "File change failed" : undefined,
        ...(durationMs !== undefined && { durationMs }),
      }];
    }

    case "mcp_tool_call":
      return [{
        type: "toolEnd",
        tool: item.tool || "unknown",
        toolUseId: item.id,
        result: item.result ? JSON.stringify(item.result) : undefined,
        error: item.error?.message,
        ...(durationMs !== undefined && { durationMs }),
      }];

    case "agent_message":
      return [{
        type: "textDelta",
        content: item.text || "",
        partial: false,
      }];

    case "reasoning": {
      const thinkingId = state.currentThinkingId || `thinking_${Date.now()}_${++state.thinkingIdCounter}`;
      state.currentThinkingId = null;
      return [{
        type: "thinking",
        thinkingId,
        content: item.text || "",
        partial: false,
      }];
    }

    case "web_search":
      return [{
        type: "toolEnd",
        tool: "WebSearch",
        toolUseId: item.id,
        result: `Search: ${item.query || ""}`,
        ...(durationMs !== undefined && { durationMs }),
      }];

    case "error":
      return [{
        type: "error",
        message: item.message || "Unknown error",
        retryable: false,
      }];

    case "todo_list":
      return [];

    default:
      return [];
  }
}

/**
 * Translate turn.failed event
 */
export function translateTurnFailed(error: { message: string }): PromptEvent[] {
  return [{
    type: "error",
    message: error.message || "Turn failed",
    retryable: false,
  }];
}

/**
 * Translate error event
 */
export function translateError(message: string | undefined): PromptEvent[] {
  return [{
    type: "error",
    message: message || "Unknown error",
    retryable: false,
  }];
}

/**
 * Store usage from turn.completed event
 */
export function storeUsage(
  usage: { input_tokens: number; output_tokens: number },
  state: TranslatorState
): void {
  state.lastUsage = {
    inputTokens: usage.input_tokens,
    outputTokens: usage.output_tokens,
  };
}

/**
 * Create result event from accumulated state
 */
export function createResultEvent(state: TranslatorState): PromptEvent {
  return {
    type: "result",
    inputTokens: state.lastUsage?.inputTokens || 0,
    outputTokens: state.lastUsage?.outputTokens || 0,
    totalTurns: 1,
  };
}

/**
 * Main event translator function
 */
export function translateEvent(
  event: CodexEvent,
  state: TranslatorState
): PromptEvent[] {
  switch (event.type) {
    case "item.started":
      return event.item ? translateItemStarted(event.item, state) : [];

    case "item.completed":
      return event.item ? translateItemCompleted(event.item, state) : [];

    case "item.updated":
      return [];

    case "turn.started":
    case "thread.started":
      return [];

    case "turn.completed":
      if (event.usage) {
        storeUsage(event.usage, state);
      }
      return [];

    case "turn.failed":
      return event.error ? translateTurnFailed(event.error) : translateError("Turn failed");

    case "error":
      return translateError(event.message);

    default:
      return [];
  }
}
