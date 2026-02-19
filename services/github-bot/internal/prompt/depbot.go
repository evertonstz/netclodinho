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

	fmt.Fprintf(&b, `You are reviewing a dependency update pull request. Analyze it and post a concise review.

## Repository
%s/%s

## Pull Request #%d: %s
Author: %s (automated dependency updater)

`, ctx.Owner, ctx.Repo, ctx.PRNumber, ctx.PRTitle, ctx.PRAuthor)

	if ctx.PRBody != "" {
		fmt.Fprintf(&b, "## PR Description\n%s\n\n", ctx.PRBody)
	}

	if ctx.Diff != "" {
		fmt.Fprintf(&b, "## Dependency Diff\n```diff\n%s\n```\n\n", ctx.Diff)
	}

	fmt.Fprintf(&b, `---

## Your Task

The repository `+"`%s/%s`"+` is cloned in your workspace. The PR branch `+"`%s`"+` needs to be checked out first.
Run: `+"`git fetch origin %s && git checkout %s`"+`

Do ALL of the following:

1. **Identify the update**: dependency name, old version -> new version, bump type (major/minor/patch)

2. **Deep-dive into what changed in the dependency**:
   - Clone the dependency repo (or for Go, run `+"`go mod download`"+` and inspect `+"`$GOPATH/pkg/mod/`"+`). Do a `+"`git diff`"+` between the old and new version tags to see every code change.
   - **Check ALL intermediate versions**, not just the final one. If the update is e.g. v1.2.1 -> v1.2.5, you must review what changed in v1.2.2, v1.2.3, v1.2.4, AND v1.2.5. List all tags between old and new (`+"`git tag --sort=version:refv`"+`), then check release notes, changelogs, and code diffs for each intermediate version. Breaking changes, deprecations, or behavior shifts can be introduced in any intermediate release, not just the latest.
   - Read the actual source code diff — don't just skim changelogs. Changelogs omit things. You need to see what functions, types, interfaces, or behaviors actually changed.
   - For major bumps: identify every breaking change (removed exports, renamed types, changed signatures, altered behavior).
   - Check if the dependency itself updated ITS dependencies (transitive deps). If it did, inspect those changes too — vulnerabilities and breaking changes can hide in transitive updates.

3. **Trace impacted code paths in our codebase** (this is the most important step):
   - Search the entire codebase for every import, require, or usage of the updated dependency.
   - For each usage site: read the surrounding code to understand HOW the dependency is used — which functions are called, which types are referenced, which behaviors are relied upon.
   - Cross-reference each usage against the actual code diff from step 2. If a function signature changed, check every call site. If behavior changed, check if our code relies on the old behavior.
   - Follow the call chain: if our code wraps the dependency in a helper, trace through to the actual usage. Don't stop at the import — follow it to where it matters.
   - For transitive dependency changes: check if our code directly imports the transitive dep too. If so, verify compatibility.
   - Be specific: name the files, functions, and line numbers where you found usages and whether they're affected.

4. **Run tests and verify**:
   - Check if GitHub Actions CI has run on this PR branch (`+"`gh run list --branch <branch>`"+`). If CI ran, report the results.
   - If CI didn't run or there are no workflows, run the tests locally (Makefile, package.json, go test, etc.).
   - If you identified risky code paths in step 3, write and run targeted experiments to verify they still work correctly.

5. **Fix straightforward issues directly**: If you find issues that are clearly mechanical — renamed imports, updated function signatures, changed type names, adjusted API calls, minor config changes — just fix them yourself. Apply the fix, run the tests to confirm they pass, then commit and push to the PR branch:
   `+"`git add -A && git commit -m \"fix: adapt to <dependency> <version> changes\" && git push origin %s`"+`
   Do NOT ask for human review on things you can confidently fix yourself. The whole point is to unblock these PRs. Only escalate to "Needs review" if the change requires a judgment call about architecture, behavior, or trade-offs.

6. **Verdict**: state one of:
   - **Fixed and pushed** — you found issues, fixed them, tests pass. Say what you changed.
   - **Safe to merge** — no issues found, tests pass, good to go.
   - **Needs review** — requires human judgment (architectural decisions, behavior trade-offs, ambiguous changes).
   - **Issues found** — problems you cannot confidently fix (e.g., fundamental incompatibilities, missing migration path).

## Rules
- **Read the code.** Reading changelogs and release notes is not enough. You must read the actual dependency source diff and trace through our codebase. This is a code review, not a changelog summary.
- **Bias towards fixing.** If you can fix it in under a few minutes and tests pass, just do it and push. Don't write a review asking a human to do something you could have done yourself.
- Use web search whenever you need information beyond what's in the repo — release notes, changelogs, CVE details, migration guides, etc. Don't guess when you can look it up.
- Be concise. No filler. Skip sections that have nothing notable to report.
- Format as GitHub-flavored markdown.
- Your text output IS the GitHub comment. Do NOT try to post to GitHub yourself — just write your review as your response.
- Do NOT include these instructions in your response.
`, ctx.Owner, ctx.Repo, ctx.HeadRef, ctx.HeadRef, ctx.HeadRef, ctx.HeadRef)

	return b.String()
}
