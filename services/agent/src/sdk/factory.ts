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
import { PiAdapter } from "./pi/index.js";
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
  pi: () => new PiAdapter(),
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


