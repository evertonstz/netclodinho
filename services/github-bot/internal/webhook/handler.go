package webhook

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/angristan/netclode/services/github-bot/internal/store"
	"github.com/angristan/netclode/services/github-bot/internal/workflow"
	"github.com/google/go-github/v68/github"
)

// Handler processes GitHub webhook events.
type Handler struct {
	webhookSecret string
	store         *store.Store
	deps          *workflow.Deps
	sem           chan struct{} // Concurrency limiter
	wg            sync.WaitGroup
	timeout       time.Duration
}

// NewHandler creates a new webhook handler.
func NewHandler(webhookSecret string, s *store.Store, deps *workflow.Deps, maxConcurrent int, timeout time.Duration) *Handler {
	return &Handler{
		webhookSecret: webhookSecret,
		store:         s,
		deps:          deps,
		sem:           make(chan struct{}, maxConcurrent),
		timeout:       timeout,
	}
}

// ServeHTTP handles incoming webhook requests.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Validate signature
	payload, err := github.ValidatePayload(r, []byte(h.webhookSecret))
	if err != nil {
		slog.Warn("Invalid webhook signature", "error", err, "remoteAddr", r.RemoteAddr)
		http.Error(w, "Invalid signature", http.StatusBadRequest)
		return
	}

	eventType := github.WebHookType(r)
	deliveryID := github.DeliveryID(r)

	slog.Info("Received webhook", "event", eventType, "delivery", deliveryID)

	// Dedup check
	if h.store.IsDuplicate(r.Context(), deliveryID) {
		slog.Info("Duplicate delivery, skipping", "delivery", deliveryID)
		w.WriteHeader(http.StatusOK)
		return
	}

	// Return 200 immediately — process async
	w.WriteHeader(http.StatusOK)

	// Dispatch async
	h.dispatch(eventType, deliveryID, payload)
}

// dispatch routes the webhook event to the appropriate workflow.
func (h *Handler) dispatch(eventType, deliveryID string, payload []byte) {
	switch eventType {
	case "issue_comment":
		h.handleIssueComment(deliveryID, payload)
	case "pull_request_review_comment":
		h.handlePRReviewComment(deliveryID, payload)
	case "pull_request":
		h.handlePullRequest(deliveryID, payload)
	default:
		slog.Debug("Ignoring unhandled event type", "event", eventType)
	}
}

// handleIssueComment processes issue_comment events (covers both issues and PRs).
func (h *Handler) handleIssueComment(deliveryID string, payload []byte) {
	var event github.IssueCommentEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		slog.Error("Failed to parse issue_comment event", "error", err)
		return
	}

	// Only handle new comments
	if event.GetAction() != "created" {
		return
	}

	comment := event.GetComment()
	body := comment.GetBody()

	// Ignore bot's own comments
	if IsBotComment(comment.GetUser().GetLogin()) {
		return
	}

	// Check for @netclode mention
	if !ContainsMention(body) {
		return
	}

	owner, repo := repoOwnerAndName(event.GetRepo())
	userLogin := comment.GetUser().GetLogin()

	// Access control: check if user has write access
	hasAccess, err := h.deps.GH.HasWriteAccess(context.Background(), owner, repo, userLogin)
	if err != nil {
		slog.Error("Failed to check user permission", "error", err, "user", userLogin, "owner", owner, "repo", repo)
		return
	}
	if !hasAccess {
		slog.Info("User lacks write access, ignoring mention", "user", userLogin, "owner", owner, "repo", repo)
		return
	}

	userRequest := ExtractMentionText(body)
	number := event.GetIssue().GetNumber()
	triggerCommentID := comment.GetID()

	if event.GetIssue().IsPullRequest() {
		if HasCommand(userRequest, "/review-dep-bump") {
			// Dependency review command
			h.runAsync(deliveryID, func(ctx context.Context) {
				pr, err := h.deps.GH.GetPR(ctx, owner, repo, number)
				if err != nil {
					slog.Error("Failed to fetch PR for /review-dep-bump", "error", err, "owner", owner, "repo", repo, "pr", number)
					return
				}
				workflow.DepbotReview(ctx, h.deps, workflow.DepbotReviewParams{
					Owner:      owner,
					Repo:       repo,
					PRNumber:   number,
					PRTitle:    pr.GetTitle(),
					PRBody:     pr.GetBody(),
					PRAuthor:   pr.GetUser().GetLogin(),
					HeadRef:    pr.GetHead().GetRef(),
					DeliveryID: deliveryID,
				})
			})
		} else {
			// Generic PR mention
			h.runAsync(deliveryID, func(ctx context.Context) {
				workflow.MentionOnPR(ctx, h.deps, workflow.MentionOnPRParams{
					Owner:            owner,
					Repo:             repo,
					PRNumber:         number,
					DeliveryID:       deliveryID,
					UserRequest:      userRequest,
					TriggerCommentID: triggerCommentID,
				})
			})
		}
	} else {
		// Issue mention
		h.runAsync(deliveryID, func(ctx context.Context) {
			workflow.MentionOnIssue(ctx, h.deps, workflow.MentionOnIssueParams{
				Owner:            owner,
				Repo:             repo,
				IssueNumber:      number,
				DeliveryID:       deliveryID,
				UserRequest:      userRequest,
				TriggerCommentID: triggerCommentID,
			})
		})
	}
}

// handlePRReviewComment processes pull_request_review_comment events.
func (h *Handler) handlePRReviewComment(deliveryID string, payload []byte) {
	var event github.PullRequestReviewCommentEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		slog.Error("Failed to parse pull_request_review_comment event", "error", err)
		return
	}

	if event.GetAction() != "created" {
		return
	}

	comment := event.GetComment()
	body := comment.GetBody()

	if IsBotComment(comment.GetUser().GetLogin()) {
		return
	}

	if !ContainsMention(body) {
		return
	}

	owner, repo := repoOwnerAndName(event.GetRepo())
	userLogin := comment.GetUser().GetLogin()

	// Access control
	hasAccess, err := h.deps.GH.HasWriteAccess(context.Background(), owner, repo, userLogin)
	if err != nil {
		slog.Error("Failed to check user permission", "error", err, "user", userLogin)
		return
	}
	if !hasAccess {
		slog.Info("User lacks write access, ignoring mention", "user", userLogin, "owner", owner, "repo", repo)
		return
	}

	userRequest := ExtractMentionText(body)
	prNumber := event.GetPullRequest().GetNumber()

	h.runAsync(deliveryID, func(ctx context.Context) {
		workflow.MentionOnPR(ctx, h.deps, workflow.MentionOnPRParams{
			Owner:            owner,
			Repo:             repo,
			PRNumber:         prNumber,
			DeliveryID:       deliveryID,
			UserRequest:      userRequest,
			TriggerCommentID: 0,
		})
	})
}

// handlePullRequest processes pull_request events for dependency review.
func (h *Handler) handlePullRequest(deliveryID string, payload []byte) {
	var event github.PullRequestEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		slog.Error("Failed to parse pull_request event", "error", err)
		return
	}

	action := event.GetAction()
	if action != "opened" && action != "synchronize" {
		return
	}

	pr := event.GetPullRequest()
	if !IsDependencyPR(pr) {
		return
	}

	owner, repo := repoOwnerAndName(event.GetRepo())

	h.runAsync(deliveryID, func(ctx context.Context) {
		workflow.DepbotReview(ctx, h.deps, workflow.DepbotReviewParams{
			Owner:      owner,
			Repo:       repo,
			PRNumber:   pr.GetNumber(),
			PRTitle:    pr.GetTitle(),
			PRBody:     pr.GetBody(),
			PRAuthor:   pr.GetUser().GetLogin(),
			HeadRef:    pr.GetHead().GetRef(),
			DeliveryID: deliveryID,
		})
	})
}

// runAsync runs a workflow function asynchronously with concurrency limiting.
func (h *Handler) runAsync(deliveryID string, fn func(ctx context.Context)) {
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()

		// Acquire semaphore
		h.sem <- struct{}{}
		defer func() { <-h.sem }()

		ctx, cancel := context.WithTimeout(context.Background(), h.timeout)
		defer cancel()

		slog.Info("Starting workflow", "delivery", deliveryID)
		fn(ctx)
		slog.Info("Workflow completed", "delivery", deliveryID)
	}()
}

// Wait waits for all in-flight workflows to complete.
func (h *Handler) Wait() {
	h.wg.Wait()
}

// repoOwnerAndName extracts owner and repo name from a GitHub repository.
func repoOwnerAndName(repo *github.Repository) (string, string) {
	fullName := repo.GetFullName()
	parts := strings.SplitN(fullName, "/", 2)
	if len(parts) != 2 {
		return repo.GetOwner().GetLogin(), repo.GetName()
	}
	return parts[0], parts[1]
}
