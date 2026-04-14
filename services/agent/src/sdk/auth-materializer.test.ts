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
      githubCopilotOAuthAccessToken: "access",
      githubCopilotOAuthRefreshToken: "refresh",
      githubCopilotOAuthTokenExpires: "42",
    }))).toEqual({
      "github-copilot": {
        type: "oauth",
        refresh: "refresh",
        access: "access",
        expires: 42,
      },
    });
  });

  it("builds Codex auth content for OAuth mode", () => {
    expect(buildCodexAuthContent(baseConfig({
      sdkType: "codex",
      model: "gpt-5-codex:oauth",
      codexAccessToken: "access",
      codexIdToken: "id-token",
      codexRefreshToken: "refresh-token",
    }))).toEqual(expect.objectContaining({
      tokens: {
        access_token: "access",
        id_token: "id-token",
        refresh_token: "refresh-token",
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
      githubCopilotOAuthAccessToken: "access",
      githubCopilotOAuthRefreshToken: "refresh",
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
      codexAccessToken: "access",
      codexIdToken: "id-token",
    }));

    expect(fileWriter.mkdir).toHaveBeenCalledWith("/tmp/home/.codex", { recursive: true });
    expect(fileWriter.writeFile).toHaveBeenCalledOnce();
  });
});
