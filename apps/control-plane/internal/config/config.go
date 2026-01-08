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
	UseWarmPool bool
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
		UseWarmPool: getEnvBool("WARM_POOL_ENABLED", false),
	}
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

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		return value == "true" || value == "1"
	}
	return defaultValue
}
