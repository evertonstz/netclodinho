/**
 * Prompt execution service - handles Claude SDK interaction and event streaming
 */

import { query } from "@anthropic-ai/claude-agent-sdk";
import type { JsonObject } from "@bufbuild/protobuf";
import { getSdkSessionId, registerSession, isSessionInitialized, markSessionInitialized } from "./session.js";
import { buildSystemPrompt } from "../utils/system-prompt.js";
import { setupRepository } from "../git.js";

const WORKSPACE_DIR = "/agent/workspace";

// Track tool names by toolUseId for matching tool results
const toolNameMap = new Map<string, string>();

// Track current content block index to toolUseId mapping for streaming
const blockIndexToToolId = new Map<number, string>();

// Track accumulated tool input JSON by block index
const blockIndexToToolInput = new Map<number, string>();

// Track current content block index to thinkingId mapping for streaming thinking
const blockIndexToThinkingId = new Map<string, string>();

// Track current parent tool use ID (set when running inside a Task/subagent)
let currentParentToolUseId: string | null = null;

// Generate unique thinking IDs
let thinkingIdCounter = 0;
function generateThinkingId(): string {
  return `thinking_${Date.now()}_${++thinkingIdCounter}`;
}

// State for repository initialization
let currentGitRepo: string | null = null;
let currentGithubToken: string | null = null;

export function getCurrentGitRepo(): string | null {
  return currentGitRepo;
}

/**
 * Event types emitted during prompt execution
 */
export type PromptEvent =
  | { type: "system"; message: string }
  | { type: "textDelta"; content: string; partial: boolean }
  | { type: "toolStart"; tool: string; toolUseId: string; parentToolUseId?: string; input?: JsonObject }
  | { type: "toolInput"; toolUseId: string; inputDelta: string; parentToolUseId?: string }
  | { type: "toolInputComplete"; toolUseId: string; parentToolUseId?: string; input: JsonObject }
  | { type: "toolEnd"; tool: string; toolUseId: string; result?: string; error?: string; parentToolUseId?: string }
  | { type: "thinking"; thinkingId: string; content: string; partial: boolean }
  | { type: "repoClone"; stage: "cloning" | "done" | "error"; repo: string; message: string }
  | { type: "result"; inputTokens: number; outputTokens: number; totalTurns: number }
  | { type: "error"; message: string; retryable: boolean };

export interface PromptConfig {
  repo?: string;
  githubToken?: string;
}

/**
 * Execute a prompt and yield events as they occur
 */
export async function* executePrompt(
  sessionId: string,
  text: string,
  config?: PromptConfig
): AsyncGenerator<PromptEvent> {
  console.log(`[prompt] ExecutePrompt (session=${sessionId}): "${text.slice(0, 100)}${text.length > 100 ? "..." : ""}"`);

  // Initialize repo for this session if config provided
  if (sessionId && config) {
    if (!isSessionInitialized(sessionId)) {
      console.log(`[prompt] Initializing session ${sessionId}`);

      currentGitRepo = config.repo || null;
      currentGithubToken = config.githubToken || null;

      if (currentGitRepo) {
        // Send clone start event
        yield { type: "repoClone", stage: "cloning", repo: currentGitRepo, message: "Cloning repository..." };

        try {
          await setupRepository(currentGitRepo, WORKSPACE_DIR, sessionId, currentGithubToken || undefined);
          yield { type: "repoClone", stage: "done", repo: currentGitRepo, message: "Repository cloned successfully" };
        } catch (error) {
          yield {
            type: "repoClone",
            stage: "error",
            repo: currentGitRepo,
            message: `Failed to clone: ${error instanceof Error ? error.message : String(error)}`,
          };
        }
      }

      markSessionInitialized(sessionId);
    }
  }

  // Reset streaming state
  blockIndexToToolId.clear();
  blockIndexToToolInput.clear();
  blockIndexToThinkingId.clear();
  currentParentToolUseId = null;
  let textWasStreamed = false;
  const streamedThinkingIds = new Set<string>();

  // Look up the SDK session ID
  const sdkSessionId = sessionId ? getSdkSessionId(sessionId) : undefined;
  console.log(`[prompt] SDK session lookup: ${sessionId} -> ${sdkSessionId || "(new session)"}`);

  try {
    const q = query({
      prompt: text,
      options: {
        cwd: WORKSPACE_DIR,
        permissionMode: "bypassPermissions",
        allowDangerouslySkipPermissions: true,
        model: "claude-opus-4-5-20251101",
        persistSession: true,
        includePartialMessages: true,
        maxThinkingTokens: 10000,
        systemPrompt: buildSystemPrompt({ currentGitRepo }),
        ...(sdkSessionId && { resume: sdkSessionId }),
      },
    });

    for await (const message of q) {
      const msgWithParent = message as { parent_tool_use_id?: string };
      if (msgWithParent.parent_tool_use_id !== undefined) {
        currentParentToolUseId = msgWithParent.parent_tool_use_id || null;
      }

      switch (message.type) {
        case "system":
          if (message.subtype === "init" && sessionId && message.session_id) {
            if (!getSdkSessionId(sessionId)) {
              registerSession(sessionId, message.session_id);
            }
          }
          yield { type: "system", message: message.subtype || "" };
          break;

        case "assistant":
          if (message.message?.content) {
            for (const block of message.message.content) {
              if (block.type === "text") {
                yield { type: "textDelta", content: textWasStreamed ? "" : block.text, partial: false };
                textWasStreamed = false;
              } else if (block.type === "tool_use") {
                const alreadyEmitted = toolNameMap.has(block.id);
                toolNameMap.set(block.id, block.name);
                if (!alreadyEmitted) {
                  const toolInput = block.input as JsonObject | undefined;
                  yield {
                    type: "toolStart",
                    tool: block.name,
                    toolUseId: block.id,
                    ...(currentParentToolUseId && { parentToolUseId: currentParentToolUseId }),
                    ...(toolInput && Object.keys(toolInput).length > 0 && { input: toolInput }),
                  };
                }
              }
            }
          }
          break;

        case "user":
          if (message.message?.content && Array.isArray(message.message.content)) {
            for (const block of message.message.content) {
              if (typeof block === "object" && block.type === "tool_result") {
                const toolName = toolNameMap.get(block.tool_use_id) ?? "unknown";
                toolNameMap.delete(block.tool_use_id);
                const isError = block.is_error === true;
                yield {
                  type: "toolEnd",
                  tool: toolName,
                  toolUseId: block.tool_use_id,
                  result: isError ? undefined : typeof block.content === "string" ? block.content : undefined,
                  error: isError ? (typeof block.content === "string" ? block.content : "Tool error") : undefined,
                  ...(currentParentToolUseId && { parentToolUseId: currentParentToolUseId }),
                };
              }
            }
          }
          break;

        case "result":
          if (message.subtype === "success") {
            yield { type: "result", inputTokens: 0, outputTokens: 0, totalTurns: message.num_turns || 0 };
          }
          break;

        case "stream_event":
          if (message.event.type === "content_block_start") {
            const contentBlock = message.event.content_block;
            if (contentBlock?.type === "tool_use") {
              blockIndexToToolId.set(message.event.index, contentBlock.id);
              toolNameMap.set(contentBlock.id, contentBlock.name);
              yield {
                type: "toolStart",
                tool: contentBlock.name,
                toolUseId: contentBlock.id,
                ...(currentParentToolUseId && { parentToolUseId: currentParentToolUseId }),
              };
            } else if (contentBlock?.type === "thinking") {
              const thinkingId = generateThinkingId();
              blockIndexToThinkingId.set(String(message.event.index), thinkingId);
            }
          } else if (message.event.type === "content_block_delta") {
            const delta = message.event.delta;
            if (delta && "text" in delta) {
              textWasStreamed = true;
              yield { type: "textDelta", content: delta.text, partial: true };
            } else if (delta && "partial_json" in delta) {
              const toolUseId = blockIndexToToolId.get(message.event.index);
              if (toolUseId) {
                // Accumulate the partial JSON
                const existing = blockIndexToToolInput.get(message.event.index) || "";
                blockIndexToToolInput.set(message.event.index, existing + delta.partial_json);
                
                yield {
                  type: "toolInput",
                  toolUseId,
                  inputDelta: delta.partial_json,
                  ...(currentParentToolUseId && { parentToolUseId: currentParentToolUseId }),
                };
              }
            } else if (delta && "thinking" in delta) {
              const thinkingDelta = delta as { type: "thinking_delta"; thinking: string };
              const thinkingId = blockIndexToThinkingId.get(String(message.event.index));
              if (thinkingId) {
                streamedThinkingIds.add(thinkingId);
                yield { type: "thinking", thinkingId, content: thinkingDelta.thinking, partial: true };
              }
            }
          } else if (message.event.type === "content_block_stop") {
            // Handle thinking block completion
            const thinkingId = blockIndexToThinkingId.get(String(message.event.index));
            if (thinkingId && streamedThinkingIds.has(thinkingId)) {
              yield { type: "thinking", thinkingId, content: "", partial: false };
            }
            
            // Handle tool input completion - emit accumulated input
            const toolUseId = blockIndexToToolId.get(message.event.index);
            const accumulatedInput = blockIndexToToolInput.get(message.event.index);
            if (toolUseId) {
              let parsedInput: JsonObject = {};
              if (accumulatedInput) {
                try {
                  parsedInput = JSON.parse(accumulatedInput) as JsonObject;
                } catch {
                  // If JSON parsing fails, wrap raw input as fallback so clients still get the event
                  console.warn(`[prompt] Failed to parse tool input JSON for ${toolUseId}, using raw fallback`);
                  parsedInput = { _raw: accumulatedInput };
                }
              }
              yield {
                type: "toolInputComplete",
                toolUseId,
                input: parsedInput,
                ...(currentParentToolUseId && { parentToolUseId: currentParentToolUseId }),
              };
            }
            
            // Cleanup
            blockIndexToToolId.delete(message.event.index);
            blockIndexToToolInput.delete(message.event.index);
            blockIndexToThinkingId.delete(String(message.event.index));
          }
          break;
      }
    }
  } catch (error) {
    console.error("[prompt] Error during prompt:", error);
    yield { type: "error", message: String(error), retryable: false };
  }
}
