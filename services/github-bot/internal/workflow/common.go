package workflow

import (
	"context"
	"fmt"
	"log/slog"

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
)

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
