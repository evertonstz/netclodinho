import { describe, expect, it } from "vitest";
import {
  getOpenCodeProvider,
  getSecretMaterializationDecisions,
  isCodexOAuthMode,
  isOpenCodeCopilotOAuthMode,
} from "./secret-materialization.js";
import type { SDKConfig } from "./types.js";

function baseConfig(overrides: Partial<SDKConfig>): SDKConfig {
  return {
    sdkType: "claude",
    workspaceDir: "/workspace",
    anthropicApiKey: "",
    ...overrides,
  };
}

describe("secret materialization policy", () => {
  it("classifies Claude as placeholder-header", () => {
    const decisions = getSecretMaterializationDecisions(baseConfig({ sdkType: "claude" }));
    expect(decisions).toEqual([
      expect.objectContaining({ credential: "anthropic", mode: "placeholder-header", source: "env" }),
    ]);
  });

  it("classifies OpenCode GitHub Copilot OAuth as direct-file", () => {
    const decisions = getSecretMaterializationDecisions(
      baseConfig({
        sdkType: "opencode",
        model: "github-copilot/claude-haiku-4.5",
        githubCopilotOAuthAccessToken: "access-token",
        githubCopilotOAuthRefreshToken: "refresh-token",
      })
    );

    expect(decisions).toEqual([
      expect.objectContaining({ credential: "github-copilot-oauth", mode: "direct-file", source: "session-config" }),
    ]);
  });

  it("classifies OpenCode Anthropic as placeholder-header", () => {
    const decisions = getSecretMaterializationDecisions(
      baseConfig({ sdkType: "opencode", model: "anthropic/claude-sonnet-4-5" })
    );

    expect(decisions).toEqual([
      expect.objectContaining({ credential: "anthropic", mode: "placeholder-header", source: "env" }),
    ]);
  });

  it("classifies Codex OAuth mode as direct-file", () => {
    const decisions = getSecretMaterializationDecisions(
      baseConfig({
        sdkType: "codex",
        model: "gpt-5-codex:oauth:high",
        codexAccessToken: "NETCLODE_PLACEHOLDER_codex_oauth_access",
        codexIdToken: "NETCLODE_PLACEHOLDER_codex_oauth_id",
      })
    );

    expect(decisions).toEqual([
      expect.objectContaining({ credential: "codex-oauth", mode: "direct-file", source: "session-config" }),
    ]);
  });

  it("detects OpenCode Copilot OAuth mode from provider and tokens", () => {
    const config = baseConfig({
      sdkType: "opencode",
      model: "github-copilot/claude-haiku-4.5",
      githubCopilotOAuthAccessToken: "access",
    });

    expect(getOpenCodeProvider(config)).toBe("github-copilot");
    expect(isOpenCodeCopilotOAuthMode(config)).toBe(true);
  });

  it("detects Codex OAuth mode from model suffix", () => {
    expect(isCodexOAuthMode(baseConfig({ sdkType: "codex", model: "gpt-5-codex:oauth" }))).toBe(true);
    expect(isCodexOAuthMode(baseConfig({ sdkType: "codex", model: "gpt-5-codex:api", openaiApiKey: "sk-openai" }))).toBe(false);
  });
});
