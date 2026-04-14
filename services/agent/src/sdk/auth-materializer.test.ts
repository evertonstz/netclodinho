import { describe, expect, it, vi } from "vitest";
import {
  buildCodexAuthContent,
  buildOpenCodeAuthContent,
  CodexAuthMaterializer,
  OpenCodeAuthMaterializer,
} from "./auth-materializer.js";
import type { SDKConfig } from "./types.js";

function baseConfig(overrides: Partial<SDKConfig>): SDKConfig {
  return {
    sdkType: "opencode",
    workspaceDir: "/workspace",
    anthropicApiKey: "",
    ...overrides,
  };
}

describe("auth materializers", () => {
  it("builds OpenCode auth content for Copilot OAuth mode", () => {
    expect(buildOpenCodeAuthContent(baseConfig({
      sdkType: "opencode",
      model: "github-copilot/claude-haiku-4.5",
      githubCopilotOAuthAccessToken: "NETCLODE_PLACEHOLDER_github_copilot_oauth_access",
      githubCopilotOAuthRefreshToken: "NETCLODE_PLACEHOLDER_github_copilot_oauth_refresh",
      githubCopilotOAuthTokenExpires: "42",
    }))).toEqual({
      "github-copilot": {
        type: "oauth",
        refresh: "NETCLODE_PLACEHOLDER_github_copilot_oauth_refresh",
        access: "NETCLODE_PLACEHOLDER_github_copilot_oauth_access",
        expires: 42,
      },
    });
  });

  it("builds Codex auth content for OAuth mode", () => {
    expect(buildCodexAuthContent(baseConfig({
      sdkType: "codex",
      model: "gpt-5-codex:oauth",
      codexAccessToken: "NETCLODE_PLACEHOLDER_codex_oauth_access",
      codexIdToken: "NETCLODE_PLACEHOLDER_codex_oauth_id",
      codexRefreshToken: "NETCLODE_PLACEHOLDER_codex_oauth_refresh",
    }))).toEqual(expect.objectContaining({
      tokens: {
        access_token: "NETCLODE_PLACEHOLDER_codex_oauth_access",
        id_token: "NETCLODE_PLACEHOLDER_codex_oauth_id",
        refresh_token: "NETCLODE_PLACEHOLDER_codex_oauth_refresh",
      },
    }));
  });

  it("writes OpenCode auth.json through the injected file writer", async () => {
    const fileWriter = {
      mkdir: vi.fn(async () => {}),
      writeFile: vi.fn(async () => {}),
    };
    const materializer = new OpenCodeAuthMaterializer("opencode-adapter", fileWriter, "/tmp/opencode");

    await materializer.materialize(baseConfig({
      sdkType: "opencode",
      model: "github-copilot/claude-haiku-4.5",
      githubCopilotOAuthAccessToken: "NETCLODE_PLACEHOLDER_github_copilot_oauth_access",
      githubCopilotOAuthRefreshToken: "NETCLODE_PLACEHOLDER_github_copilot_oauth_refresh",
    }));

    expect(fileWriter.mkdir).toHaveBeenCalledWith("/tmp/opencode", { recursive: true });
    expect(fileWriter.writeFile).toHaveBeenCalledOnce();
  });

  it("writes Codex auth.json through the injected file writer", async () => {
    const fileWriter = {
      mkdir: vi.fn(async () => {}),
      writeFile: vi.fn(async () => {}),
    };
    const materializer = new CodexAuthMaterializer("codex-adapter", fileWriter, {}, "/tmp/home");

    await materializer.materialize(baseConfig({
      sdkType: "codex",
      model: "gpt-5-codex:oauth",
      codexAccessToken: "NETCLODE_PLACEHOLDER_codex_oauth_access",
      codexIdToken: "NETCLODE_PLACEHOLDER_codex_oauth_id",
    }));

    expect(fileWriter.mkdir).toHaveBeenCalledWith("/tmp/home/.codex", { recursive: true });
    expect(fileWriter.writeFile).toHaveBeenCalledOnce();
  });
});
