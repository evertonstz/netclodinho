package api

import (
	"testing"

	v1 "github.com/angristan/netclode/services/control-plane/gen/netclode/v1"
	"github.com/angristan/netclode/services/control-plane/internal/session"
)

func TestApplyAgentSessionSecrets_CopilotOAuthUsesPlaceholders(t *testing.T) {
	cfg := &session.AgentSessionConfig{
		GitHubCopilotOAuthAccessToken:  "real-access-token",
		GitHubCopilotOAuthRefreshToken: "real-refresh-token",
		GitHubCopilotOAuthTokenExpires: "1234567890",
	}
	out := &v1.SessionConfig{}
	applyAgentSessionSecrets(out, cfg)

	if got := out.GetGithubCopilotOauthAccessToken(); got != boxliteGitHubCopilotOAuthAccessPlaceholder {
		t.Fatalf("copilot access = %q, want placeholder", got)
	}
	if got := out.GetGithubCopilotOauthRefreshToken(); got != boxliteGitHubCopilotOAuthRefreshPlaceholder {
		t.Fatalf("copilot refresh = %q, want placeholder", got)
	}
	if got := out.GetGithubCopilotOauthTokenExpires(); got != "1234567890" {
		t.Fatalf("expires = %q, want passthrough", got)
	}
}

func TestApplyAgentSessionSecrets_CodexOAuthUsesPlaceholders(t *testing.T) {
	cfg := &session.AgentSessionConfig{
		CodexAccessToken:  "real-codex-access",
		CodexIdToken:      "real-codex-id",
		CodexRefreshToken: "real-codex-refresh",
	}
	out := &v1.SessionConfig{}
	applyAgentSessionSecrets(out, cfg)

	if got := out.GetCodexAccessToken(); got != boxliteCodexOAuthAccessPlaceholder {
		t.Fatalf("codex access = %q, want placeholder", got)
	}
	if got := out.GetCodexIdToken(); got != boxliteCodexOAuthIdPlaceholder {
		t.Fatalf("codex id = %q, want placeholder", got)
	}
	if got := out.GetCodexRefreshToken(); got != boxliteCodexOAuthRefreshPlaceholder {
		t.Fatalf("codex refresh = %q, want placeholder", got)
	}
}

func TestApplyAgentSessionSecrets_NoLeakOfRealTokens(t *testing.T) {
	cfg := &session.AgentSessionConfig{
		GitHubCopilotOAuthAccessToken:  "gho_real_access",
		GitHubCopilotOAuthRefreshToken: "gho_real_refresh",
		CodexAccessToken:               "codex_real_access",
		CodexIdToken:                   "codex_real_id",
		CodexRefreshToken:              "codex_real_refresh",
	}
	out := &v1.SessionConfig{}
	applyAgentSessionSecrets(out, cfg)

	for _, field := range []string{
		out.GetGithubCopilotOauthAccessToken(),
		out.GetGithubCopilotOauthRefreshToken(),
		out.GetCodexAccessToken(),
		out.GetCodexIdToken(),
		out.GetCodexRefreshToken(),
	} {
		if field == "gho_real_access" || field == "gho_real_refresh" ||
			field == "codex_real_access" || field == "codex_real_id" || field == "codex_real_refresh" {
			t.Fatalf("real token leaked into session config field: %q", field)
		}
	}
}
