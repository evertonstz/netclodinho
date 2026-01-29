package api

import "testing"

func TestExtractSessionIDFromPodName(t *testing.T) {
	tests := []struct {
		name     string
		podName  string
		expected string
	}{
		{
			name:     "simple session format",
			podName:  "sess-abc123",
			expected: "abc123",
		},
		{
			name:     "session with suffix (sessionID includes dashes)",
			podName:  "sess-abc123-xyz",
			expected: "abc123-xyz",
		},
		{
			name:     "session with multiple dashes in suffix",
			podName:  "sess-abc123-pool-warm-1",
			expected: "abc123-pool-warm-1",
		},
		{
			name:     "non-session pod name",
			podName:  "control-plane-abc123",
			expected: "",
		},
		{
			name:     "empty string",
			podName:  "",
			expected: "",
		},
		{
			name:     "only sess prefix",
			podName:  "sess-",
			expected: "",
		},
		{
			name:     "uuid-style session ID",
			podName:  "sess-550e8400e29b",
			expected: "550e8400e29b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractSessionIDFromPodName(tt.podName)
			if result != tt.expected {
				t.Errorf("extractSessionIDFromPodName(%q) = %q, want %q", tt.podName, result, tt.expected)
			}
		})
	}
}
