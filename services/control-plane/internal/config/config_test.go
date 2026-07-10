package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetEnvBool(t *testing.T) {
	tests := []struct {
		name         string
		envValue     string
		defaultValue bool
		expected     bool
	}{
		{
			name:         "true string",
			envValue:     "true",
			defaultValue: false,
			expected:     true,
		},
		{
			name:         "1 string",
			envValue:     "1",
			defaultValue: false,
			expected:     true,
		},
		{
			name:         "false string",
			envValue:     "false",
			defaultValue: true,
			expected:     false,
		},
		{
			name:         "0 string",
			envValue:     "0",
			defaultValue: true,
			expected:     false,
		},
		{
			name:         "empty uses default true",
			envValue:     "",
			defaultValue: true,
			expected:     true,
		},
		{
			name:         "empty uses default false",
			envValue:     "",
			defaultValue: false,
			expected:     false,
		},
		{
			name:         "invalid uses false",
			envValue:     "invalid",
			defaultValue: true,
			expected:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := "TEST_BOOL_VAR"
			if tt.envValue != "" {
				os.Setenv(key, tt.envValue)
				defer os.Unsetenv(key)
			} else {
				os.Unsetenv(key)
			}

			result := getEnvBool(key, tt.defaultValue)
			if result != tt.expected {
				t.Errorf("getEnvBool(%q, %v) = %v, want %v", tt.envValue, tt.defaultValue, result, tt.expected)
			}
		})
	}
}

func TestLoadWithWarmPoolEnabled(t *testing.T) {
	// Test default (enabled)
	os.Unsetenv("WARM_POOL_ENABLED")
	cfg := Load()
	if !cfg.UseWarmPool {
		t.Error("UseWarmPool should be true by default")
	}

	// Test explicitly disabled
	os.Setenv("WARM_POOL_ENABLED", "false")
	defer os.Unsetenv("WARM_POOL_ENABLED")

	cfg = Load()
	if cfg.UseWarmPool {
		t.Error("UseWarmPool should be false when WARM_POOL_ENABLED=false")
	}
}

func TestLoadWithSandboxTemplate(t *testing.T) {
	// Test default
	os.Unsetenv("SANDBOX_TEMPLATE")
	cfg := Load()
	if cfg.SandboxTemplate != "netclode-agent" {
		t.Errorf("SandboxTemplate = %q, want %q", cfg.SandboxTemplate, "netclode-agent")
	}

	// Test custom value
	os.Setenv("SANDBOX_TEMPLATE", "custom-template")
	defer os.Unsetenv("SANDBOX_TEMPLATE")

	cfg = Load()
	if cfg.SandboxTemplate != "custom-template" {
		t.Errorf("SandboxTemplate = %q, want %q", cfg.SandboxTemplate, "custom-template")
	}
}

func TestEffectiveBoxliteHomeDir(t *testing.T) {
	cfg := &Config{BoxliteHomeDir: "/custom/boxlite"}
	if got := cfg.EffectiveBoxliteHomeDir(); got != "/custom/boxlite" {
		t.Fatalf("EffectiveBoxliteHomeDir() = %q, want %q", got, "/custom/boxlite")
	}

	cfg = &Config{}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		want := filepath.Join(home, ".boxlite")
		if got := cfg.EffectiveBoxliteHomeDir(); got != want {
			t.Fatalf("EffectiveBoxliteHomeDir() = %q, want %q", got, want)
		}
	}
}

func TestLoadWithRuntimeMode(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		want     RuntimeMode
	}{
		{
			name:     "default",
			envValue: "",
			want:     RuntimeModeKubernetes,
		},
		{
			name:     "boxlite",
			envValue: "boxlite",
			want:     RuntimeModeBoxlite,
		},
		{
			name:     "docker",
			envValue: "docker",
			want:     RuntimeModeDocker,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Unsetenv("RUNTIME_MODE")
			if tt.envValue != "" {
				os.Setenv("RUNTIME_MODE", tt.envValue)
				defer os.Unsetenv("RUNTIME_MODE")
			}

			cfg := Load()
			if cfg.RuntimeMode != tt.want {
				t.Errorf("RuntimeMode = %q, want %q", cfg.RuntimeMode, tt.want)
			}
		})
	}
}

func TestValidate_UnknownMode(t *testing.T) {
	cfg := &Config{RuntimeMode: "invalid"}
	if err := cfg.Validate(); err == nil {
		t.Error("Validate() should return a non-nil error for an unknown RuntimeMode")
	}
}

func TestValidate_KnownModes(t *testing.T) {
	for _, mode := range []RuntimeMode{RuntimeModeKubernetes, RuntimeModeBoxlite, RuntimeModeDocker} {
		cfg := &Config{RuntimeMode: mode}
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate() returned error for known mode %q: %v", mode, err)
		}
	}
}

func TestLoadWithDockerFields(t *testing.T) {
	os.Setenv("DOCKER_NETWORK", "netclode_default")
	defer os.Unsetenv("DOCKER_NETWORK")
	os.Setenv("DOCKER_HOST", "unix:///var/run/docker.sock")
	defer os.Unsetenv("DOCKER_HOST")

	cfg := Load()
	if cfg.DockerNetwork != "netclode_default" {
		t.Errorf("DockerNetwork = %q, want %q", cfg.DockerNetwork, "netclode_default")
	}
	if cfg.DockerHost != "unix:///var/run/docker.sock" {
		t.Errorf("DockerHost = %q, want %q", cfg.DockerHost, "unix:///var/run/docker.sock")
	}
}

func TestLoadWithDockerAgentCPURL(t *testing.T) {
	// Set: DOCKER_AGENT_CP_URL is read verbatim into cfg.DockerAgentCPURL.
	t.Run("set", func(t *testing.T) {
		t.Setenv("DOCKER_AGENT_CP_URL", "http://cp.example:9000")
		cfg := Load()
		if cfg.DockerAgentCPURL != "http://cp.example:9000" {
			t.Errorf("DockerAgentCPURL = %q, want %q", cfg.DockerAgentCPURL, "http://cp.example:9000")
		}
	})

	// Unset: default is empty string (compose-DNS default is resolved in the
	// runtime, not in Load(), because it depends on cfg.Port).
	t.Run("unset", func(t *testing.T) {
		os.Unsetenv("DOCKER_AGENT_CP_URL")
		cfg := Load()
		if cfg.DockerAgentCPURL != "" {
			t.Errorf("DockerAgentCPURL = %q, want empty string when unset", cfg.DockerAgentCPURL)
		}
	})
}

func TestLoadWithMaxActiveSessions(t *testing.T) {
	// Test default (was changed from 2 to 5)
	os.Unsetenv("MAX_ACTIVE_SESSIONS")
	cfg := Load()
	if cfg.MaxActiveSessions != 5 {
		t.Errorf("MaxActiveSessions = %d, want %d", cfg.MaxActiveSessions, 5)
	}

	// Test custom value
	os.Setenv("MAX_ACTIVE_SESSIONS", "5")
	defer os.Unsetenv("MAX_ACTIVE_SESSIONS")

	cfg = Load()
	if cfg.MaxActiveSessions != 5 {
		t.Errorf("MaxActiveSessions = %d, want %d", cfg.MaxActiveSessions, 5)
	}

	// Test disabled (0)
	os.Setenv("MAX_ACTIVE_SESSIONS", "0")
	cfg = Load()
	if cfg.MaxActiveSessions != 0 {
		t.Errorf("MaxActiveSessions = %d, want %d", cfg.MaxActiveSessions, 0)
	}
}
