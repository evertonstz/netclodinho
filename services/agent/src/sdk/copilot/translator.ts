/**
 * Copilot event translator
 *
 * Translates Copilot SDK SessionEvents to our common PromptEvent format.
 */

import type { JsonObject } from "@bufbuild/protobuf";
import type { PromptEvent } from "../types.js";

/**
 * Copilot SDK event structure
 */
export interface CopilotEvent {
  type: string;
  id?: string;
  data?: Record<string, unknown>;
}

/**
 * State tracked during event translation
 */
export interface TranslatorState {
  toolNameMap: Map<string, string>;
  toolStartTimes: Map<string, number>;
  currentThinkingId: string | null;
  thinkingIdCounter: number;
  currentTextMessageId: string | null;
  textMessageIdCounter: number;
  lastUsage: { inputTokens: number; outputTokens: number } | null;
}

/**
 * Create initial translator state
 */
export function createTranslatorState(): TranslatorState {
  return {
    toolNameMap: new Map(),
    toolStartTimes: new Map(),
    currentThinkingId: null,
    thinkingIdCounter: 0,
    currentTextMessageId: null,
    textMessageIdCounter: 0,
    lastUsage: null,
  };
}

/**
 * Reset translator state for a new prompt
 */
export function resetTranslatorState(state: TranslatorState): void {
  state.toolNameMap.clear();
  state.toolStartTimes.clear();
  state.currentThinkingId = null;
  state.currentTextMessageId = null;
  state.lastUsage = null;
}

/**
 * Translate assistant.message_delta event
 */
export function translateMessageDelta(
  data: { deltaContent?: string; content?: string },
  state: TranslatorState
): PromptEvent | null {
  const textContent = data.deltaContent || data.content;
  if (!textContent) return null;

  if (!state.currentTextMessageId) {
    state.currentTextMessageId = `msg_${Date.now()}_${++state.textMessageIdCounter}`;
  }

  return {
    type: "textDelta",
    content: textContent,
    partial: true,
    messageId: state.currentTextMessageId,
  };
}

/**
 * Translate assistant.message event (final message)
 */
export function translateMessage(
  data: { content?: string },
  state: TranslatorState
): PromptEvent {
  const messageId = state.currentTextMessageId || `msg_${Date.now()}_${++state.textMessageIdCounter}`;
  state.currentTextMessageId = null;

  return {
    type: "textDelta",
    content: data.content || "",
    partial: false,
    messageId,
  };
}

/**
 * Translate assistant.reasoning_delta event
 */
export function translateReasoningDelta(
  data: { deltaContent?: string },
  state: TranslatorState
): PromptEvent | null {
  if (!data.deltaContent) return null;

  if (!state.currentThinkingId) {
    state.currentThinkingId = `thinking_${Date.now()}_${++state.thinkingIdCounter}`;
  }

  return {
    type: "thinking",
    thinkingId: state.currentThinkingId,
    content: data.deltaContent,
    partial: true,
  };
}

/**
 * Translate assistant.reasoning event (end of reasoning)
 */
export function translateReasoning(state: TranslatorState): PromptEvent {
  const thinkingId = state.currentThinkingId || `thinking_${Date.now()}_${++state.thinkingIdCounter}`;
  state.currentThinkingId = null;
  state.currentTextMessageId = null;

  return {
    type: "thinking",
    thinkingId,
    content: "",
    partial: false,
  };
}

/**
 * Translate tool.execution_start event
 */
export function translateToolStart(
  data: { toolName?: string; toolCallId?: string; arguments?: Record<string, unknown> },
  eventId: string | undefined,
  state: TranslatorState
): PromptEvent {
  const toolName = data.toolName || "unknown";
  const toolCallId = data.toolCallId || eventId || `tool_${Date.now()}`;

  state.toolNameMap.set(toolCallId, toolName);
  state.toolStartTimes.set(toolCallId, Date.now());

  return {
    type: "toolStart",
    tool: toolName,
    toolUseId: toolCallId,
    input: data.arguments as JsonObject | undefined,
  };
}

/**
 * Translate tool.execution_complete event
 */
export function translateToolComplete(
  data: { toolCallId?: string; result?: string; error?: string; resultType?: string },
  eventId: string | undefined,
  state: TranslatorState
): PromptEvent {
  const toolCallId = data.toolCallId || eventId || "unknown";
  const toolName = state.toolNameMap.get(toolCallId) || "unknown";
  state.toolNameMap.delete(toolCallId);

  const startTime = state.toolStartTimes.get(toolCallId);
  state.toolStartTimes.delete(toolCallId);
  const durationMs = startTime ? Date.now() - startTime : undefined;

  const isError = data.resultType === "failure" || data.resultType === "rejected" || data.resultType === "denied";
  state.currentTextMessageId = null;

  return {
    type: "toolEnd",
    tool: toolName,
    toolUseId: toolCallId,
    result: isError ? undefined : data.result,
    error: isError ? (data.error || data.result) : undefined,
    ...(durationMs !== undefined && { durationMs }),
  };
}

/**
 * Translate assistant.usage event
 */
export function translateUsage(
  data: { inputTokens?: number; outputTokens?: number },
  state: TranslatorState
): null {
  state.lastUsage = {
    inputTokens: data.inputTokens || 0,
    outputTokens: data.outputTokens || 0,
  };
  return null;
}

/**
 * Translate session.error event
 */
export function translateSessionError(
  data: { message?: string; errorType?: string }
): PromptEvent {
  return {
    type: "error",
    message: data.message || data.errorType || "Unknown session error",
    retryable: false,
  };
}

/**
 * Translate session.idle to result event
 */
export function translateSessionIdle(state: TranslatorState): PromptEvent {
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
  event: CopilotEvent,
  state: TranslatorState
): PromptEvent | null {
  const data = event.data || {};

  switch (event.type) {
    case "assistant.message_delta":
      return translateMessageDelta(data as { deltaContent?: string; content?: string }, state);

    case "assistant.message":
      return translateMessage(data as { content?: string }, state);

    case "assistant.reasoning_delta":
      return translateReasoningDelta(data as { deltaContent?: string }, state);

    case "assistant.reasoning":
      return translateReasoning(state);

    case "tool.execution_start":
      return translateToolStart(
        data as { toolName?: string; toolCallId?: string; arguments?: Record<string, unknown> },
        event.id,
        state
      );

    case "tool.execution_complete":
      return translateToolComplete(
        data as { toolCallId?: string; result?: string; error?: string; resultType?: string },
        event.id,
        state
      );

    case "assistant.usage":
      return translateUsage(data as { inputTokens?: number; outputTokens?: number }, state);

    case "session.error":
      return translateSessionError(data as { message?: string; errorType?: string });

    default:
      return null;
  }
}
