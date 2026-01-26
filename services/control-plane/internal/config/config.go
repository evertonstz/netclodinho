package config

import (
	"encoding/base64"
	"os"
	"strconv"
)

type Config struct {
	Port               int
	AnthropicAPIKey    string
	GitHubCopilotToken string // GitHub PAT with Copilot scope (for Copilot SDK)
	K8sNamespace       string
	AgentImage         string
	SandboxTemplate    string
	DefaultCPUs        int
	DefaultMemoryMB    int
	RedisURL           string
	UseWarmPool        bool
	MaxActiveSessions  int

	// GitHub App integration (for repo-scoped tokens)
	GitHubAppID          int64
	GitHubAppPrivateKey  string // PEM-encoded private key
	GitHubInstallationID int64
}

func Load() *Config {
	return &Config{
		Port:               getEnvInt("PORT", 3000),
		AnthropicAPIKey:    getEnv("ANTHROPIC_API_KEY", ""),
		GitHubCopilotToken: getEnv("GITHUB_COPILOT_TOKEN", ""),
		K8sNamespace:       getEnv("K8S_NAMESPACE", "netclode"),
		AgentImage:         getEnv("AGENT_IMAGE", "ghcr.io/angristan/netclode-agent:latest"),
		SandboxTemplate:    getEnv("SANDBOX_TEMPLATE", "netclode-agent"),
		DefaultCPUs:        getEnvInt("DEFAULT_CPUS", 2),
		DefaultMemoryMB:    getEnvInt("DEFAULT_MEMORY_MB", 2048),
		RedisURL:           getEnv("REDIS_URL", "redis://redis-sessions.netclode.svc.cluster.local:6379"),
		UseWarmPool:        getEnvBool("WARM_POOL_ENABLED", true),
		MaxActiveSessions:  getEnvInt("MAX_ACTIVE_SESSIONS", 5),

		// GitHub App integration
		GitHubAppID:          getEnvInt64("GITHUB_APP_ID", 0),
		GitHubAppPrivateKey:  getGitHubPrivateKey(),
		GitHubInstallationID: getEnvInt64("GITHUB_INSTALLATION_ID", 0),
	}
}

// getGitHubPrivateKey returns the GitHub App private key.
// It first checks GITHUB_APP_PRIVATE_KEY_B64 (base64-encoded),
// then falls back to GITHUB_APP_PRIVATE_KEY (raw PEM).
func getGitHubPrivateKey() string {
	// Try base64-encoded version first (for .env files where multiline is tricky)
	if b64 := os.Getenv("GITHUB_APP_PRIVATE_KEY_B64"); b64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(b64)
		if err == nil {
			return string(decoded)
		}
	}
	// Fall back to raw PEM
	return os.Getenv("GITHUB_APP_PRIVATE_KEY")
}

// HasGitHubApp returns true if GitHub App is configured.
func (c *Config) HasGitHubApp() bool {
	return c.GitHubAppID > 0 && c.GitHubAppPrivateKey != "" && c.GitHubInstallationID > 0
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return defaultValue
}

func getEnvInt64(key string, defaultValue int64) int64 {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.ParseInt(value, 10, 64); err == nil {
			return i
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		return value == "true" || value == "1"
	}
	return defaultValue
}
