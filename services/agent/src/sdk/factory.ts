/**
 * SDK Adapter Factory
 *
 * Creates the appropriate SDK adapter based on configuration.
 */

import type { SDKAdapter, SDKConfig, SdkType } from "./types.js";
import { ClaudeSDKAdapter } from "./claude/index.js";
import { OpenCodeAdapter } from "./opencode/index.js";
import { CopilotAdapter } from "./copilot/index.js";
import { CodexAdapter } from "./codex/index.js";

// Singleton adapters per SDK type
const adapterInstances: Map<SdkType, SDKAdapter> = new Map();

/**
 * Create or get an SDK adapter instance
 * Adapters are singletons per SDK type
 */
export async function createSDKAdapter(config: SDKConfig): Promise<SDKAdapter> {
  const existing = adapterInstances.get(config.sdkType);
  if (existing) {
    return existing;
  }

  let adapter: SDKAdapter;

  switch (config.sdkType) {
    case "opencode":
      console.log("[sdk-factory] Creating OpenCode adapter");
      adapter = new OpenCodeAdapter();
      break;
    case "copilot":
      console.log("[sdk-factory] Creating Copilot adapter");
      adapter = new CopilotAdapter();
      break;
    case "codex":
      console.log("[sdk-factory] Creating Codex adapter");
      adapter = new CodexAdapter();
      break;
    case "claude":
    default:
      console.log("[sdk-factory] Creating Claude SDK adapter");
      adapter = new ClaudeSDKAdapter();
      break;
  }

  await adapter.initialize(config);
  adapterInstances.set(config.sdkType, adapter);

  return adapter;
}

/**
 * Get SDK type from proto enum string
 */
export function parseSdkType(sdkTypeStr: string | undefined): SdkType {
  if (!sdkTypeStr) return "claude";

  switch (sdkTypeStr) {
    case "SDK_TYPE_OPENCODE":
    case "OPENCODE":
    case "opencode":
      return "opencode";
    case "SDK_TYPE_COPILOT":
    case "COPILOT":
    case "copilot":
      return "copilot";
    case "SDK_TYPE_CODEX":
    case "CODEX":
    case "codex":
      return "codex";
    case "SDK_TYPE_CLAUDE":
    case "CLAUDE":
    case "claude":
    default:
      return "claude";
  }
}

/**
 * Get the current SDK adapter (if initialized)
 */
export function getAdapter(sdkType: SdkType): SDKAdapter | undefined {
  return adapterInstances.get(sdkType);
}

/**
 * Shutdown all adapters (called on agent shutdown)
 */
export async function shutdownAllAdapters(): Promise<void> {
  for (const [sdkType, adapter] of adapterInstances) {
    console.log(`[sdk-factory] Shutting down ${sdkType} adapter`);
    await adapter.shutdown();
  }
  adapterInstances.clear();
}
