import { describe, it, expect, vi } from "vitest";
import { ClaudeSDKAdapter } from "./claude/index.js";
import { CodexAdapter } from "./codex/index.js";
import { CopilotAdapter } from "./copilot/index.js";
import { OpenCodeAdapter } from "./opencode/index.js";
import { PiAdapter } from "./pi/index.js";
import {
  createNetclodeAgent,
  createNetclodeAgentFactory,
  createPromptBackend,
} from "./factory.js";
import { createAgentCapabilities, type NetclodePromptBackend, type SDKConfig } from "./types.js";

function makeConfig(overrides: Partial<SDKConfig> = {}): SDKConfig {
  return {
    sdkType: "claude",
    workspaceDir: "/workspace",
    anthropicApiKey: "test-key",
    ...overrides,
  };
}

describe("backend factory", () => {
  it("creates the expected default prompt backend for each sdk type", () => {
    expect(createPromptBackend("claude")).toBeInstanceOf(ClaudeSDKAdapter);
    expect(createPromptBackend("opencode")).toBeInstanceOf(OpenCodeAdapter);
    expect(createPromptBackend("copilot")).toBeInstanceOf(CopilotAdapter);
    expect(createPromptBackend("codex")).toBeInstanceOf(CodexAdapter);
    expect(createPromptBackend("pi")).toBeInstanceOf(PiAdapter);
  });

  it("creates a composed Netclode agent with injected dependencies", async () => {
    const initialize = vi.fn(async () => {});
    const backend: NetclodePromptBackend = {
      capabilities: createAgentCapabilities({ interrupt: true }),
      initialize,
      async *executePrompt() {
        yield { type: "system", message: "ok" };
      },
      async interrupt() {},
      async shutdown() {},
    };

    const agent = await createNetclodeAgent(makeConfig(), {
      backendFactories: {
        claude: () => backend,
      },
      titleGenerator: { generateTitle: async () => "title" },
      gitInspector: {
        getGitStatus: async () => [],
        getGitDiff: async () => "",
      },
    });

    expect(initialize).toHaveBeenCalledWith(makeConfig());
    expect(agent.capabilities.titleGeneration).toBe(true);
    expect(agent.capabilities.gitStatus).toBe(true);
    expect(agent.capabilities.interrupt).toBe(true);
  });

  it("builds reusable factories with injected dependencies", async () => {
    const initialize = vi.fn(async () => {});
    const factory = createNetclodeAgentFactory({
      backendFactories: {
        claude: () => ({
          capabilities: createAgentCapabilities({ interrupt: true }),
          initialize,
          async *executePrompt() {
            yield { type: "system", message: "ok" };
          },
          async interrupt() {},
          async shutdown() {},
        }),
      },
    });

    await factory(makeConfig());
    expect(initialize).toHaveBeenCalledTimes(1);
  });
});
