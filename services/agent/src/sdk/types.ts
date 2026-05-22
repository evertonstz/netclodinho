/**
 * Netclode agent backend contract.
 *
 * The control-plane speaks protobuf to the sandbox agent. Inside the sandbox,
 * Netclode owns this backend/runtime contract so provider-specific SDKs remain
 * implementation details rather than the primary mental model.
 */

import type { JsonObject } from "@bufbuild/protobuf";

export type SdkType = "claude" | "opencode" | "copilot" | "codex";

/**
 * Backend for Copilot sessions.
 */
export type CopilotBackend = "github" | "anthropic";

/**
 * Explicit capability declaration for Netclode backends/runtime.
 */
export interface AgentCapabilities {
  interrupt: boolean;
  titleGeneration: boolean;
  gitStatus: boolean;
  gitDiff: boolean;
  toolStreaming: boolean;
  thinkingStreaming: boolean;
}

export const DEFAULT_AGENT_CAPABILITIES: AgentCapabilities = {
  interrupt: false,
  titleGeneration: false,
  gitStatus: false,
  gitDiff: false,
  toolStreaming: false,
  thinkingStreaming: false,
};

export type AgentCapabilityName = keyof AgentCapabilities;

export function createAgentCapabilities(overrides: Partial<AgentCapabilities> = {}): AgentCapabilities {
  return {
    ...DEFAULT_AGENT_CAPABILITIES,
    ...overrides,
  };
}

export class UnsupportedAgentCapabilityError extends Error {
  readonly capability: AgentCapabilityName;

  constructor(capability: AgentCapabilityName, message?: string) {
    super(message ?? `Agent capability '${capability}' is not supported`);
    this.name = "UnsupportedAgentCapabilityError";
    this.capability = capability;
  }
}

/**
 * Configuration used to initialize a Netclode backend/runtime.
 */
export interface NetclodeAgentConfig {
  sdkType: SdkType;
  workspaceDir: string;
  anthropicApiKey: string;
  openaiApiKey?: string; // OpenAI API key (for Codex API mode)
  mistralApiKey?: string; // Mistral API key (for OpenCode backend)
  githubCopilotToken?: string; // GitHub PAT with Copilot scope (for Copilot backend auth)
  model?: string; // e.g. anthropic/claude-sonnet-4-0 or codex-mini-latest:api
  copilotBackend?: CopilotBackend;
  codexAccessToken?: string;
  codexIdToken?: string;
  codexRefreshToken?: string;
  reasoningEffort?: string;
  ollamaUrl?: string;
  openCodeApiKey?: string;
  zaiApiKey?: string;
  openRouterApiKey?: string; // OpenRouter API key (multi-provider gateway, for OpenCode sessions)
  githubCopilotOAuthAccessToken?: string;
  githubCopilotOAuthRefreshToken?: string;
  githubCopilotOAuthTokenExpires?: string;
}

/**
 * Configuration passed to prompt execution.
 */
export interface PromptConfig {
  repos?: string[];
  githubToken?: string;
}

/**
 * Context for repo-aware helper operations.
 */
export interface RepositoryContext {
  repos?: string[];
  githubToken?: string;
}

/**
 * Normalized git file change shape used by the Netclode runtime.
 */
export interface AgentGitFileChange {
  path: string;
  status: "modified" | "added" | "deleted" | "renamed" | "copied" | "untracked" | "ignored" | "unmerged";
  staged: boolean;
  linesAdded?: number;
  linesRemoved?: number;
  repo: string;
}

/**
 * Event types emitted during prompt execution.
 */
export type PromptEvent =
  | { type: "system"; message: string }
  | { type: "textDelta"; content: string; partial: boolean; messageId?: string }
  | { type: "toolStart"; tool: string; toolUseId: string; parentToolUseId?: string; input?: JsonObject }
  | { type: "toolInput"; toolUseId: string; inputDelta: string; parentToolUseId?: string }
  | { type: "toolInputComplete"; toolUseId: string; parentToolUseId?: string; input: JsonObject }
  | { type: "toolEnd"; tool: string; toolUseId: string; result?: string; error?: string; parentToolUseId?: string; durationMs?: number }
  | { type: "thinking"; thinkingId: string; content: string; partial: boolean }
  | { type: "repoClone"; stage: "cloning" | "done" | "error"; repo: string; message: string }
  | { type: "result"; inputTokens: number; outputTokens: number; totalTurns: number }
  | { type: "error"; message: string; retryable: boolean };

/**
 * Backend-specific prompt runner. SDKs/providers implement this interface.
 */
export interface NetclodePromptBackend {
  readonly capabilities: AgentCapabilities;
  initialize(config: NetclodeAgentConfig): Promise<void>;
  executePrompt(sessionId: string, text: string, config?: PromptConfig): AsyncGenerator<PromptEvent>;
  interrupt(): Promise<void> | void;
  shutdown(): Promise<void>;
}

export interface TitleGenerator {
  generateTitle(prompt: string): Promise<string>;
}

export interface GitInspector {
  getGitStatus(context?: RepositoryContext): Promise<AgentGitFileChange[]>;
  getGitDiff(context?: RepositoryContext, file?: string): Promise<string>;
}

export interface SessionBootstrapper {
  initializeSessionRepos(sessionId: string, repos: string[], githubToken?: string): AsyncGenerator<PromptEvent>;
}

/**
 * Composed runtime used by the sandbox transport adapter.
 */
export interface NetclodeAgent {
  readonly capabilities: AgentCapabilities;
  executePrompt(sessionId: string, text: string, config?: PromptConfig): AsyncGenerator<PromptEvent>;
  interrupt(): Promise<void>;
  generateTitle(prompt: string): Promise<string>;
  getGitStatus(context?: RepositoryContext): Promise<AgentGitFileChange[]>;
  getGitDiff(context?: RepositoryContext, file?: string): Promise<string>;
  shutdown(): Promise<void>;
}

/**
 * Transitional compatibility aliases. Prefer the Netclode* names in new code.
 */
export type SDKConfig = NetclodeAgentConfig;
export type SDKAdapter = NetclodePromptBackend;
