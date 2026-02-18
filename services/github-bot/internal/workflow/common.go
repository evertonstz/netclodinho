package workflow

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	pb "github.com/angristan/netclode/services/control-plane/gen/netclode/v1"
	"github.com/angristan/netclode/services/github-bot/internal/controlplane"
	"github.com/angristan/netclode/services/github-bot/internal/ghclient"
	"github.com/angristan/netclode/services/github-bot/internal/prompt"
	"github.com/angristan/netclode/services/github-bot/internal/store"
	"github.com/google/go-github/v68/github"
)

const (
	// maxCommentSize is the maximum size of a GitHub comment in characters.
	maxCommentSize = 60000
	// thinkingMessage is the initial comment posted while the bot is working.
	thinkingMessage = "🫡 Looking into it..."
	// postTimeout is the timeout for GitHub API calls after session completion.
	// Uses a fresh context so that posting succeeds even if the session context expired.
	postTimeout = 30 * time.Second
)

// postContext returns a fresh context for GitHub API calls that should
// succeed even if the session context has expired.
func postContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), postTimeout)
}

// Deps holds shared dependencies for all workflows.
type Deps struct {
	GH      *ghclient.Client
	CP      *controlplane.Client
	Store   *store.Store
	SdkType pb.SdkType
	Model   string
}

// postThinkingComment posts an initial "working on it" comment and returns the comment ID.
func postThinkingComment(ctx context.Context, gh *ghclient.Client, owner, repo string, number int, deliveryID string) (int64, error) {
	body := fmt.Sprintf("<!-- netclode:%s -->\n%s", deliveryID, thinkingMessage)
	commentID, err := gh.PostComment(ctx, owner, repo, number, body)
	if err != nil {
		return 0, fmt.Errorf("post thinking comment: %w", err)
	}
	slog.Info("Posted thinking comment", "owner", owner, "repo", repo, "number", number, "commentID", commentID)
	return commentID, nil
}

// formatCommentBody truncates the body if it exceeds maxCommentSize and
// prepends a delivery ID marker for idempotency.
func formatCommentBody(deliveryID, body string) string {
	if len(body) > maxCommentSize {
		body = body[:maxCommentSize] + "\n\n---\n*Response truncated (exceeded GitHub comment size limit).*"
	}
	return fmt.Sprintf("<!-- netclode:%s -->\n%s", deliveryID, body)
}

// updateComment edits an existing comment with the final response.
func updateComment(ctx context.Context, gh *ghclient.Client, owner, repo string, commentID int64, deliveryID, body string) error {
	body = formatCommentBody(deliveryID, body)
	if err := gh.EditComment(ctx, owner, repo, commentID, body); err != nil {
		return fmt.Errorf("edit comment: %w", err)
	}
	return nil
}

// updateCommentWithError edits the thinking comment to show an error.
func updateCommentWithError(ctx context.Context, gh *ghclient.Client, owner, repo string, commentID int64, deliveryID string, err error) {
	body := fmt.Sprintf("Netclode encountered an error: %s", err.Error())
	if updateErr := updateComment(ctx, gh, owner, repo, commentID, deliveryID, body); updateErr != nil {
		slog.Error("Failed to update comment with error", "error", updateErr, "originalError", err)
	}
}

// recoverResponse attempts to fetch the agent's response from the control plane
// after a timeout. The session may have completed server-side even though the
// client stream was torn down. Returns the response text, or "" if recovery fails.
func recoverResponse(cp *controlplane.Client, sessionID string) string {
	if sessionID == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := cp.RecoverSession(ctx, sessionID)
	if err != nil {
		slog.Warn("Failed to recover session response after timeout", "sessionID", sessionID, "error", err)
		if result != nil && result.Response != "" {
			return result.Response
		}
		return ""
	}
	if result.Response != "" {
		slog.Info("Recovered session response after timeout", "sessionID", sessionID, "responseLen", len(result.Response))
	}
	return result.Response
}

// deleteSession cleans up a session in the background.
func deleteSession(cp *controlplane.Client, sessionID string) {
	if sessionID == "" {
		return
	}
	ctx := context.Background()
	if err := cp.DeleteSession(ctx, sessionID); err != nil {
		slog.Warn("Failed to delete session", "sessionID", sessionID, "error", err)
	} else {
		slog.Info("Deleted session", "sessionID", sessionID)
	}
}

// buildCommentThread converts GitHub issue comments into prompt context.
func buildCommentThread(comments []*github.IssueComment) []prompt.CommentContext {
	result := make([]prompt.CommentContext, 0, len(comments))
	for _, c := range comments {
		result = append(result, prompt.CommentContext{
			Author: c.GetUser().GetLogin(),
			Body:   c.GetBody(),
		})
	}
	return result
}
