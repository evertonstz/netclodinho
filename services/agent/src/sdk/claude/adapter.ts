/**
 * Claude Code SDK Adapter
 *
 * Wraps the @anthropic-ai/claude-agent-sdk integration
 */

import { query } from "@anthropic-ai/claude-agent-sdk";
import type { SDKAdapter, SDKConfig, PromptConfig, PromptEvent } from "../types.js";
import { getSdkSessionId, registerSession, isSessionInitialized, markSessionInitialized } from "../../services/session.js";
import { buildSystemPrompt } from "../../utils/system-prompt.js";
import { setupRepository } from "../../git.js";
import {
  createTranslatorState,
  resetTranslatorState,
  translateMessage,
  type TranslatorState,
  type ClaudeMessage,
} from "./translator.js";

const WORKSPACE_DIR = "/agent/workspace";

export class ClaudeSDKAdapter implements SDKAdapter {
  private config: SDKConfig | null = null;
  private interruptSignal = false;
  private abortController: AbortController | null = null;
  private currentGitRepo: string | null = null;
  private currentGithubToken: string | null = null;
  private translatorState: TranslatorState = createTranslatorState();

  async initialize(config: SDKConfig): Promise<void> {
    this.config = config;
    console.log("[claude-adapter] Initialized with workspace:", config.workspaceDir);
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

    // Reset translator state for new prompt
    resetTranslatorState(this.translatorState);

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

        // Translate the message using the translator
        // Cast to access SDK-specific properties that aren't in the base type
        const msg = message as Record<string, unknown>;
        const claudeMessage: ClaudeMessage = {
          type: message.type as ClaudeMessage["type"],
          subtype: msg.subtype as string | undefined,
          session_id: msg.session_id as string | undefined,
          num_turns: msg.num_turns as number | undefined,
          parent_tool_use_id: msg.parent_tool_use_id as string | undefined,
          message: msg.message as ClaudeMessage["message"],
          event: msg.event as ClaudeMessage["event"],
        };

        const result = translateMessage(claudeMessage, this.translatorState);

        // Handle session registration
        if (result.sessionId && sessionId && !getSdkSessionId(sessionId)) {
          registerSession(sessionId, result.sessionId);
        }

        // Yield all translated events
        for (const event of result.events) {
          yield event;
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
