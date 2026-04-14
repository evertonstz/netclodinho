import { describe, expect, it } from "vitest";
import { OpenCodeAdapter } from "./adapter.js";
import type { SDKConfig } from "../types.js";

function makeConfig(overrides: Partial<SDKConfig> = {}): SDKConfig {
  return {
    sdkType: "opencode",
    workspaceDir: "/workspace",
    anthropicApiKey: "",
    model: "github-copilot/claude-haiku-4.5",
    githubCopilotOAuthAccessToken: "real-access-token",
    githubCopilotOAuthRefreshToken: "real-refresh-token",
    githubCopilotOAuthTokenExpires: "1234567890",
    ...overrides,
  };
}

describe("OpenCodeAdapter auth materialization", () => {
  it("builds auth.json with real Copilot OAuth tokens instead of placeholders", () => {
    const adapter = new OpenCodeAdapter() as unknown as {
      config: SDKConfig;
      buildOpencodeAuthContent: () => Record<string, unknown> | null;
    };
    adapter.config = makeConfig();

    const authContent = adapter.buildOpencodeAuthContent();
    expect(authContent).toEqual({
      "github-copilot": {
        type: "oauth",
        refresh: "real-refresh-token",
        access: "real-access-token",
        expires: 1234567890,
      },
    });
    expect(JSON.stringify(authContent)).not.toContain("NETCLODE_PLACEHOLDER");
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
