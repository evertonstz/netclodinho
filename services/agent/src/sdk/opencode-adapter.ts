/**
 * OpenCode SDK Adapter
 *
 * Spawns opencode serve and communicates via REST API + SSE events
 */

import { spawn, type ChildProcess } from "node:child_process";
import type { SDKAdapter, SDKConfig, PromptConfig, PromptEvent } from "./types.js";
import { isSessionInitialized, markSessionInitialized } from "../services/session.js";
import { setupRepository } from "../git.js";

const WORKSPACE_DIR = "/agent/workspace";
const OPENCODE_PORT = 4096;
const OPENCODE_HOST = "127.0.0.1";

interface OpenCodeServer {
  url: string;
  process: ChildProcess;
}

// OpenCode session ID mapping (Netclode session ID -> OpenCode session ID)
const openCodeSessionMap = new Map<string, string>();

export class OpenCodeAdapter implements SDKAdapter {
  private config: SDKConfig | null = null;
  private server: OpenCodeServer | null = null;
  private interruptSignal = false;
  private currentGitRepo: string | null = null;
  private currentGithubToken: string | null = null;

  async initialize(config: SDKConfig): Promise<void> {
    this.config = config;
    console.log("[opencode-adapter] Initializing with model:", config.model);

    // Start opencode serve process
    await this.startServer();
  }

  private async startServer(): Promise<void> {
    if (this.server) {
      console.log("[opencode-adapter] Server already running at", this.server.url);
      return;
    }

    console.log("[opencode-adapter] Starting opencode serve...");

    const args = ["serve", `--hostname=${OPENCODE_HOST}`, `--port=${OPENCODE_PORT}`];

    // Build OpenCode config as JSON
    const opencodeConfig = {
      model: this.config?.model || "anthropic/claude-sonnet-4-0",
      logLevel: "INFO",
      // Bypass all permissions for sandboxed environment
      permission: {
        edit: "allow",
        bash: "allow",
        webfetch: "allow",
        mcp: "allow",
      },
    };

    const proc = spawn("opencode", args, {
      env: {
        ...process.env,
        OPENCODE_CONFIG_CONTENT: JSON.stringify(opencodeConfig),
        // Use existing Anthropic API key
        ANTHROPIC_API_KEY: this.config?.anthropicApiKey || process.env.ANTHROPIC_API_KEY,
      },
      stdio: ["pipe", "pipe", "pipe"],
      cwd: WORKSPACE_DIR,
    });

    // Wait for server to be ready - use polling only, stdout detection can be unreliable
    const url = await new Promise<string>((resolve, reject) => {
      const timeout = setTimeout(() => {
        proc.kill();
        reject(new Error("Timeout waiting for opencode server to start"));
      }, 30000);

      let stdout = "";
      let stderr = "";
      let resolved = false;

      const doResolve = (url: string) => {
        if (resolved) return;
        resolved = true;
        clearInterval(pollInterval);
        clearTimeout(timeout);
        resolve(url);
      };

      proc.stdout?.on("data", (chunk) => {
        stdout += chunk.toString();
        console.log("[opencode-adapter] stdout:", chunk.toString().trim());
      });

      proc.stderr?.on("data", (chunk) => {
        stderr += chunk.toString();
        console.error("[opencode-adapter] stderr:", chunk.toString().trim());
      });

      proc.on("exit", (code) => {
        if (resolved) return;
        clearInterval(pollInterval);
        clearTimeout(timeout);
        reject(new Error(`opencode serve exited with code ${code}\nstdout: ${stdout}\nstderr: ${stderr}`));
      });

      proc.on("error", (error) => {
        if (resolved) return;
        clearInterval(pollInterval);
        clearTimeout(timeout);
        reject(error);
      });

      // Poll the server until it's ready - this is the reliable method
      // Only resolve when the server actually responds to HTTP requests
      const pollInterval = setInterval(async () => {
        if (resolved) return;
        try {
          const res = await fetch(`http://${OPENCODE_HOST}:${OPENCODE_PORT}/session`, {
            method: "GET",
            signal: AbortSignal.timeout(1000),
          });
          if (res.ok) {
            console.log("[opencode-adapter] Server responded to health check");
            doResolve(`http://${OPENCODE_HOST}:${OPENCODE_PORT}`);
          }
        } catch {
          // Server not ready yet, keep polling
        }
      }, 200);
    });

    this.server = { url, process: proc };
    console.log("[opencode-adapter] Server started at:", url);
  }

  async *executePrompt(sessionId: string, text: string, promptConfig?: PromptConfig): AsyncGenerator<PromptEvent> {
    if (!this.server) {
      throw new Error("OpenCode server not initialized");
    }

    console.log(
      `[opencode-adapter] ExecutePrompt (session=${sessionId}): "${text.slice(0, 100)}${text.length > 100 ? "..." : ""}"`
    );

    // Initialize repo for this session if needed
    if (sessionId && promptConfig) {
      if (!isSessionInitialized(sessionId)) {
        console.log(`[opencode-adapter] Initializing session ${sessionId}`);

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

    // Get or create OpenCode session
    let ocSessionId = openCodeSessionMap.get(sessionId);

    if (!ocSessionId) {
      // Create new OpenCode session
      const createResponse = await fetch(`${this.server.url}/session`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "x-opencode-directory": WORKSPACE_DIR,
        },
        body: JSON.stringify({}),
      });

      if (!createResponse.ok) {
        const errorText = await createResponse.text();
        throw new Error(`Failed to create OpenCode session: ${createResponse.statusText} - ${errorText}`);
      }

      const sessionData = (await createResponse.json()) as { id: string };
      ocSessionId = sessionData.id;
      openCodeSessionMap.set(sessionId, ocSessionId);
      console.log(`[opencode-adapter] Created OpenCode session: ${ocSessionId}`);
    } else {
      console.log(`[opencode-adapter] Reusing OpenCode session: ${ocSessionId}`);
    }

    // Clear interrupt signal
    this.clearInterruptSignal();

    // Create event queue for yielding
    const eventQueue: PromptEvent[] = [];
    let resolveNextEvent: ((value: PromptEvent | null) => void) | null = null;
    let completed = false;

    // Subscribe to events via SSE
    const eventUrl = `${this.server.url}/event?directory=${encodeURIComponent(WORKSPACE_DIR)}`;

    const processEvents = async () => {
      try {
        const eventResponse = await fetch(eventUrl, {
          headers: { Accept: "text/event-stream" },
        });

        if (!eventResponse.ok || !eventResponse.body) {
          throw new Error(`Failed to subscribe to events: ${eventResponse.statusText}`);
        }

        const reader = eventResponse.body.getReader();
        const decoder = new TextDecoder();
        let buffer = "";

        while (!this.interruptSignal && !completed) {
          const { done, value } = await reader.read();
          if (done) break;

          buffer += decoder.decode(value, { stream: true });
          const lines = buffer.split("\n");
          buffer = lines.pop() || "";

          for (const line of lines) {
            if (line.startsWith("data: ")) {
              const data = line.slice(6);
              if (data === "[DONE]") {
                completed = true;
                break;
              }
              try {
                const event = JSON.parse(data);
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
              } catch (e) {
                console.error("[opencode-adapter] Failed to parse event:", e, "data:", data);
              }
            }
          }
        }

        reader.releaseLock();
      } catch (error) {
        console.error("[opencode-adapter] Event stream error:", error);
        const errorEvent: PromptEvent = {
          type: "error",
          message: `Event stream error: ${error instanceof Error ? error.message : String(error)}`,
          retryable: false,
        };
        if (resolveNextEvent) {
          resolveNextEvent(errorEvent);
          resolveNextEvent = null;
        } else {
          eventQueue.push(errorEvent);
        }
      } finally {
        completed = true;
        if (resolveNextEvent) {
          resolveNextEvent(null);
        }
      }
    };

    // Start event processing in background
    const eventProcessor = processEvents();

    // Send prompt (async - returns immediately)
    try {
      const promptResponse = await fetch(`${this.server.url}/session/${ocSessionId}/prompt_async`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "x-opencode-directory": WORKSPACE_DIR,
        },
        body: JSON.stringify({
          parts: [{ type: "text", text }],
        }),
      });

      if (!promptResponse.ok) {
        const errorText = await promptResponse.text();
        throw new Error(`Failed to send prompt: ${promptResponse.statusText} - ${errorText}`);
      }
    } catch (error) {
      yield {
        type: "error",
        message: `Failed to send prompt: ${error instanceof Error ? error.message : String(error)}`,
        retryable: false,
      };
      return;
    }

    // Yield events from queue
    while (!completed || eventQueue.length > 0) {
      if (this.interruptSignal) {
        yield { type: "system", message: "interrupted" };
        return;
      }

      if (eventQueue.length > 0) {
        yield eventQueue.shift()!;
      } else if (!completed) {
        // Wait for next event
        const event = await new Promise<PromptEvent | null>((resolve) => {
          resolveNextEvent = resolve;
        });
        if (event) {
          yield event;
        }
      }
    }

    await eventProcessor;
  }

  /**
   * Translate OpenCode events to PromptEvent format
   */
  private translateEvent(event: { type: string; properties?: Record<string, unknown> }): PromptEvent | null {
    const props = event.properties || {};

    switch (event.type) {
      case "message.part.updated": {
        const part = props.part as Record<string, unknown> | undefined;
        const delta = props.delta as string | undefined;

        if (!part) return null;

        switch (part.type) {
          case "text":
            return {
              type: "textDelta",
              content: delta || (part.text as string) || "",
              partial: !!delta,
            };

          case "reasoning":
            return {
              type: "thinking",
              thinkingId: (part.id as string) || `thinking_${Date.now()}`,
              content: delta || (part.text as string) || "",
              partial: !!delta,
            };

          case "tool": {
            const state = part.state as Record<string, unknown> | undefined;
            if (!state) return null;

            const status = state.status as string;
            const toolName = part.tool as string;
            const callId = part.callID as string;

            if (status === "pending" || status === "running") {
              // Only emit toolStart once when first seen
              if (status === "pending") {
                return {
                  type: "toolStart",
                  tool: toolName,
                  toolUseId: callId,
                  input: state.input as Record<string, unknown> as import("@bufbuild/protobuf").JsonObject | undefined,
                };
              }
              return null;
            } else if (status === "completed") {
              return {
                type: "toolEnd",
                tool: toolName,
                toolUseId: callId,
                result: state.output as string | undefined,
              };
            } else if (status === "error") {
              return {
                type: "toolEnd",
                tool: toolName,
                toolUseId: callId,
                error: state.error as string | undefined,
              };
            }
            return null;
          }
        }
        return null;
      }

      case "message.updated": {
        const info = props.info as Record<string, unknown> | undefined;
        if (!info) return null;

        if (info.role === "assistant" && info.time) {
          const time = info.time as Record<string, unknown>;
          if (time.completed) {
            const tokens = info.tokens as Record<string, number> | undefined;
            return {
              type: "result",
              inputTokens: tokens?.input || 0,
              outputTokens: tokens?.output || 0,
              totalTurns: 1,
            };
          }
        }

        // Check for errors
        if (info.error) {
          const error = info.error as Record<string, unknown>;
          const errorData = error.data as Record<string, string> | undefined;
          return {
            type: "error",
            message: errorData?.message || "Unknown error",
            retryable: false,
          };
        }
        return null;
      }

      default:
        return null;
    }
  }

  setInterruptSignal(): void {
    this.interruptSignal = true;
    console.log("[opencode-adapter] Interrupt signal set");
  }

  clearInterruptSignal(): void {
    this.interruptSignal = false;
  }

  isInterrupted(): boolean {
    return this.interruptSignal;
  }

  getCurrentGitRepo(): string | null {
    return this.currentGitRepo;
  }

  async shutdown(): Promise<void> {
    console.log("[opencode-adapter] Shutting down...");

    if (this.server) {
      this.server.process.kill();
      this.server = null;
    }

    openCodeSessionMap.clear();
  }
}
