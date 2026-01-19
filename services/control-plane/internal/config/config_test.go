package config

import (
	"os"
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
