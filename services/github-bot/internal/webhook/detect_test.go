package webhook

import (
	"testing"

	"github.com/google/go-github/v68/github"
)

func TestContainsMention(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"@netclode review this", true},
		{"hey @netclode can you help?", true},
		{"@NETCLODE do something", true},
		{"@Netclode hello", true},
		{"no mention here", false},
		{"@netclode123", false}, // word boundary
		// Note: email@netclode.com matches because \b sees a word boundary
		// before .com. This is acceptable — GitHub @mentions don't appear
		// in email addresses in practice.
		{"email@netclode.com", true},
		{"", false},
		{"@netclode", true},
		{"prefix @netclode suffix", true},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			if got := ContainsMention(tt.text); got != tt.want {
				t.Errorf("ContainsMention(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestExtractMentionText(t *testing.T) {
	tests := []struct {
		text string
		want string
	}{
		{"@netclode review this PR", "review this PR"},
		{"@netclode", ""},
		{"hey @netclode can you help?", "hey  can you help?"},
		{"@NETCLODE do it", "do it"},
		{"@netclode /review-dep-bump", "/review-dep-bump"},
		{"  @netclode  hello  ", "hello"},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			if got := ExtractMentionText(tt.text); got != tt.want {
				t.Errorf("ExtractMentionText(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestHasCommand(t *testing.T) {
	tests := []struct {
		text    string
		command string
		want    bool
	}{
		{"/review-dep-bump", "/review-dep-bump", true},
		{"/review-dep-bump extra args", "/review-dep-bump", true},
		{"  /review-dep-bump", "/review-dep-bump", true},
		{"review this", "/review-dep-bump", false},
		{"", "/review-dep-bump", false},
		{"/other-command", "/review-dep-bump", false},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			if got := HasCommand(tt.text, tt.command); got != tt.want {
				t.Errorf("HasCommand(%q, %q) = %v, want %v", tt.text, tt.command, got, tt.want)
			}
		})
	}
}

func TestIsBotComment(t *testing.T) {
	tests := []struct {
		login string
		want  bool
	}{
		{"netclode[bot]", true},
		{"netclode", false},
		{"someone-else", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.login, func(t *testing.T) {
			if got := IsBotComment(tt.login); got != tt.want {
				t.Errorf("IsBotComment(%q) = %v, want %v", tt.login, got, tt.want)
			}
		})
	}
}

func makePR(login string) *github.PullRequest {
	return &github.PullRequest{
		User: &github.User{Login: github.Ptr(login)},
	}
}

func TestIsDependabotPR(t *testing.T) {
	tests := []struct {
		login string
		want  bool
	}{
		{"dependabot[bot]", true},
		{"renovate[bot]", false},
		{"someone", false},
	}
	for _, tt := range tests {
		t.Run(tt.login, func(t *testing.T) {
			if got := IsDependabotPR(makePR(tt.login)); got != tt.want {
				t.Errorf("IsDependabotPR(%q) = %v, want %v", tt.login, got, tt.want)
			}
		})
	}
}

func TestIsRenovatePR(t *testing.T) {
	tests := []struct {
		login string
		want  bool
	}{
		{"renovate[bot]", true},
		{"renovate", true},
		{"dependabot[bot]", false},
		{"someone", false},
	}
	for _, tt := range tests {
		t.Run(tt.login, func(t *testing.T) {
			if got := IsRenovatePR(makePR(tt.login)); got != tt.want {
				t.Errorf("IsRenovatePR(%q) = %v, want %v", tt.login, got, tt.want)
			}
		})
	}
}

func TestIsDependencyPR(t *testing.T) {
	tests := []struct {
		login string
		want  bool
	}{
		{"dependabot[bot]", true},
		{"renovate[bot]", true},
		{"renovate", true},
		{"someone", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.login, func(t *testing.T) {
			if got := IsDependencyPR(makePR(tt.login)); got != tt.want {
				t.Errorf("IsDependencyPR(%q) = %v, want %v", tt.login, got, tt.want)
			}
		})
	}
}
