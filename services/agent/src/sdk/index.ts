/**
 * Netclode agent backend layer.
 *
 * Transport-facing code should depend on the Netclode-owned runtime/backend
 * contract defined here rather than vendor SDK concepts.
 */

// Types
export type {
  AgentCapabilities,
  AgentCapabilityName,
  AgentGitFileChange,
  NetclodeAgent,
  NetclodeAgentConfig,
  NetclodePromptBackend,
  TitleGenerator,
  GitInspector,
  SessionBootstrapper,
  RepositoryContext,
  SDKAdapter,
  SDKConfig,
  PromptConfig,
  PromptEvent,
  SdkType,
  CopilotBackend,
} from "./types.js";
export {
  DEFAULT_AGENT_CAPABILITIES,
  UnsupportedAgentCapabilityError,
  createAgentCapabilities,
} from "./types.js";

// Runtime composition
export {
  ComposedNetclodeAgent,
  createGitInspector,
  createSessionBootstrapper,
  createTitleGenerator,
  type NetclodeAgentDependencies,
  type GitInspectorDependencies,
} from "./runtime.js";

// Factory
export {
  createNetclodeAgent,
  createNetclodeAgentFactory,
  createPromptBackend,
  createSDKAdapter,
  createSDKAdapterFactory,
  parseSdkType,
} from "./factory.js";

// Adapters
export { ClaudeSDKAdapter } from "./claude/index.js";
export { OpenCodeAdapter } from "./opencode/index.js";
export { CopilotAdapter } from "./copilot/index.js";
export { CodexAdapter } from "./codex/index.js";

// Utilities
export {
  normalizeToolName,
  toSnakeCase,
  normalizeToolInput,
  generateThinkingId,
  generateMessageId,
  parseToolInput,
  calculateDuration,
  TOOL_NAME_MAP,
} from "./utils/index.js";

export {
  getSecretMaterializationDecisions,
  getOpenCodeProvider,
  isOpenCodeCopilotOAuthMode,
  isCodexOAuthMode,
  logSecretMaterialization,
  type SecretMaterializationDecision,
  type SecretMaterializationMode,
} from "./secret-materialization.js";

export {
  buildOpenCodeAuthContent,
  buildCodexAuthContent,
  NoopAuthMaterializer,
  OpenCodeAuthMaterializer,
  CodexAuthMaterializer,
  type BackendAuthMaterializer,
  type AuthFileWriter,
} from "./auth-materializer.js";
