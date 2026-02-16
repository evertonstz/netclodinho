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

// MentionOnPR handles an @netclode mention on a pull request.
func MentionOnPR(ctx context.Context, deps *Deps, params MentionOnPRParams) {
	logger := slog.With("owner", params.Owner, "repo", params.Repo, "pr", params.PRNumber, "deliveryID", params.DeliveryID)
	logger.Info("Processing PR mention")

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

	// 3. Add eyes reaction to the triggering comment
	if params.TriggerCommentID > 0 {
		_ = deps.GH.ReactToComment(ctx, params.Owner, params.Repo, params.TriggerCommentID, "eyes")
	}

	// 4. Fetch PR context
	pr, err := deps.GH.GetPR(ctx, params.Owner, params.Repo, params.PRNumber)
	if err != nil {
		logger.Error("Failed to get PR", "error", err)
		updateCommentWithError(ctx, deps.GH, params.Owner, params.Repo, commentID, params.DeliveryID, err)
		return
	}

	diff, truncated, err := deps.GH.DownloadPRDiffTruncated(ctx, params.Owner, params.Repo, params.PRNumber, prompt.MaxDiffChars)
	if err != nil {
		logger.Warn("Failed to get PR diff, continuing without", "error", err)
	}

	comments, err := deps.GH.ListIssueComments(ctx, params.Owner, params.Repo, params.PRNumber, prompt.MaxCommentsInThread)
	if err != nil {
		logger.Warn("Failed to list comments, continuing without", "error", err)
	}

	// 4. Build prompt
	promptText := prompt.BuildPRMentionPrompt(prompt.PRMentionContext{
		Owner:       params.Owner,
		Repo:        params.Repo,
		PRNumber:    params.PRNumber,
		PRTitle:     pr.GetTitle(),
		PRBody:      pr.GetBody(),
		HeadRef:     pr.GetHead().GetRef(),
		Diff:        diff,
		DiffTrunc:   truncated,
		Comments:    buildCommentThread(comments),
		UserRequest: params.UserRequest,
	})

	// 6. Run session
	repoFullName := fmt.Sprintf("%s/%s", params.Owner, params.Repo)
	result, err := deps.CP.RunSession(ctx, controlplane.RunSessionOpts{
		Repo:        repoFullName,
		RepoAccess:  pb.RepoAccess_REPO_ACCESS_WRITE,
		SdkType:     deps.SdkType,
		Model:       deps.Model,
		Prompt:      promptText,
		SessionName: fmt.Sprintf("PR #%d mention: %s", params.PRNumber, truncateString(params.UserRequest, 50)),
		OnSessionID: func(sessionID string) {
			// Update in-flight tracking with the session ID
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

	// 7. Post result
	if result.Response == "" {
		result.Response = "*No response from agent.*"
	}
	if err := updateComment(ctx, deps.GH, params.Owner, params.Repo, commentID, params.DeliveryID, result.Response); err != nil {
		logger.Error("Failed to update comment with response", "error", err)
	}
	logger.Info("PR mention completed", "responseLen", len(result.Response))
}

// MentionOnIssue handles an @netclode mention on an issue.
func MentionOnIssue(ctx context.Context, deps *Deps, params MentionOnIssueParams) {
	logger := slog.With("owner", params.Owner, "repo", params.Repo, "issue", params.IssueNumber, "deliveryID", params.DeliveryID)
	logger.Info("Processing issue mention")

	// 1. Post thinking comment
	commentID, err := postThinkingComment(ctx, deps.GH, params.Owner, params.Repo, params.IssueNumber, params.DeliveryID)
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
			Number:     params.IssueNumber,
		}); err != nil {
			logger.Warn("Failed to track in-flight session", "error", err)
		}
		defer deps.Store.ClearInFlight(ctx, params.DeliveryID)
	}

	// 3. Add eyes reaction to the triggering comment
	if params.TriggerCommentID > 0 {
		_ = deps.GH.ReactToComment(ctx, params.Owner, params.Repo, params.TriggerCommentID, "eyes")
	}

	// 4. Fetch issue context
	issue, err := deps.GH.GetIssue(ctx, params.Owner, params.Repo, params.IssueNumber)
	if err != nil {
		logger.Error("Failed to get issue", "error", err)
		updateCommentWithError(ctx, deps.GH, params.Owner, params.Repo, commentID, params.DeliveryID, err)
		return
	}

	comments, err := deps.GH.ListIssueComments(ctx, params.Owner, params.Repo, params.IssueNumber, prompt.MaxCommentsInThread)
	if err != nil {
		logger.Warn("Failed to list comments, continuing without", "error", err)
	}

	// Extract labels
	var labels []string
	for _, l := range issue.Labels {
		labels = append(labels, l.GetName())
	}

	// 4. Build prompt
	promptText := prompt.BuildIssueMentionPrompt(prompt.IssueMentionContext{
		Owner:       params.Owner,
		Repo:        params.Repo,
		IssueNumber: params.IssueNumber,
		IssueTitle:  issue.GetTitle(),
		IssueBody:   issue.GetBody(),
		Labels:      labels,
		Comments:    buildCommentThread(comments),
		UserRequest: params.UserRequest,
	})

	// 6. Run session
	repoFullName := fmt.Sprintf("%s/%s", params.Owner, params.Repo)
	result, err := deps.CP.RunSession(ctx, controlplane.RunSessionOpts{
		Repo:        repoFullName,
		RepoAccess:  pb.RepoAccess_REPO_ACCESS_READ,
		SdkType:     deps.SdkType,
		Model:       deps.Model,
		Prompt:      promptText,
		SessionName: fmt.Sprintf("Issue #%d mention: %s", params.IssueNumber, truncateString(params.UserRequest, 50)),
		OnSessionID: func(sessionID string) {
			if deps.Store != nil {
				if err := deps.Store.TrackInFlight(ctx, store.InFlightSession{
					DeliveryID: params.DeliveryID,
					SessionID:  sessionID,
					CommentID:  commentID,
					Owner:      params.Owner,
					Repo:       params.Repo,
					Number:     params.IssueNumber,
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

	// 7. Post result
	if result.Response == "" {
		result.Response = "*No response from agent.*"
	}
	if err := updateComment(ctx, deps.GH, params.Owner, params.Repo, commentID, params.DeliveryID, result.Response); err != nil {
		logger.Error("Failed to update comment with response", "error", err)
	}
	logger.Info("Issue mention completed", "responseLen", len(result.Response))
}

// MentionOnPRParams contains parameters for a PR mention workflow.
type MentionOnPRParams struct {
	Owner            string
	Repo             string
	PRNumber         int
	DeliveryID       string
	UserRequest      string
	TriggerCommentID int64 // The comment that triggered the mention (for adding reaction)
}

// MentionOnIssueParams contains parameters for an issue mention workflow.
type MentionOnIssueParams struct {
	Owner            string
	Repo             string
	IssueNumber      int
	DeliveryID       string
	UserRequest      string
	TriggerCommentID int64
}

// truncateString returns s if it fits within maxLen, otherwise truncates and
// appends "...". Note: when maxLen < len(s), the result is maxLen+3 chars
// (the ellipsis is always appended outside the limit). This is intentional —
// callers use this for display purposes where a few extra chars are fine.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
