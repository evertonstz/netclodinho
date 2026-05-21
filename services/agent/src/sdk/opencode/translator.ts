/**
 * OpenCode event translator
 *
 * Translates OpenCode SSE events to our common PromptEvent format.
 * Extracted for testability.
 */

import type { JsonObject } from "@bufbuild/protobuf";
import type { PromptEvent } from "../types.js";
import { normalizeToolName, normalizeToolInput } from "../utils/index.js";

/**
 * OpenCode SSE event structure
 */
export interface OpenCodeEvent {
  type: string;
  properties?: Record<string, unknown>;
}

/**
 * State tracked during event translation
 */
export interface TranslatorState {
  assistantMessageIds: Set<string>;
  toolStartTimes: Map<string, number>;
  toolStartEmitted: Set<string>;
  currentTextPartId: string | null;
  currentTextMessageId: string | null;
  textMessageIdCounter: number;
  lastUsage: { inputTokens: number; outputTokens: number } | null;
  textPartContent: Map<string, string>;
  reasoningPartContent: Map<string, string>;
  partTypes: Map<string, "text" | "reasoning">;
}

/**
 * Create initial translator state
 */
export function createTranslatorState(): TranslatorState {
  return {
    assistantMessageIds: new Set(),
    toolStartTimes: new Map(),
    toolStartEmitted: new Set(),
    currentTextPartId: null,
    currentTextMessageId: null,
    textMessageIdCounter: 0,
    lastUsage: null,
    textPartContent: new Map(),
    reasoningPartContent: new Map(),
    partTypes: new Map(),
  };
}

/**
 * Reset translator state for a new prompt
 */
export function resetTranslatorState(state: TranslatorState): void {
  state.assistantMessageIds.clear();
  state.toolStartTimes.clear();
  state.toolStartEmitted.clear();
  state.currentTextPartId = null;
  state.currentTextMessageId = null;
  state.lastUsage = null;
  state.textPartContent.clear();
  state.reasoningPartContent.clear();
  state.partTypes.clear();
}

/**
 * Translate a message.part.updated event
 */
export function translateMessagePartUpdated(
  part: Record<string, unknown>,
  delta: string | undefined,
  state: TranslatorState
): PromptEvent | null {
  // Auto-register unknown messageIds for tool/reasoning.
  // Text parts require confirmed assistant messageIds (set by message.updated).
  const messageId = part.messageID as string | undefined;

  switch (part.type) {
    case "tool":
      if (messageId && !state.assistantMessageIds.has(messageId)) {
        state.assistantMessageIds.add(messageId);
      }
      return translateToolPart(part, state);
    case "reasoning":
      if (messageId && !state.assistantMessageIds.has(messageId)) {
        state.assistantMessageIds.add(messageId);
      }
      // Track part type for routing message.part.delta events (always field:"text")
      if (part.id) state.partTypes.set(part.id as string, "reasoning");
      return translateReasoningPart(part, delta, state);
    case "text":
      if (messageId && !state.assistantMessageIds.has(messageId)) {
        return null; // only emit for confirmed assistant messages
      }
      // Track part type for routing message.part.delta events (always field:"text")
      if (part.id) state.partTypes.set(part.id as string, "text");
      return translateTextPartUpdated(part, state);
    case "step-start":
    case "step-finish":
    default:
      return null;
  }
}

function translateTextPartUpdated(
  part: Record<string, unknown>,
  state: TranslatorState
): PromptEvent | null {
  const partId = part.id as string | undefined;
  const newText = (part.text as string) || "";
  if (!partId || !newText) return null;

  // Strip reasoning content that was already emitted as a thinking event.
  // DeepSeek models duplicate reasoning into text parts.
  const reasoningText = Array.from(state.reasoningPartContent.values()).join("");
  let effectiveText = newText;
  if (reasoningText && newText.startsWith(reasoningText)) {
    effectiveText = newText.slice(reasoningText.length);
  }
  if (!effectiveText) return null;

  // Track final text but don't emit — streaming is handled by message.part.delta.
  // The control-plane accumulates from deltas and stores on session.idle.
  return null;
}

/**
 * Translate a text part
 */
function translateTextPart(
  part: Record<string, unknown>,
  delta: string | undefined,
  state: TranslatorState
): PromptEvent | null {
  // Only use delta content, not part.text (which is accumulated text)
  if (!delta) return null;

  // Each text part gets its own message ID
  const partId = part.id as string;
  if (partId !== state.currentTextPartId) {
    state.currentTextPartId = partId;
    state.currentTextMessageId = `msg_${Date.now()}_${++state.textMessageIdCounter}`;
  }

  return {
    type: "textDelta",
    content: delta,
    partial: true,
    messageId: state.currentTextMessageId || undefined,
  };
}

/**
 * Translate a reasoning part
 */
function translateReasoningPart(
  part: Record<string, unknown>,
  delta: string | undefined,
  state: TranslatorState
): PromptEvent | null {
  const partId = part.id as string;
  const newContent = delta || (part.text as string) || "";

  // Check if reasoning part is complete (OpenCode sets time.end on finish)
  const timeEnd = (part.time as Record<string, unknown> | undefined)?.end;
  const timeStart = (part.time as Record<string, unknown> | undefined)?.start;
  // If part.time.end exists and no delta, reasoning is complete → partial: false
  // If delta exists (streaming) or no time.end (still in progress) → partial: true
  const isComplete = !!timeEnd && !delta;

  const prevContent = state.reasoningPartContent.get(partId) || "__unset__";
  if (newContent === prevContent && !isComplete) return null; // no change from last emit
  state.reasoningPartContent.set(partId, newContent);

  // Emit even with empty content — OpenCode sends empty event first to
  // position the thinking bubble, then fills it later.
  // partial: false when part.time.end signals completion at the correct position.
  return {
    type: "thinking",
    thinkingId: partId || `thinking_${Date.now()}`,
    content: newContent,
    partial: !isComplete,
  };
}

/**
 * Translate a tool part
 */
function translateToolPart(
  part: Record<string, unknown>,
  state: TranslatorState
): PromptEvent | null {
  const partState = part.state as Record<string, unknown> | undefined;
  if (!partState) return null;

  const status = partState.status as string;
  const toolName = normalizeToolName(part.tool as string);
  const callId = part.callID as string;

  // Get input from state.input or parse from state.raw (JSON string)
  let input = partState.input as Record<string, unknown> | undefined;
  if (!input && partState.raw) {
    try {
      input = JSON.parse(partState.raw as string);
    } catch {
      // raw might be incomplete during streaming
    }
  }

  const normalizedInput = normalizeToolInput(input);

  switch (status) {
    case "pending":
      // Just track the start time, don't emit yet
      if (!state.toolStartTimes.has(callId)) {
        state.toolStartTimes.set(callId, Date.now());
      }
      return null;

    case "running":
      // Input is now complete, emit toolStart if we haven't already
      if (!state.toolStartEmitted.has(callId)) {
        state.toolStartEmitted.add(callId);
        if (!state.toolStartTimes.has(callId)) {
          state.toolStartTimes.set(callId, Date.now());
        }
        // Split text: subsequent text deltas go to a new message bubble
        state.currentTextMessageId = null;
        state.currentTextPartId = null;
        return {
          type: "toolStart",
          tool: toolName,
          toolUseId: callId,
          input: normalizedInput as JsonObject | undefined,
        };
      }
      return null;

    case "completed":
      // Split text: text after tool goes to a new message bubble
      state.currentTextMessageId = null;
      state.currentTextPartId = null;
      return translateToolCompleted(toolName, callId, partState, normalizedInput, state);

    case "error":
      state.currentTextMessageId = null;
      state.currentTextPartId = null;
      return translateToolError(toolName, callId, partState, normalizedInput, state);

    default:
      return null;
  }
}

/**
 * Handle tool completed status
 */
function translateToolCompleted(
  toolName: string,
  callId: string,
  partState: Record<string, unknown>,
  normalizedInput: Record<string, unknown> | undefined,
  state: TranslatorState
): PromptEvent {
  // If we never emitted toolStart, emit it now
  if (!state.toolStartEmitted.has(callId)) {
    state.toolStartEmitted.add(callId);
    if (!state.toolStartTimes.has(callId)) {
      state.toolStartTimes.set(callId, Date.now());
    }
    return {
      type: "toolStart",
      tool: toolName,
      toolUseId: callId,
      input: normalizedInput as JsonObject | undefined,
    };
  }

  const startTime = state.toolStartTimes.get(callId);
  state.toolStartTimes.delete(callId);
  state.toolStartEmitted.delete(callId);
  const durationMs = startTime ? Date.now() - startTime : undefined;

  return {
    type: "toolEnd",
    tool: toolName,
    toolUseId: callId,
    result: partState.output as string | undefined,
    ...(durationMs !== undefined && { durationMs }),
  };
}

/**
 * Handle tool error status
 */
function translateToolError(
  toolName: string,
  callId: string,
  partState: Record<string, unknown>,
  normalizedInput: Record<string, unknown> | undefined,
  state: TranslatorState
): PromptEvent {
  // If we never emitted toolStart, emit it now
  if (!state.toolStartEmitted.has(callId)) {
    state.toolStartEmitted.add(callId);
    if (!state.toolStartTimes.has(callId)) {
      state.toolStartTimes.set(callId, Date.now());
    }
    return {
      type: "toolStart",
      tool: toolName,
      toolUseId: callId,
      input: normalizedInput as JsonObject | undefined,
    };
  }

  const startTime = state.toolStartTimes.get(callId);
  state.toolStartTimes.delete(callId);
  state.toolStartEmitted.delete(callId);
  const durationMs = startTime ? Date.now() - startTime : undefined;

  return {
    type: "toolEnd",
    tool: toolName,
    toolUseId: callId,
    error: partState.error as string | undefined,
    ...(durationMs !== undefined && { durationMs }),
  };
}

/**
 * Translate a message.updated event
 */
export function translateMessageUpdated(
  info: Record<string, unknown>,
  state: TranslatorState
): PromptEvent | null {
  // Track assistant message IDs so we can filter message.part.updated events
  if (info.role === "assistant" && info.id) {
    state.assistantMessageIds.add(info.id as string);
  }

  // Track usage data for later (emitted with session.idle)
  if (info.role === "assistant" && info.time) {
    const time = info.time as Record<string, unknown>;
    if (time.completed) {
      const tokens = info.tokens as Record<string, number> | undefined;
      if (tokens) {
        state.lastUsage = {
          inputTokens: (state.lastUsage?.inputTokens || 0) + (tokens.input || 0),
          outputTokens: (state.lastUsage?.outputTokens || 0) + (tokens.output || 0),
        };
      }
    }
  }

  // Check for errors
  if (info.error) {
    const error = info.error as Record<string, unknown>;
    const errorData = error.data as Record<string, string> | undefined;
    return {
      type: "error",
      message: errorData?.message || "Unknown error",
      retryable: false,
    };
  }

  return null;
}

/**
 * Translate a session.idle event
 */
export function translateSessionIdle(state: TranslatorState): PromptEvent {
  // Reset per-turn state so next turn gets a fresh messageId
  state.currentTextMessageId = null;
  state.currentTextPartId = null;
  return {
    type: "result",
    inputTokens: state.lastUsage?.inputTokens || 0,
    outputTokens: state.lastUsage?.outputTokens || 0,
    totalTurns: 1,
  };
}

/**
 * Return final thinking events for all active reasoning parts.
 * Called after session.idle to mark thinking bubbles as complete.
 */
export function finalizeActiveThinking(state: TranslatorState): PromptEvent[] {
  const events: PromptEvent[] = [];
  for (const [partId, content] of state.reasoningPartContent) {
    events.push({
      type: "thinking",
      thinkingId: partId,
      content,
      partial: false,
    });
  }
  return events;
}

/**
 * Main event translator function
 */
export function translateEvent(
  event: OpenCodeEvent,
  state: TranslatorState
): PromptEvent | null {
  const props = event.properties || {};

  switch (event.type) {
    case "message.part.delta": {
      // OpenCode 1.15+ emits ALL deltas with field:"text", regardless of whether
      // the token is from a text part or a reasoning part. The part type (text vs
      // reasoning) is determined by message.part.updated events tracked in
      // state.partTypes.
      //
      // Event format: { type: "message.part.delta", properties: { messageID, partID, field, delta } }
      const messageId = props.messageID as string | undefined;
      const field = props.field as string | undefined;
      const delta = props.delta as string | undefined;
      const partId = props.partID as string | undefined;

      // Only text field deltas are emitted by OpenCode. "reasoning" never appears.
      if (field !== "text" || !delta) return null;

      // Route delta based on part type tracked from message.part.updated events.
      // If we haven't seen the part.updated yet, we can't determine routing — drop.
      const partType = partId ? state.partTypes.get(partId) : undefined;
      if (!partType) return null;

      if (partType === "reasoning") {
        return {
          type: "thinking",
          thinkingId: partId || `thinking_${Date.now()}`,
          content: delta,
          partial: true,
        };
      }

      // Text delta — streaming only. Final text comes via message.part.updated.
      if (!messageId) return null;
      if (!state.assistantMessageIds.has(messageId)) {
        state.assistantMessageIds.add(messageId);
      }

      if (!state.currentTextMessageId) {
        state.currentTextMessageId = `msg_${Date.now()}_${++state.textMessageIdCounter}`;
      }

      return {
        type: "textDelta",
        content: delta,
        partial: true,
        messageId: state.currentTextMessageId,
      };
    }

    case "message.part.updated": {
      // Used for tool state transitions (pending → running → completed/error).
      // All part types now pass through — text content arrives via
      // message.part.updated with part.type="text" in OpenCode 1.15+.
      const part = props.part as Record<string, unknown> | undefined;
      if (!part) return null;
      return translateMessagePartUpdated(part, undefined, state);
    }

    case "message.updated": {
      const info = props.info as Record<string, unknown> | undefined;
      if (!info) return null;
      return translateMessageUpdated(info, state);
    }

    case "session.idle":
      return translateSessionIdle(state);

    default:
      return null;
  }
}
