package github

import (
	"testing"
)

// TestNormalizeRepoURL tests URL normalization.
func TestNormalizeRepoURL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Already full URLs - returned as-is
		{"https://github.com/owner/repo", "https://github.com/owner/repo"},
		{"https://github.com/owner/repo.git", "https://github.com/owner/repo.git"},
		{"http://github.com/owner/repo", "http://github.com/owner/repo"},

		// owner/repo format -> full URL
		{"owner/repo", "https://github.com/owner/repo.git"},
		{"some-org/my-project", "https://github.com/some-org/my-project.git"},

		// github.com/owner/repo (without protocol)
		{"github.com/owner/repo", "https://github.com/owner/repo.git"},

		// Single name - returned as-is
		{"myrepo", "myrepo"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := NormalizeRepoURL(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeRepoURL(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
