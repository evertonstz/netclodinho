package prompt

import (
	"strings"
	"testing"
)

func TestBuildPRMentionPrompt(t *testing.T) {
	ctx := PRMentionContext{
		Owner:       "angristan",
		Repo:        "netclode",
		PRNumber:    42,
		PRTitle:     "Add feature X",
		PRBody:      "This PR adds feature X.",
		HeadRef:     "feature-x",
		Diff:        "+added line\n-removed line",
		UserRequest: "review this please",
		Comments: []CommentContext{
			{Author: "alice", Body: "looks good"},
		},
	}
	result := BuildPRMentionPrompt(ctx)

	mustContain := []string{
		"angristan/netclode",
		"#42: Add feature X",
		"This PR adds feature X.",
		"`feature-x`",
		"+added line",
		"review this please",
		"**alice**: looks good",
		"git fetch origin feature-x && git checkout feature-x",
		"Do the work",
		"web search",
		"Do NOT try to post to GitHub yourself",
	}
	for _, s := range mustContain {
		if !strings.Contains(result, s) {
			t.Errorf("prompt missing %q", s)
		}
	}
}

func TestBuildPRMentionPrompt_DiffTruncated(t *testing.T) {
	ctx := PRMentionContext{
		Owner:       "o",
		Repo:        "r",
		PRNumber:    1,
		PRTitle:     "t",
		HeadRef:     "b",
		Diff:        "some diff",
		DiffTrunc:   true,
		UserRequest: "help",
	}
	result := BuildPRMentionPrompt(ctx)
	if !strings.Contains(result, "Diff was truncated") {
		t.Error("prompt should contain truncation note when DiffTrunc is true")
	}
}

func TestBuildPRMentionPrompt_NoDiff(t *testing.T) {
	ctx := PRMentionContext{
		Owner:       "o",
		Repo:        "r",
		PRNumber:    1,
		PRTitle:     "t",
		HeadRef:     "b",
		UserRequest: "help",
	}
	result := BuildPRMentionPrompt(ctx)
	if strings.Contains(result, "```diff") {
		t.Error("prompt should not contain diff section when diff is empty")
	}
}

func TestBuildPRMentionPrompt_NoComments(t *testing.T) {
	ctx := PRMentionContext{
		Owner:       "o",
		Repo:        "r",
		PRNumber:    1,
		PRTitle:     "t",
		HeadRef:     "b",
		UserRequest: "help",
	}
	result := BuildPRMentionPrompt(ctx)
	if strings.Contains(result, "Comment Thread") {
		t.Error("prompt should not contain comment thread section when no comments")
	}
}

func TestBuildIssueMentionPrompt(t *testing.T) {
	ctx := IssueMentionContext{
		Owner:       "angristan",
		Repo:        "netclode",
		IssueNumber: 99,
		IssueTitle:  "Bug report",
		IssueBody:   "Something is broken.",
		Labels:      []string{"bug", "critical"},
		Comments: []CommentContext{
			{Author: "bob", Body: "can confirm"},
		},
		UserRequest: "fix this",
	}
	result := BuildIssueMentionPrompt(ctx)

	mustContain := []string{
		"angristan/netclode",
		"#99: Bug report",
		"Something is broken.",
		"bug, critical",
		"**bob**: can confirm",
		"fix this",
		"Do the work",
		"web search",
		"Do NOT try to post to GitHub yourself",
	}
	for _, s := range mustContain {
		if !strings.Contains(result, s) {
			t.Errorf("prompt missing %q", s)
		}
	}
}

func TestBuildIssueMentionPrompt_NoLabels(t *testing.T) {
	ctx := IssueMentionContext{
		Owner:       "o",
		Repo:        "r",
		IssueNumber: 1,
		IssueTitle:  "t",
		UserRequest: "help",
	}
	result := BuildIssueMentionPrompt(ctx)
	if strings.Contains(result, "## Labels") {
		t.Error("prompt should not contain labels section when no labels")
	}
}

func TestBuildDepbotPrompt(t *testing.T) {
	ctx := DepbotContext{
		Owner:    "angristan",
		Repo:     "netclode",
		PRNumber: 58,
		PRTitle:  "Bump jwt from 4.5.1 to 4.5.2",
		PRBody:   "Bumps jwt version.",
		PRAuthor: "dependabot[bot]",
		HeadRef:  "dependabot/go_modules/jwt-4.5.2",
		Diff:     "+jwt v4.5.2\n-jwt v4.5.1",
	}
	result := BuildDepbotPrompt(ctx)

	mustContain := []string{
		"angristan/netclode",
		"#58: Bump jwt from 4.5.1 to 4.5.2",
		"dependabot[bot]",
		"+jwt v4.5.2",
		"git fetch origin dependabot/go_modules/jwt-4.5.2",
		"Deep-dive into what changed",
		"Trace impacted code paths",
		"transitive dep",
		"Transitive dependencies are critical",
		"ALSO a direct dependency of the project",
		"lockfile diff",
		"Do NOT dismiss transitive deps",
		"ENTIRE codebase for imports/usages of each changed transitive dep",
		"Follow the call chain",
		"Read the code",
		"gh run list",
		"Fix straightforward issues directly",
		"Fixed and pushed",
		"Bias towards fixing",
		"git push origin dependabot/go_modules/jwt-4.5.2",
		"Safe to merge",
		"web search",
		"Do NOT try to post to GitHub yourself",
	}
	for _, s := range mustContain {
		if !strings.Contains(result, s) {
			t.Errorf("prompt missing %q", s)
		}
	}
}

func TestBuildDepbotPrompt_NoDiff(t *testing.T) {
	ctx := DepbotContext{
		Owner:    "o",
		Repo:     "r",
		PRNumber: 1,
		PRTitle:  "t",
		PRAuthor: "dependabot[bot]",
		HeadRef:  "b",
	}
	result := BuildDepbotPrompt(ctx)
	if strings.Contains(result, "```diff") {
		t.Error("prompt should not contain diff section when diff is empty")
	}
}
