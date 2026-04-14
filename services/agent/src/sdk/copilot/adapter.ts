/**
 * GitHub Copilot backend
 *
 * Uses @github/copilot-sdk to communicate with GitHub Copilot or Anthropic.
 *
 * ## Authentication
 *
 * The Copilot SDK supports two backends, selectable via copilotBackend config:
 *
 * 1. **Anthropic Backend** (copilotBackend: "anthropic")
 *    - Uses Anthropic API directly (BYOK mode)
 *    - Requires ANTHROPIC_API_KEY environment variable
 *    - Recommended for self-hosted deployments
 *
 * 2. **GitHub Backend** (copilotBackend: "github")
 *    - Uses GitHub's Copilot API service
 *    - Requires GITHUB_TOKEN with Copilot access
 *    - Supports premium request tracking and model billing multipliers
 *    - Access to GPT-4o, Claude models via GitHub Copilot
 */

import { CopilotClient, type CopilotSession, type SessionEvent } from "@github/copilot-sdk";
import type { NetclodePromptBackend, SDKConfig, PromptConfig, PromptEvent, CopilotBackend } from "../types.js";
import { createAgentCapabilities } from "../types.js";
import { NoopAuthMaterializer, type BackendAuthMaterializer } from "../auth-materializer.js";
import {
  createTranslatorState,
  resetTranslatorState,
  translateEvent,
  translateSessionIdle,
  type TranslatorState,
  type CopilotEvent,
} from "./translator.js";
import { getSdkSessionId, registerSession } from "../../services/session.js";
import { WORKSPACE_DIR } from "../../constants.js";
import { buildSystemPromptText } from "../../utils/system-prompt.js";

export class CopilotAdapter implements NetclodePromptBackend {
  readonly capabilities = createAgentCapabilities({
    interrupt: true,
    toolStreaming: true,
    thinkingStreaming: false,
  });

  constructor(
    private readonly authMaterializer: BackendAuthMaterializer = new NoopAuthMaterializer("copilot-backend"),
  ) {}

  private config: SDKConfig | null = null;
  private client: CopilotClient | null = null;
  private interruptSignal = false;
  private backend: CopilotBackend = "anthropic";
  private translatorState: TranslatorState = createTranslatorState();

  async initialize(config: SDKConfig): Promise<void> {
    this.config = config;

    // Determine backend: use explicit config, or auto-detect from available credentials
    if (config.copilotBackend) {
      this.backend = config.copilotBackend;
    } else if (config.githubCopilotToken) {
      // If GitHub Copilot token is provided and user didn't specify, prefer GitHub backend
      this.backend = "github";
    } else if (config.anthropicApiKey) {
      this.backend = "anthropic";
    }

    console.log("[copilot-adapter] Initializing with backend:", this.backend);
    console.log("[copilot-adapter] Model:", config.model || "default");
    console.log("[copilot-adapter] GitHub Copilot token available:", Boolean(config.githubCopilotToken));
    console.log("[copilot-adapter] Anthropic API key available:", Boolean(config.anthropicApiKey));
    await this.authMaterializer.materialize(config);

    // Build environment for the client
    const clientEnv: Record<string, string | undefined> = {
      ...process.env,
    };

    // The Copilot SDK always uses GITHUB_TOKEN internally regardless of backend.
    // Map our GITHUB_COPILOT_TOKEN placeholder so BoxLite can substitute the real PAT in-flight.
    if (config.githubCopilotToken) {
      clientEnv.GITHUB_TOKEN = config.githubCopilotToken;
    }

    // Create CopilotClient with stdio transport
    this.client = new CopilotClient({
      cwd: WORKSPACE_DIR,
      logLevel: "info",
      autoStart: true,
      autoRestart: true,
      env: clientEnv,
    });

    console.log("[copilot-adapter] Client created");
  }

  async *executePrompt(sessionId: string, text: string, promptConfig?: PromptConfig): AsyncGenerator<PromptEvent> {
    if (!this.client) {
      throw new Error("Copilot client not initialized");
    }

    // Reset translator state for new prompt
    resetTranslatorState(this.translatorState);

    // Get repos from config for system prompt
    const currentGitRepos = promptConfig?.repos?.filter(Boolean) ?? [];

    console.log(
      `[copilot-adapter] ExecutePrompt (session=${sessionId}): "${text.slice(0, 100)}${text.length > 100 ? "..." : "..."}"`
    );

    // Clear interrupt signal
    this.clearInterruptSignal();

    // Get or create Copilot session (persisted mapping survives pod restarts)
    let session: CopilotSession;
    const existingSessionId = getSdkSessionId(sessionId);

    try {
      if (existingSessionId) {
        console.log(`[copilot-adapter] Resuming Copilot session: ${existingSessionId}`);
        session = await this.client.resumeSession(existingSessionId, {
          streaming: true,
          // Auto-approve all permissions - we're in an isolated sandbox
          onPermissionRequest: async () => ({ kind: "approved" }),
        });
      } else {
        // Build provider config based on backend setting
        let providerConfig: { type: "anthropic"; baseUrl: string; apiKey: string } | undefined;
        
        if (this.backend === "anthropic" && this.config?.anthropicApiKey) {
          providerConfig = {
            type: "anthropic" as const,
            baseUrl: "https://api.anthropic.com",
            apiKey: this.config.anthropicApiKey,
          };
        }
        // For GitHub backend, no provider config needed - uses GITHUB_TOKEN from env

        // Default model depends on backend (Anthropic uses dashes, Copilot uses dots)
        const defaultModel = this.backend === "anthropic" 
          ? "claude-sonnet-4-5" 
          : "claude-sonnet-4.5"; // GitHub Copilot default (Sonnet 4.5)

        // Strip :anthropic suffix from model ID (used for routing, not the actual model name)
        const rawModel = this.config?.model || defaultModel;
        const model = rawModel.replace(/:anthropic$/, "");

        console.log(`[copilot-adapter] Creating new Copilot session`);
        console.log(`[copilot-adapter] Backend: ${this.backend}`);
        console.log(`[copilot-adapter] Model: ${model}`);

        // Build system prompt with repo information
        const systemPromptContent = buildSystemPromptText({ currentGitRepos });

        session = await this.client.createSession({
          model,
          streaming: true,
          // Auto-approve all permissions - we're in an isolated sandbox
          onPermissionRequest: async () => ({ kind: "approved" }),
          // Append our custom system prompt to the default Copilot prompt
          systemMessage: {
            mode: "append",
            content: systemPromptContent,
          },
          ...(providerConfig && { provider: providerConfig }),
        });

        // Get the session ID from the first event or session object
        // The session ID is available after creation, persist it to survive pod restarts
        const sessionInfo = await session.getMessages();
        const startEvent = sessionInfo.find((e) => e.type === "session.start" || e.type === "session.resume");
        if (startEvent && "sessionId" in startEvent) {
          registerSession(sessionId, (startEvent as { sessionId: string }).sessionId);
        }
      }
    } catch (error) {
      console.error("[copilot-adapter] Failed to create/resume session:", error);
      yield {
        type: "error",
        message: `Failed to create session: ${error instanceof Error ? error.message : String(error)}`,
        retryable: true,
      };
      return;
    }

    // Create event queue for yielding
    const eventQueue: PromptEvent[] = [];
    let resolveNextEvent: ((value: PromptEvent | null) => void) | null = null;
    let completed = false;

    // Subscribe to session events
    const unsubscribe = session.on((event: SessionEvent) => {
      if (this.interruptSignal) {
        return;
      }

      // Log all events for debugging
      console.log(`[copilot-adapter] Event: ${event.type}`, JSON.stringify(event.data).slice(0, 200));

      // Handle session.idle specially - it needs to emit the result event
      if (event.type === "session.idle") {
        completed = true;
        const resultEvent = translateSessionIdle(this.translatorState);
        if (resolveNextEvent) {
          resolveNextEvent(resultEvent);
          resolveNextEvent = null;
        } else {
          eventQueue.push(resultEvent);
        }
        return;
      }

      // Translate other events using the translator
      const copilotEvent: CopilotEvent = {
        type: event.type,
        id: event.id,
        data: event.data as Record<string, unknown>,
      };
      const promptEvent = translateEvent(copilotEvent, this.translatorState);
      if (promptEvent) {
        if (resolveNextEvent) {
          resolveNextEvent(promptEvent);
          resolveNextEvent = null;
        } else {
          eventQueue.push(promptEvent);
        }

        // Check for completion
        if (promptEvent.type === "result" || promptEvent.type === "error") {
          completed = true;
        }
      }
    });

    try {
      // Send the prompt
      await session.send({ prompt: text });

      // Yield events from queue until completion
      while (!completed || eventQueue.length > 0) {
        if (this.interruptSignal) {
          yield { type: "system", message: "interrupted" };
          await session.abort();
          return;
        }

        if (eventQueue.length > 0) {
          yield eventQueue.shift()!;
        } else if (!completed) {
          // Wait for next event
          const event = await new Promise<PromptEvent | null>((resolve) => {
            resolveNextEvent = resolve;
            // Timeout after 5 minutes to prevent hanging
            setTimeout(() => resolve(null), 300000);
          });
          if (event) {
            yield event;
          }
        }
      }
    } catch (error) {
      console.error("[copilot-adapter] Error during prompt execution:", error);
      yield {
        type: "error",
        message: `Prompt execution error: ${error instanceof Error ? error.message : String(error)}`,
        retryable: false,
      };
    } finally {
      unsubscribe();
    }
  }

  setInterruptSignal(): void {
    this.interruptSignal = true;
    console.log("[copilot-backend] Interrupt signal set");
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
    console.log("[copilot-backend] Shutting down...");

    if (this.client) {
      try {
        await this.client.stop();
      } catch (error) {
        console.error("[copilot-adapter] Error stopping client:", error);
      }
      this.client = null;
    }

    resetTranslatorState(this.translatorState);
  }
}
