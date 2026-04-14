import type { CopilotBackend, SDKConfig } from "./types.js";

export type SecretMaterializationMode = "placeholder-header" | "direct-file" | "direct-env" | "none";

export interface SecretMaterializationDecision {
  credential: string;
  mode: SecretMaterializationMode;
  source: "env" | "session-config" | "none";
  reason: string;
}

function getCopilotBackend(config: SDKConfig): CopilotBackend {
  if (config.copilotBackend) return config.copilotBackend;
  if (config.githubCopilotToken) return "github";
  return "anthropic";
}

export function getOpenCodeProvider(config: SDKConfig): string {
  const model = config.model || "anthropic/claude-sonnet-4-0";
  return model.includes("/") ? model.split("/", 2)[0] : "anthropic";
}

export function isOpenCodeCopilotOAuthMode(config: SDKConfig): boolean {
  return getOpenCodeProvider(config) === "github-copilot" &&
    Boolean(config.githubCopilotOAuthAccessToken || config.githubCopilotOAuthRefreshToken);
}

export function isCodexOAuthMode(config: SDKConfig): boolean {
  const modelHasOAuthSuffix = config.model?.includes(":oauth");
  const modelHasApiSuffix = config.model?.includes(":api");
  if (modelHasOAuthSuffix) return true;
  if (modelHasApiSuffix) return false;
  return Boolean(config.codexAccessToken && !config.openaiApiKey);
}

export function getSecretMaterializationDecisions(config: SDKConfig): SecretMaterializationDecision[] {
  switch (config.sdkType) {
    case "claude":
      return [{
        credential: "anthropic",
        mode: "placeholder-header",
        source: "env",
        reason: "Claude uses the runtime-provided ANTHROPIC_API_KEY placeholder and BoxLite host-side substitution.",
      }];

    case "copilot": {
      const backend = getCopilotBackend(config);
      if (backend === "github") {
        return [{
          credential: "github-copilot-pat",
          mode: "placeholder-header",
          source: "env",
          reason: "GitHub Copilot backend reads GITHUB_COPILOT_TOKEN from env and uses standard outbound HTTPS requests.",
        }];
      }
      return [{
        credential: "anthropic",
        mode: "placeholder-header",
        source: "env",
        reason: "Anthropic backend uses the runtime-provided ANTHROPIC_API_KEY placeholder.",
      }];
    }

    case "opencode": {
      const provider = getOpenCodeProvider(config);
      if (provider === "github-copilot") {
        if (isOpenCodeCopilotOAuthMode(config)) {
          return [{
            credential: "github-copilot-oauth",
            mode: "direct-file",
            source: "session-config",
            reason: "OpenCode GitHub Copilot OAuth persists credentials in auth.json, so BoxLite mode must materialize real OAuth tokens.",
          }];
        }
        return [{
          credential: "github-copilot-oauth",
          mode: "none",
          source: "none",
          reason: "GitHub Copilot model selected but OAuth tokens are not configured.",
        }];
      }
      if (provider === "ollama") {
        return [{
          credential: "ollama",
          mode: "none",
          source: "none",
          reason: "Ollama requests do not require Netclode-managed API secrets.",
        }];
      }
      return [{
        credential: provider,
        mode: "placeholder-header",
        source: "env",
        reason: `OpenCode ${provider} models use runtime-provided provider env placeholders and allowlisted outbound hosts.`,
      }];
    }

    case "codex":
      if (isCodexOAuthMode(config)) {
        return [{
          credential: "codex-oauth",
          mode: "direct-file",
          source: "session-config",
          reason: "Codex OAuth mode requires real tokens in ~/.codex/auth.json.",
        }];
      }
      return [{
        credential: "openai",
        mode: "placeholder-header",
        source: "env",
        reason: "Codex API mode uses OPENAI_API_KEY from env for standard outbound HTTPS requests.",
      }];

    default:
      return [];
  }
}

export function logSecretMaterialization(adapterName: string, config: SDKConfig): void {
  for (const decision of getSecretMaterializationDecisions(config)) {
    console.log(
      `[${adapterName}] Secret materialization: credential=${decision.credential}, mode=${decision.mode}, source=${decision.source}`
    );
  }
}
