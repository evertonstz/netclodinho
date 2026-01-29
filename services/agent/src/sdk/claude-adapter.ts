/**
 * Claude Code SDK Adapter
 *
 * Wraps the @anthropic-ai/claude-agent-sdk integration
 */

import { query } from "@anthropic-ai/claude-agent-sdk";
import type { JsonObject } from "@bufbuild/protobuf";
import type { SDKAdapter, SDKConfig, PromptConfig, PromptEvent } from "./types.js";
import { getSdkSessionId, registerSession, isSessionInitialized, markSessionInitialized } from "../services/session.js";
import { buildSystemPrompt } from "../utils/system-prompt.js";
import { setupRepository } from "../git.js";

const WORKSPACE_DIR = "/agent/workspace";

export class ClaudeSDKAdapter implements SDKAdapter {
  private config: SDKConfig | null = null;
  private interruptSignal = false;
  private abortController: AbortController | null = null;
  private currentGitRepo: string | null = null;
  private currentGithubToken: string | null = null;

  // Track tool names by toolUseId
  private toolNameMap = new Map<string, string>();
  private toolStartTimes = new Map<string, number>(); // Track tool start times for duration calculation
  private blockIndexToToolId = new Map<number, string>();
  private blockIndexToToolInput = new Map<number, string>();
  private blockIndexToThinkingId = new Map<string, string>();
  private blockIndexToThinkingContent = new Map<string, string>(); // Accumulate thinking content
  private currentParentToolUseId: string | null = null;
  private thinkingIdCounter = 0;
  // Track text blocks - each text block becomes a separate message with its own ID
  private currentTextMessageId: string | null = null;
  private textMessageIdCounter = 0;

  async initialize(config: SDKConfig): Promise<void> {
    this.config = config;
    console.log("[claude-adapter] Initialized with workspace:", config.workspaceDir);
  }

  private generateThinkingId(): string {
    return `thinking_${Date.now()}_${++this.thinkingIdCounter}`;
  }

  async *executePrompt(sessionId: string, text: string, promptConfig?: PromptConfig): AsyncGenerator<PromptEvent> {
    console.log(
      `[claude-adapter] ExecutePrompt (session=${sessionId}): "${text.slice(0, 100)}${text.length > 100 ? "..." : ""}"`
    );

    // Initialize repo for this session if config provided
    if (sessionId && promptConfig) {
      if (!isSessionInitialized(sessionId)) {
        console.log(`[claude-adapter] Initializing session ${sessionId}`);

        this.currentGitRepo = promptConfig.repo || null;
        this.currentGithubToken = promptConfig.githubToken || null;

        if (this.currentGitRepo) {
          yield { type: "repoClone", stage: "cloning", repo: this.currentGitRepo, message: "Cloning repository..." };

          try {
            await setupRepository(
              this.currentGitRepo,
              WORKSPACE_DIR,
              sessionId,
              this.currentGithubToken || undefined
            );
            yield { type: "repoClone", stage: "done", repo: this.currentGitRepo, message: "Repository cloned successfully" };
          } catch (error) {
            yield {
              type: "repoClone",
              stage: "error",
              repo: this.currentGitRepo,
              message: `Failed to clone: ${error instanceof Error ? error.message : String(error)}`,
            };
          }
        }

        markSessionInitialized(sessionId);
      }
    }

    // Reset streaming state
    this.blockIndexToToolId.clear();
    this.blockIndexToToolInput.clear();
    this.blockIndexToThinkingId.clear();
    this.currentParentToolUseId = null;
    this.currentTextMessageId = null;
    let textWasStreamed = false;
    const streamedThinkingIds = new Set<string>();

    // Look up the SDK session ID
    const sdkSessionId = sessionId ? getSdkSessionId(sessionId) : undefined;
    console.log(`[claude-adapter] SDK session lookup: ${sessionId} -> ${sdkSessionId || "(new session)"}`);

    // Clear any previous interrupt signal at start
    this.clearInterruptSignal();

    // Create an AbortController for this query
    this.abortController = new AbortController();

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
          systemPrompt: buildSystemPrompt({ currentGitRepo: this.currentGitRepo }),
          abortController: this.abortController,
          ...(sdkSessionId && { resume: sdkSessionId }),
        },
      });

      for await (const message of q) {
        // Check for interrupt signal at each iteration
        if (this.isInterrupted()) {
          console.log("[claude-adapter] Interrupted by user");
          yield { type: "system", message: "interrupted" };
          return;
        }
        const msgWithParent = message as { parent_tool_use_id?: string };
        if (msgWithParent.parent_tool_use_id !== undefined) {
          this.currentParentToolUseId = msgWithParent.parent_tool_use_id || null;
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
                  const alreadyEmitted = this.toolNameMap.has(block.id);
                  this.toolNameMap.set(block.id, block.name);
                  if (!alreadyEmitted) {
                    this.toolStartTimes.set(block.id, Date.now());
                    const toolInput = block.input as JsonObject | undefined;
                    yield {
                      type: "toolStart",
                      tool: block.name,
                      toolUseId: block.id,
                      ...(this.currentParentToolUseId && { parentToolUseId: this.currentParentToolUseId }),
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
                  const toolName = this.toolNameMap.get(block.tool_use_id) ?? "unknown";
                  this.toolNameMap.delete(block.tool_use_id);
                  const startTime = this.toolStartTimes.get(block.tool_use_id);
                  this.toolStartTimes.delete(block.tool_use_id);
                  const durationMs = startTime ? Date.now() - startTime : undefined;
                  const isError = block.is_error === true;
                  yield {
                    type: "toolEnd",
                    tool: toolName,
                    toolUseId: block.tool_use_id,
                    result: isError ? undefined : typeof block.content === "string" ? block.content : undefined,
                    error: isError ? (typeof block.content === "string" ? block.content : "Tool error") : undefined,
                    ...(this.currentParentToolUseId && { parentToolUseId: this.currentParentToolUseId }),
                    ...(durationMs !== undefined && { durationMs }),
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
                this.blockIndexToToolId.set(message.event.index, contentBlock.id);
                this.toolNameMap.set(contentBlock.id, contentBlock.name);
                this.toolStartTimes.set(contentBlock.id, Date.now());
                yield {
                  type: "toolStart",
                  tool: contentBlock.name,
                  toolUseId: contentBlock.id,
                  ...(this.currentParentToolUseId && { parentToolUseId: this.currentParentToolUseId }),
                };
              } else if (contentBlock?.type === "thinking") {
                const thinkingId = this.generateThinkingId();
                this.blockIndexToThinkingId.set(String(message.event.index), thinkingId);
              } else if (contentBlock?.type === "text") {
                // Each text block becomes a separate message with its own ID
                this.currentTextMessageId = `msg_${Date.now()}_${++this.textMessageIdCounter}`;
              }
            } else if (message.event.type === "content_block_delta") {
              const delta = message.event.delta;
              if (delta && "text" in delta) {
                textWasStreamed = true;
                yield {
                  type: "textDelta",
                  content: delta.text,
                  partial: true,
                  ...(this.currentTextMessageId && { messageId: this.currentTextMessageId }),
                };
              } else if (delta && "partial_json" in delta) {
                const toolUseId = this.blockIndexToToolId.get(message.event.index);
                if (toolUseId) {
                  // Accumulate the partial JSON
                  const existing = this.blockIndexToToolInput.get(message.event.index) || "";
                  this.blockIndexToToolInput.set(message.event.index, existing + delta.partial_json);

                  yield {
                    type: "toolInput",
                    toolUseId,
                    inputDelta: delta.partial_json,
                    ...(this.currentParentToolUseId && { parentToolUseId: this.currentParentToolUseId }),
                  };
                }
              } else if (delta && "thinking" in delta) {
                const thinkingDelta = delta as { type: "thinking_delta"; thinking: string };
                const thinkingId = this.blockIndexToThinkingId.get(String(message.event.index));
                if (thinkingId) {
                  streamedThinkingIds.add(thinkingId);
                  // Accumulate the thinking content
                  const existing = this.blockIndexToThinkingContent.get(String(message.event.index)) || "";
                  this.blockIndexToThinkingContent.set(String(message.event.index), existing + thinkingDelta.thinking);
                  yield { type: "thinking", thinkingId, content: thinkingDelta.thinking, partial: true };
                }
              }
            } else if (message.event.type === "content_block_stop") {
              // Handle thinking block completion - emit accumulated full content
              const thinkingId = this.blockIndexToThinkingId.get(String(message.event.index));
              const accumulatedThinking = this.blockIndexToThinkingContent.get(String(message.event.index));
              if (thinkingId && streamedThinkingIds.has(thinkingId)) {
                yield { type: "thinking", thinkingId, content: accumulatedThinking || "", partial: false };
              }

              // Handle tool input completion - emit accumulated input
              const toolUseId = this.blockIndexToToolId.get(message.event.index);
              const accumulatedInput = this.blockIndexToToolInput.get(message.event.index);
              if (toolUseId) {
                let parsedInput: JsonObject = {};
                if (accumulatedInput) {
                  try {
                    parsedInput = JSON.parse(accumulatedInput) as JsonObject;
                  } catch {
                    // If JSON parsing fails, wrap raw input as fallback so clients still get the event
                    console.warn(`[claude-adapter] Failed to parse tool input JSON for ${toolUseId}, using raw fallback`);
                    parsedInput = { _raw: accumulatedInput };
                  }
                }
                yield {
                  type: "toolInputComplete",
                  toolUseId,
                  input: parsedInput,
                  ...(this.currentParentToolUseId && { parentToolUseId: this.currentParentToolUseId }),
                };
              }

              // Cleanup
              this.blockIndexToToolId.delete(message.event.index);
              this.blockIndexToToolInput.delete(message.event.index);
              this.blockIndexToThinkingId.delete(String(message.event.index));
              this.blockIndexToThinkingContent.delete(String(message.event.index));
            }
            break;
        }
      }
    } catch (error) {
      console.error("[claude-adapter] Error during prompt:", error);
      yield { type: "error", message: String(error), retryable: false };
    }
  }

  setInterruptSignal(): void {
    this.interruptSignal = true;
    // Abort the query to cancel in-flight API calls and tool executions
    if (this.abortController) {
      this.abortController.abort();
      console.log("[claude-adapter] Interrupt signal set and query aborted");
    } else {
      console.log("[claude-adapter] Interrupt signal set");
    }
  }

  clearInterruptSignal(): void {
    this.interruptSignal = false;
    this.abortController = null;
  }

  isInterrupted(): boolean {
    return this.interruptSignal;
  }

  getCurrentGitRepo(): string | null {
    return this.currentGitRepo;
  }

  async shutdown(): Promise<void> {
    console.log("[claude-adapter] Shutdown");
    // Claude SDK doesn't need explicit cleanup
  }
}
