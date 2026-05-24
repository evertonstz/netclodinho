/**
 * Pi event translator
 *
 * Maps Pi AgentEvent (from @earendil-works/pi-agent-core) to our
 * unified PromptEvent union.
 *
 * Pi event flow for a prompt:
 *   agent_start → turn_start → message_update* → message_end →
 *   [tool_execution_start → tool_execution_update* → tool_execution_end]+ →
 *   turn_end → ... → agent_end
 *
 * message_update carries an AssistantMessageEvent with deltas:
 *   - text_delta        → text streaming
 *   - thinking_start    → thinking block opened (partial=true, empty content)
 *   - thinking_delta    → thinking/reasoning content delta
 *   - thinking_end      → thinking block closed (partial=false)
 *   - toolcall_start    → tool call block opened (→ toolStart)
 *   - toolcall_delta    → tool arg delta (indexed by contentIndex)
 *   - toolcall_end      → final tool call (registers id/name; → toolStart if needed)
 */

import type { JsonObject } from "@bufbuild/protobuf";
import type { PromptEvent } from "../types.js";

// ── Pi event types ─────────────────────────────────────────────────────────

/** An agent message (user, assistant, toolResult, or custom). */
export interface PiAgentMessage {
  role: string;
  content: unknown;
  timestamp?: number;
}

/** Content block within an assistant message. */
export interface PiToolCall {
  type: "toolCall";
  id: string;
  name: string;
  args: Record<string, unknown>;
}

export interface PiTextContent {
  type: "text";
  text: string;
}

export interface PiThinkingContent {
  type: "thinking";
  thinking: string;
  signature?: string;
}

export interface PiAssistantMessage {
  role: "assistant";
  content: unknown[];
}

/** Low-level assistant message event streamed by the provider. */
export interface PiAssistantMessageEvent {
  type: string;
  contentIndex?: number;
  delta?: string;
  content?: string;
  toolCall?: PiToolCall;
  partial?: PiAssistantMessage;
}

/** High-level agent lifecycle event. */
export interface PiAgentEvent {
  type: string;
  message?: PiAgentMessage;
  messages?: PiAgentMessage[];
  assistantMessageEvent?: PiAssistantMessageEvent;
  toolCallId?: string;
  toolName?: string;
  args?: Record<string, unknown>;
  partialResult?: unknown;
  result?: unknown;
  isError?: boolean;
  error?: string;
}

// ── Translation state ──────────────────────────────────────────────────────

export interface PiTranslatorState {
  /**
   * Map of contentIndex → { id, name } for in-progress tool calls.
   * Populated from toolcall_end events (which carry the full ToolCall).
   * Used to emit toolStart on first delta for a new content index.
   */
  contentIndexToTool: Map<number, { id: string; name: string }>;
  /** Set of tool call IDs that have emitted toolStart. */
  startedToolIds: Set<string>;
  /** Set of thinking IDs that have been marked partial=false already. */
  closedThinking: Set<string>;
  /** Set of thinking IDs currently open (started, not yet ended). */
  openThinking: Set<string>;
  /** Set of tool call IDs currently in progress (toolStart emitted, toolEnd not yet). */
  openTools: Set<string>;
  /** Map of toolCallId → epoch ms start time. */
  toolStartTimes: Map<string, number>;
}

export function createPiTranslatorState(): PiTranslatorState {
  return {
    contentIndexToTool: new Map(),
    startedToolIds: new Set(),
    closedThinking: new Set(),
    openThinking: new Set(),
    openTools: new Set(),
    toolStartTimes: new Map(),
  };
}

export function resetPiTranslatorState(state: PiTranslatorState): void {
  state.contentIndexToTool.clear();
  state.startedToolIds.clear();
  state.closedThinking.clear();
  state.openThinking.clear();
  state.openTools.clear();
  state.toolStartTimes.clear();
}

/** Return close events for any thinking blocks that are still open. */
export function flushOpenThinking(state: PiTranslatorState): PromptEvent[] {
  const events: PromptEvent[] = [];
  for (const thinkingId of state.openThinking) {
    events.push({
      type: "thinking",
      thinkingId,
      content: "",
      partial: false,
    });
  }
  return events;
}

export function flushOpenTools(state: PiTranslatorState): PromptEvent[] {
  const events: PromptEvent[] = [];
  for (const toolUseId of state.openTools) {
    let toolName = "unknown";
    for (const [, info] of state.contentIndexToTool) {
      if (info.id === toolUseId) { toolName = info.name; break; }
    }
    const startTime = state.toolStartTimes.get(toolUseId);
    events.push({
      type: "toolEnd",
      tool: toolName,
      toolUseId,
      error: "Tool call interrupted",
      durationMs: startTime ? Date.now() - startTime : undefined,
    });
  }
  return events;
}
// ── Main translator ────────────────────────────────────────────────────────

export function translatePiEvent(
  event: PiAgentEvent,
  state: PiTranslatorState,
): PromptEvent | null {
  switch (event.type) {
    case "message_update":
      return translateMessageUpdate(event, state);

    case "tool_execution_start":
      return translateToolExecutionStart(event, state);

    case "tool_execution_end":
      return translateToolExecutionEnd(event, state);

    case "agent_end":
      return translateAgentEnd(event);

    case "agent_start":
    case "turn_start":
    case "turn_end":
    case "message_start":
    case "message_end":
    case "tool_execution_update":
      // Internal lifecycle events — no user-visible prompt event.
      return null;

    default:
      return null;
  }
}

// ── Sub-translators ────────────────────────────────────────────────────────

function translateMessageUpdate(
  event: PiAgentEvent,
  state: PiTranslatorState,
): PromptEvent | null {
  const evt = event.assistantMessageEvent;
  if (!evt) return null;

  switch (evt.type) {
    case "text_delta":
      if (!evt.delta) return null;
      return {
        type: "textDelta",
        content: evt.delta,
        partial: true,
      };

    case "thinking_start": {
      if (evt.contentIndex === undefined) return null;
      const thinkingId = `pi-thinking-${evt.contentIndex}`;
      state.openThinking.add(thinkingId);
      return {
        type: "thinking",
        thinkingId,
        content: "",
        partial: true,
      };
    }

    case "thinking_delta": {
      if (!evt.delta) return null;
      if (evt.contentIndex === undefined) return null;
      const thinkingId = `pi-thinking-${evt.contentIndex}`;
      return {
        type: "thinking",
        thinkingId,
        content: evt.delta,
        partial: true,
      };
    }

    case "thinking_end": {
      if (evt.contentIndex === undefined) return null;
      const thinkingId = `pi-thinking-${evt.contentIndex}`;
      if (state.closedThinking.has(thinkingId)) return null;
      state.closedThinking.add(thinkingId);
      state.openThinking.delete(thinkingId);
      return {
        type: "thinking",
        thinkingId,
        content: evt.content ?? "",
        partial: false,
      };
    }

    case "toolcall_start": {
      if (evt.contentIndex === undefined) return null;
      const ci = evt.contentIndex;

      // Try to extract tool id/name from the partial message content
      const toolInfo = extractToolInfo(
        event.message as PiAgentMessage | undefined,
        ci,
      );
      if (toolInfo) {
        state.contentIndexToTool.set(ci, toolInfo);
        if (!state.startedToolIds.has(toolInfo.id)) {
          state.startedToolIds.add(toolInfo.id);
          state.openTools.add(toolInfo.id);
          return {
            type: "toolStart",
            tool: toolInfo.name,
            toolUseId: toolInfo.id,
          };
        }
      }
      return null;
    }

    case "toolcall_delta": {
      if (evt.contentIndex === undefined) return null;
      const ci = evt.contentIndex;
      // toolcall_start always arrives before deltas in all Pi providers.
      const tool = state.contentIndexToTool.get(ci);
      if (!tool || !evt.delta) return null;
      return {
        type: "toolInput",
        toolUseId: tool.id,
        inputDelta: evt.delta,
      };
    }

    case "toolcall_end": {
      // Final tool call — register the tool and emit toolStart if not yet emitted
      if (evt.toolCall && evt.contentIndex !== undefined) {
        const tc = evt.toolCall;
        state.contentIndexToTool.set(evt.contentIndex, { id: tc.id, name: tc.name });

        if (!state.startedToolIds.has(tc.id)) {
          state.startedToolIds.add(tc.id);
          state.openTools.add(tc.id);
          return {
            type: "toolStart",
            tool: tc.name,
            toolUseId: tc.id,
            input: tc.args as JsonObject,
          };
        }
      }
      return null;
    }

    default:
      return null;
  }
}

/** Try to extract tool id/name from the partial message content at a given index. */
function extractToolInfo(
  message: PiAgentMessage | undefined,
  contentIndex: number,
): { id: string; name: string } | null {
  if (!message || message.role !== "assistant") return null;
  const content = (message as unknown as PiAssistantMessage).content;
  if (!Array.isArray(content) || contentIndex >= content.length) return null;
  const block = content[contentIndex] as PiToolCall | undefined;
  if (block?.type === "toolCall" && block.id && block.name) {
    return { id: block.id, name: block.name };
  }
  return null;
}

function translateToolExecutionStart(
  event: PiAgentEvent,
  state: PiTranslatorState,
): PromptEvent | null {
  if (!event.toolCallId) return null;
  state.toolStartTimes.set(event.toolCallId, Date.now());
  return null; // toolStart emitted via message_update / toolcall_end
}

function translateToolExecutionEnd(
  event: PiAgentEvent,
  state: PiTranslatorState,
): PromptEvent | null {
  if (!event.toolCallId) return null;

  const toolName = event.toolName ?? "unknown";
  const startTime = state.toolStartTimes.get(event.toolCallId);
  const durationMs = startTime ? Date.now() - startTime : undefined;

  state.startedToolIds.delete(event.toolCallId);
  state.openTools.delete(event.toolCallId);
  state.toolStartTimes.delete(event.toolCallId);
  let resultStr: string | undefined;
  let errorStr: string | undefined;

  if (event.isError) {
    errorStr = typeof event.result === "string"
      ? event.result
      : "Tool execution failed";
  } else if (event.result !== undefined) {
    resultStr = typeof event.result === "string"
      ? event.result
      : JSON.stringify(event.result);
  }

  return {
    type: "toolEnd",
    tool: toolName,
    toolUseId: event.toolCallId,
    result: resultStr,
    error: errorStr,
    durationMs,
  };
}

function translateAgentEnd(event: PiAgentEvent): PromptEvent | null {
  const messages = event.messages;
  if (!messages || messages.length === 0) {
    return { type: "result", inputTokens: 0, outputTokens: 0, totalTurns: 0 };
  }

  let inputTokens = 0;
  let outputTokens = 0;
  let totalTurns = 0;

  for (const msg of messages) {
    // usage is a top-level field on assistant messages in the Pi SDK.
    const msgAny = msg as unknown as Record<string, unknown>;
    const usage = msgAny.usage as Record<string, unknown> | undefined;
    if (usage && typeof usage === "object") {
      inputTokens += (usage.inputTokens as number) ?? 0;
      outputTokens += (usage.outputTokens as number) ?? 0;
    }
  }

  totalTurns = messages.filter((m) => m.role === "assistant").length;

  return { type: "result", inputTokens, outputTokens, totalTurns };
}
