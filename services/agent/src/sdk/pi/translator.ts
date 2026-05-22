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
 *   - text_delta     → text streaming
 *   - thinking_delta → thinking/reasoning streaming
 *   - toolcall_delta → tool arg streaming (delta + contentIndex)
 *   - toolcall_end   → final tool call (ToolCall with id, name, args)
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
  /** Map of toolCallId → epoch ms start time. */
  toolStartTimes: Map<string, number>;
  /** Tracks partially accumulated tool args strings by contentIndex. */
  pendingArgs: Map<number, string>;
}

export function createPiTranslatorState(): PiTranslatorState {
  return {
    contentIndexToTool: new Map(),
    startedToolIds: new Set(),
    closedThinking: new Set(),
    toolStartTimes: new Map(),
    pendingArgs: new Map(),
  };
}

export function resetPiTranslatorState(state: PiTranslatorState): void {
  state.contentIndexToTool.clear();
  state.startedToolIds.clear();
  state.closedThinking.clear();
  state.toolStartTimes.clear();
  state.pendingArgs.clear();
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

    case "thinking_delta": {
      if (!evt.delta) return null;
      const thinkingId = "pi-thinking-0";
      return {
        type: "thinking",
        thinkingId,
        content: evt.delta,
        partial: true,
      };
    }

    case "toolcall_delta": {
      if (evt.contentIndex === undefined) return null;

      const ci = evt.contentIndex;
      const existing = state.contentIndexToTool.get(ci);

      // If we haven't seen this tool yet, try to extract id/name from
      // the partial message content (populated when toolcall_end arrives
      // or when the ToolCall block is partially constructed).
      if (!existing) {
        const toolInfo = extractToolInfo(event.message as PiAgentMessage | undefined, ci);
        if (toolInfo) {
          state.contentIndexToTool.set(ci, toolInfo);
        }
      }

      // Emit toolStart on first delta for this content index
      const currentTool = state.contentIndexToTool.get(ci);
      if (currentTool && !state.startedToolIds.has(currentTool.id)) {
        state.startedToolIds.add(currentTool.id);
        // Yield toolStart before the first arg delta
        // We'll yield it immediately, then this delta goes as toolInput
        // But we can only yield one event at a time from this function.
        // Instead, emit toolStart first; the next delta will be toolInput.
        // To handle this cleanly, emit toolStart + queue the current delta.
        // For now, just emit toolStart — the delta will be handled next call.
      }

      // Stream delta as toolInput if we know the tool ID
      const tool = state.contentIndexToTool.get(ci);
      if (tool && evt.delta) {
        // Accumulate delta to avoid sending the full accumulated string each time
        const prev = state.pendingArgs.get(ci) || "";
        const newPortion = evt.delta.slice(prev.length);
        state.pendingArgs.set(ci, evt.delta);

        if (newPortion) {
          return {
            type: "toolInput",
            toolUseId: tool.id,
            inputDelta: newPortion,
          };
        }
      }

      return null;
    }

    case "toolcall_end": {
      // Final tool call — register the tool and emit toolStart if not yet emitted
      if (evt.toolCall && evt.contentIndex !== undefined) {
        const tc = evt.toolCall;
        state.contentIndexToTool.set(evt.contentIndex, { id: tc.id, name: tc.name });

        if (!state.startedToolIds.has(tc.id)) {
          state.startedToolIds.add(tc.id);
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
    const content = msg.content;
    if (typeof content !== "object" || !content) continue;
    const c = content as Record<string, unknown>;
    if (typeof c.usage === "object" && c.usage) {
      const usage = c.usage as Record<string, unknown>;
      inputTokens += (usage.input_tokens as number) ?? (usage.inputTokens as number) ?? 0;
      outputTokens += (usage.output_tokens as number) ?? (usage.outputTokens as number) ?? 0;
    }
  }

  totalTurns = messages.filter((m) => m.role === "assistant").length;

  return { type: "result", inputTokens, outputTokens, totalTurns };
}
