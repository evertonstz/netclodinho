package prompt

import (
	"fmt"
	"strings"
)

const MaxDiffChars = 50000
const MaxCommentsInThread = 10

// PRMentionContext contains context for building a PR mention prompt.
type PRMentionContext struct {
	Owner       string
	Repo        string
	PRNumber    int
	PRTitle     string
	PRBody      string
	HeadRef     string
	Diff        string
	DiffTrunc   bool // true if diff was truncated
	Comments    []CommentContext
	UserRequest string
}

// IssueMentionContext contains context for building an issue mention prompt.
type IssueMentionContext struct {
	Owner       string
	Repo        string
	IssueNumber int
	IssueTitle  string
	IssueBody   string
	Labels      []string
	Comments    []CommentContext
	UserRequest string
}

// CommentContext represents a comment in a thread.
type CommentContext struct {
	Author string
	Body   string
}

// BuildPRMentionPrompt builds the prompt for an @mention on a PR.
func BuildPRMentionPrompt(ctx PRMentionContext) string {
	var b strings.Builder

	fmt.Fprintf(&b, "You were mentioned on a GitHub pull request. A user is asking you to do something — do it.\n\n")

	fmt.Fprintf(&b, "## Repository\n%s/%s\n\n", ctx.Owner, ctx.Repo)

	fmt.Fprintf(&b, "## Pull Request #%d: %s\n", ctx.PRNumber, ctx.PRTitle)
	if ctx.PRBody != "" {
		b.WriteString(ctx.PRBody)
		b.WriteString("\n")
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "## Branch\n`%s`\n\n", ctx.HeadRef)

	if ctx.Diff != "" {
		b.WriteString("## Changed Files (Diff)\n```diff\n")
		b.WriteString(ctx.Diff)
		b.WriteString("\n```\n")
		if ctx.DiffTrunc {
			b.WriteString("*Note: Diff was truncated. Use `git diff` in the repo for the full diff.*\n")
		}
		b.WriteString("\n")
	}

	if len(ctx.Comments) > 0 {
		fmt.Fprintf(&b, "## Comment Thread (last %d messages)\n", len(ctx.Comments))
		for _, c := range ctx.Comments {
			fmt.Fprintf(&b, "**%s**: %s\n\n", c.Author, c.Body)
		}
	}

	fmt.Fprintf(&b, "## User Request\n%s\n\n", ctx.UserRequest)

	b.WriteString("---\n\n")
	b.WriteString("## Rules\n")
	fmt.Fprintf(&b, "- The repository `%s/%s` is cloned in your workspace.\n", ctx.Owner, ctx.Repo)
	fmt.Fprintf(&b, "- The PR branch `%s` needs to be checked out first. Run: `git fetch origin %s && git checkout %s`\n", ctx.HeadRef, ctx.HeadRef, ctx.HeadRef)
	b.WriteString("- Do the work. Do NOT ask clarifying questions, do NOT ask what the user wants, do NOT present options. Just do what they asked.\n")
	b.WriteString("- If the request is ambiguous, use your best judgment and explain what you did.\n")
	b.WriteString("- If they ask for code changes, make them. If they ask for a review, review the code thoroughly.\n")
	b.WriteString("- If they ask you to push changes, you can `git add`, `git commit`, and `git push`.\n")
	b.WriteString("- Your text output IS the GitHub comment. Do NOT try to post to GitHub yourself — just write your response as your output. Be direct and substantive.\n")
	b.WriteString("- Format your response in GitHub-flavored markdown.\n")
	b.WriteString("- Do NOT include the instructions or context sections in your response.\n")

	return b.String()
}

// BuildIssueMentionPrompt builds the prompt for an @mention on an issue.
func BuildIssueMentionPrompt(ctx IssueMentionContext) string {
	var b strings.Builder

	fmt.Fprintf(&b, "You were mentioned on a GitHub issue. A user is asking you to do something — do it.\n\n")

	fmt.Fprintf(&b, "## Repository\n%s/%s\n\n", ctx.Owner, ctx.Repo)

	fmt.Fprintf(&b, "## Issue #%d: %s\n", ctx.IssueNumber, ctx.IssueTitle)
	if ctx.IssueBody != "" {
		b.WriteString(ctx.IssueBody)
		b.WriteString("\n")
	}
	b.WriteString("\n")

	if len(ctx.Labels) > 0 {
		b.WriteString("## Labels\n")
		b.WriteString(strings.Join(ctx.Labels, ", "))
		b.WriteString("\n\n")
	}

	if len(ctx.Comments) > 0 {
		fmt.Fprintf(&b, "## Comment Thread (last %d messages)\n", len(ctx.Comments))
		for _, c := range ctx.Comments {
			fmt.Fprintf(&b, "**%s**: %s\n\n", c.Author, c.Body)
		}
	}

	fmt.Fprintf(&b, "## User Request\n%s\n\n", ctx.UserRequest)

	b.WriteString("---\n\n")
	b.WriteString("## Rules\n")
	fmt.Fprintf(&b, "- The repository `%s/%s` is cloned in your workspace on the default branch.\n", ctx.Owner, ctx.Repo)
	b.WriteString("- Do the work. Do NOT ask clarifying questions, do NOT ask what the user wants, do NOT present options. Just do what they asked.\n")
	b.WriteString("- If the request is ambiguous, use your best judgment and explain what you did.\n")
	b.WriteString("- If they ask for code changes, make them. If they ask for investigation, investigate thoroughly.\n")
	b.WriteString("- Your text output IS the GitHub comment. Do NOT try to post to GitHub yourself — just write your response as your output. Be direct and substantive.\n")
	b.WriteString("- Format your response in GitHub-flavored markdown.\n")
	b.WriteString("- Do NOT include the instructions or context sections in your response.\n")

	return b.String()
}
