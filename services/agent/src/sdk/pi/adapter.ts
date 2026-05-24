/**
 * Pi backend
 *
 * Uses @earendil-works/pi-agent-core Agent class with a subscribe-based
 * event model. Bridges to our async-generator PromptEvent contract via
 * a Promise + push queue pattern.
 *
 * Auth: file-based OAuth identical to OpenCode. Writes ~/.pi/agent/auth.json
 * using the same buildOpenCodeAuthContent() builder. Non-Copilot providers
 * use env vars (ANTHROPIC_API_KEY, MISTRAL_API_KEY, OPENROUTER_API_KEY)
 * which BoxLite sets via secret proxy.
 */

import * as fs from "node:fs/promises";
import { readFileSync } from "node:fs";
import { Agent } from "@earendil-works/pi-agent-core";
import { getModel } from "@earendil-works/pi-ai";
import { createCodingTools } from "@earendil-works/pi-coding-agent";
import type { NetclodePromptBackend, SDKConfig, PromptConfig, PromptEvent } from "../types.js";
import { createAgentCapabilities } from "../types.js";
import { buildOpenCodeAuthContent } from "../auth-materializer.js";
import {
  createPiTranslatorState,
  resetPiTranslatorState,
  translatePiEvent,
  flushOpenThinking,
  flushOpenTools,
  type PiTranslatorState,
  type PiAgentEvent,
} from "./translator.js";
import { getSdkSessionId, registerSession } from "../../services/session.js";
import { WORKSPACE_DIR } from "../../constants.js";

// ── Pi agent user/group workaround ─────────────────────────────────────────
// BoxLite VMs run as agent:agent (uid 1000), so the agent home is /agent.
// Pi reads ~/.pi/agent/auth.json — write it at /agent/.pi/agent/auth.json.
const PI_AUTH_DIR = "/agent/.pi/agent";
const PI_AUTH_FILE = "/agent/.pi/agent/auth.json";

// ── Model string parsing ───────────────────────────────────────────────────

interface ParsedModel {
  provider: string;
  modelId: string;
}

function parseModelString(modelStr: string | undefined): ParsedModel {
  if (!modelStr) return { provider: "anthropic", modelId: "claude-sonnet-4-20250514" };

  // Pi model format: provider/model-id (e.g. "anthropic/claude-sonnet-4-0")
  // OpenRouter: openrouter/provider/model-id → provider="openrouter", modelId="provider/model-id"
  const slashIdx = modelStr.indexOf("/");
  if (slashIdx === -1) return { provider: "anthropic", modelId: modelStr };

  const provider = modelStr.slice(0, slashIdx);
  const modelId = modelStr.slice(slashIdx + 1);

  return { provider, modelId };
}

// ── Adapter ────────────────────────────────────────────────────────────────

export class PiAdapter implements NetclodePromptBackend {
  readonly capabilities = createAgentCapabilities({
    interrupt: true,
    toolStreaming: true,
    thinkingStreaming: true,
  });

  private config: SDKConfig | null = null;
  private agent: Agent | null = null;
  private translatorState: PiTranslatorState = createPiTranslatorState();

  // ── Initialization ─────────────────────────────────────────────────────

  async initialize(config: SDKConfig): Promise<void> {
    this.config = config;
    console.log("[pi-backend] Initializing with model:", config.model);

    await this.writeAuthFile(config);

    this.agent = await this.createAgent(config);
  }

  private async writeAuthFile(config: SDKConfig): Promise<void> {
    const authContent = buildOpenCodeAuthContent(config);
    if (!authContent) return;

    await fs.mkdir(PI_AUTH_DIR, { recursive: true });
    await fs.writeFile(PI_AUTH_FILE, JSON.stringify(authContent, null, 2), {
      encoding: "utf-8",
      mode: 0o600,
    });
    console.log("[pi-backend] Wrote auth.json for Copilot OAuth (file-based mode)");
  }

  private async createAgent(config: SDKConfig): Promise<Agent> {
    const { provider, modelId } = parseModelString(config.model);
    const model = getModel(provider as never, modelId as never);

    if (!model) {
      throw new Error(
        `[pi-backend] Model not found: ${provider}/${modelId}. ` +
        `Check that the provider and model ID are supported by @earendil-works/pi-ai.`,
      );
    }

    // API keys via env vars (set by BoxLite/secret proxy)
    if (config.anthropicApiKey) process.env.ANTHROPIC_API_KEY = config.anthropicApiKey;
    if (config.openaiApiKey) process.env.OPENAI_API_KEY = config.openaiApiKey;
    if (config.mistralApiKey) process.env.MISTRAL_API_KEY = config.mistralApiKey;
    if (config.openRouterApiKey) process.env.OPENROUTER_API_KEY = config.openRouterApiKey;

    const thinkingLevel = config.reasoningEffort === "max"
      ? "xhigh" as const
      : config.reasoningEffort === "high"
        ? "high" as const
        : config.reasoningEffort === "medium"
          ? "medium" as const
          : "off" as const;

    const codingTools = createCodingTools(WORKSPACE_DIR);
    const agent = new Agent({
      initialState: {
        systemPrompt:
          "You are a helpful coding assistant. " +
          "You have access to file read/write, bash commands, and editing tools. " +
          "Work step by step. Be thorough.",
        model,
        thinkingLevel,
        tools: codingTools,
      },
      getApiKey: (providerName: string) => {
        // Only handle GitHub Copilot OAuth — other providers use env vars
        if (providerName !== "github-copilot") return undefined;
        try {
          const raw = readFileSync(PI_AUTH_FILE, "utf-8");
          const parsed = JSON.parse(raw);
          return parsed["github-copilot"]?.access || undefined;
        } catch {
          return undefined;
        }
      },
    });

    console.log("[pi-backend] Agent created with model:", provider, modelId);
    return agent;
  }

  // ── Session management ─────────────────────────────────────────────────

  private getOrCreateSessionId(controlPlaneSessionId: string): string {
    let sdkSessionId = getSdkSessionId(controlPlaneSessionId);
    if (!sdkSessionId) {
      // Use the same ID — no server-side session in Pi's Agent class.
      // Session persistence is handled by JsonlSessionRepo on disk.
      sdkSessionId = controlPlaneSessionId;
      registerSession(controlPlaneSessionId, sdkSessionId);
    }
    return sdkSessionId;
  }

  // ── Prompt execution ───────────────────────────────────────────────────

  async *executePrompt(
    sessionId: string,
    text: string,
    _promptConfig?: PromptConfig,
  ): AsyncGenerator<PromptEvent> {
    if (!this.agent) {
      yield { type: "error", message: "Pi agent not initialized", retryable: false };
      return;
    }

    const sdkSessionId = this.getOrCreateSessionId(sessionId);
    this.agent.sessionId = sdkSessionId;

    console.log(
      `[pi-backend] ExecutePrompt (session=${sessionId}): "${text.slice(0, 100)}${text.length > 100 ? "..." : ""}"`,
    );

    // Close any thinking bubbles still open from the previous prompt
    for (const evt of flushOpenThinking(this.translatorState)) {
      yield evt;
    }

    for (const evt of flushOpenTools(this.translatorState)) {
      yield evt;
    }

    resetPiTranslatorState(this.translatorState);

    // Event bridge: subscribe → push to queue → yield from generator
    const eventQueue: PromptEvent[] = [];
    let resolveNext: (() => void) | null = null;
    let promptDone = false;
    let promptError: Error | null = null;

    const unsub = this.agent.subscribe((rawEvent, _signal) => {
      try {
        const translated = translatePiEvent(
          rawEvent as unknown as PiAgentEvent,
          this.translatorState,
        );
        if (translated) {
          eventQueue.push(translated);
          resolveNext?.();
        }
      } catch (err) {
        console.error("[pi-backend] Translation error:", err);
      }
    });

    // Start prompt (non-blocking)
    this.agent.prompt(text).then(
      () => {
        promptDone = true;
        resolveNext?.();
      },
      (err: Error) => {
        promptError = err;
        resolveNext?.();
      },
    );

    // Drain queue until done
    try {
      while (!promptDone && !promptError) {
        while (eventQueue.length > 0) {
          yield eventQueue.shift()!;
        }
        if (!promptDone && !promptError) {
          await new Promise<void>((r) => { resolveNext = r; });
        }
      }

      // Drain remaining events
      while (eventQueue.length > 0) {
        yield eventQueue.shift()!;
      }

      if (promptError) {
        const err = promptError as Error;
        // Close any thinking/tool bubbles still open (stream may have errored mid-flight)
        for (const evt of flushOpenThinking(this.translatorState)) {
          yield evt;
        }
        for (const evt of flushOpenTools(this.translatorState)) {
          yield evt;
        }
        yield {
          type: "error",
          message: err.message,
          retryable: true,
        };
      }
    } finally {
      unsub();
    }
  }

  // ── Interrupt ──────────────────────────────────────────────────────────

  interrupt(): void {
    console.log("[pi-backend] Interrupt requested");
    // abort() is synchronous (void return)
    this.agent?.abort();
  }

  // ── Shutdown ───────────────────────────────────────────────────────────

  async shutdown(): Promise<void> {
    console.log("[pi-backend] Shutting down...");
    resetPiTranslatorState(this.translatorState);
    this.agent = null;
    this.config = null;
  }
}
