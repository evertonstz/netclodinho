/**
 * OpenAI Codex SDK Adapter
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
import type { SDKAdapter, SDKConfig, PromptConfig, PromptEvent } from "../types.js";
import { isSessionInitialized, markSessionInitialized } from "../../services/session.js";
import { setupRepository } from "../../git.js";
import {
  createTranslatorState,
  resetTranslatorState,
  translateEvent,
  storeUsage,
  createResultEvent,
  type TranslatorState,
  type CodexEvent,
} from "./translator.js";
import * as fs from "node:fs/promises";
import * as path from "node:path";
import * as os from "node:os";

const WORKSPACE_DIR = "/agent/workspace";

// Codex session ID mapping (Netclode session ID -> Codex thread ID)
const codexThreadMap = new Map<string, string>();

export class CodexAdapter implements SDKAdapter {
  private config: SDKConfig | null = null;
  private codex: Codex | null = null;
  private thread: Thread | null = null;
  private interruptSignal = false;
  private currentGitRepo: string | null = null;
  private currentGithubToken: string | null = null;
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
    const modelHasOAuthSuffix = config.model?.includes(":oauth");
    const isApiMode = modelHasApiSuffix || Boolean(config.openaiApiKey && !config.codexAccessToken);
    const isOAuthMode = modelHasOAuthSuffix || Boolean(config.codexAccessToken && !config.openaiApiKey);

    console.log("[codex-adapter] Initializing");
    console.log("[codex-adapter] Model:", this.cleanedModel || "default");
    console.log("[codex-adapter] Auth mode:", isApiMode ? "API key" : isOAuthMode ? "OAuth" : "unknown");
    console.log("[codex-adapter] Reasoning effort:", this.reasoningEffort || "default");

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
      // OAuth mode: write tokens to ~/.codex/auth.json
      // The Codex CLI binary reads credentials from this location
      await this.writeCodexAuth(config.codexAccessToken, config.codexIdToken, config.codexRefreshToken);
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
   * Write OAuth tokens to Codex auth file
   * The Codex CLI reads from ~/.codex/auth.json
   */
  private async writeCodexAuth(accessToken: string, idToken: string, refreshToken?: string): Promise<void> {
    const codexHome = process.env.CODEX_HOME || path.join(os.homedir(), ".codex");
    await fs.mkdir(codexHome, { recursive: true });

    const authData = {
      tokens: {
        access_token: accessToken,
        id_token: idToken,
        refresh_token: refreshToken || "",
      },
      last_refresh: new Date().toISOString(),
    };

    const authPath = path.join(codexHome, "auth.json");
    await fs.writeFile(authPath, JSON.stringify(authData, null, 2), { mode: 0o600 });
    console.log("[codex-adapter] OAuth tokens written to", authPath);
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

    // Initialize repo for this session if needed
    if (sessionId && promptConfig) {
      if (!isSessionInitialized(sessionId)) {
        console.log(`[codex-adapter] Initializing session ${sessionId}`);

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
            yield {
              type: "repoClone",
              stage: "done",
              repo: this.currentGitRepo,
              message: "Repository cloned successfully",
            };
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

    // Clear interrupt signal
    this.clearInterruptSignal();

    // Get or create Codex thread
    const existingThreadId = codexThreadMap.get(sessionId);

    try {
      if (existingThreadId) {
        console.log(`[codex-adapter] Resuming Codex thread: ${existingThreadId}`);
        this.thread = this.codex.resumeThread(existingThreadId, {
          workingDirectory: WORKSPACE_DIR,
          sandboxMode: "danger-full-access",
          approvalPolicy: "never",
          model: this.cleanedModel,
          modelReasoningEffort: this.reasoningEffort as ModelReasoningEffort,
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

        // Capture thread ID from first event
        if (event.type === "thread.started" && this.thread.id) {
          codexThreadMap.set(sessionId, this.thread.id);
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
    console.log("[codex-adapter] Interrupt signal set");
  }

  clearInterruptSignal(): void {
    this.interruptSignal = false;
    resetTranslatorState(this.translatorState);
  }

  isInterrupted(): boolean {
    return this.interruptSignal;
  }

  getCurrentGitRepo(): string | null {
    return this.currentGitRepo;
  }

  async shutdown(): Promise<void> {
    console.log("[codex-adapter] Shutting down...");
    this.thread = null;
    this.codex = null;
    codexThreadMap.clear();
    resetTranslatorState(this.translatorState);
  }
}
