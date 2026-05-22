/**
 * OpenCode backend
 *
 * Uses @opencode-ai/sdk to manage the opencode server process and
 * typed API calls (session create, prompt, abort, SSE events).
 */

import * as fs from "node:fs/promises";
import * as path from "node:path";
import { createOpencode, type OpencodeClient } from "@opencode-ai/sdk";
import type { Config } from "@opencode-ai/sdk";
import type { NetclodePromptBackend, SDKConfig, PromptConfig, PromptEvent } from "../types.js";
import { createAgentCapabilities } from "../types.js";
import { OpenCodeAuthMaterializer, type BackendAuthMaterializer } from "../auth-materializer.js";
import {
  createTranslatorState,
  resetTranslatorState,
  translateEvent,
  finalizeActiveThinking,
  type TranslatorState,
} from "./translator.js";
import { getSdkSessionId, registerSession } from "../../services/session.js";
import { WORKSPACE_DIR } from "../../constants.js";
import { buildSystemPromptText } from "../../utils/system-prompt.js";
import { getOpenCodeProvider } from "../secret-materialization.js";

const OPENCODE_PORT = 4096;
const OPENCODE_HOST = "127.0.0.1";

export class OpenCodeAdapter implements NetclodePromptBackend {
  readonly capabilities = createAgentCapabilities({
    interrupt: true,
    toolStreaming: true,
    thinkingStreaming: true,
  });

  constructor(
    private readonly authMaterializer: BackendAuthMaterializer = new OpenCodeAuthMaterializer("opencode-backend"),
  ) {}

  private config: SDKConfig | null = null;
  private sdkClient: OpencodeClient | null = null;
  private sdkServer: { url: string; close: () => void } | null = null;
  private interruptSignal = false;
  private ollamaUrl: string | null = null;
  private translatorState: TranslatorState = createTranslatorState();

  async initialize(config: SDKConfig): Promise<void> {
    this.config = config;
    this.ollamaUrl = config.ollamaUrl || null;
    console.log("[opencode-backend] Initializing with model:", config.model, "ollamaUrl:", this.ollamaUrl);
    await this.authMaterializer.materialize(config);
    await this.startServer();
  }

  // ── Provider config ──────────────────────────────────────────────────────

  /** Build the provider config object passed to createOpencode. */
  private buildProviderConfig(): Record<string, unknown> {
    const model = this.config?.model || "anthropic/claude-sonnet-4-0";
    const thinkingLevel = this.config?.reasoningEffort;
    const providerId = this.config ? getOpenCodeProvider(this.config) : "anthropic";
    const [, modelName = model] = model.includes("/") ? model.split("/", 2) : [providerId, model];
    const thinkingBudget = thinkingLevel === "max" ? 32000 : thinkingLevel === "high" ? 16000 : 0;

    const providerConfig: Record<string, unknown> = {};

    if (thinkingBudget > 0) {
      providerConfig[providerId] = {
        models: {
          [modelName]: {
            options: {
              thinking: { type: "enabled", budgetTokens: thinkingBudget },
            },
          },
        },
      };
    }

    if (providerId === "ollama" && this.ollamaUrl) {
      console.log("[opencode-adapter] Configuring Ollama provider with URL:", this.ollamaUrl);
      const ollamaBaseUrl = this.ollamaUrl.endsWith("/v1")
        ? this.ollamaUrl
        : this.ollamaUrl.replace(/\/$/, "") + "/v1";
      providerConfig["ollama"] = {
        npm: "@ai-sdk/openai-compatible",
        name: "Ollama",
        options: { baseURL: ollamaBaseUrl },
        models: {
          [modelName]: {
            name: modelName,
            tools: true,
            reasoning: true,
          },
        },
      };
    }

    return providerConfig;
  }

  // ── Server lifecycle ─────────────────────────────────────────────────────

  private async startServer(): Promise<void> {
    if (this.sdkServer) {
      console.log("[opencode-adapter] Server already running at", this.sdkServer.url);
      return;
    }

    console.log("[opencode-adapter] Starting opencode serve via SDK...");

    const model = this.config?.model || "anthropic/claude-sonnet-4-0";
    const providerId = this.config ? getOpenCodeProvider(this.config) : "anthropic";
    const isZenModel = providerId === "opencode";
    const isCopilotModel = providerId === "github-copilot";
    const providerConfigObj = this.buildProviderConfig();

    const opencodeConfig: Record<string, unknown> = {
      model,
      logLevel: "INFO",
      instructions: [".netclode-instructions.md"],
      permission: {
        edit: "allow",
        bash: "allow",
        webfetch: "allow",
        mcp: "allow",
        question: "deny",
      },
    };

    if (Object.keys(providerConfigObj).length > 0) {
      opencodeConfig["provider"] = providerConfigObj;
    }

    // The SDK passes config via OPENCODE_CONFIG_CONTENT.
    // API keys flow through process.env which the server inherits.
    // BoxLite substitutes OAuth placeholders at the HTTP layer.
    process.env.ANTHROPIC_API_KEY = this.config?.anthropicApiKey || process.env.ANTHROPIC_API_KEY;
    if (this.config?.openaiApiKey) process.env.OPENAI_API_KEY = this.config.openaiApiKey;
    if (this.config?.mistralApiKey) process.env.MISTRAL_API_KEY = this.config.mistralApiKey;
    if (this.config?.openRouterApiKey) process.env.OPENROUTER_API_KEY = this.config.openRouterApiKey;
    if (this.config?.openCodeApiKey) {
      process.env.OPENCODE_API_KEY = this.config.openCodeApiKey;
    } else if (isZenModel) {
      process.env.OPENCODE_API_KEY = "public";
    }
    if (this.config?.zaiApiKey) process.env.ZHIPU_API_KEY = this.config.zaiApiKey;
    process.env.OPENCODE_DISABLE_DEFAULT_PLUGINS = "true";
    if (!isZenModel && !isCopilotModel) {
      process.env.OPENCODE_DISABLE_MODELS_FETCH = "true";
    }

    const { client, server } = await createOpencode({
      hostname: OPENCODE_HOST,
      port: OPENCODE_PORT,
      timeout: 60_000,
      config: opencodeConfig as Config,
    });

    this.sdkClient = client;
    this.sdkServer = server;
    console.log("[opencode-adapter] Server started at:", server.url);
  }

  // ── Instructions file ────────────────────────────────────────────────────

  /**
   * Write custom instructions file to workspace.
   * OpenCode reads this via the `instructions` config field.
   */
  private async writeInstructionsFile(currentGitRepos: string[]): Promise<void> {
    const systemPromptText = buildSystemPromptText({ currentGitRepos });
    const instructionsPath = path.join(WORKSPACE_DIR, ".netclode-instructions.md");

    try {
      await fs.writeFile(instructionsPath, systemPromptText, "utf-8");
      console.log("[opencode-adapter] Wrote .netclode-instructions.md with system prompt");
    } catch (error) {
      console.error("[opencode-adapter] Failed to write instructions file:", error);
    }
  }

  // ── Session abort ────────────────────────────────────────────────────────

  private async abortSession(sessionId: string | undefined): Promise<void> {
    if (!this.sdkClient || !sessionId) return;

    try {
      await this.sdkClient.session.abort({
        path: { id: sessionId },
        query: { directory: WORKSPACE_DIR },
      });
      console.log(`[opencode-adapter] Session ${sessionId} aborted via SDK`);
    } catch (error) {
      console.error("[opencode-adapter] Error aborting session:", error);
    }
  }

  // ── Prompt execution ─────────────────────────────────────────────────────

  async *executePrompt(
    sessionId: string,
    text: string,
    promptConfig?: PromptConfig,
  ): AsyncGenerator<PromptEvent> {
    if (!this.sdkClient || !this.sdkServer) {
      throw new Error("OpenCode server not initialized");
    }

    console.log(
      `[opencode-adapter] ExecutePrompt (session=${sessionId}): "${text.slice(0, 100)}${text.length > 100 ? "..." : ""}"`,
    );

    // Write instructions file with system prompt (includes repo info when available)
    const currentGitRepos = promptConfig?.repos?.filter(Boolean) ?? [];
    await this.writeInstructionsFile(currentGitRepos);

    // Get or create OpenCode session (persisted mapping survives pod restarts)
    let ocSessionId = getSdkSessionId(sessionId);

    if (!ocSessionId) {
      const result = await this.sdkClient.session.create({
        query: { directory: WORKSPACE_DIR },
      });
      if (!result.data?.id) {
        throw new Error("Failed to create OpenCode session: no id returned");
      }
      ocSessionId = result.data.id;
      registerSession(sessionId, ocSessionId);
      console.log(`[opencode-adapter] Created OpenCode session: ${ocSessionId}`);
    } else {
      console.log(`[opencode-adapter] Reusing OpenCode session: ${ocSessionId}`);
    }

    // Clear interrupt signal and reset translator state
    this.clearInterruptSignal();

    // Subscribe to SSE events before sending prompt
    let events: Awaited<ReturnType<typeof this.sdkClient.event.subscribe>>;
    try {
      events = await this.sdkClient.event.subscribe({
        query: { directory: WORKSPACE_DIR },
      });
    } catch (error) {
      yield {
        type: "error",
        message: `Failed to subscribe to events: ${error instanceof Error ? error.message : String(error)}`,
        retryable: false,
      };
      return;
    }
    console.log("[opencode-adapter] SSE subscription established, sending prompt");

    // Send prompt asynchronously
    const promptPromise = this.sdkClient.session.promptAsync({
      path: { id: ocSessionId },
      query: { directory: WORKSPACE_DIR },
      body: { parts: [{ type: "text", text }] },
    });

    // Stream events from SSE, yielding translated PromptEvent values
    try {
      for await (const event of events.stream) {
        if (this.interruptSignal) {
          await this.abortSession(ocSessionId);
          yield { type: "system", message: "interrupted" };
          return;
        }

        const promptEvent = translateEvent(event, this.translatorState);
        if (promptEvent) {
          if (promptEvent.type === "result" || promptEvent.type === "error") {
            for (const evt of finalizeActiveThinking(this.translatorState)) {
              yield evt;
            }
          }
          yield promptEvent;
        }
      }
    } catch (error) {
      console.error("[opencode-adapter] Event stream error:", error);
      yield {
        type: "error",
        message: `Event stream error: ${error instanceof Error ? error.message : String(error)}`,
        retryable: false,
      };
    }

    // Ensure prompt sent completion doesn't throw unhandled
    try {
      await promptPromise;
    } catch (error) {
      console.error("[opencode-adapter] promptAsync error:", error);
    }
  }

  // ── Interrupt ────────────────────────────────────────────────────────────

  setInterruptSignal(): void {
    this.interruptSignal = true;
    console.log("[opencode-backend] Interrupt signal set");
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

  // ── Shutdown ─────────────────────────────────────────────────────────────

  async shutdown(): Promise<void> {
    console.log("[opencode-backend] Shutting down...");

    if (this.sdkServer) {
      this.sdkServer.close(); // internally calls proc.kill()
      this.sdkServer = null;
      this.sdkClient = null;
    }

    resetTranslatorState(this.translatorState);
  }
}
