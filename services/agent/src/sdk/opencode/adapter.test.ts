import { describe, expect, it } from "vitest";
import { OpenCodeAdapter } from "./adapter.js";
import type { SDKConfig } from "../types.js";

function makeConfig(overrides: Partial<SDKConfig> = {}): SDKConfig {
  return {
    sdkType: "opencode",
    workspaceDir: "/workspace",
    anthropicApiKey: "",
    model: "github-copilot/claude-haiku-4.5",
    githubCopilotOAuthAccessToken: "NETCLODE_PLACEHOLDER_github_copilot_oauth_access",
    githubCopilotOAuthRefreshToken: "NETCLODE_PLACEHOLDER_github_copilot_oauth_refresh",
    githubCopilotOAuthTokenExpires: "1234567890",
    ...overrides,
  };
}

describe("OpenCodeAdapter auth materialization", () => {
  it("builds auth.json with the Copilot OAuth values provided in session config", () => {
    const adapter = new OpenCodeAdapter() as unknown as {
      config: SDKConfig;
      buildOpencodeAuthContent: () => Record<string, unknown> | null;
    };
    adapter.config = makeConfig();

    const authContent = adapter.buildOpencodeAuthContent();
    expect(authContent).toEqual({
      "github-copilot": {
        type: "oauth",
        refresh: "NETCLODE_PLACEHOLDER_github_copilot_oauth_refresh",
        access: "NETCLODE_PLACEHOLDER_github_copilot_oauth_access",
        expires: 1234567890,
      },
    });
    expect(JSON.stringify(authContent)).toContain("NETCLODE_PLACEHOLDER_github_copilot_oauth_access");
  });

  it("does not build auth.json for non-Copilot OpenCode models", () => {
    const adapter = new OpenCodeAdapter() as unknown as {
      config: SDKConfig;
      buildOpencodeAuthContent: () => Record<string, unknown> | null;
    };
    adapter.config = makeConfig({ model: "anthropic/claude-sonnet-4-5" });

    expect(adapter.buildOpencodeAuthContent()).toBeNull();
  });
});
