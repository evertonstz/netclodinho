/**
 * Netclode backend factory.
 *
 * Creates provider-backed prompt backends and composes them into the runtime
 * used by the sandbox transport layer.
 */

import type { NetclodeAgent, NetclodeAgentConfig, NetclodePromptBackend, SdkType } from "./types.js";
import { ClaudeSDKAdapter } from "./claude/index.js";
import { OpenCodeAdapter } from "./opencode/index.js";
import { CopilotAdapter } from "./copilot/index.js";
import { CodexAdapter } from "./codex/index.js";
import {
  ComposedNetclodeAgent,
  createGitInspector,
  createSessionBootstrapper,
  createTitleGenerator,
  type NetclodeAgentDependencies,
} from "./runtime.js";

export type PromptBackendFactory = () => NetclodePromptBackend;

export interface NetclodeAgentFactoryDependencies extends NetclodeAgentDependencies {
  backendFactories?: Partial<Record<SdkType, PromptBackendFactory>>;
}

const defaultBackendFactories: Record<SdkType, PromptBackendFactory> = {
  claude: () => new ClaudeSDKAdapter(),
  opencode: () => new OpenCodeAdapter(),
  copilot: () => new CopilotAdapter(),
  codex: () => new CodexAdapter(),
};

function resolveBackendFactory(
  sdkType: SdkType,
  dependencies: NetclodeAgentFactoryDependencies = {},
): PromptBackendFactory {
  return dependencies.backendFactories?.[sdkType] ?? defaultBackendFactories[sdkType] ?? defaultBackendFactories.claude;
}

export function createPromptBackend(
  sdkType: SdkType,
  dependencies: NetclodeAgentFactoryDependencies = {},
): NetclodePromptBackend {
  return resolveBackendFactory(sdkType, dependencies)();
}

export async function createNetclodeAgent(
  config: NetclodeAgentConfig,
  dependencies: NetclodeAgentFactoryDependencies = {},
): Promise<NetclodeAgent> {
  const backend = createPromptBackend(config.sdkType, dependencies);
  await backend.initialize(config);

  return new ComposedNetclodeAgent(backend, {
    titleGenerator: dependencies.titleGenerator ?? createTitleGenerator(),
    gitInspector: dependencies.gitInspector ?? createGitInspector(config.workspaceDir),
    sessionBootstrapper: dependencies.sessionBootstrapper ?? createSessionBootstrapper(),
  });
}

export function createNetclodeAgentFactory(
  dependencies: NetclodeAgentFactoryDependencies = {},
): (config: NetclodeAgentConfig) => Promise<NetclodeAgent> {
  return (config: NetclodeAgentConfig) => createNetclodeAgent(config, dependencies);
}

/**
 * Get SDK type from proto enum string.
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
 * Transitional compatibility helpers. Prefer createNetclodeAgent in new code.
 */
export const createSDKAdapter = createNetclodeAgent;
export const createSDKAdapterFactory = createNetclodeAgentFactory;
