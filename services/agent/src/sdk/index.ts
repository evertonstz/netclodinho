/**
 * SDK Abstraction Layer
 *
 * Provides a unified interface for different AI SDK backends.
 */

// Types
export type {
  SDKAdapter,
  SDKConfig,
  PromptConfig,
  PromptEvent,
  SdkType,
  CopilotBackend,
} from "./types.js";

// Factory
export {
  createSDKAdapter,
  parseSdkType,
  getAdapter,
  shutdownAllAdapters,
} from "./factory.js";

// Adapters
export { ClaudeSDKAdapter } from "./claude/index.js";
export { OpenCodeAdapter } from "./opencode/index.js";
export { CopilotAdapter, type CopilotModelInfo } from "./copilot/index.js";
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
