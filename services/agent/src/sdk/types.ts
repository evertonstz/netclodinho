/**
 * SDK abstraction layer types
 *
 * This module defines the interface for SDK adapters, allowing the agent
 * to use different AI SDKs (Claude Code SDK, OpenCode SDK, etc.)
 */

import type { JsonObject } from "@bufbuild/protobuf";

export type SdkType = "claude" | "opencode";

/**
 * Configuration for SDK initialization
 */
export interface SDKConfig {
  sdkType: SdkType;
  workspaceDir: string;
  anthropicApiKey: string;
  model?: string; // e.g., "anthropic/claude-sonnet-4-0" for OpenCode
}

/**
 * Configuration passed to executePrompt
 */
export interface PromptConfig {
  repo?: string;
  githubToken?: string;
}

/**
 * Event types emitted during prompt execution
 */
export type PromptEvent =
  | { type: "system"; message: string }
  | { type: "textDelta"; content: string; partial: boolean }
  | { type: "toolStart"; tool: string; toolUseId: string; parentToolUseId?: string; input?: JsonObject }
  | { type: "toolInput"; toolUseId: string; inputDelta: string; parentToolUseId?: string }
  | { type: "toolInputComplete"; toolUseId: string; parentToolUseId?: string; input: JsonObject }
  | { type: "toolEnd"; tool: string; toolUseId: string; result?: string; error?: string; parentToolUseId?: string }
  | { type: "thinking"; thinkingId: string; content: string; partial: boolean }
  | { type: "repoClone"; stage: "cloning" | "done" | "error"; repo: string; message: string }
  | { type: "result"; inputTokens: number; outputTokens: number; totalTurns: number }
  | { type: "error"; message: string; retryable: boolean };

/**
 * SDK Adapter interface
 * All SDK implementations must implement this interface
 */
export interface SDKAdapter {
  /**
   * Initialize the SDK adapter
   * Called once when the adapter is created
   */
  initialize(config: SDKConfig): Promise<void>;

  /**
   * Execute a prompt and yield events
   * @param sessionId - Netclode session ID (for session mapping)
   * @param text - The prompt text
   * @param config - Additional configuration (repo, github token)
   */
  executePrompt(sessionId: string, text: string, config?: PromptConfig): AsyncGenerator<PromptEvent>;

  /**
   * Set the interrupt signal to stop prompt execution
   */
  setInterruptSignal(): void;

  /**
   * Clear the interrupt signal
   */
  clearInterruptSignal(): void;

  /**
   * Check if interrupt was requested
   */
  isInterrupted(): boolean;

  /**
   * Get the current git repo (for system prompt)
   */
  getCurrentGitRepo(): string | null;

  /**
   * Shutdown the SDK adapter
   * Called when the agent is shutting down
   */
  shutdown(): Promise<void>;
}
