/**
 * GitHub Copilot SDK Adapter
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

import { CopilotClient, type CopilotSession, type SessionEvent, type ModelInfo } from "@github/copilot-sdk";
import type { JsonObject } from "@bufbuild/protobuf";
import type { SDKAdapter, SDKConfig, PromptConfig, PromptEvent, CopilotBackend } from "./types.js";
import { isSessionInitialized, markSessionInitialized } from "../services/session.js";
import { setupRepository } from "../git.js";

const WORKSPACE_DIR = "/agent/workspace";

// Copilot session ID mapping (Netclode session ID -> Copilot session ID)
const copilotSessionMap = new Map<string, string>();

/**
 * Simplified model info for our API
 */
export interface CopilotModelInfo {
  id: string;
  name: string;
  provider?: string;
  billingMultiplier?: number;
  supportsVision?: boolean;
}

export class CopilotAdapter implements SDKAdapter {
  private config: SDKConfig | null = null;
  private client: CopilotClient | null = null;
  private interruptSignal = false;
  private currentGitRepo: string | null = null;
  private currentGithubToken: string | null = null;
  private backend: CopilotBackend = "anthropic";

  // Track tool names from execution_start for execution_complete events
  private toolNameMap = new Map<string, string>();

  // Accumulate usage data for result event
  private lastUsage: { inputTokens: number; outputTokens: number } | null = null;

  // Track current thinking block ID for correlating streaming reasoning deltas
  private currentThinkingId: string | null = null;
  private thinkingIdCounter = 0;

  async initialize(config: SDKConfig): Promise<void> {
    this.config = config;

    // Determine backend: use explicit config, or auto-detect from available credentials
    if (config.copilotBackend) {
      this.backend = config.copilotBackend;
    } else if (config.githubToken) {
      // If GitHub token is provided and user didn't specify, prefer GitHub backend
      this.backend = "github";
    } else if (config.anthropicApiKey) {
      this.backend = "anthropic";
    }

    console.log("[copilot-adapter] Initializing with backend:", this.backend);
    console.log("[copilot-adapter] Model:", config.model || "default");
    console.log("[copilot-adapter] GitHub token available:", Boolean(config.githubToken));
    console.log("[copilot-adapter] Anthropic API key available:", Boolean(config.anthropicApiKey));

    // Build environment for the client
    const clientEnv: Record<string, string | undefined> = {
      ...process.env,
    };

    // For GitHub backend, ensure GITHUB_TOKEN is set
    if (this.backend === "github" && config.githubToken) {
      clientEnv.GITHUB_TOKEN = config.githubToken;
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

  /**
   * Get backend type for this adapter
   */
  getBackend(): CopilotBackend {
    return this.backend;
  }

/**
   * List available models from the Copilot SDK
   * For GitHub backend, this returns models with billing multipliers
   * For Anthropic backend, this returns hardcoded Anthropic models
   */
  async listModels(): Promise<CopilotModelInfo[]> {
    if (!this.client) {
      throw new Error("Copilot client not initialized");
    }

    if (this.backend === "anthropic") {
      // For Anthropic BYOK, return hardcoded models
      // The Copilot SDK doesn't provide model listing for BYOK
      return [
        {
          id: "claude-sonnet-4-20250514",
          name: "Claude Sonnet 4",
          provider: "anthropic",
          supportsVision: true,
        },
        {
          id: "claude-3-5-sonnet-20241022",
          name: "Claude 3.5 Sonnet",
          provider: "anthropic",
          supportsVision: true,
        },
        {
          id: "claude-3-5-haiku-20241022",
          name: "Claude 3.5 Haiku",
          provider: "anthropic",
          supportsVision: true,
        },
      ];
    }

    // For GitHub backend, use SDK's listModels
    try {
      const models = await this.client.listModels();
      console.log("[copilot-adapter] Listed models from GitHub:", models.length);
      
      // Transform to our simplified format
      return models.map((m) => ({
        id: m.id,
        name: m.name,
        billingMultiplier: m.billing?.multiplier,
        supportsVision: m.capabilities?.supports?.vision,
      }));
    } catch (error) {
      console.error("[copilot-adapter] Failed to list models:", error);
      throw error;
    }
  }

  /**
   * Get GitHub Copilot authentication status
   * Only meaningful for GitHub backend
   */
  async getAuthStatus(): Promise<{ isAuthenticated: boolean; authType?: string; login?: string }> {
    if (!this.client) {
      throw new Error("Copilot client not initialized");
    }

    if (this.backend === "anthropic") {
      // For Anthropic BYOK, auth is based on having the API key
      return {
        isAuthenticated: Boolean(this.config?.anthropicApiKey),
        authType: "api-key",
      };
    }

    try {
      const status = await this.client.getAuthStatus();
      return {
        isAuthenticated: status.isAuthenticated,
        authType: status.authType,
        login: status.login,
      };
    } catch (error) {
      console.error("[copilot-adapter] Failed to get auth status:", error);
      return { isAuthenticated: false };
    }
  }

  async *executePrompt(sessionId: string, text: string, promptConfig?: PromptConfig): AsyncGenerator<PromptEvent> {
    if (!this.client) {
      throw new Error("Copilot client not initialized");
    }

    // Reset thinking tracking for new prompt
    this.currentThinkingId = null;

    console.log(
      `[copilot-adapter] ExecutePrompt (session=${sessionId}): "${text.slice(0, 100)}${text.length > 100 ? "..." : ""}"`
    );

    // Initialize repo for this session if needed
    if (sessionId && promptConfig) {
      if (!isSessionInitialized(sessionId)) {
        console.log(`[copilot-adapter] Initializing session ${sessionId}`);

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

    // Clear interrupt signal and reset state
    this.clearInterruptSignal();
    this.lastUsage = null;

    // Get or create Copilot session
    let session: CopilotSession;
    const existingSessionId = copilotSessionMap.get(sessionId);

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

        // Default model depends on backend
        const defaultModel = this.backend === "anthropic" 
          ? "claude-sonnet-4-20250514" 
          : "gpt-4o"; // GitHub Copilot default

        console.log(`[copilot-adapter] Creating new Copilot session`);
        console.log(`[copilot-adapter] Backend: ${this.backend}`);
        console.log(`[copilot-adapter] Model: ${this.config?.model || defaultModel}`);

        session = await this.client.createSession({
          model: this.config?.model || defaultModel,
          streaming: true,
          // Auto-approve all permissions - we're in an isolated sandbox
          onPermissionRequest: async () => ({ kind: "approved" }),
          ...(providerConfig && { provider: providerConfig }),
        });

        // Get the session ID from the first event or session object
        // The session ID is available after creation
        const sessionInfo = await session.getMessages();
        const startEvent = sessionInfo.find((e) => e.type === "session.start" || e.type === "session.resume");
        if (startEvent && "sessionId" in startEvent) {
          copilotSessionMap.set(sessionId, (startEvent as { sessionId: string }).sessionId);
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

      const promptEvent = this.translateEvent(event);
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

      // Track session.idle to know when to emit result
      if (event.type === "session.idle") {
        completed = true;
        // Emit result event with accumulated usage
        const resultEvent: PromptEvent = {
          type: "result",
          inputTokens: this.lastUsage?.inputTokens || 0,
          outputTokens: this.lastUsage?.outputTokens || 0,
          totalTurns: 1,
        };
        if (resolveNextEvent) {
          resolveNextEvent(resultEvent);
          resolveNextEvent = null;
        } else {
          eventQueue.push(resultEvent);
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

  /**
   * Translate Copilot SDK events to PromptEvent format
   */
  private translateEvent(event: SessionEvent): PromptEvent | null {
    // Log all events for debugging
    console.log(`[copilot-adapter] Event: ${event.type}`, JSON.stringify(event.data).slice(0, 200));

    switch (event.type) {
      case "assistant.message_delta": {
        const data = event.data as { deltaContent?: string; content?: string };
        const textContent = data.deltaContent || data.content;
        if (textContent) {
          return {
            type: "textDelta",
            content: textContent,
            partial: true,
          };
        }
        return null;
      }

      case "assistant.message": {
        const data = event.data as { content?: string };
        // Final message - emit the content with partial: false to signal completion
        // The content may be the full response if streaming wasn't used for deltas
        return {
          type: "textDelta",
          content: data.content || "",
          partial: false,
        };
      }

      case "assistant.reasoning_delta": {
        const data = event.data as { deltaContent?: string };
        if (data.deltaContent) {
          // Generate a new thinkingId if we don't have one for this reasoning block
          if (!this.currentThinkingId) {
            this.currentThinkingId = `thinking_${Date.now()}_${++this.thinkingIdCounter}`;
          }
          return {
            type: "thinking",
            thinkingId: this.currentThinkingId,
            content: data.deltaContent,
            partial: true,
          };
        }
        return null;
      }

      case "assistant.reasoning": {
        // End of reasoning block - emit final event and reset tracking
        const thinkingId = this.currentThinkingId || `thinking_${Date.now()}_${++this.thinkingIdCounter}`;
        this.currentThinkingId = null; // Reset for next reasoning block
        return {
          type: "thinking",
          thinkingId,
          content: "",
          partial: false,
        };
      }

      case "tool.execution_start": {
        const data = event.data as { toolName?: string; toolCallId?: string; arguments?: Record<string, unknown> };
        const toolName = data.toolName || "unknown";
        const toolCallId = data.toolCallId || event.id;

        // Track tool name for execution_complete
        this.toolNameMap.set(toolCallId, toolName);

        return {
          type: "toolStart",
          tool: toolName,
          toolUseId: toolCallId,
          input: data.arguments as JsonObject | undefined,
        };
      }

      case "tool.execution_complete": {
        const data = event.data as {
          toolCallId?: string;
          result?: string;
          error?: string;
          resultType?: string;
        };
        const toolCallId = data.toolCallId || event.id;
        const toolName = this.toolNameMap.get(toolCallId) || "unknown";
        this.toolNameMap.delete(toolCallId);

        const isError = data.resultType === "failure" || data.resultType === "rejected" || data.resultType === "denied";

        return {
          type: "toolEnd",
          tool: toolName,
          toolUseId: toolCallId,
          result: isError ? undefined : data.result,
          error: isError ? (data.error || data.result) : undefined,
        };
      }

      case "assistant.usage": {
        const data = event.data as { inputTokens?: number; outputTokens?: number };
        // Accumulate usage for result event
        this.lastUsage = {
          inputTokens: data.inputTokens || 0,
          outputTokens: data.outputTokens || 0,
        };
        return null;
      }

      case "session.error": {
        const data = event.data as { message?: string; errorType?: string };
        return {
          type: "error",
          message: data.message || data.errorType || "Unknown session error",
          retryable: false,
        };
      }

      default:
        return null;
    }
  }

  setInterruptSignal(): void {
    this.interruptSignal = true;
    console.log("[copilot-adapter] Interrupt signal set");
  }

  clearInterruptSignal(): void {
    this.interruptSignal = false;
    this.toolNameMap.clear();
  }

  isInterrupted(): boolean {
    return this.interruptSignal;
  }

  getCurrentGitRepo(): string | null {
    return this.currentGitRepo;
  }

  async shutdown(): Promise<void> {
    console.log("[copilot-adapter] Shutting down...");

    if (this.client) {
      try {
        await this.client.stop();
      } catch (error) {
        console.error("[copilot-adapter] Error stopping client:", error);
      }
      this.client = null;
    }

    copilotSessionMap.clear();
    this.toolNameMap.clear();
  }
}
