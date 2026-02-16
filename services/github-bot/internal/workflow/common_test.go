package workflow

import (
	"strings"
	"testing"

	"github.com/angristan/netclode/services/github-bot/internal/prompt"
	"github.com/google/go-github/v68/github"
)

func TestFormatCommentBody_Short(t *testing.T) {
	body := "Here is the review."
	result := formatCommentBody("delivery-123", body)

	wantPrefix := "<!-- netclode:delivery-123 -->\n"
	if !strings.HasPrefix(result, wantPrefix) {
		t.Errorf("missing delivery marker prefix, got %q", result[:50])
	}
	if !strings.HasSuffix(result, body) {
		t.Errorf("body not preserved, got %q", result)
	}
}

func TestFormatCommentBody_ExactlyAtLimit(t *testing.T) {
	body := strings.Repeat("x", maxCommentSize)
	result := formatCommentBody("d", body)

	if strings.Contains(result, "truncated") {
		t.Error("body at exactly maxCommentSize should not be truncated")
	}
}

func TestFormatCommentBody_Truncated(t *testing.T) {
	body := strings.Repeat("x", maxCommentSize+500)
	result := formatCommentBody("d", body)

	if !strings.Contains(result, "Response truncated") {
		t.Error("body exceeding maxCommentSize should contain truncation notice")
	}
	// The truncated body portion should be exactly maxCommentSize chars
	// (after the marker prefix, before the truncation trailer)
	markerEnd := strings.Index(result, "\n") + 1
	rest := result[markerEnd:]
	truncIdx := strings.Index(rest, "\n\n---\n")
	if truncIdx != maxCommentSize {
		t.Errorf("truncated body length = %d, want %d", truncIdx, maxCommentSize)
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		maxLen int
		want   string
	}{
		{"short", "hello", 10, "hello"},
		{"exact", "hello", 5, "hello"},
		{"truncated", "hello world", 5, "hello..."},
		{"empty", "", 5, ""},
		{"zero max", "hello", 0, "..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncateString(tt.s, tt.maxLen); got != tt.want {
				t.Errorf("truncateString(%q, %d) = %q, want %q", tt.s, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestBuildCommentThread(t *testing.T) {
	comments := []*github.IssueComment{
		{
			User: &github.User{Login: github.Ptr("alice")},
			Body: github.Ptr("first comment"),
		},
		{
			User: &github.User{Login: github.Ptr("bob")},
			Body: github.Ptr("second comment"),
		},
	}

	result := buildCommentThread(comments)

	if len(result) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(result))
	}
	if result[0].Author != "alice" || result[0].Body != "first comment" {
		t.Errorf("comment[0] = %+v, want {alice, first comment}", result[0])
	}
	if result[1].Author != "bob" || result[1].Body != "second comment" {
		t.Errorf("comment[1] = %+v, want {bob, second comment}", result[1])
	}
}

func TestBuildCommentThread_Empty(t *testing.T) {
	result := buildCommentThread(nil)
	if len(result) != 0 {
		t.Errorf("expected empty slice, got %d items", len(result))
	}
}

func TestBuildCommentThread_NilUser(t *testing.T) {
	comments := []*github.IssueComment{
		{Body: github.Ptr("orphan comment")},
	}
	result := buildCommentThread(comments)
	if len(result) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(result))
	}
	if result[0] != (prompt.CommentContext{Author: "", Body: "orphan comment"}) {
		t.Errorf("unexpected result: %+v", result[0])
	}
}

func TestBuildCommentThread_NilBody(t *testing.T) {
	comments := []*github.IssueComment{
		{User: &github.User{Login: github.Ptr("alice")}},
	}
	result := buildCommentThread(comments)
	if len(result) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(result))
	}
	if result[0] != (prompt.CommentContext{Author: "alice", Body: ""}) {
		t.Errorf("unexpected result: %+v", result[0])
	}
}
