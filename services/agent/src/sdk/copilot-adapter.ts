/**
 * GitHub Copilot SDK Adapter
 *
 * Uses @github/copilot-sdk to communicate with GitHub Copilot CLI.
 *
 * ## Authentication
 *
 * The Copilot SDK supports two authentication modes:
 *
 * 1. **BYOK (Bring Your Own Key)** - Uses Anthropic API directly
 *    - Set ANTHROPIC_API_KEY environment variable
 *    - Calls Anthropic API instead of GitHub Copilot
 *    - Recommended for self-hosted deployments
 *
 * 2. **GitHub Copilot Auth** - Uses GitHub's Copilot service
 *    - Requires GITHUB_TOKEN with Copilot access, OR
 *    - Interactive device flow login (not suitable for server use)
 *    - NOT currently supported in Netclode (use BYOK mode)
 *
 * For Netclode, BYOK mode with Anthropic is recommended since we already
 * have ANTHROPIC_API_KEY configured for the OpenCode adapter.
 */

import { CopilotClient, type CopilotSession, type SessionEvent } from "@github/copilot-sdk";
import type { JsonObject } from "@bufbuild/protobuf";
import type { SDKAdapter, SDKConfig, PromptConfig, PromptEvent } from "./types.js";
import { isSessionInitialized, markSessionInitialized } from "../services/session.js";
import { setupRepository } from "../git.js";

const WORKSPACE_DIR = "/agent/workspace";

// Copilot session ID mapping (Netclode session ID -> Copilot session ID)
const copilotSessionMap = new Map<string, string>();

export class CopilotAdapter implements SDKAdapter {
  private config: SDKConfig | null = null;
  private client: CopilotClient | null = null;
  private interruptSignal = false;
  private currentGitRepo: string | null = null;
  private currentGithubToken: string | null = null;

  // Track tool names from execution_start for execution_complete events
  private toolNameMap = new Map<string, string>();

  // Accumulate usage data for result event
  private lastUsage: { inputTokens: number; outputTokens: number } | null = null;

  async initialize(config: SDKConfig): Promise<void> {
    this.config = config;
    const hasAnthropicKey = Boolean(config.anthropicApiKey);
    console.log("[copilot-adapter] Initializing with model:", config.model);
    console.log("[copilot-adapter] BYOK mode:", hasAnthropicKey ? "enabled (Anthropic)" : "disabled (needs GitHub auth)");

    // Create CopilotClient with stdio transport
    this.client = new CopilotClient({
      cwd: WORKSPACE_DIR,
      logLevel: "info",
      autoStart: true,
      autoRestart: true,
      // Pass environment variables
      env: {
        ...process.env,
      },
    });

    console.log("[copilot-adapter] Client created");
  }

  async *executePrompt(sessionId: string, text: string, promptConfig?: PromptConfig): AsyncGenerator<PromptEvent> {
    if (!this.client) {
      throw new Error("Copilot client not initialized");
    }

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
        // Build provider config for BYOK mode if Anthropic API key is available
        const providerConfig = this.config?.anthropicApiKey
          ? {
              type: "anthropic" as const,
              baseUrl: "https://api.anthropic.com",
              apiKey: this.config.anthropicApiKey,
            }
          : undefined;

        console.log(`[copilot-adapter] Creating new Copilot session`);
        console.log(`[copilot-adapter] Using BYOK provider: ${providerConfig ? "Anthropic" : "NONE (requires GitHub auth)"}`);
        console.log(`[copilot-adapter] Model: ${this.config?.model || "claude-sonnet-4-20250514"}`);

        session = await this.client.createSession({
          model: this.config?.model || "claude-sonnet-4-20250514",
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
          return {
            type: "thinking",
            thinkingId: `thinking_${event.id}`,
            content: data.deltaContent,
            partial: true,
          };
        }
        return null;
      }

      case "assistant.reasoning": {
        const data = event.data as { content?: string };
        return {
          type: "thinking",
          thinkingId: `thinking_${event.id}`,
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
