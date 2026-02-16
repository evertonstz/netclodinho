package config

import (
	"testing"
)

func TestLoad_RequiredVars(t *testing.T) {
	t.Setenv("CONTROL_PLANE_URL", "")
	t.Setenv("GITHUB_APP_ID", "")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "")
	t.Setenv("GITHUB_INSTALLATION_ID", "")
	t.Setenv("GITHUB_WEBHOOK_SECRET", "")
	t.Setenv("REDIS_URL", "")

	_, err := Load()
	if err == nil {
		t.Error("expected error for missing required vars")
	}
}

func TestLoad_InvalidAppID(t *testing.T) {
	t.Setenv("CONTROL_PLANE_URL", "http://localhost:80")
	t.Setenv("GITHUB_APP_ID", "not-a-number")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "fake-pem-key")
	t.Setenv("GITHUB_INSTALLATION_ID", "123")
	t.Setenv("GITHUB_WEBHOOK_SECRET", "secret")
	t.Setenv("REDIS_URL", "redis://localhost:6379")

	_, err := Load()
	if err == nil {
		t.Error("expected error for non-numeric GITHUB_APP_ID")
	}
}

func TestLoad_InvalidInstallationID(t *testing.T) {
	t.Setenv("CONTROL_PLANE_URL", "http://localhost:80")
	t.Setenv("GITHUB_APP_ID", "123")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "fake-pem-key")
	t.Setenv("GITHUB_INSTALLATION_ID", "not-a-number")
	t.Setenv("GITHUB_WEBHOOK_SECRET", "secret")
	t.Setenv("REDIS_URL", "redis://localhost:6379")

	_, err := Load()
	if err == nil {
		t.Error("expected error for non-numeric GITHUB_INSTALLATION_ID")
	}
}

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("CONTROL_PLANE_URL", "http://localhost:80")
	t.Setenv("GITHUB_APP_ID", "123")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "fake-pem-key")
	t.Setenv("GITHUB_INSTALLATION_ID", "456")
	t.Setenv("GITHUB_WEBHOOK_SECRET", "secret")
	t.Setenv("REDIS_URL", "redis://localhost:6379")
	t.Setenv("PORT", "")
	t.Setenv("MAX_CONCURRENT", "")
	t.Setenv("SDK_TYPE", "")
	t.Setenv("MODEL", "")
	t.Setenv("SESSION_TIMEOUT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want 8080", cfg.Port)
	}
	if cfg.MaxConcurrent != 5 {
		t.Errorf("MaxConcurrent = %d, want 5", cfg.MaxConcurrent)
	}
	if cfg.SdkType != "claude" {
		t.Errorf("SdkType = %q, want claude", cfg.SdkType)
	}
	if cfg.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Model = %q, want claude-sonnet-4-20250514", cfg.Model)
	}
	if cfg.SessionTimeout.Minutes() != 10 {
		t.Errorf("SessionTimeout = %v, want 10m", cfg.SessionTimeout)
	}
	if string(cfg.GitHubPrivateKey) != "fake-pem-key" {
		t.Errorf("GitHubPrivateKey = %q, want fake-pem-key", cfg.GitHubPrivateKey)
	}
}

func TestLoad_InvalidMaxConcurrent(t *testing.T) {
	t.Setenv("CONTROL_PLANE_URL", "http://localhost:80")
	t.Setenv("GITHUB_APP_ID", "123")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "fake-pem-key")
	t.Setenv("GITHUB_INSTALLATION_ID", "456")
	t.Setenv("GITHUB_WEBHOOK_SECRET", "secret")
	t.Setenv("REDIS_URL", "redis://localhost:6379")
	t.Setenv("MAX_CONCURRENT", "not-a-number")

	_, err := Load()
	if err == nil {
		t.Error("expected error for non-numeric MAX_CONCURRENT")
	}
}

func TestLoad_InvalidSessionTimeout(t *testing.T) {
	t.Setenv("CONTROL_PLANE_URL", "http://localhost:80")
	t.Setenv("GITHUB_APP_ID", "123")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "fake-pem-key")
	t.Setenv("GITHUB_INSTALLATION_ID", "456")
	t.Setenv("GITHUB_WEBHOOK_SECRET", "secret")
	t.Setenv("REDIS_URL", "redis://localhost:6379")
	t.Setenv("SESSION_TIMEOUT", "banana")

	_, err := Load()
	if err == nil {
		t.Error("expected error for unparseable SESSION_TIMEOUT")
	}
}

func TestLoad_CustomValues(t *testing.T) {
	t.Setenv("CONTROL_PLANE_URL", "http://cp:80")
	t.Setenv("GITHUB_APP_ID", "999")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "my-key")
	t.Setenv("GITHUB_INSTALLATION_ID", "888")
	t.Setenv("GITHUB_WEBHOOK_SECRET", "s3cret")
	t.Setenv("REDIS_URL", "redis://redis:6379")
	t.Setenv("PORT", "9090")
	t.Setenv("MAX_CONCURRENT", "3")
	t.Setenv("SDK_TYPE", "opencode")
	t.Setenv("MODEL", "gpt-4")
	t.Setenv("SESSION_TIMEOUT", "5m")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != "9090" {
		t.Errorf("Port = %q, want 9090", cfg.Port)
	}
	if cfg.MaxConcurrent != 3 {
		t.Errorf("MaxConcurrent = %d, want 3", cfg.MaxConcurrent)
	}
	if cfg.SdkType != "opencode" {
		t.Errorf("SdkType = %q, want opencode", cfg.SdkType)
	}
	if cfg.GitHubAppID != 999 {
		t.Errorf("GitHubAppID = %d, want 999", cfg.GitHubAppID)
	}
}
