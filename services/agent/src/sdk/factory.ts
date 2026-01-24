/**
 * SDK Adapter Factory
 *
 * Creates the appropriate SDK adapter based on configuration.
 */

import type { SDKAdapter, SDKConfig, SdkType } from "./types.js";
import { ClaudeSDKAdapter } from "./claude-adapter.js";
import { OpenCodeAdapter } from "./opencode-adapter.js";

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
