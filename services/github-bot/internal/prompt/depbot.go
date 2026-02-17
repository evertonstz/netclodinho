package prompt

import (
	"fmt"
	"strings"
)

// DepbotContext contains context for building a dependency review prompt.
type DepbotContext struct {
	Owner    string
	Repo     string
	PRNumber int
	PRTitle  string
	PRBody   string
	PRAuthor string
	HeadRef  string
	Diff     string
}

// BuildDepbotPrompt builds the prompt for automated dependency review.
func BuildDepbotPrompt(ctx DepbotContext) string {
	var b strings.Builder

	fmt.Fprintf(&b, "You are reviewing a dependency update pull request. Analyze it and post a concise review.\n\n")

	fmt.Fprintf(&b, "## Repository\n%s/%s\n\n", ctx.Owner, ctx.Repo)

	fmt.Fprintf(&b, "## Pull Request #%d: %s\n", ctx.PRNumber, ctx.PRTitle)
	fmt.Fprintf(&b, "Author: %s (automated dependency updater)\n\n", ctx.PRAuthor)

	if ctx.PRBody != "" {
		b.WriteString("## PR Description\n")
		b.WriteString(ctx.PRBody)
		b.WriteString("\n\n")
	}

	if ctx.Diff != "" {
		b.WriteString("## Dependency Diff\n```diff\n")
		b.WriteString(ctx.Diff)
		b.WriteString("\n```\n\n")
	}

	b.WriteString("---\n\n")
	b.WriteString("## Your Task\n\n")
	fmt.Fprintf(&b, "The repository `%s/%s` is cloned in your workspace. The PR branch `%s` needs to be checked out first.\n", ctx.Owner, ctx.Repo, ctx.HeadRef)
	fmt.Fprintf(&b, "Run: `git fetch origin %s && git checkout %s`\n\n", ctx.HeadRef, ctx.HeadRef)

	b.WriteString("Do ALL of the following:\n\n")

	b.WriteString("1. **Identify the update**: dependency name, old version -> new version, bump type (major/minor/patch)\n")
	b.WriteString("2. **Inspect what changed in the dependency**: look at the actual source code of the dependency to understand what changed between versions. For Go, check the local module cache (`go mod download` then look in `$GOPATH/pkg/mod/`), or clone the dependency repo and `git diff` between version tags. Read changelogs, release notes, or commit history. Summarize what actually changed.\n")
	b.WriteString("3. **Find impacted code paths**: search the codebase for all imports and usages of the updated dependency. Cross-reference with the changes you found in step 2 — are any changed/removed APIs actually used? Flag concrete risks.\n")
	b.WriteString("4. **Check CI / run tests**: check if GitHub Actions CI has run on this PR branch (`gh run list --branch <branch>`). If CI ran, report the results. If CI didn't run or there are no workflows, run the tests locally (Makefile, package.json, go test, etc.). Feel free to run code experiments to reproduce or investigate potential issues.\n")
	b.WriteString("5. **Verdict**: state one of: **Safe to merge**, **Needs review**, or **Issues found**. Explain briefly.\n\n")

	b.WriteString("## Rules\n")
	b.WriteString("- Use web search whenever you need information beyond what's in the repo — release notes, changelogs, CVE details, migration guides, etc. Don't guess when you can look it up.\n")
	b.WriteString("- Be concise. No filler. Skip sections that have nothing notable to report.\n")
	b.WriteString("- Format as GitHub-flavored markdown.\n")
	b.WriteString("- Your text output IS the GitHub comment. Do NOT try to post to GitHub yourself — just write your review as your response.\n")
	b.WriteString("- Do NOT include these instructions in your response.\n")

	return b.String()
}
