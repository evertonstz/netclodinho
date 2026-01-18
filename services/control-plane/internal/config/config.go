package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port                  int
	AnthropicAPIKey       string
	K8sNamespace          string
	AgentImage            string
	SandboxTemplate       string
	DefaultCPUs           int
	DefaultMemoryMB       int
	RedisURL              string
	MaxMessagesPerSession int
	MaxEventsPerSession   int
	UseWarmPool           bool
	MaxActiveSessions     int

	// GitHub App integration
	GitHubAppID          int64
	GitHubAppPrivateKey  string // PEM-encoded private key
	GitHubInstallationID int64
}

func Load() *Config {
	return &Config{
		Port:                  getEnvInt("PORT", 3000),
		AnthropicAPIKey:       getEnv("ANTHROPIC_API_KEY", ""),
		K8sNamespace:          getEnv("K8S_NAMESPACE", "netclode"),
		AgentImage:            getEnv("AGENT_IMAGE", "ghcr.io/angristan/netclode-agent:latest"),
		SandboxTemplate:       getEnv("SANDBOX_TEMPLATE", "netclode-agent"),
		DefaultCPUs:           getEnvInt("DEFAULT_CPUS", 2),
		DefaultMemoryMB:       getEnvInt("DEFAULT_MEMORY_MB", 2048),
		RedisURL:              getEnv("REDIS_URL", "redis://redis-sessions.netclode.svc.cluster.local:6379"),
		MaxMessagesPerSession: getEnvInt("MAX_MESSAGES_PER_SESSION", 1000),
		MaxEventsPerSession:   getEnvInt("MAX_EVENTS_PER_SESSION", 50),
		UseWarmPool:           getEnvBool("WARM_POOL_ENABLED", false),
		MaxActiveSessions:     getEnvInt("MAX_ACTIVE_SESSIONS", 2),

		// GitHub App integration
		GitHubAppID:          getEnvInt64("GITHUB_APP_ID", 0),
		GitHubAppPrivateKey:  getEnv("GITHUB_APP_PRIVATE_KEY", ""),
		GitHubInstallationID: getEnvInt64("GITHUB_INSTALLATION_ID", 0),
	}
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
