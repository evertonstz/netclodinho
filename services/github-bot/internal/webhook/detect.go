package webhook

import (
	"regexp"
	"strings"

	"github.com/google/go-github/v68/github"
)

var mentionRegexp = regexp.MustCompile(`(?i)@netclode\b`)

// IsDependabotPR checks if a PR was created by Dependabot.
func IsDependabotPR(pr *github.PullRequest) bool {
	return pr.GetUser().GetLogin() == "dependabot[bot]"
}

// IsRenovatePR checks if a PR was created by Renovate.
func IsRenovatePR(pr *github.PullRequest) bool {
	login := pr.GetUser().GetLogin()
	return login == "renovate[bot]" || login == "renovate"
}

// IsDependencyPR checks if a PR is a dependency update (by bot author only).
func IsDependencyPR(pr *github.PullRequest) bool {
	return IsDependabotPR(pr) || IsRenovatePR(pr)
}

// ContainsMention checks if text contains an @netclode mention.
func ContainsMention(text string) bool {
	return mentionRegexp.MatchString(text)
}

// ExtractMentionText extracts the user's request from a comment,
// removing the @netclode mention itself.
func ExtractMentionText(text string) string {
	result := mentionRegexp.ReplaceAllString(text, "")
	return strings.TrimSpace(result)
}

// IsBotComment checks if a comment was made by the bot itself.
func IsBotComment(login string) bool {
	return login == "netclode[bot]"
}
