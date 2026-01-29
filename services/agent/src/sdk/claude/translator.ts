/**
 * Claude Code SDK event translator
 *
 * Translates Claude SDK messages to our common PromptEvent format.
 */

import type { JsonObject } from "@bufbuild/protobuf";
import type { PromptEvent } from "../types.js";

/**
 * Content block types from Claude SDK
 */
export interface TextBlock {
  type: "text";
  text: string;
}

export interface ToolUseBlock {
  type: "tool_use";
  id: string;
  name: string;
  input: Record<string, unknown>;
}

export interface ToolResultBlock {
  type: "tool_result";
  tool_use_id: string;
  content: string | unknown;
  is_error?: boolean;
}

export type ContentBlock = TextBlock | ToolUseBlock | ToolResultBlock | { type: string };

/**
 * Stream event delta types
 */
export interface TextDelta {
  type: "text_delta";
  text: string;
}

export interface PartialJsonDelta {
  type: "input_json_delta";
  partial_json: string;
}

export interface ThinkingDelta {
  type: "thinking_delta";
  thinking: string;
}

export type StreamDelta = TextDelta | PartialJsonDelta | ThinkingDelta | { type: string };

/**
 * State tracked during event translation
 */
export interface TranslatorState {
  toolNameMap: Map<string, string>;
  toolStartTimes: Map<string, number>;
  blockIndexToToolId: Map<number, string>;
  blockIndexToToolInput: Map<number, string>;
  blockIndexToThinkingId: Map<string, string>;
  blockIndexToThinkingContent: Map<string, string>;
  streamedThinkingIds: Set<string>;
  currentParentToolUseId: string | null;
  currentTextMessageId: string | null;
  textMessageIdCounter: number;
  thinkingIdCounter: number;
  textWasStreamed: boolean;
}

/**
 * Create initial translator state
 */
export function createTranslatorState(): TranslatorState {
  return {
    toolNameMap: new Map(),
    toolStartTimes: new Map(),
    blockIndexToToolId: new Map(),
    blockIndexToToolInput: new Map(),
    blockIndexToThinkingId: new Map(),
    blockIndexToThinkingContent: new Map(),
    streamedThinkingIds: new Set(),
    currentParentToolUseId: null,
    currentTextMessageId: null,
    textMessageIdCounter: 0,
    thinkingIdCounter: 0,
    textWasStreamed: false,
  };
}

/**
 * Reset translator state for a new prompt
 */
export function resetTranslatorState(state: TranslatorState): void {
  state.blockIndexToToolId.clear();
  state.blockIndexToToolInput.clear();
  state.blockIndexToThinkingId.clear();
  state.blockIndexToThinkingContent.clear();
  state.streamedThinkingIds.clear();
  state.currentParentToolUseId = null;
  state.currentTextMessageId = null;
  state.textWasStreamed = false;
}

/**
 * Generate a unique thinking ID
 */
export function generateThinkingId(state: TranslatorState): string {
  return `thinking_${Date.now()}_${++state.thinkingIdCounter}`;
}

/**
 * Generate a unique message ID
 */
export function generateMessageId(state: TranslatorState): string {
  return `msg_${Date.now()}_${++state.textMessageIdCounter}`;
}

/**
 * Translate text block from assistant message
 */
export function translateTextBlock(
  block: TextBlock,
  state: TranslatorState
): PromptEvent {
  const content = state.textWasStreamed ? "" : block.text;
  state.textWasStreamed = false;
  return {
    type: "textDelta",
    content,
    partial: false,
  };
}

/**
 * Translate tool_use block to toolStart event
 */
export function translateToolUseBlock(
  block: ToolUseBlock,
  state: TranslatorState
): PromptEvent | null {
  const alreadyEmitted = state.toolNameMap.has(block.id);
  state.toolNameMap.set(block.id, block.name);

  if (alreadyEmitted) return null;

  state.toolStartTimes.set(block.id, Date.now());
  const toolInput = block.input as JsonObject | undefined;

  return {
    type: "toolStart",
    tool: block.name,
    toolUseId: block.id,
    ...(state.currentParentToolUseId && { parentToolUseId: state.currentParentToolUseId }),
    ...(toolInput && Object.keys(toolInput).length > 0 && { input: toolInput }),
  };
}

/**
 * Translate tool_result block to toolEnd event
 */
export function translateToolResultBlock(
  block: ToolResultBlock,
  state: TranslatorState
): PromptEvent {
  const toolName = state.toolNameMap.get(block.tool_use_id) ?? "unknown";
  state.toolNameMap.delete(block.tool_use_id);

  const startTime = state.toolStartTimes.get(block.tool_use_id);
  state.toolStartTimes.delete(block.tool_use_id);
  const durationMs = startTime ? Date.now() - startTime : undefined;

  const isError = block.is_error === true;
  const content = typeof block.content === "string" ? block.content : undefined;

  return {
    type: "toolEnd",
    tool: toolName,
    toolUseId: block.tool_use_id,
    result: isError ? undefined : content,
    error: isError ? (content || "Tool error") : undefined,
    ...(state.currentParentToolUseId && { parentToolUseId: state.currentParentToolUseId }),
    ...(durationMs !== undefined && { durationMs }),
  };
}

/**
 * Translate content_block_start for tool_use
 */
export function translateToolBlockStart(
  index: number,
  id: string,
  name: string,
  state: TranslatorState
): PromptEvent {
  state.blockIndexToToolId.set(index, id);
  state.toolNameMap.set(id, name);
  state.toolStartTimes.set(id, Date.now());

  return {
    type: "toolStart",
    tool: name,
    toolUseId: id,
    ...(state.currentParentToolUseId && { parentToolUseId: state.currentParentToolUseId }),
  };
}

/**
 * Translate content_block_start for thinking
 */
export function translateThinkingBlockStart(
  index: number,
  state: TranslatorState
): string {
  const thinkingId = generateThinkingId(state);
  state.blockIndexToThinkingId.set(String(index), thinkingId);
  return thinkingId;
}

/**
 * Translate content_block_start for text
 */
export function translateTextBlockStart(state: TranslatorState): string {
  state.currentTextMessageId = generateMessageId(state);
  return state.currentTextMessageId;
}

/**
 * Translate text delta in stream
 */
export function translateTextDelta(
  text: string,
  state: TranslatorState
): PromptEvent {
  state.textWasStreamed = true;
  return {
    type: "textDelta",
    content: text,
    partial: true,
    ...(state.currentTextMessageId && { messageId: state.currentTextMessageId }),
  };
}

/**
 * Translate partial JSON delta (tool input)
 */
export function translatePartialJsonDelta(
  index: number,
  partialJson: string,
  state: TranslatorState
): PromptEvent | null {
  const toolUseId = state.blockIndexToToolId.get(index);
  if (!toolUseId) return null;

  const existing = state.blockIndexToToolInput.get(index) || "";
  state.blockIndexToToolInput.set(index, existing + partialJson);

  return {
    type: "toolInput",
    toolUseId,
    inputDelta: partialJson,
    ...(state.currentParentToolUseId && { parentToolUseId: state.currentParentToolUseId }),
  };
}

/**
 * Translate thinking delta
 */
export function translateThinkingDelta(
  index: number,
  thinking: string,
  state: TranslatorState
): PromptEvent | null {
  const thinkingId = state.blockIndexToThinkingId.get(String(index));
  if (!thinkingId) return null;

  state.streamedThinkingIds.add(thinkingId);
  const existing = state.blockIndexToThinkingContent.get(String(index)) || "";
  state.blockIndexToThinkingContent.set(String(index), existing + thinking);

  return {
    type: "thinking",
    thinkingId,
    content: thinking,
    partial: true,
  };
}

/**
 * Translate content_block_stop for thinking
 */
export function translateThinkingBlockStop(
  index: number,
  state: TranslatorState
): PromptEvent | null {
  const thinkingId = state.blockIndexToThinkingId.get(String(index));
  const accumulatedThinking = state.blockIndexToThinkingContent.get(String(index));

  if (!thinkingId || !state.streamedThinkingIds.has(thinkingId)) return null;

  state.blockIndexToThinkingId.delete(String(index));
  state.blockIndexToThinkingContent.delete(String(index));

  return {
    type: "thinking",
    thinkingId,
    content: accumulatedThinking || "",
    partial: false,
  };
}

/**
 * Translate content_block_stop for tool input
 */
export function translateToolInputBlockStop(
  index: number,
  state: TranslatorState
): PromptEvent | null {
  const toolUseId = state.blockIndexToToolId.get(index);
  const accumulatedInput = state.blockIndexToToolInput.get(index);

  if (!toolUseId) return null;

  state.blockIndexToToolId.delete(index);
  state.blockIndexToToolInput.delete(index);

  let parsedInput: JsonObject = {};
  if (accumulatedInput) {
    try {
      parsedInput = JSON.parse(accumulatedInput) as JsonObject;
    } catch {
      parsedInput = { _raw: accumulatedInput };
    }
  }

  return {
    type: "toolInputComplete",
    toolUseId,
    input: parsedInput,
    ...(state.currentParentToolUseId && { parentToolUseId: state.currentParentToolUseId }),
  };
}

/**
 * Parse tool input JSON safely
 */
export function parseToolInput(jsonString: string | undefined): JsonObject {
  if (!jsonString) return {};
  try {
    return JSON.parse(jsonString) as JsonObject;
  } catch {
    return { _raw: jsonString };
  }
}

// ============================================================================
// Message Types and Main Translator Function
// ============================================================================

/**
 * Stream event types from Claude SDK
 */
export interface ContentBlockStartEvent {
  type: "content_block_start";
  index: number;
  content_block?: {
    type: string;
    id?: string;
    name?: string;
  };
}

export interface ContentBlockDeltaEvent {
  type: "content_block_delta";
  index: number;
  delta?: {
    type?: string;
    text?: string;
    partial_json?: string;
    thinking?: string;
  };
}

export interface ContentBlockStopEvent {
  type: "content_block_stop";
  index: number;
}

export type StreamEvent = ContentBlockStartEvent | ContentBlockDeltaEvent | ContentBlockStopEvent;

/**
 * Claude SDK message structure
 */
export interface ClaudeMessage {
  type: "system" | "assistant" | "user" | "result" | "stream_event";
  subtype?: string;
  session_id?: string;
  num_turns?: number;
  parent_tool_use_id?: string;
  message?: {
    content?: ContentBlock[];
  };
  event?: StreamEvent;
}

/**
 * Result from translateMessage - includes events and state updates
 */
export interface TranslateResult {
  events: PromptEvent[];
  sessionId?: string;  // Set when system.init message provides session_id
}

/**
 * Translate an assistant message with content blocks
 */
export function translateAssistantMessage(
  content: ContentBlock[],
  state: TranslatorState
): PromptEvent[] {
  const events: PromptEvent[] = [];

  for (const block of content) {
    if (block.type === "text") {
      events.push(translateTextBlock(block as TextBlock, state));
    } else if (block.type === "tool_use") {
      const event = translateToolUseBlock(block as ToolUseBlock, state);
      if (event) events.push(event);
    }
  }

  return events;
}

/**
 * Translate a user message with tool results
 */
export function translateUserMessage(
  content: ContentBlock[],
  state: TranslatorState
): PromptEvent[] {
  const events: PromptEvent[] = [];

  for (const block of content) {
    if (typeof block === "object" && block.type === "tool_result") {
      events.push(translateToolResultBlock(block as ToolResultBlock, state));
    }
  }

  return events;
}

/**
 * Translate a stream event (content_block_start/delta/stop)
 */
export function translateStreamEvent(
  event: StreamEvent,
  state: TranslatorState
): PromptEvent[] {
  const events: PromptEvent[] = [];

  switch (event.type) {
    case "content_block_start": {
      const contentBlock = event.content_block;
      if (contentBlock?.type === "tool_use" && contentBlock.id && contentBlock.name) {
        events.push(translateToolBlockStart(event.index, contentBlock.id, contentBlock.name, state));
      } else if (contentBlock?.type === "thinking") {
        translateThinkingBlockStart(event.index, state);
        // No event emitted for thinking start, just tracking
      } else if (contentBlock?.type === "text") {
        translateTextBlockStart(state);
        // No event emitted for text start, just tracking
      }
      break;
    }

    case "content_block_delta": {
      const delta = event.delta;
      if (delta && "text" in delta && delta.text) {
        events.push(translateTextDelta(delta.text, state));
      } else if (delta && "partial_json" in delta && delta.partial_json) {
        const partialEvent = translatePartialJsonDelta(event.index, delta.partial_json, state);
        if (partialEvent) events.push(partialEvent);
      } else if (delta && "thinking" in delta && delta.thinking) {
        const thinkingEvent = translateThinkingDelta(event.index, delta.thinking, state);
        if (thinkingEvent) events.push(thinkingEvent);
      }
      break;
    }

    case "content_block_stop": {
      // Handle thinking block completion
      const thinkingEvent = translateThinkingBlockStop(event.index, state);
      if (thinkingEvent) events.push(thinkingEvent);

      // Handle tool input completion
      const toolEvent = translateToolInputBlockStop(event.index, state);
      if (toolEvent) events.push(toolEvent);
      break;
    }
  }

  return events;
}

/**
 * Main message translator function
 *
 * Translates Claude SDK messages to PromptEvent arrays.
 * Call this for each message received from the Claude SDK.
 */
export function translateMessage(
  message: ClaudeMessage,
  state: TranslatorState
): TranslateResult {
  const events: PromptEvent[] = [];
  let sessionId: string | undefined;

  // Track parent tool use ID for nested tool calls
  if (message.parent_tool_use_id !== undefined) {
    state.currentParentToolUseId = message.parent_tool_use_id || null;
  }

  switch (message.type) {
    case "system":
      if (message.subtype === "init" && message.session_id) {
        sessionId = message.session_id;
      }
      events.push({ type: "system", message: message.subtype || "" });
      break;

    case "assistant":
      if (message.message?.content) {
        events.push(...translateAssistantMessage(message.message.content, state));
      }
      break;

    case "user":
      if (message.message?.content && Array.isArray(message.message.content)) {
        events.push(...translateUserMessage(message.message.content, state));
      }
      break;

    case "result":
      if (message.subtype === "success") {
        events.push({
          type: "result",
          inputTokens: 0,
          outputTokens: 0,
          totalTurns: message.num_turns || 0,
        });
      }
      break;

    case "stream_event":
      if (message.event) {
        events.push(...translateStreamEvent(message.event, state));
      }
      break;
  }

  return { events, sessionId };
}
