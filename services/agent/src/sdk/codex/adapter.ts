/**
 * OpenAI Codex backend
 *
 * Uses @openai/codex-sdk to communicate with OpenAI's Codex agent.
 *
 * ## Authentication
 *
 * The Codex SDK supports two authentication modes:
 *
 * 1. **API Key Mode** (default)
 *    - Uses OPENAI_API_KEY environment variable
 *    - Standard OpenAI API authentication
 *
 * 2. **ChatGPT OAuth Mode**
 *    - Uses OAuth tokens from ChatGPT login
 *    - Tokens written to ~/.codex/auth.json
 *    - Allows using ChatGPT subscription for Codex
 */

import { Codex, type Thread, type ThreadEvent, type ModelReasoningEffort } from "@openai/codex-sdk";
import type { NetclodePromptBackend, SDKConfig, PromptConfig, PromptEvent } from "../types.js";
import { createAgentCapabilities } from "../types.js";
import { CodexAuthMaterializer, type BackendAuthMaterializer } from "../auth-materializer.js";
import {
  createTranslatorState,
  resetTranslatorState,
  translateEvent,
  storeUsage,
  createResultEvent,
  type TranslatorState,
  type CodexEvent,
} from "./translator.js";
import { getSdkSessionId, registerSession } from "../../services/session.js";
import * as fs from "node:fs/promises";
import * as path from "node:path";
import * as os from "node:os";
import { WORKSPACE_DIR } from "../../constants.js";
import { buildSystemPromptText } from "../../utils/system-prompt.js";
import { isCodexOAuthMode } from "../secret-materialization.js";

export class CodexAdapter implements NetclodePromptBackend {
  readonly capabilities = createAgentCapabilities({
    interrupt: true,
    toolStreaming: true,
    thinkingStreaming: false,
  });

  constructor(
    private readonly authMaterializer: BackendAuthMaterializer = new CodexAuthMaterializer("codex-backend"),
  ) {}

  private config: SDKConfig | null = null;
  private codex: Codex | null = null;
  private thread: Thread | null = null;
  private interruptSignal = false;
  private translatorState: TranslatorState = createTranslatorState();

  // Cleaned model name (without :api/:oauth/:effort suffixes)
  private cleanedModel: string | undefined = undefined;

  // Reasoning effort level (low, medium, high, minimal, xhigh)
  private reasoningEffort: string | undefined = undefined;

  async initialize(config: SDKConfig): Promise<void> {
    this.config = config;

    // Strip :api/:oauth and :effort suffixes from model
    // Format: model:auth:effort (e.g., gpt-5-codex:oauth:high)
    this.cleanedModel = config.model?.replace(/:(api|oauth)(:(low|medium|high|minimal|xhigh))?$/, "");
    this.reasoningEffort = config.reasoningEffort;

    // Determine auth mode from model suffix or available credentials
    const modelHasApiSuffix = config.model?.includes(":api");
    const isOAuthMode = isCodexOAuthMode(config);
    const isApiMode = modelHasApiSuffix || (!isOAuthMode && Boolean(config.openaiApiKey));

    console.log("[codex-adapter] Initializing");
    console.log("[codex-adapter] Model:", this.cleanedModel || "default");
    console.log("[codex-adapter] Auth mode:", isApiMode ? "API key" : isOAuthMode ? "OAuth" : "unknown");
    console.log("[codex-adapter] Reasoning effort:", this.reasoningEffort || "default");
    await this.authMaterializer.materialize(config);

    // Build clean env object without undefined values
    const buildEnv = (overrides: Record<string, string | undefined> = {}): Record<string, string> => {
      const env: Record<string, string> = {};
      for (const [key, value] of Object.entries(process.env)) {
        if (value !== undefined) {
          env[key] = value;
        }
      }
      for (const [key, value] of Object.entries(overrides)) {
        if (value !== undefined) {
          env[key] = value;
        } else {
          delete env[key];
        }
      }
      return env;
    };

    // Determine which credentials to use based on auth mode
    if (isOAuthMode && config.codexAccessToken && config.codexIdToken) {
      console.log("[codex-adapter] Using OAuth authentication (ChatGPT subscription)");

      this.codex = new Codex({
        // For OAuth, don't pass apiKey - let it use auth.json
        // Remove any OPENAI_API_KEY to force OAuth
        env: buildEnv({ OPENAI_API_KEY: undefined }),
      });
    } else if (isApiMode && config.openaiApiKey) {
      // API key mode: use OPENAI_API_KEY
      console.log("[codex-adapter] Using API key authentication");

      this.codex = new Codex({
        apiKey: config.openaiApiKey,
        env: buildEnv({ OPENAI_API_KEY: config.openaiApiKey }),
      });
    } else {
      // Fallback: use environment variable
      console.log("[codex-adapter] Using environment OPENAI_API_KEY");

      this.codex = new Codex({
        env: buildEnv(),
      });
    }

    console.log("[codex-adapter] Client created");
  }

  /**
   * Write global AGENTS.md to ~/.codex/ with system prompt
   * Codex reads this file for global instructions
   */
  private async writeGlobalAgentsMd(currentGitRepos: string[]): Promise<void> {
    const systemPromptText = buildSystemPromptText({ currentGitRepos });
    const codexHome = process.env.CODEX_HOME || path.join(os.homedir(), ".codex");

    try {
      await fs.mkdir(codexHome, { recursive: true });
      const agentsMdPath = path.join(codexHome, "AGENTS.md");
      await fs.writeFile(agentsMdPath, systemPromptText, "utf-8");
      console.log("[codex-adapter] Wrote global AGENTS.md to", agentsMdPath);
    } catch (error) {
      console.error("[codex-adapter] Failed to write global AGENTS.md:", error);
    }
  }

  async *executePrompt(sessionId: string, text: string, promptConfig?: PromptConfig): AsyncGenerator<PromptEvent> {
    if (!this.codex) {
      throw new Error("Codex client not initialized");
    }

    // Reset translator state for new prompt
    resetTranslatorState(this.translatorState);

    console.log(
      `[codex-adapter] ExecutePrompt (session=${sessionId}): "${text.slice(0, 100)}${text.length > 100 ? "..." : ""}"`
    );

    // Write global AGENTS.md with system prompt (includes repo info when available)
    const currentGitRepos = promptConfig?.repos?.filter(Boolean) ?? [];
    await this.writeGlobalAgentsMd(currentGitRepos);

    // Clear interrupt signal
    this.clearInterruptSignal();

    // Get or create Codex thread (persisted mapping survives pod restarts)
    const existingThreadId = getSdkSessionId(sessionId);

    try {
      if (existingThreadId) {
        console.log(`[codex-adapter] Resuming Codex thread: ${existingThreadId}`);
        this.thread = this.codex.resumeThread(existingThreadId, {
          workingDirectory: WORKSPACE_DIR,
          sandboxMode: "danger-full-access",
          approvalPolicy: "never",
          model: this.cleanedModel,
          modelReasoningEffort: this.reasoningEffort as ModelReasoningEffort,
          skipGitRepoCheck: true, // We handle git setup ourselves
        });
      } else {
        console.log(`[codex-adapter] Creating new Codex thread`);
        this.thread = this.codex.startThread({
          workingDirectory: WORKSPACE_DIR,
          sandboxMode: "danger-full-access",
          approvalPolicy: "never",
          model: this.cleanedModel,
          modelReasoningEffort: this.reasoningEffort as ModelReasoningEffort,
          skipGitRepoCheck: true, // We handle git setup ourselves
        });
      }
    } catch (error) {
      console.error("[codex-adapter] Failed to create/resume thread:", error);
      yield {
        type: "error",
        message: `Failed to create thread: ${error instanceof Error ? error.message : String(error)}`,
        retryable: true,
      };
      return;
    }

    try {
      // Run the prompt with streaming
      const { events } = await this.thread.runStreamed(text);

      for await (const event of events) {
        if (this.interruptSignal) {
          yield { type: "system", message: "interrupted" };
          return;
        }

        // Capture thread ID from first event and persist the mapping
        if (event.type === "thread.started" && this.thread.id) {
          registerSession(sessionId, this.thread.id);
        }

        // Track usage from turn.completed
        if (event.type === "turn.completed") {
          storeUsage(event.usage, this.translatorState);
        }

        // Translate and yield events using the translator
        const codexEvent: CodexEvent = {
          type: event.type,
          item: "item" in event ? event.item : undefined,
          error: "error" in event ? event.error : undefined,
          message: "message" in event ? event.message : undefined,
          usage: "usage" in event ? event.usage : undefined,
        };
        const promptEvents = translateEvent(codexEvent, this.translatorState);
        for (const pe of promptEvents) {
          yield pe;
        }
      }

      // Emit final result
      yield createResultEvent(this.translatorState);
    } catch (error) {
      console.error("[codex-adapter] Error during prompt execution:", error);
      yield {
        type: "error",
        message: `Prompt execution error: ${error instanceof Error ? error.message : String(error)}`,
        retryable: false,
      };
    }
  }

  setInterruptSignal(): void {
    this.interruptSignal = true;
    console.log("[codex-backend] Interrupt signal set");
  }

  async interrupt(): Promise<void> {
    this.setInterruptSignal();
  }

  clearInterruptSignal(): void {
    this.interruptSignal = false;
    resetTranslatorState(this.translatorState);
  }

  isInterrupted(): boolean {
    return this.interruptSignal;
  }

  async shutdown(): Promise<void> {
    console.log("[codex-backend] Shutting down...");
    this.thread = null;
    this.codex = null;
    resetTranslatorState(this.translatorState);
  }
}
