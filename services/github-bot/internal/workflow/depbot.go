package workflow

import (
	"context"
	"fmt"
	"log/slog"

	pb "github.com/angristan/netclode/services/control-plane/gen/netclode/v1"
	"github.com/angristan/netclode/services/github-bot/internal/controlplane"
	"github.com/angristan/netclode/services/github-bot/internal/prompt"
	"github.com/angristan/netclode/services/github-bot/internal/store"
)

// DepbotReviewParams contains parameters for a dependency review workflow.
type DepbotReviewParams struct {
	Owner      string
	Repo       string
	PRNumber   int
	PRTitle    string
	PRBody     string
	PRAuthor   string
	HeadRef    string
	DeliveryID string
}

// DepbotReview handles automated dependency PR review.
func DepbotReview(ctx context.Context, deps *Deps, params DepbotReviewParams) {
	logger := slog.With("owner", params.Owner, "repo", params.Repo, "pr", params.PRNumber, "author", params.PRAuthor, "deliveryID", params.DeliveryID)
	logger.Info("Processing dependency review")

	// Check if the bot already commented on this PR (avoid reprocessing on PR updates).
	existing, err := deps.GH.ListIssueComments(ctx, params.Owner, params.Repo, params.PRNumber, 50)
	if err != nil {
		logger.Warn("Failed to list existing comments, proceeding anyway", "error", err)
	} else {
		for _, c := range existing {
			if c.GetUser().GetLogin() == "netclode[bot]" {
				logger.Info("Bot already commented on this PR, skipping")
				return
			}
		}
	}

	// 1. Post thinking comment
	commentID, err := postThinkingComment(ctx, deps.GH, params.Owner, params.Repo, params.PRNumber, params.DeliveryID)
	if err != nil {
		logger.Error("Failed to post thinking comment", "error", err)
		return
	}

	// 2. Track in-flight state for crash recovery
	if deps.Store != nil {
		if err := deps.Store.TrackInFlight(ctx, store.InFlightSession{
			DeliveryID: params.DeliveryID,
			CommentID:  commentID,
			Owner:      params.Owner,
			Repo:       params.Repo,
			Number:     params.PRNumber,
		}); err != nil {
			logger.Warn("Failed to track in-flight session", "error", err)
		}
		defer deps.Store.ClearInFlight(ctx, params.DeliveryID)
	}

	// 3. Fetch PR diff
	diff, _, err := deps.GH.DownloadPRDiffTruncated(ctx, params.Owner, params.Repo, params.PRNumber, prompt.MaxDiffChars)
	if err != nil {
		logger.Error("Failed to get PR diff", "error", err)
		updateCommentWithError(ctx, deps.GH, params.Owner, params.Repo, commentID, params.DeliveryID, err)
		return
	}

	// 3. Build prompt
	promptText := prompt.BuildDepbotPrompt(prompt.DepbotContext{
		Owner:    params.Owner,
		Repo:     params.Repo,
		PRNumber: params.PRNumber,
		PRTitle:  params.PRTitle,
		PRBody:   params.PRBody,
		PRAuthor: params.PRAuthor,
		HeadRef:  params.HeadRef,
		Diff:     diff,
	})

	// 5. Run session
	repoFullName := fmt.Sprintf("%s/%s", params.Owner, params.Repo)
	result, err := deps.CP.RunSession(ctx, controlplane.RunSessionOpts{
		Repo:        repoFullName,
		RepoAccess:  pb.RepoAccess_REPO_ACCESS_READ,
		SdkType:     deps.SdkType,
		Model:       deps.Model,
		Prompt:      promptText,
		SessionName: fmt.Sprintf("Dep review: %s", truncateString(params.PRTitle, 60)),
		OnSessionID: func(sessionID string) {
			if deps.Store != nil {
				if err := deps.Store.TrackInFlight(ctx, store.InFlightSession{
					DeliveryID: params.DeliveryID,
					SessionID:  sessionID,
					CommentID:  commentID,
					Owner:      params.Owner,
					Repo:       params.Repo,
					Number:     params.PRNumber,
				}); err != nil {
					logger.Warn("Failed to update in-flight session with ID", "error", err)
				}
			}
		},
	})
	if result != nil && result.SessionID != "" {
		defer func() { go deleteSession(deps.CP, result.SessionID) }()
	}
	if err != nil {
		logger.Error("Session failed", "error", err)
		if result != nil && result.Response != "" {
			response := result.Response + "\n\n---\n*Note: Session timed out or encountered an error. The above is a partial response.*"
			if updateErr := updateComment(ctx, deps.GH, params.Owner, params.Repo, commentID, params.DeliveryID, response); updateErr != nil {
				logger.Error("Failed to update comment with partial response", "error", updateErr)
			}
			return
		}
		updateCommentWithError(ctx, deps.GH, params.Owner, params.Repo, commentID, params.DeliveryID, err)
		return
	}

	// 5. Post result
	if result.Response == "" {
		result.Response = "*No response from agent.*"
	}
	if err := updateComment(ctx, deps.GH, params.Owner, params.Repo, commentID, params.DeliveryID, result.Response); err != nil {
		logger.Error("Failed to update comment with response", "error", err)
	}
	logger.Info("Dependency review completed", "responseLen", len(result.Response))
}
