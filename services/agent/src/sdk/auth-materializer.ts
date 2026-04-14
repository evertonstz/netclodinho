import * as fs from "node:fs/promises";
import * as os from "node:os";
import * as path from "node:path";
import type { SecretMaterializationDecision } from "./secret-materialization.js";
import { isCodexOAuthMode, isOpenCodeCopilotOAuthMode, logSecretMaterialization } from "./secret-materialization.js";
import type { SDKConfig } from "./types.js";

export interface BackendAuthMaterializer {
  materialize(config: SDKConfig): Promise<void>;
}

export interface AuthFileWriter {
  mkdir(path: string, options?: { recursive?: boolean }): Promise<unknown>;
  writeFile(
    path: string,
    data: string,
    options?: { encoding?: BufferEncoding; mode?: number },
  ): Promise<unknown>;
}

export function buildOpenCodeAuthContent(config: SDKConfig): Record<string, unknown> | null {
  if (!isOpenCodeCopilotOAuthMode(config)) {
    return null;
  }

  const accessToken = config.githubCopilotOAuthAccessToken || "";
  const refreshToken = config.githubCopilotOAuthRefreshToken || accessToken;
  const expires = Number.parseInt(config.githubCopilotOAuthTokenExpires || "0", 10) || 0;

  if (!accessToken && !refreshToken) {
    return null;
  }

  return {
    "github-copilot": {
      type: "oauth",
      refresh: refreshToken,
      access: accessToken,
      expires,
    },
  };
}

export function buildCodexAuthContent(config: SDKConfig): Record<string, unknown> | null {
  if (!isCodexOAuthMode(config) || !config.codexAccessToken || !config.codexIdToken) {
    return null;
  }

  return {
    tokens: {
      access_token: config.codexAccessToken,
      id_token: config.codexIdToken,
      refresh_token: config.codexRefreshToken || "",
    },
    last_refresh: new Date().toISOString(),
  };
}

export class NoopAuthMaterializer implements BackendAuthMaterializer {
  constructor(private readonly adapterName: string) {}

  async materialize(config: SDKConfig): Promise<void> {
    logSecretMaterialization(this.adapterName, config);
  }
}

export class OpenCodeAuthMaterializer implements BackendAuthMaterializer {
  constructor(
    private readonly adapterName: string = "opencode-adapter",
    private readonly fileWriter: AuthFileWriter = fs,
    private readonly authDir: string = "/agent/.local/share/opencode",
  ) {}

  async materialize(config: SDKConfig): Promise<void> {
    logSecretMaterialization(this.adapterName, config);

    const authContent = buildOpenCodeAuthContent(config);
    if (!authContent) return;

    const authFile = path.join(this.authDir, "auth.json");
    await this.fileWriter.mkdir(this.authDir, { recursive: true });
    await this.fileWriter.writeFile(authFile, JSON.stringify(authContent, null, 2), {
      encoding: "utf-8",
      mode: 0o600,
    });
    console.log(`[${this.adapterName}] Wrote opencode auth.json for GitHub Copilot OAuth (direct-file mode)`);
  }
}

export class CodexAuthMaterializer implements BackendAuthMaterializer {
  constructor(
    private readonly adapterName: string = "codex-adapter",
    private readonly fileWriter: AuthFileWriter = fs,
    private readonly env: NodeJS.ProcessEnv = process.env,
    private readonly homeDir: string = os.homedir(),
  ) {}

  private getCodexHome(): string {
    return this.env.CODEX_HOME || path.join(this.homeDir, ".codex");
  }

  async materialize(config: SDKConfig): Promise<void> {
    logSecretMaterialization(this.adapterName, config);

    const authContent = buildCodexAuthContent(config);
    if (!authContent) return;

    const codexHome = this.getCodexHome();
    const authFile = path.join(codexHome, "auth.json");
    await this.fileWriter.mkdir(codexHome, { recursive: true });
    await this.fileWriter.writeFile(authFile, JSON.stringify(authContent, null, 2), {
      encoding: "utf-8",
      mode: 0o600,
    });
    console.log(`[${this.adapterName}] OAuth tokens written to ${authFile}`);
  }
}

export function selectPrimarySecretDecision(
  decisions: SecretMaterializationDecision[],
): SecretMaterializationDecision | undefined {
  return decisions[0];
}
