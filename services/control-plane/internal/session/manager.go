package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	pb "github.com/angristan/netclode/services/control-plane/gen/netclode/v1"
	"github.com/angristan/netclode/services/control-plane/internal/config"
	"github.com/angristan/netclode/services/control-plane/internal/github"
	"github.com/angristan/netclode/services/control-plane/internal/k8s"
	"github.com/angristan/netclode/services/control-plane/internal/storage"
	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	sandboxReadyTimeout = 120 * time.Second
)

// SessionUpdateCallback is called when a session is updated (e.g., auto-paused).
// This allows the API layer to broadcast updates to connected clients.
type SessionUpdateCallback func(session *pb.Session)

// AgentSessionConfig contains typed configuration for an agent session.
type AgentSessionConfig struct {
	SessionID          string
	AnthropicAPIKey    string
	OpenAIAPIKey       string // For Codex API mode
	MistralAPIKey      string // For OpenCode Mistral models
	GitHubToken        string // For git credentials (from GitHub App)
	GitHubCopilotToken string // For Copilot SDK
	Repo               string
	RepoAccess         *pb.RepoAccess
	SdkType            *pb.SdkType
	Model              string
	CopilotBackend     *pb.CopilotBackend
	CodexAccessToken   string // For Codex OAuth mode
	CodexIdToken       string // For Codex OAuth mode
	CodexRefreshToken  string // For Codex OAuth mode
	ReasoningEffort    string // For Codex reasoning effort (low, medium, high)
}

// AgentConnection represents a connected agent that can receive commands.
type AgentConnection interface {
	ExecutePrompt(text string) error
	Interrupt() error
	GenerateTitle(requestID, prompt string) error
	GetGitStatus(requestID string) error
	GetGitDiff(requestID string, file *string) error
	SendTerminalInput(data string) error
	ResizeTerminal(cols, rows int) error
	UpdateGitCredentials(token string, repoAccess pb.RepoAccess) error
}

// Manager handles session lifecycle and agent communication.
type Manager struct {
	storage      storage.Storage
	k8s          k8s.Runtime
	config       *config.Config
	githubClient *github.Client

	sessions map[string]*SessionState
	agents   map[string]AgentConnection // sessionID -> agent connection
	mu       sync.RWMutex

	// onSessionUpdated is called when a session is updated internally (e.g., auto-pause).
	onSessionUpdated SessionUpdateCallback

	// Models cache
	copilotModelsCache   []*pb.ModelInfo
	copilotModelsCacheAt time.Time
	modelsDevCache       map[string]*modelsDevCacheEntry
	copilotModelsMu      sync.RWMutex
}

type modelsDevCacheEntry struct {
	models    []*pb.ModelInfo
	fetchedAt time.Time
}

// NewManager creates a new session manager.
// githubClient can be nil if GitHub App is not configured.
func NewManager(store storage.Storage, k8sRuntime k8s.Runtime, cfg *config.Config, githubClient *github.Client) *Manager {
	return &Manager{
		storage:      store,
		k8s:          k8sRuntime,
		config:       cfg,
		githubClient: githubClient,
		sessions:     make(map[string]*SessionState),
		agents:       make(map[string]AgentConnection),
	}
}

// createRepoToken generates a GitHub token scoped to the specific repo with the given access level.
// Returns empty string if GitHub App is not configured.
func (m *Manager) createRepoToken(ctx context.Context, repo string, access *pb.RepoAccess) string {
	if m.githubClient == nil {
		slog.Warn("GitHub App not configured, cannot create repo-scoped token")
		return ""
	}

	// Determine access level for GitHub API (default to read)
	ghAccess := github.RepoAccessRead
	if access != nil && *access == pb.RepoAccess_REPO_ACCESS_WRITE {
		ghAccess = github.RepoAccessWrite
	}

	token, err := m.githubClient.CreateRepoToken(ctx, repo, ghAccess)
	if err != nil {
		slog.Error("Failed to create repo-scoped token", "repo", repo, "access", access, "error", err)
		return ""
	}

	return token.Token
}

// SetOnSessionUpdated sets a callback that is called when a session is updated internally.
// This is used for broadcasting auto-pause events to connected clients.
func (m *Manager) SetOnSessionUpdated(cb SessionUpdateCallback) {
	m.onSessionUpdated = cb
}

// Initialize loads sessions from storage and reconciles with K8s state.
func (m *Manager) Initialize(ctx context.Context) error {
	slog.Info("Initializing session manager")

	// Load all sessions from storage
	sessions, err := m.storage.GetAllSessions(ctx)
	if err != nil {
		return fmt.Errorf("load sessions: %w", err)
	}

	slog.Info("Loaded sessions from storage", "count", len(sessions))

	// Load into memory with channel-based state
	for _, s := range sessions {
		state := NewSessionState(s)
		// Load persisted restore snapshot ID if any
		if restoreID, err := m.storage.GetRestoreSnapshotID(ctx, s.Id); err == nil && restoreID != "" {
			state.RestoreSnapshotID = restoreID
			slog.Info("Loaded restore snapshot ID", "sessionID", s.Id, "snapshotID", restoreID)
		}
		m.sessions[s.Id] = state
	}

	// Reconcile with K8s
	sandboxes, err := m.k8s.ListSandboxes(ctx)
	if err != nil {
		slog.Warn("Failed to list sandboxes, skipping reconciliation", "error", err)
		return nil
	}

	sandboxMap := make(map[string]k8s.SandboxInfo)
	for _, sb := range sandboxes {
		sandboxMap[sb.SessionID] = sb
	}

	// Update session statuses based on K8s state
	for id, state := range m.sessions {
		sb, exists := sandboxMap[id]
		if !exists {
			// No sandbox exists
			if state.Session.Status == pb.SessionStatus_SESSION_STATUS_RUNNING || state.Session.Status == pb.SessionStatus_SESSION_STATUS_CREATING {
				slog.Info("Session has no sandbox, marking as paused", "sessionID", id)
				state.Session.Status = pb.SessionStatus_SESSION_STATUS_PAUSED
				_ = m.storage.UpdateSessionStatus(ctx, id, pb.SessionStatus_SESSION_STATUS_PAUSED)
			}
		} else if sb.Ready {
			state.ServiceFQDN = sb.ServiceFQDN
			// If session was running, mark as interrupted (agent was processing when we lost connection)
			// If session was creating, mark as ready (sandbox is ready to accept prompts)
			if state.Session.Status == pb.SessionStatus_SESSION_STATUS_RUNNING {
				slog.Info("Session was running on restart, marking as interrupted", "sessionID", id)
				state.Session.Status = pb.SessionStatus_SESSION_STATUS_INTERRUPTED
				_ = m.storage.UpdateSessionStatus(ctx, id, pb.SessionStatus_SESSION_STATUS_INTERRUPTED)
			} else if state.Session.Status == pb.SessionStatus_SESSION_STATUS_CREATING {
				state.Session.Status = pb.SessionStatus_SESSION_STATUS_READY
				_ = m.storage.UpdateSessionStatus(ctx, id, pb.SessionStatus_SESSION_STATUS_READY)
			}
		} else {
			// Sandbox exists but is not ready - mark as paused since we can't communicate with it
			if state.Session.Status == pb.SessionStatus_SESSION_STATUS_RUNNING || state.Session.Status == pb.SessionStatus_SESSION_STATUS_READY {
				slog.Info("Session sandbox exists but is not ready, marking as paused", "sessionID", id)
				state.Session.Status = pb.SessionStatus_SESSION_STATUS_PAUSED
				state.ServiceFQDN = "" // Clear any stale FQDN
				_ = m.storage.UpdateSessionStatus(ctx, id, pb.SessionStatus_SESSION_STATUS_PAUSED)
			}
		}
	}

	slog.Info("Session manager initialized", "sessions", len(m.sessions), "sandboxes", len(sandboxes))

	// Enforce active session limit on startup
	m.enforceActiveLimit(ctx)

	return nil
}

// Close closes all session states.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, state := range m.sessions {
		state.Close()
	}
}

// generateID creates a new session ID.
func generateID() string {
	return uuid.NewString()[:12]
}

// Create creates a new session.
func (m *Manager) Create(ctx context.Context, name string, repo *string, repoAccess *pb.RepoAccess, sdkType *pb.SdkType, model *string, copilotBackend *pb.CopilotBackend, tailnetAccess *bool) (*pb.Session, error) {
	// Ensure we have a slot for a new active session
	m.ensureActiveSlot(ctx, "")

	id := generateID()
	now := timestamppb.Now()

	if name == "" {
		name = "Session " + id[:6]
	}

	session := &pb.Session{
		Id:             id,
		Name:           name,
		Status:         pb.SessionStatus_SESSION_STATUS_CREATING,
		Repo:           repo,
		RepoAccess:     repoAccess,
		CreatedAt:      now,
		LastActiveAt:   now,
		SdkType:        sdkType,
		Model:          model,
		CopilotBackend: copilotBackend,
	}

	// Save to storage
	if err := m.storage.SaveSession(ctx, session); err != nil {
		return nil, fmt.Errorf("save session: %w", err)
	}

	// Add to memory with channel-based state
	state := NewSessionState(session)
	m.mu.Lock()
	m.sessions[id] = state
	m.mu.Unlock()

	// Determine tailnet setting (default: disabled)
	tailnetEnabled := false
	if tailnetAccess != nil {
		tailnetEnabled = *tailnetAccess
	}

	// Start sandbox creation in background
	go m.createSandbox(context.Background(), id, repo, repoAccess, tailnetEnabled)

	return session, nil
}

func (m *Manager) createSandbox(ctx context.Context, sessionID string, repo *string, repoAccess *pb.RepoAccess, tailnetEnabled bool) {
	if m.config.UseWarmPool {
		m.createSandboxViaClaim(ctx, sessionID, repo, repoAccess, tailnetEnabled)
	} else {
		m.createSandboxDirect(ctx, sessionID, repo, repoAccess, tailnetEnabled)
	}
}

// createSandboxDirect creates a sandbox directly (legacy mode).
// If restoreSnapshotID is provided, the PVC is restored from that snapshot BEFORE creating the sandbox.
// If resuming from a paused session, uses the stored PVC name.
func (m *Manager) createSandboxDirect(ctx context.Context, sessionID string, repo *string, repoAccess *pb.RepoAccess, tailnetEnabled bool, restoreSnapshotID ...string) {
	env := map[string]string{
		"SESSION_ID":        sessionID,
		"ANTHROPIC_API_KEY": m.config.AnthropicAPIKey,
	}

	// If restoring from snapshot, create the PVC first and wait for restore to complete
	// BEFORE creating the sandbox. This ensures the restore job finishes before the pod mounts.
	if len(restoreSnapshotID) > 0 && restoreSnapshotID[0] != "" {
		snapID := restoreSnapshotID[0]
		slog.Info("Creating PVC from snapshot before sandbox", "sessionID", sessionID, "snapshotID", snapID)

		// Create standalone PVC from snapshot
		pvcName, err := m.k8s.CreatePVCFromSnapshot(ctx, sessionID, snapID)
		if err != nil {
			slog.Error("Failed to create PVC from snapshot", "sessionID", sessionID, "error", err)
			m.updateSessionStatus(ctx, sessionID, pb.SessionStatus_SESSION_STATUS_ERROR)
			m.emitSessionError(ctx, sessionID, fmt.Sprintf("failed to create PVC from snapshot: %v", err))
			return
		}

		// Wait for JuiceFS restore job to complete BEFORE creating sandbox.
		// We must wait because the pod will fail with I/O errors if it tries to access
		// the filesystem before the restore completes.
		slog.Info("Waiting for snapshot restore job", "sessionID", sessionID, "snapshotID", snapID)
		if err := m.k8s.WaitForRestoreJob(ctx, sessionID, snapID, 5*time.Minute); err != nil {
			slog.Error("Snapshot restore job failed", "sessionID", sessionID, "error", err)
			// Cleanup: delete the PVC we created
			if delErr := m.k8s.DeletePVC(ctx, sessionID); delErr != nil {
				slog.Error("Failed to cleanup PVC after restore failure", "sessionID", sessionID, "error", delErr)
			}
			m.updateSessionStatus(ctx, sessionID, pb.SessionStatus_SESSION_STATUS_ERROR)
			m.emitSessionError(ctx, sessionID, fmt.Sprintf("snapshot restore failed: %v", err))
			return
		}
		slog.Info("Snapshot restore completed, creating sandbox with existing PVC", "sessionID", sessionID, "pvc", pvcName)

		// Ensure the session anchor ConfigMap exists and owns the new PVC.
		// This prevents the PVC from being garbage-collected if the sandbox fails or is paused.
		if err := m.k8s.EnsureSessionAnchor(ctx, sessionID); err != nil {
			slog.Warn("Failed to create session anchor", "sessionID", sessionID, "error", err)
		} else if err := m.k8s.AddSessionAnchorToPVC(ctx, sessionID, pvcName); err != nil {
			slog.Warn("Failed to add session anchor to PVC", "sessionID", sessionID, "pvc", pvcName, "error", err)
		}

		// Delete the old orphaned PVC in background (non-blocking)
		// Only if it's different from the new PVC (avoids deleting the newly created one)
		go func(sessionID, newPVCName string) {
			bgCtx := context.Background()
			if oldPVCName, err := m.storage.GetOldPVCName(bgCtx, sessionID); err == nil && oldPVCName != "" {
				if oldPVCName == newPVCName {
					slog.Info("Skipping old PVC deletion (same as new PVC)", "sessionID", sessionID, "pvc", oldPVCName)
				} else {
					if err := m.k8s.DeletePVCByName(bgCtx, oldPVCName); err != nil {
						slog.Warn("Failed to delete old PVC after restore", "sessionID", sessionID, "pvc", oldPVCName, "error", err)
					} else {
						slog.Info("Deleted old PVC after restore", "sessionID", sessionID, "pvc", oldPVCName)
					}
				}
				_ = m.storage.ClearOldPVCName(bgCtx, sessionID)
			}
		}(sessionID, pvcName)

		// Pass the existing PVC name so sandbox uses it instead of creating a new one
		env[k8s.ExistingPVCEnvKey] = pvcName
	} else {
		// Not restoring from snapshot - check if we have an existing PVC (resume after pause)
		if existingPVC, err := m.storage.GetPVCName(ctx, sessionID); err == nil && existingPVC != "" {
			slog.Info("Resuming with existing PVC", "sessionID", sessionID, "pvc", existingPVC)
			env[k8s.ExistingPVCEnvKey] = existingPVC
		}
	}

	// Pass GitHub Copilot token if configured
	if m.config.GitHubCopilotToken != "" {
		env["GITHUB_COPILOT_TOKEN"] = m.config.GitHubCopilotToken
	}

	// Setup GitHub repo access if configured
	if repo != nil && *repo != "" {
		env["GIT_REPO"] = github.NormalizeRepoURL(*repo)

		// Generate repo-scoped token via GitHub App for git credentials
		if token := m.createRepoToken(ctx, *repo, repoAccess); token != "" {
			env["GITHUB_TOKEN"] = token
		}
	}

	// Create sandbox
	if err := m.k8s.CreateSandbox(ctx, sessionID, env); err != nil {
		slog.Error("Failed to create sandbox", "sessionID", sessionID, "error", err)
		m.updateSessionStatus(ctx, sessionID, pb.SessionStatus_SESSION_STATUS_ERROR)
		m.emitSessionError(ctx, sessionID, err.Error())
		return
	}

	// Wait for ready using informer-based watching
	fqdn, err := m.k8s.WaitForReady(ctx, sessionID, sandboxReadyTimeout)
	if err != nil {
		slog.Error("Sandbox failed to become ready", "sessionID", sessionID, "error", err)
		// Cleanup: delete the sandbox to avoid resource leak
		if delErr := m.k8s.DeleteSandbox(ctx, sessionID); delErr != nil {
			slog.Error("Failed to cleanup sandbox after timeout", "sessionID", sessionID, "error", delErr)
		} else {
			slog.Info("Cleaned up sandbox after timeout", "sessionID", sessionID)
		}
		m.updateSessionStatus(ctx, sessionID, pb.SessionStatus_SESSION_STATUS_ERROR)
		m.emitSessionError(ctx, sessionID, err.Error())
		return
	}

	// Apply network policies
	// Always enable internet access (required for LLM API calls)
	if err := m.k8s.ConfigureNetwork(ctx, sessionID, true); err != nil {
		slog.Error("Failed to apply internet access policy", "sessionID", sessionID, "error", err)
		// Non-fatal: continue with sandbox creation, but log the error
	} else {
		slog.Info("Applied internet access policy", "sessionID", sessionID)
	}
	if tailnetEnabled {
		if err := m.k8s.ConfigureTailnetAccess(ctx, sessionID, true); err != nil {
			slog.Error("Failed to apply tailnet access policy", "sessionID", sessionID, "error", err)
			// Non-fatal: continue with sandbox creation, but log the error
		} else {
			slog.Info("Applied tailnet access policy", "sessionID", sessionID)
		}
	}

	// Create Tailscale-exposed service for preview URLs
	if err := m.k8s.CreateSandboxService(ctx, sessionID); err != nil {
		slog.Warn("Failed to create sandbox service", "sessionID", sessionID, "error", err)
		// Non-fatal: sandbox still works, just no preview URLs
	}

	// Store PVC name for resume after pause
	if pvcName, err := m.k8s.GetPVCName(ctx, sessionID); err == nil {
		if err := m.storage.SetPVCName(ctx, sessionID, pvcName); err != nil {
			slog.Warn("Failed to store PVC name", "sessionID", sessionID, "pvc", pvcName, "error", err)
		} else {
			slog.Info("Stored PVC name", "sessionID", sessionID, "pvc", pvcName)
		}

		// Ensure the session anchor ConfigMap exists and owns the PVC.
		// This prevents the PVC from being garbage-collected when the Sandbox is deleted (during pause).
		if err := m.k8s.EnsureSessionAnchor(ctx, sessionID); err != nil {
			slog.Warn("Failed to create session anchor", "sessionID", sessionID, "error", err)
		} else if err := m.k8s.AddSessionAnchorToPVC(ctx, sessionID, pvcName); err != nil {
			slog.Warn("Failed to add session anchor to PVC", "sessionID", sessionID, "pvc", pvcName, "error", err)
		}
	}

	// Update state and check for pending prompt
	var pendingPrompt string
	m.mu.Lock()
	if state, ok := m.sessions[sessionID]; ok {
		state.ServiceFQDN = fqdn
		pendingPrompt = state.PendingPrompt
		state.PendingPrompt = "" // Clear it
		// Only set RUNNING if there's a pending prompt, otherwise READY
		if pendingPrompt != "" {
			state.Session.Status = pb.SessionStatus_SESSION_STATUS_RUNNING
		} else {
			state.Session.Status = pb.SessionStatus_SESSION_STATUS_READY
		}
	}
	m.mu.Unlock()

	newStatus := pb.SessionStatus_SESSION_STATUS_READY
	if pendingPrompt != "" {
		newStatus = pb.SessionStatus_SESSION_STATUS_RUNNING
	}
	if err := m.storage.UpdateSessionStatus(ctx, sessionID, newStatus); err != nil {
		slog.Error("Failed to update session status", "sessionID", sessionID, "error", err)
	}

	// Emit update
	if session := m.getSession(sessionID); session != nil {
		m.emitSessionUpdated(ctx, session)
	}

	// Process pending prompt if any
	if pendingPrompt != "" {
		slog.Info("Processing pending prompt", "sessionID", sessionID)
		if err := m.SendPrompt(ctx, sessionID, pendingPrompt); err != nil {
			slog.Error("Failed to send pending prompt", "sessionID", sessionID, "error", err)
		}
	}

	slog.Info("Session sandbox ready", "sessionID", sessionID, "fqdn", fqdn, "status", newStatus)
}

// createSandboxViaClaim uses SandboxClaim for warm pool allocation
func (m *Manager) createSandboxViaClaim(ctx context.Context, sessionID string, repo *string, repoAccess *pb.RepoAccess, tailnetEnabled bool) {
	// Always use the same template to leverage the warm pool
	// Network restrictions are applied via ConfigureNetwork() after claiming
	templateName := m.config.SandboxTemplate

	// Create SandboxClaim to request from warm pool
	if err := m.k8s.CreateSandboxClaim(ctx, sessionID, templateName); err != nil {
		slog.Error("Failed to create sandbox claim", "sessionID", sessionID, "error", err)
		m.updateSessionStatus(ctx, sessionID, pb.SessionStatus_SESSION_STATUS_ERROR)
		m.emitSessionError(ctx, sessionID, err.Error())
		return
	}

	// Wait for claim to be bound
	sandboxName, err := m.k8s.WaitForClaimBound(ctx, sessionID, sandboxReadyTimeout)
	if err != nil {
		slog.Error("Claim failed to bind", "sessionID", sessionID, "error", err)
		// Cleanup: delete the SandboxClaim
		if delErr := m.k8s.DeleteSandboxClaim(ctx, sessionID); delErr != nil {
			slog.Error("Failed to cleanup sandbox claim after timeout", "sessionID", sessionID, "error", delErr)
		} else {
			slog.Info("Cleaned up sandbox claim after timeout", "sessionID", sessionID)
		}
		m.updateSessionStatus(ctx, sessionID, pb.SessionStatus_SESSION_STATUS_ERROR)
		m.emitSessionError(ctx, sessionID, err.Error())
		return
	}

	slog.Info("Claim bound to sandbox", "sessionID", sessionID, "sandbox", sandboxName)

	// Label the sandbox so the informer can track it
	if err := m.k8s.LabelSandbox(ctx, sandboxName, sessionID); err != nil {
		slog.Error("Failed to label sandbox", "sessionID", sessionID, "sandbox", sandboxName, "error", err)
		// Cleanup: delete the claim and sandbox - without label we can't manage it
		if delErr := m.k8s.DeleteSandboxClaim(ctx, sessionID); delErr != nil {
			slog.Error("Failed to cleanup sandbox claim", "sessionID", sessionID, "error", delErr)
		}
		if delErr := m.k8s.DeleteSandbox(ctx, sessionID); delErr != nil {
			slog.Error("Failed to cleanup sandbox", "sessionID", sessionID, "error", delErr)
		}
		m.updateSessionStatus(ctx, sessionID, pb.SessionStatus_SESSION_STATUS_ERROR)
		m.emitSessionError(ctx, sessionID, "failed to label sandbox")
		return
	}

	// Get sandbox to retrieve serviceFQDN
	sandbox, err := m.k8s.GetSandboxByName(ctx, sandboxName)
	if err != nil {
		slog.Error("Failed to get bound sandbox", "sessionID", sessionID, "sandbox", sandboxName, "error", err)
		// Cleanup: delete the claim and sandbox
		if delErr := m.k8s.DeleteSandboxClaim(ctx, sessionID); delErr != nil {
			slog.Error("Failed to cleanup sandbox claim", "sessionID", sessionID, "error", delErr)
		}
		if delErr := m.k8s.DeleteSandbox(ctx, sessionID); delErr != nil {
			slog.Error("Failed to cleanup sandbox", "sessionID", sessionID, "error", delErr)
		}
		m.updateSessionStatus(ctx, sessionID, pb.SessionStatus_SESSION_STATUS_ERROR)
		m.emitSessionError(ctx, sessionID, err.Error())
		return
	}

	fqdn := sandbox.Status.ServiceFQDN

	// If fqdn is empty but sandbox is ready, construct it manually.
	// The warm pool controller doesn't populate serviceFQDN in sandbox status.
	if fqdn == "" && sandbox.IsReady() {
		fqdn = fmt.Sprintf("%s.%s.svc.cluster.local", sandboxName, m.config.K8sNamespace)
		slog.Info("Constructed serviceFQDN", "sessionID", sessionID, "fqdn", fqdn)
	}

	// Warm pool sandboxes should already be Ready, but verify
	if !sandbox.IsReady() {
		// Wait for sandbox to become ready (shouldn't happen with warm pool)
		slog.Warn("Bound sandbox not ready yet, waiting", "sessionID", sessionID, "sandbox", sandboxName)
		fqdn, err = m.k8s.WaitForReady(ctx, sessionID, sandboxReadyTimeout)
		if err != nil {
			slog.Error("Sandbox not ready", "sessionID", sessionID, "error", err)
			// Cleanup: delete the claim and sandbox to avoid resource leak
			if delErr := m.k8s.DeleteSandboxClaim(ctx, sessionID); delErr != nil {
				slog.Error("Failed to cleanup sandbox claim after timeout", "sessionID", sessionID, "error", delErr)
			}
			if delErr := m.k8s.DeleteSandbox(ctx, sessionID); delErr != nil {
				slog.Error("Failed to cleanup sandbox after timeout", "sessionID", sessionID, "error", delErr)
			} else {
				slog.Info("Cleaned up sandbox after timeout", "sessionID", sessionID)
			}
			m.updateSessionStatus(ctx, sessionID, pb.SessionStatus_SESSION_STATUS_ERROR)
			m.emitSessionError(ctx, sessionID, err.Error())
			return
		}
	}

	// Apply network policies AFTER sandbox is ready
	// Always enable internet access (required for LLM API calls)
	if err := m.k8s.ConfigureNetwork(ctx, sessionID, true); err != nil {
		slog.Error("Failed to apply internet access policy", "sessionID", sessionID, "error", err)
		// Non-fatal: continue with sandbox creation, but log the error
	} else {
		slog.Info("Applied internet access policy", "sessionID", sessionID)
	}
	if tailnetEnabled {
		if err := m.k8s.ConfigureTailnetAccess(ctx, sessionID, true); err != nil {
			slog.Error("Failed to apply tailnet access policy", "sessionID", sessionID, "error", err)
			// Non-fatal: continue with sandbox creation, but log the error
		} else {
			slog.Info("Applied tailnet access policy", "sessionID", sessionID)
		}
	}

	// Create Tailscale-exposed service for preview URLs
	if err := m.k8s.CreateSandboxService(ctx, sessionID); err != nil {
		slog.Warn("Failed to create sandbox service", "sessionID", sessionID, "error", err)
		// Non-fatal: sandbox still works, just no preview URLs
	}

	// Store PVC name for resume after pause
	// Use GetPVCName which handles warm pool correctly (reads pod-name annotation)
	if pvcName, err := m.k8s.GetPVCName(ctx, sessionID); err == nil {
		if err := m.storage.SetPVCName(ctx, sessionID, pvcName); err != nil {
			slog.Warn("Failed to store PVC name", "sessionID", sessionID, "pvc", pvcName, "error", err)
		} else {
			slog.Info("Stored PVC name", "sessionID", sessionID, "pvc", pvcName)
		}

		// Ensure the session anchor ConfigMap exists and owns the PVC.
		// This prevents the PVC from being garbage-collected when the Sandbox is deleted (during pause).
		if err := m.k8s.EnsureSessionAnchor(ctx, sessionID); err != nil {
			slog.Warn("Failed to create session anchor", "sessionID", sessionID, "error", err)
		} else if err := m.k8s.AddSessionAnchorToPVC(ctx, sessionID, pvcName); err != nil {
			slog.Warn("Failed to add session anchor to PVC", "sessionID", sessionID, "pvc", pvcName, "error", err)
		}
	}

	// Update state and check for pending prompt
	var pendingPrompt string
	m.mu.Lock()
	if state, ok := m.sessions[sessionID]; ok {
		state.ServiceFQDN = fqdn
		pendingPrompt = state.PendingPrompt
		state.PendingPrompt = "" // Clear it
		// Only set RUNNING if there's a pending prompt, otherwise READY
		if pendingPrompt != "" {
			state.Session.Status = pb.SessionStatus_SESSION_STATUS_RUNNING
		} else {
			state.Session.Status = pb.SessionStatus_SESSION_STATUS_READY
		}
	}
	m.mu.Unlock()

	newStatus := pb.SessionStatus_SESSION_STATUS_READY
	if pendingPrompt != "" {
		newStatus = pb.SessionStatus_SESSION_STATUS_RUNNING
	}
	if err := m.storage.UpdateSessionStatus(ctx, sessionID, newStatus); err != nil {
		slog.Error("Failed to update session status", "sessionID", sessionID, "error", err)
	}

	// Emit update
	if session := m.getSession(sessionID); session != nil {
		m.emitSessionUpdated(ctx, session)
	}

	// Process pending prompt if any
	if pendingPrompt != "" {
		slog.Info("Processing pending prompt (warm pool)", "sessionID", sessionID)
		if err := m.SendPrompt(ctx, sessionID, pendingPrompt); err != nil {
			slog.Error("Failed to send pending prompt", "sessionID", sessionID, "error", err)
		}
	}

	slog.Info("Session sandbox ready (warm pool)", "sessionID", sessionID, "fqdn", fqdn, "status", newStatus)
}

func (m *Manager) updateSessionStatus(ctx context.Context, sessionID string, status pb.SessionStatus) {
	m.mu.Lock()
	if state, ok := m.sessions[sessionID]; ok {
		state.Session.Status = status
	}
	m.mu.Unlock()

	_ = m.storage.UpdateSessionStatus(ctx, sessionID, status)

	if session := m.getSession(sessionID); session != nil {
		// Emit to session-specific subscribers
		m.emitSessionUpdated(ctx, session)

		// Also broadcast globally so clients on the sessions list page see the update
		if m.onSessionUpdated != nil {
			m.onSessionUpdated(session)
		}
	}
}

func (m *Manager) updateSessionName(ctx context.Context, sessionID, name string) {
	m.mu.Lock()
	if state, ok := m.sessions[sessionID]; ok {
		state.Session.Name = name
	}
	m.mu.Unlock()

	_ = m.storage.UpdateSessionField(ctx, sessionID, "name", name)

	if session := m.getSession(sessionID); session != nil {
		// Emit to session-specific subscribers
		m.emitSessionUpdated(ctx, session)

		// Also broadcast globally so clients on the sessions list page see the update
		if m.onSessionUpdated != nil {
			m.onSessionUpdated(session)
		}
	}
}

// updateLastActiveAt updates the last active timestamp for a session.
func (m *Manager) updateLastActiveAt(ctx context.Context, sessionID string) {
	now := timestamppb.Now()

	m.mu.Lock()
	if state, ok := m.sessions[sessionID]; ok {
		state.Session.LastActiveAt = now
	}
	m.mu.Unlock()

	_ = m.storage.UpdateSessionField(ctx, sessionID, "lastActiveAt", now.AsTime().UTC().Format(time.RFC3339))
}

// waitForSandbox waits for an existing sandbox to become ready (used when sandbox already exists).
func (m *Manager) waitForSandbox(ctx context.Context, sessionID string) {
	fqdn, err := m.k8s.WaitForReady(ctx, sessionID, sandboxReadyTimeout)
	if err != nil {
		slog.Error("Sandbox failed to become ready", "sessionID", sessionID, "error", err)
		m.updateSessionStatus(ctx, sessionID, pb.SessionStatus_SESSION_STATUS_ERROR)
		m.emitSessionError(ctx, sessionID, err.Error())
		return
	}

	// Update state and check for pending prompt
	var pendingPrompt string
	m.mu.Lock()
	if state, ok := m.sessions[sessionID]; ok {
		state.ServiceFQDN = fqdn
		state.Session.Status = pb.SessionStatus_SESSION_STATUS_READY
		pendingPrompt = state.PendingPrompt
		state.PendingPrompt = "" // Clear it
	}
	m.mu.Unlock()

	if err := m.storage.UpdateSessionStatus(ctx, sessionID, pb.SessionStatus_SESSION_STATUS_READY); err != nil {
		slog.Error("Failed to update session status", "sessionID", sessionID, "error", err)
	}

	// Emit update
	if session := m.getSession(sessionID); session != nil {
		m.emitSessionUpdated(ctx, session)
	}

	// Process pending prompt if any
	if pendingPrompt != "" {
		slog.Info("Processing pending prompt (waitForSandbox)", "sessionID", sessionID)
		if err := m.SendPrompt(ctx, sessionID, pendingPrompt); err != nil {
			slog.Error("Failed to send pending prompt", "sessionID", sessionID, "error", err)
		}
	}

	slog.Info("Sandbox ready", "sessionID", sessionID, "fqdn", fqdn)
}

// Resume resumes a paused session.
func (m *Manager) Resume(ctx context.Context, id string) (*pb.Session, error) {
	m.mu.Lock()
	state, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return nil, fmt.Errorf("session %s not found", id)
	}

	// If session is already CREATING or RESUMING, a sandbox creation is already in progress.
	// Don't start another one - just return and let the caller queue the prompt.
	if state.Session.Status == pb.SessionStatus_SESSION_STATUS_CREATING ||
		state.Session.Status == pb.SessionStatus_SESSION_STATUS_RESUMING {
		slog.Info("Session already creating/resuming, skipping Resume", "sessionID", id, "status", state.Session.Status.String())
		m.mu.Unlock()
		return state.Session, nil
	}
	m.mu.Unlock()

	// Ensure we have a slot (exclude the session we're resuming from being paused)
	m.ensureActiveSlot(ctx, id)

	// Check if sandbox exists and is ready
	status, err := m.k8s.GetStatus(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get sandbox status: %w", err)
	}

	if status.Exists && status.Ready && status.ServiceFQDN != "" {
		// Sandbox already running and ready to accept prompts
		m.mu.Lock()
		state.ServiceFQDN = status.ServiceFQDN
		// Preserve RUNNING status if already set (e.g., during warm pool creation)
		// Only change to READY if we were in a different state (PAUSED, RESUMING, etc.)
		wasAlreadyActive := state.Session.Status == pb.SessionStatus_SESSION_STATUS_READY ||
			state.Session.Status == pb.SessionStatus_SESSION_STATUS_RUNNING
		if !wasAlreadyActive {
			state.Session.Status = pb.SessionStatus_SESSION_STATUS_READY
		}
		m.mu.Unlock()

		// Only update storage if status actually changed
		if !wasAlreadyActive {
			_ = m.storage.UpdateSessionStatus(ctx, id, pb.SessionStatus_SESSION_STATUS_READY)
		}
		return state.Session, nil
	}

	if status.Exists {
		// Sandbox exists but not ready yet - just wait for it
		m.mu.Lock()
		state.Session.Status = pb.SessionStatus_SESSION_STATUS_RESUMING
		m.mu.Unlock()

		_ = m.storage.UpdateSessionStatus(ctx, id, pb.SessionStatus_SESSION_STATUS_RESUMING)

		// Wait for existing sandbox to become ready
		go m.waitForSandbox(context.Background(), id)

		return state.Session, nil
	}

	// No sandbox exists - create new one (resuming from paused state)
	m.mu.Lock()
	state.Session.Status = pb.SessionStatus_SESSION_STATUS_RESUMING
	restoreSnapshotID := state.RestoreSnapshotID
	state.RestoreSnapshotID = "" // Clear it in memory
	m.mu.Unlock()

	_ = m.storage.UpdateSessionStatus(ctx, id, pb.SessionStatus_SESSION_STATUS_RESUMING)

	// Always use direct sandbox creation for resume (bypasses warm pool).
	// This ensures we use the session's existing PVC instead of getting a fresh warm pool PVC.
	// createSandboxDirect will automatically use the stored PVC name if available.
	// Note: Resume always uses default tailnetEnabled=false since we don't store network config
	if restoreSnapshotID != "" {
		slog.Info("Resuming with snapshot restore", "sessionID", id, "snapshotID", restoreSnapshotID)
		_ = m.storage.ClearRestoreSnapshotID(ctx, id) // Clear from storage
		go m.createSandboxDirect(context.Background(), id, state.Session.Repo, state.Session.RepoAccess, false, restoreSnapshotID)
	} else {
		slog.Info("Resuming session", "sessionID", id)
		go m.createSandboxDirect(context.Background(), id, state.Session.Repo, state.Session.RepoAccess, false)
	}

	return state.Session, nil
}

// Pause pauses a running session.
func (m *Manager) Pause(ctx context.Context, id string) (*pb.Session, error) {
	m.mu.Lock()
	state, ok := m.sessions[id]
	m.mu.Unlock()

	if !ok {
		return nil, fmt.Errorf("session %s not found", id)
	}

	// Delete claim if using warm pool
	if m.config.UseWarmPool {
		if err := m.k8s.DeleteSandboxClaim(ctx, id); err != nil {
			slog.Warn("Failed to delete sandbox claim", "sessionID", id, "error", err)
		}
	}

	// Delete Tailscale service
	if err := m.k8s.DeleteSandboxService(ctx, id); err != nil {
		slog.Warn("Failed to delete sandbox service", "sessionID", id, "error", err)
	}

	// Delete network restriction policy if any
	if err := m.k8s.DeleteNetworkRestriction(ctx, id); err != nil {
		slog.Warn("Failed to delete network restriction policy", "sessionID", id, "error", err)
	}

	// Delete sandbox - PVC survives because it has the session anchor ConfigMap as a second owner
	if err := m.k8s.DeleteSandbox(ctx, id); err != nil {
		slog.Warn("Failed to delete sandbox", "sessionID", id, "error", err)
	}

	// Delete secret (only in direct mode - warm pool uses shared secret)
	if !m.config.UseWarmPool {
		if err := m.k8s.DeleteSecret(ctx, id); err != nil {
			slog.Warn("Failed to delete secret", "sessionID", id, "error", err)
		}
	}

	// Update state
	m.mu.Lock()
	state.ServiceFQDN = ""
	state.Session.Status = pb.SessionStatus_SESSION_STATUS_PAUSED
	m.mu.Unlock()

	_ = m.storage.UpdateSessionStatus(ctx, id, pb.SessionStatus_SESSION_STATUS_PAUSED)

	return state.Session, nil
}

// Delete deletes a session and all its resources.
func (m *Manager) Delete(ctx context.Context, id string) error {
	// Close the session state (stops broadcast loop)
	m.mu.Lock()
	if state, ok := m.sessions[id]; ok {
		state.Close()
	}
	delete(m.sessions, id)
	m.mu.Unlock()

	// Delete K8s VolumeSnapshots first (before PVC deletion)
	if snapshots, err := m.k8s.ListVolumeSnapshots(ctx, id); err != nil {
		slog.Warn("Failed to list VolumeSnapshots during session delete", "sessionID", id, "error", err)
	} else {
		for _, snap := range snapshots {
			if err := m.k8s.DeleteVolumeSnapshot(ctx, id, snap.SnapshotID); err != nil {
				slog.Warn("Failed to delete VolumeSnapshot during session delete",
					"sessionID", id, "snapshotID", snap.SnapshotID, "error", err)
			}
		}
	}

	// Delete K8s resources
	_ = m.k8s.DeleteSandboxClaim(ctx, id)
	_ = m.k8s.DeleteSandboxService(ctx, id)
	_ = m.k8s.DeleteNetworkRestriction(ctx, id)
	_ = m.k8s.DeleteSandbox(ctx, id)
	_ = m.k8s.DeleteSecret(ctx, id)
	_ = m.k8s.DeleteSessionAnchor(ctx, id)
	_ = m.k8s.DeletePVC(ctx, id)

	// Delete from storage
	if err := m.storage.DeleteSession(ctx, id); err != nil {
		return fmt.Errorf("delete session from storage: %w", err)
	}

	slog.Info("Session deleted", "sessionID", id)
	return nil
}

// DeleteAll deletes all sessions and their resources.
// Returns the list of deleted session IDs.
func (m *Manager) DeleteAll(ctx context.Context) ([]string, error) {
	// Get all session IDs
	m.mu.Lock()
	sessionIDs := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		sessionIDs = append(sessionIDs, id)
	}
	m.mu.Unlock()

	var deletedIDs []string
	var lastErr error

	for _, id := range sessionIDs {
		if err := m.Delete(ctx, id); err != nil {
			slog.Error("Failed to delete session during DeleteAll", "sessionID", id, "error", err)
			lastErr = err
			continue
		}
		deletedIDs = append(deletedIDs, id)
	}

	slog.Info("DeleteAll completed", "deleted", len(deletedIDs), "total", len(sessionIDs))

	if lastErr != nil {
		return deletedIDs, fmt.Errorf("some sessions failed to delete: %w", lastErr)
	}

	return deletedIDs, nil
}

// ExposePort exposes a port for a session via Tailscale and persists the event.
func (m *Manager) ExposePort(ctx context.Context, sessionID string, port int) (string, error) {
	if err := m.k8s.ExposePort(ctx, sessionID, port); err != nil {
		return "", err
	}

	previewURL := fmt.Sprintf("http://sandbox-%s:%d", sessionID, port)
	port32 := int32(port)

	// Create and persist the port_exposed event
	event := &pb.AgentEvent{
		Kind:      pb.AgentEventKind_AGENT_EVENT_KIND_PORT_EXPOSED,
		Timestamp: timestamppb.Now(),
		Payload: &pb.AgentEvent_PortExposed{
			PortExposed: &pb.PortExposedPayload{
				Port:       port32,
				PreviewUrl: &previewURL,
			},
		},
	}

	persistedEvent := &pb.Event{
		Id:        "evt_" + uuid.NewString()[:12],
		Timestamp: timestamppb.Now(),
		Event:     event,
	}
	if err := m.storage.AppendEvent(ctx, sessionID, persistedEvent); err != nil {
		slog.Warn("Failed to persist port exposed event", "sessionID", sessionID, "error", err)
	}

	// Emit to all connected clients
	m.emitAgentEvent(ctx, sessionID, event)

	return previewURL, nil
}

// List returns all sessions, reconciling with K8s state.
func (m *Manager) List(ctx context.Context) ([]pb.Session, error) {
	// Reconcile with K8s
	sandboxes, err := m.k8s.ListSandboxes(ctx)
	if err != nil {
		slog.Warn("Failed to list sandboxes", "error", err)
	}

	sandboxMap := make(map[string]k8s.SandboxInfo)
	for _, sb := range sandboxes {
		sandboxMap[sb.SessionID] = sb
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	sessions := make([]pb.Session, 0, len(m.sessions))
	for id, state := range m.sessions {
		sb, exists := sandboxMap[id]
		if !exists && (state.Session.Status == pb.SessionStatus_SESSION_STATUS_RUNNING || state.Session.Status == pb.SessionStatus_SESSION_STATUS_CREATING) {
			state.Session.Status = pb.SessionStatus_SESSION_STATUS_PAUSED
			_ = m.storage.UpdateSessionStatus(ctx, id, pb.SessionStatus_SESSION_STATUS_PAUSED)
		} else if exists && sb.Ready {
			state.ServiceFQDN = sb.ServiceFQDN
		}
		sessions = append(sessions, *state.Session)
	}

	return sessions, nil
}

// Get returns a session by ID.
func (m *Manager) Get(ctx context.Context, id string) (*pb.Session, error) {
	m.mu.RLock()
	state, ok := m.sessions[id]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("session %s not found", id)
	}

	return state.Session, nil
}

// GetWithHistory returns a session with its message and event history.
// Also returns the lastNotificationID for cursor-based subscription.
func (m *Manager) GetWithHistory(ctx context.Context, id string) (*pb.Session, []pb.Message, []pb.Event, bool, string, error) {
	session, err := m.Get(ctx, id)
	if err != nil {
		return nil, nil, nil, false, "", err
	}

	messages, err := m.storage.GetMessages(ctx, id, nil)
	if err != nil {
		return nil, nil, nil, false, "", fmt.Errorf("get messages: %w", err)
	}

	events, err := m.storage.GetEvents(ctx, id, 0) // 0 = no limit
	if err != nil {
		return nil, nil, nil, false, "", fmt.Errorf("get events: %w", err)
	}

	// Get the latest notification ID for cursor-based subscription
	// This allows the client to subscribe starting from where the history ends
	lastNotificationID, err := m.storage.GetLastNotificationID(ctx, id)
	if err != nil {
		slog.Warn("Failed to get last notification ID, using $", "sessionID", id, "error", err)
		lastNotificationID = "$" // Default to "only new" if error
	}

	// Convert to value slices
	msgSlice := make([]pb.Message, len(messages))
	for i, msg := range messages {
		msgSlice[i] = *msg
	}

	evtSlice := make([]pb.Event, len(events))
	for i, e := range events {
		evtSlice[i] = *e
	}

	return session, msgSlice, evtSlice, false, lastNotificationID, nil
}

// GetAllWithMeta returns all sessions with metadata.
func (m *Manager) GetAllWithMeta(ctx context.Context) ([]pb.SessionSummary, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]pb.SessionSummary, 0, len(m.sessions))
	for id, state := range m.sessions {
		meta := pb.SessionSummary{Session: state.Session}

		count, err := m.storage.GetMessageCount(ctx, id)
		if err == nil {
			count32 := int32(count)
			meta.MessageCount = &count32
		}

		lastMsg, err := m.storage.GetLastMessage(ctx, id)
		if err == nil && lastMsg != nil {
			meta.LastMessageId = &lastMsg.Id
		}

		result = append(result, meta)
	}

	return result, nil
}

func (m *Manager) getSession(id string) *pb.Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if state, ok := m.sessions[id]; ok {
		return state.Session
	}
	return nil
}

func (m *Manager) getState(id string) *SessionState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.sessions[id]
}

// isActiveStatus returns true if the status represents an active session (using resources).
func isActiveStatus(status pb.SessionStatus) bool {
	return status == pb.SessionStatus_SESSION_STATUS_CREATING ||
		status == pb.SessionStatus_SESSION_STATUS_READY ||
		status == pb.SessionStatus_SESSION_STATUS_RUNNING
}

// countActiveSessions returns the number of active sessions.
// Must be called with m.mu held (read or write).
func (m *Manager) countActiveSessionsLocked() int {
	count := 0
	for _, state := range m.sessions {
		if isActiveStatus(state.Session.Status) {
			count++
		}
	}
	return count
}

// findOldestActiveSession finds the oldest active session by LastActiveAt.
// Excludes the session with excludeID. Must be called with m.mu held.
func (m *Manager) findOldestActiveSessionLocked(excludeID string) *SessionState {
	var oldest *SessionState
	var oldestTime time.Time

	for id, state := range m.sessions {
		if id == excludeID {
			continue
		}
		if !isActiveStatus(state.Session.Status) {
			continue
		}

		if state.Session.LastActiveAt == nil {
			continue
		}
		lastActive := state.Session.LastActiveAt.AsTime()

		if oldest == nil || lastActive.Before(oldestTime) {
			oldest = state
			oldestTime = lastActive
		}
	}

	return oldest
}

// ensureActiveSlot ensures there's room for a new active session.
// If at the limit, it pauses oldest active sessions until there's room (excluding excludeID).
// Returns the last paused session ID (if any) or empty string.
func (m *Manager) ensureActiveSlot(ctx context.Context, excludeID string) string {
	maxActive := m.config.MaxActiveSessions
	if maxActive <= 0 {
		return ""
	}

	var lastPausedID string

	for {
		m.mu.Lock()
		activeCount := m.countActiveSessionsLocked()

		if activeCount < maxActive {
			m.mu.Unlock()
			return lastPausedID
		}

		// Find oldest active session to pause
		oldest := m.findOldestActiveSessionLocked(excludeID)
		if oldest == nil {
			m.mu.Unlock()
			return lastPausedID
		}

		oldestID := oldest.Session.Id
		m.mu.Unlock()

		// Pause the oldest session
		slog.Info("Auto-pausing session to make room", "sessionID", oldestID, "activeCount", activeCount, "maxActive", maxActive)
		pausedSession, err := m.Pause(ctx, oldestID)
		if err != nil {
			slog.Error("Failed to auto-pause session", "sessionID", oldestID, "error", err)
			return lastPausedID
		}

		// Notify callback so API can broadcast to clients
		if m.onSessionUpdated != nil && pausedSession != nil {
			m.onSessionUpdated(pausedSession)
		}

		lastPausedID = oldestID
	}
}

// enforceActiveLimit pauses sessions until the active count is within MaxActiveSessions.
// This is called on startup to clean up excess sessions.
func (m *Manager) enforceActiveLimit(ctx context.Context) {
	maxActive := m.config.MaxActiveSessions
	if maxActive <= 0 {
		return
	}

	for {
		m.mu.Lock()
		activeCount := m.countActiveSessionsLocked()

		if activeCount <= maxActive {
			m.mu.Unlock()
			return
		}

		// Find oldest active session to pause
		oldest := m.findOldestActiveSessionLocked("")
		if oldest == nil {
			m.mu.Unlock()
			return
		}

		oldestID := oldest.Session.Id
		m.mu.Unlock()

		slog.Info("Enforcing active limit on startup", "sessionID", oldestID, "activeCount", activeCount, "maxActive", maxActive)
		if _, err := m.Pause(ctx, oldestID); err != nil {
			slog.Error("Failed to pause session during limit enforcement", "sessionID", oldestID, "error", err)
			return
		}
	}
}

// Subscribe creates a StreamSubscriber for a session.
// lastNotificationID specifies where to start reading:
//   - "$" = only new notifications
//   - "0" = from the beginning of the stream
//   - "<stream-id>" = from after the given ID (exclusive)
//
// The subscriber reads from Redis Streams and is closed when ctx is cancelled.
func (m *Manager) Subscribe(ctx context.Context, id string, lastNotificationID string) (*StreamSubscriber, error) {
	state := m.getState(id)
	if state == nil {
		return nil, fmt.Errorf("session %s not found", id)
	}

	client := m.storage.GetRedisClient()
	sub := NewStreamSubscriber(id, lastNotificationID, client)
	go sub.Run(ctx)

	return sub, nil
}

// ListGitHubRepos returns repositories accessible to the GitHub App installation.
func (m *Manager) ListGitHubRepos(ctx context.Context) ([]GitHubRepo, error) {
	if m.githubClient == nil {
		slog.Warn("ListGitHubRepos called but GitHub App is not configured")
		return nil, nil
	}

	repos, err := m.githubClient.ListRepos(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing repos: %w", err)
	}

	result := make([]GitHubRepo, len(repos))
	for i, r := range repos {
		result[i] = GitHubRepo{
			Name:        r.Name,
			FullName:    r.FullName,
			Private:     r.Private,
			Description: r.Description,
		}
	}

	return result, nil
}

// GitHubRepo represents a GitHub repository (kept for API compatibility).
type GitHubRepo struct {
	Name        string
	FullName    string
	Private     bool
	Description string
}

// GetSessionIDByPodName finds the session ID for a pod name (used by warm pool agents).
func (m *Manager) GetSessionIDByPodName(ctx context.Context, podName string) (string, error) {
	return m.k8s.GetSessionIDByPodName(ctx, podName)
}

// GetSessionConfig returns session configuration for the agent.
// This is used by agents to get session-specific config when using warm pools.
func (m *Manager) GetSessionConfig(ctx context.Context, sessionID string) (*AgentSessionConfig, error) {
	m.mu.RLock()
	state, ok := m.sessions[sessionID]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}

	config := &AgentSessionConfig{
		SessionID:          sessionID,
		AnthropicAPIKey:    m.config.AnthropicAPIKey,
		OpenAIAPIKey:       m.config.OpenAIAPIKey,
		MistralAPIKey:      m.config.MistralAPIKey,
		GitHubCopilotToken: m.config.GitHubCopilotToken,
		SdkType:            state.Session.SdkType,
		CopilotBackend:     state.Session.CopilotBackend,
	}

	if state.Session.Model != nil {
		model := *state.Session.Model
		config.Model = model

		// For Anthropic direct models (ID ends with :anthropic), force Anthropic backend
		if strings.HasSuffix(model, ":anthropic") {
			anthropicBackend := pb.CopilotBackend_COPILOT_BACKEND_ANTHROPIC
			config.CopilotBackend = &anthropicBackend
		}

		// For Codex SDK, parse model format: base:auth:effort (e.g., gpt-5-codex:oauth:high)
		// Also supports legacy format: base:auth (e.g., gpt-5-codex:oauth)
		parts := strings.Split(model, ":")
		if len(parts) >= 2 {
			authMode := parts[len(parts)-1]
			// Check if last part is a reasoning effort level
			if authMode == "low" || authMode == "medium" || authMode == "high" || authMode == "minimal" || authMode == "xhigh" {
				config.ReasoningEffort = authMode
				// Auth mode is second-to-last
				if len(parts) >= 3 {
					authMode = parts[len(parts)-2]
				} else {
					authMode = ""
				}
			}

			// Set credentials based on auth mode
			if authMode == "api" {
				config.OpenAIAPIKey = m.config.OpenAIAPIKey
			} else if authMode == "oauth" {
				config.CodexAccessToken = m.config.CodexAccessToken
				config.CodexIdToken = m.config.CodexIdToken
				config.CodexRefreshToken = m.config.CodexRefreshToken
			}
		}
	}

	// Setup GitHub repo access if configured
	if state.Session.Repo != nil && *state.Session.Repo != "" {
		config.Repo = github.NormalizeRepoURL(*state.Session.Repo)
		config.RepoAccess = state.Session.RepoAccess

		// Generate repo-scoped token via GitHub App for git credentials
		if token := m.createRepoToken(ctx, *state.Session.Repo, state.Session.RepoAccess); token != "" {
			config.GitHubToken = token
		}
	}

	return config, nil
}

// UpdateRepoAccess changes the repository access level for a session and sends new credentials to the agent.
func (m *Manager) UpdateRepoAccess(ctx context.Context, sessionID string, newAccess pb.RepoAccess) error {
	slog.Info("UpdateRepoAccess called", "sessionID", sessionID, "newAccess", newAccess)

	m.mu.Lock()
	state, ok := m.sessions[sessionID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("session %s not found", sessionID)
	}

	// Need repo to generate scoped token
	repo := ""
	if state.Session.Repo != nil {
		repo = *state.Session.Repo
	}

	// Update the session's repo access
	state.Session.RepoAccess = &newAccess
	m.mu.Unlock()

	// Generate new repo-scoped token for this access level
	newToken := m.createRepoToken(ctx, repo, &newAccess)
	if newToken == "" && repo != "" {
		slog.Warn("Failed to create GitHub token (GitHub App not configured?)", "sessionID", sessionID, "access", newAccess)
	}

	// Persist to Redis
	if err := m.storage.SaveSession(ctx, state.Session); err != nil {
		slog.Warn("Failed to persist session after repo access update", "sessionID", sessionID, "error", err)
	}

	// Send new credentials to the agent if connected
	m.mu.RLock()
	agent, agentConnected := m.agents[sessionID]
	m.mu.RUnlock()

	if agentConnected && newToken != "" {
		if err := agent.UpdateGitCredentials(newToken, newAccess); err != nil {
			slog.Warn("Failed to send updated credentials to agent", "sessionID", sessionID, "error", err)
			return fmt.Errorf("failed to update agent credentials: %w", err)
		}
		slog.Info("Sent updated git credentials to agent", "sessionID", sessionID, "access", newAccess)
	}

	// Emit session update
	m.emitSessionUpdated(ctx, state.Session)

	return nil
}

// publishNotification publishes a notification to Redis Streams.
// All StreamSubscribers reading from this session's stream will receive it.
func (m *Manager) publishNotification(ctx context.Context, sessionID string, notification *storage.Notification) {
	if _, err := m.storage.PublishNotification(ctx, sessionID, notification); err != nil {
		slog.Warn("Failed to publish notification", "sessionID", sessionID, "type", notification.Type, "error", err)
	}
}

// protoJsonOpts configures protojson to emit readable enum names instead of numbers.
var protoJsonOpts = protojson.MarshalOptions{
	UseEnumNumbers: false,
}

// emitSessionUpdated broadcasts a session update to all subscribers.
func (m *Manager) emitSessionUpdated(ctx context.Context, session *pb.Session) {
	payload, _ := protoJsonOpts.Marshal(session)
	m.publishNotification(ctx, session.Id, &storage.Notification{
		Type:      "session_update",
		Payload:   payload,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// emitSessionError broadcasts a session error to all subscribers.
func (m *Manager) emitSessionError(ctx context.Context, sessionID, errMsg string) {
	payload, _ := protoJsonOpts.Marshal(&pb.Error{
		Code:      "SESSION_ERROR",
		Message:   errMsg,
		SessionId: &sessionID,
	})
	m.publishNotification(ctx, sessionID, &storage.Notification{
		Type:      "session_error",
		Payload:   payload,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// emitAgentEvent broadcasts an agent event to all subscribers.
func (m *Manager) emitAgentEvent(ctx context.Context, sessionID string, event *pb.AgentEvent) {
	payload, _ := protoJsonOpts.Marshal(event)
	m.publishNotification(ctx, sessionID, &storage.Notification{
		Type:      "event",
		Payload:   payload,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// emitAgentMessage broadcasts an agent message to all subscribers.
func (m *Manager) emitAgentMessage(ctx context.Context, sessionID, messageID, content string, partial bool) {
	payload, _ := protoJsonOpts.Marshal(&pb.AgentMessageResponse{
		SessionId: sessionID,
		MessageId: messageID,
		Content:   content,
		Partial:   partial,
	})
	m.publishNotification(ctx, sessionID, &storage.Notification{
		Type:      "message",
		Payload:   payload,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// emitAgentDone broadcasts agent completion to all subscribers.
func (m *Manager) emitAgentDone(ctx context.Context, sessionID string) {
	payload, _ := protoJsonOpts.Marshal(&pb.AgentDoneResponse{
		SessionId: sessionID,
	})
	m.publishNotification(ctx, sessionID, &storage.Notification{
		Type:      "agent_done",
		Payload:   payload,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// emitAgentError broadcasts an agent error to all subscribers.
func (m *Manager) emitAgentError(ctx context.Context, sessionID, errMsg string) {
	payload, _ := protoJsonOpts.Marshal(&pb.Error{
		Code:      "AGENT_ERROR",
		Message:   errMsg,
		SessionId: &sessionID,
	})
	m.publishNotification(ctx, sessionID, &storage.Notification{
		Type:      "agent_error",
		Payload:   payload,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// emitUserMessage broadcasts a user message to all subscribers.
func (m *Manager) emitUserMessage(ctx context.Context, sessionID, text string) {
	payload, _ := protoJsonOpts.Marshal(&pb.UserMessageResponse{
		SessionId: sessionID,
		Content:   text,
	})
	m.publishNotification(ctx, sessionID, &storage.Notification{
		Type:      "user_message",
		Payload:   payload,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// EmitEvent broadcasts an event from a sandbox to all connected clients.
// This is used by the internal API to receive events from entrypoint scripts.
func (m *Manager) EmitEvent(ctx context.Context, sessionID string, event *pb.AgentEvent) error {
	// Verify session exists
	m.mu.RLock()
	_, ok := m.sessions[sessionID]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}

	// Emit the event
	m.emitAgentEvent(ctx, sessionID, event)
	slog.Debug("Emitted internal event", "sessionID", sessionID, "kind", event.Kind)
	return nil
}

// emitTerminalOutput broadcasts terminal output to all connected clients.
func (m *Manager) emitTerminalOutput(ctx context.Context, sessionID, data string) {
	payload, _ := protoJsonOpts.Marshal(&pb.TerminalOutputResponse{
		SessionId: sessionID,
		Data:      data,
	})
	m.publishNotification(ctx, sessionID, &storage.Notification{
		Type:      "terminal_output",
		Payload:   payload,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// SendTerminalInput sends input to the agent terminal.
func (m *Manager) SendTerminalInput(ctx context.Context, sessionID, data string) error {
	m.mu.RLock()
	agent, ok := m.agents[sessionID]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("no agent connected for session %s", sessionID)
	}

	return agent.SendTerminalInput(data)
}

// ResizeTerminal resizes the agent terminal.
func (m *Manager) ResizeTerminal(ctx context.Context, sessionID string, cols, rows int) error {
	m.mu.RLock()
	agent, ok := m.agents[sessionID]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("no agent connected for session %s", sessionID)
	}

	return agent.ResizeTerminal(cols, rows)
}

// RegisterAgentConnection registers an agent connection for a session.
func (m *Manager) RegisterAgentConnection(sessionID string, conn AgentConnection) {
	m.mu.Lock()
	m.agents[sessionID] = conn

	// Check for pending prompt and session status
	var pendingPrompt string
	var wasInterrupted bool
	var isRunning bool
	if state, ok := m.sessions[sessionID]; ok {
		// Session is ready to handle prompts if RUNNING or READY (sandbox available)
		isRunning = (state.Session.Status == pb.SessionStatus_SESSION_STATUS_RUNNING ||
			state.Session.Status == pb.SessionStatus_SESSION_STATUS_READY)
		// Grab pending prompt if session is ready to handle it
		// During restore, session is RESUMING until restore job completes
		if isRunning && state.PendingPrompt != "" {
			pendingPrompt = state.PendingPrompt
			state.PendingPrompt = "" // Clear it
		} else if state.PendingPrompt != "" {
			slog.Warn("Pending prompt exists but session not ready", "sessionID", sessionID, "status", state.Session.Status.String(), "pendingPrompt", state.PendingPrompt[:min(50, len(state.PendingPrompt))])
		}
		wasInterrupted = (state.Session.Status == pb.SessionStatus_SESSION_STATUS_INTERRUPTED)
	} else {
		slog.Warn("Session not found in RegisterAgentConnection", "sessionID", sessionID)
	}
	m.mu.Unlock()

	slog.Info("Agent connection registered", "sessionID", sessionID, "wasInterrupted", wasInterrupted, "isRunning", isRunning, "hasPendingPrompt", pendingPrompt != "")

	ctx := context.Background()

	// If session was interrupted, emit reconnect event (status stays interrupted until user sends prompt)
	if wasInterrupted {
		event := &pb.AgentEvent{
			Kind:      pb.AgentEventKind_AGENT_EVENT_KIND_AGENT_RECONNECTED,
			Timestamp: timestamppb.Now(),
		}
		m.emitAgentEvent(ctx, sessionID, event)
		slog.Info("Emitted agent_reconnected event", "sessionID", sessionID)
	}

	// Send pending prompt if any (for prompts queued before agent connected during session creation)
	if pendingPrompt != "" {
		slog.Info("Sending pending prompt to agent", "sessionID", sessionID)
		if err := m.SendPrompt(ctx, sessionID, pendingPrompt); err != nil {
			slog.Error("Failed to send pending prompt", "sessionID", sessionID, "error", err)
		}
	}
}

// UnregisterAgentConnection unregisters an agent connection.
// If the session was running, it will be marked as interrupted.
func (m *Manager) UnregisterAgentConnection(sessionID string) {
	m.mu.Lock()
	wasRunning := false
	if state, ok := m.sessions[sessionID]; ok {
		wasRunning = (state.Session.Status == pb.SessionStatus_SESSION_STATUS_RUNNING)
	}
	delete(m.agents, sessionID)
	m.mu.Unlock()

	slog.Info("Agent connection unregistered", "sessionID", sessionID, "wasRunning", wasRunning)

	// If session was running, mark as interrupted and emit event
	if wasRunning {
		ctx := context.Background()
		m.updateSessionStatus(ctx, sessionID, pb.SessionStatus_SESSION_STATUS_INTERRUPTED)

		event := &pb.AgentEvent{
			Kind:      pb.AgentEventKind_AGENT_EVENT_KIND_AGENT_DISCONNECTED,
			Timestamp: timestamppb.Now(),
		}
		m.emitAgentEvent(ctx, sessionID, event)

		slog.Info("Session marked as interrupted due to agent disconnect", "sessionID", sessionID)
	}
}

// GetAgentConnection returns the agent connection for a session (or nil if not connected).
func (m *Manager) GetAgentConnection(sessionID string) AgentConnection {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.agents[sessionID]
}

// ListModels returns available models for the specified SDK type.
// For Copilot SDK, returns a combined list of GitHub Copilot and Anthropic (BYOK) models.
// For Codex SDK, returns OpenAI models with "gpt-codex" family.
func (m *Manager) ListModels(sdkType pb.SdkType, copilotBackend *pb.CopilotBackend) []*pb.ModelInfo {
	switch sdkType {
	case pb.SdkType_SDK_TYPE_CLAUDE:
		return m.fetchModelsFromModelsDev("anthropic")
	case pb.SdkType_SDK_TYPE_OPENCODE:
		return m.fetchOpenCodeModels()
	case pb.SdkType_SDK_TYPE_COPILOT:
		// Return combined list of GitHub Copilot + Anthropic (BYOK) models
		return m.fetchCopilotModels()
	case pb.SdkType_SDK_TYPE_CODEX:
		// Return OpenAI models with "gpt-codex" family
		return m.fetchCodexModels()
	default:
		return m.fetchModelsFromModelsDev("anthropic")
	}
}

// modelsDevResponse represents the response from models.dev API
type modelsDevResponse map[string]modelsDevProvider

type modelsDevProvider struct {
	ID     string                    `json:"id"`
	Name   string                    `json:"name"`
	Models map[string]modelsDevModel `json:"models"`
}

type modelsDevModel struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Family      string            `json:"family"`
	Attachment  bool              `json:"attachment"`
	Reasoning   bool              `json:"reasoning"`
	ToolCall    bool              `json:"tool_call"`
	Modalities  modelsDevModality `json:"modalities"`
	Cost        modelsDevCost     `json:"cost"`
	Limit       modelsDevLimit    `json:"limit"`
	ReleaseDate string            `json:"release_date"`
}

type modelsDevModality struct {
	Input  []string `json:"input"`
	Output []string `json:"output"`
}

type modelsDevCost struct {
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
}

type modelsDevLimit struct {
	Context int `json:"context"`
	Output  int `json:"output"`
}

// fetchModelsFromModelsDev fetches models from models.dev API for a specific provider
func (m *Manager) fetchModelsFromModelsDev(provider string) []*pb.ModelInfo {
	// Check cache first (valid for 5 minutes)
	m.copilotModelsMu.RLock()
	cacheKey := "modelsDev_" + provider
	if cached, ok := m.modelsDevCache[cacheKey]; ok && time.Since(cached.fetchedAt) < 5*time.Minute {
		models := cached.models
		m.copilotModelsMu.RUnlock()
		return models
	}
	m.copilotModelsMu.RUnlock()

	// Fetch from models.dev
	models := m.doFetchModelsFromModelsDev(provider)
	if models == nil {
		return getModelsFallback(provider)
	}

	// Update cache
	m.copilotModelsMu.Lock()
	if m.modelsDevCache == nil {
		m.modelsDevCache = make(map[string]*modelsDevCacheEntry)
	}
	m.modelsDevCache[cacheKey] = &modelsDevCacheEntry{
		models:    models,
		fetchedAt: time.Now(),
	}
	m.copilotModelsMu.Unlock()

	return models
}

func (m *Manager) doFetchModelsFromModelsDev(provider string) []*pb.ModelInfo {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://models.dev/api.json")
	if err != nil {
		slog.Error("Failed to fetch from models.dev", "error", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("models.dev API error", "status", resp.StatusCode)
		return nil
	}

	var data modelsDevResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		slog.Error("Failed to decode models.dev response", "error", err)
		return nil
	}

	providerData, ok := data[provider]
	if !ok {
		slog.Error("Provider not found in models.dev", "provider", provider)
		return nil
	}

	models := make([]*pb.ModelInfo, 0, len(providerData.Models))
	for _, model := range providerData.Models {
		capabilities := []string{"chat", "code"}
		for _, input := range model.Modalities.Input {
			if input == "image" {
				capabilities = append(capabilities, "vision")
				break
			}
		}
		if model.Reasoning {
			capabilities = append(capabilities, "reasoning")
		}

		models = append(models, &pb.ModelInfo{
			Id:           model.ID,
			Name:         model.Name,
			Provider:     strPtr(providerData.Name),
			Capabilities: capabilities,
		})
	}

	slog.Info("Fetched models from models.dev", "provider", provider, "count", len(models))
	return models
}

// fetchOpenCodeModels fetches models for OpenCode SDK based on configured API keys
func (m *Manager) fetchOpenCodeModels() []*pb.ModelInfo {
	var models []*pb.ModelInfo

	// Add Anthropic models if ANTHROPIC_API_KEY is set
	if m.config.AnthropicAPIKey != "" {
		anthropicModels := m.fetchModelsFromModelsDev("anthropic")
		models = append(models, anthropicModels...)
	}

	// Add OpenAI models if OPENAI_API_KEY is set
	if m.config.OpenAIAPIKey != "" {
		openaiModels := m.fetchModelsFromModelsDev("openai")
		models = append(models, openaiModels...)
	}

	// Add Mistral models if MISTRAL_API_KEY is set
	if m.config.MistralAPIKey != "" {
		mistralModels := m.fetchModelsFromModelsDev("mistral")
		models = append(models, mistralModels...)
	}

	if len(models) == 0 {
		slog.Warn("No OpenCode credentials configured (need ANTHROPIC_API_KEY, OPENAI_API_KEY, or MISTRAL_API_KEY)")
	}

	return models
}

// fetchCopilotModels fetches combined GitHub Copilot + Anthropic models
func (m *Manager) fetchCopilotModels() []*pb.ModelInfo {
	// Check which credentials are available
	hasGitHubCopilot := m.config.GitHubCopilotToken != ""
	hasAnthropicKey := m.config.AnthropicAPIKey != ""

	// If neither credential is available, return empty list
	if !hasGitHubCopilot && !hasAnthropicKey {
		slog.Warn("No Copilot credentials configured (need GITHUB_COPILOT_TOKEN or ANTHROPIC_API_KEY)")
		return nil
	}

	// Check cache first (valid for 5 minutes)
	// Note: Cache is shared regardless of credentials, so we filter after retrieving
	m.copilotModelsMu.RLock()
	if m.copilotModelsCache != nil && time.Since(m.copilotModelsCacheAt) < 5*time.Minute {
		models := m.filterCopilotModelsByCredentials(m.copilotModelsCache, hasGitHubCopilot, hasAnthropicKey)
		m.copilotModelsMu.RUnlock()
		return models
	}
	m.copilotModelsMu.RUnlock()

	// Fetch from models.dev - get both github-copilot and anthropic providers
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://models.dev/api.json")
	if err != nil {
		slog.Error("Failed to fetch from models.dev", "error", err)
		return m.getCopilotModelsFallback()
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("models.dev API error", "status", resp.StatusCode)
		return m.getCopilotModelsFallback()
	}

	var data modelsDevResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		slog.Error("Failed to decode models.dev response", "error", err)
		return m.getCopilotModelsFallback()
	}

	var models []*pb.ModelInfo

	// Add GitHub Copilot models (always fetch for cache, filter later)
	if copilotData, ok := data["github-copilot"]; ok {
		for _, model := range copilotData.Models {
			capabilities := []string{"chat", "code"}
			for _, input := range model.Modalities.Input {
				if input == "image" {
					capabilities = append(capabilities, "vision")
					break
				}
			}
			if model.Reasoning {
				capabilities = append(capabilities, "reasoning")
			}

			models = append(models, &pb.ModelInfo{
				Id:           model.ID,
				Name:         model.Name,
				Provider:     strPtr("Copilot"),
				Capabilities: capabilities,
			})
		}
	}

	// Add Anthropic models for BYOK (always fetch for cache, filter later)
	if anthropicData, ok := data["anthropic"]; ok {
		for _, model := range anthropicData.Models {
			capabilities := []string{"chat", "code"}
			for _, input := range model.Modalities.Input {
				if input == "image" {
					capabilities = append(capabilities, "vision")
					break
				}
			}
			if model.Reasoning {
				capabilities = append(capabilities, "reasoning")
			}

			models = append(models, &pb.ModelInfo{
				Id:           model.ID + ":anthropic",
				Name:         model.Name,
				Provider:     strPtr("Anthropic"),
				Capabilities: capabilities,
			})
		}
	}

	slog.Info("Fetched Copilot models from models.dev", "count", len(models))

	// Update cache with ALL models (filter happens on retrieval)
	m.copilotModelsMu.Lock()
	m.copilotModelsCache = models
	m.copilotModelsCacheAt = time.Now()
	m.copilotModelsMu.Unlock()

	// Return filtered models based on available credentials
	return m.filterCopilotModelsByCredentials(models, hasGitHubCopilot, hasAnthropicKey)
}

// filterCopilotModelsByCredentials filters models based on available credentials
func (m *Manager) filterCopilotModelsByCredentials(models []*pb.ModelInfo, hasGitHubCopilot, hasAnthropicKey bool) []*pb.ModelInfo {
	// If both credentials available, return all models
	if hasGitHubCopilot && hasAnthropicKey {
		return models
	}

	var filtered []*pb.ModelInfo
	for _, model := range models {
		// Anthropic direct models (ID ends with :anthropic) require Anthropic API key
		isAnthropicDirect := strings.HasSuffix(model.Id, ":anthropic")
		if isAnthropicDirect && hasAnthropicKey {
			filtered = append(filtered, model)
		}

		// Copilot models require GitHub Copilot token
		if !isAnthropicDirect && hasGitHubCopilot {
			filtered = append(filtered, model)
		}
	}

	return filtered
}

// getModelsFallback returns fallback models for a provider when models.dev is unavailable
func getModelsFallback(provider string) []*pb.ModelInfo {
	switch provider {
	case "anthropic":
		return []*pb.ModelInfo{
			{Id: "claude-sonnet-4-5", Name: "Claude Sonnet 4.5", Provider: strPtr("Anthropic"), Capabilities: []string{"chat", "vision", "code", "reasoning"}},
			{Id: "claude-sonnet-4-0", Name: "Claude Sonnet 4", Provider: strPtr("Anthropic"), Capabilities: []string{"chat", "vision", "code"}},
		}
	case "opencode":
		return []*pb.ModelInfo{
			{Id: "anthropic/claude-sonnet-4-5", Name: "Claude Sonnet 4.5", Provider: strPtr("Anthropic"), Capabilities: []string{"chat", "vision", "code", "reasoning"}},
			{Id: "anthropic/claude-sonnet-4-0", Name: "Claude Sonnet 4", Provider: strPtr("Anthropic"), Capabilities: []string{"chat", "vision", "code"}},
		}
	default:
		return nil
	}
}

// getCopilotModelsFallback returns fallback models when models.dev is unavailable
// Models are filtered based on available credentials
func (m *Manager) getCopilotModelsFallback() []*pb.ModelInfo {
	hasGitHubCopilot := m.config.GitHubCopilotToken != ""
	hasAnthropicKey := m.config.AnthropicAPIKey != ""

	var models []*pb.ModelInfo

	// GitHub Copilot models (require GITHUB_COPILOT_TOKEN)
	if hasGitHubCopilot {
		models = append(models,
			&pb.ModelInfo{Id: "claude-sonnet-4.5", Name: "Claude Sonnet 4.5", Provider: strPtr("Copilot"), Capabilities: []string{"chat", "vision", "code", "reasoning"}},
			&pb.ModelInfo{Id: "claude-sonnet-4", Name: "Claude Sonnet 4", Provider: strPtr("Copilot"), Capabilities: []string{"chat", "vision", "code"}},
			&pb.ModelInfo{Id: "gpt-4o", Name: "GPT-4o", Provider: strPtr("Copilot"), Capabilities: []string{"chat", "vision", "code"}},
			&pb.ModelInfo{Id: "gemini-2.5-pro", Name: "Gemini 2.5 Pro", Provider: strPtr("Copilot"), Capabilities: []string{"chat", "vision", "code"}},
		)
	}

	// Anthropic direct models (require ANTHROPIC_API_KEY)
	if hasAnthropicKey {
		models = append(models,
			&pb.ModelInfo{Id: "claude-sonnet-4-5:anthropic", Name: "Claude Sonnet 4.5", Provider: strPtr("Anthropic"), Capabilities: []string{"chat", "vision", "code", "reasoning"}},
			&pb.ModelInfo{Id: "claude-sonnet-4-0:anthropic", Name: "Claude Sonnet 4", Provider: strPtr("Anthropic"), Capabilities: []string{"chat", "vision", "code"}},
		)
	}

	return models
}

// fetchCodexModels fetches OpenAI Codex models (family: gpt-codex or gpt-codex-mini)
// Returns models with auth mode suffix based on available credentials:
// - ":api" suffix when OPENAI_API_KEY is configured
// - ":oauth" suffix when CODEX_ACCESS_TOKEN is configured
// If both are configured, returns both sets of models
func (m *Manager) fetchCodexModels() []*pb.ModelInfo {
	hasAPIKey := m.config.OpenAIAPIKey != ""
	hasOAuth := m.config.CodexAccessToken != ""

	// If neither is configured, return empty
	if !hasAPIKey && !hasOAuth {
		return nil
	}

	// Check cache first (valid for 5 minutes)
	// Cache key includes auth modes to handle config changes
	cacheKey := fmt.Sprintf("codex:api=%t:oauth=%t", hasAPIKey, hasOAuth)
	m.copilotModelsMu.RLock()
	if cached, ok := m.modelsDevCache[cacheKey]; ok && time.Since(cached.fetchedAt) < 5*time.Minute {
		models := cached.models
		m.copilotModelsMu.RUnlock()
		return models
	}
	m.copilotModelsMu.RUnlock()

	// Fetch base models from models.dev
	baseModels := m.doFetchCodexModels()
	if baseModels == nil {
		baseModels = m.getCodexBaseModels()
	}

	// Reasoning effort levels to generate
	effortLevels := []struct {
		suffix string
		label  string
	}{
		{"low", "Low"},
		{"medium", "Med"},
		{"high", "High"},
		{"xhigh", "xHigh"},
	}

	// Create models with auth mode and reasoning effort suffixes
	// Format: model:auth:effort (e.g., gpt-5-codex:oauth:high)
	var models []*pb.ModelInfo
	for _, base := range baseModels {
		if hasAPIKey {
			for _, effort := range effortLevels {
				models = append(models, &pb.ModelInfo{
					Id:              fmt.Sprintf("%s:api:%s", base.Id, effort.suffix),
					Name:            base.Name,
					Provider:        strPtr("OpenAI"),
					Capabilities:    base.Capabilities,
					ReasoningEffort: strPtr(effort.label),
				})
			}
		}
		if hasOAuth {
			for _, effort := range effortLevels {
				models = append(models, &pb.ModelInfo{
					Id:              fmt.Sprintf("%s:oauth:%s", base.Id, effort.suffix),
					Name:            base.Name,
					Provider:        strPtr("ChatGPT"),
					Capabilities:    base.Capabilities,
					ReasoningEffort: strPtr(effort.label),
				})
			}
		}
	}

	// Update cache
	m.copilotModelsMu.Lock()
	if m.modelsDevCache == nil {
		m.modelsDevCache = make(map[string]*modelsDevCacheEntry)
	}
	m.modelsDevCache[cacheKey] = &modelsDevCacheEntry{
		models:    models,
		fetchedAt: time.Now(),
	}
	m.copilotModelsMu.Unlock()

	slog.Info("Fetched Codex models", "count", len(models), "hasAPIKey", hasAPIKey, "hasOAuth", hasOAuth)
	return models
}

func (m *Manager) doFetchCodexModels() []*pb.ModelInfo {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://models.dev/api.json")
	if err != nil {
		slog.Error("Failed to fetch from models.dev", "error", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("models.dev API error", "status", resp.StatusCode)
		return nil
	}

	var data modelsDevResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		slog.Error("Failed to decode models.dev response", "error", err)
		return nil
	}

	openaiData, ok := data["openai"]
	if !ok {
		slog.Error("OpenAI provider not found in models.dev")
		return nil
	}

	var models []*pb.ModelInfo
	for _, model := range openaiData.Models {
		// Filter to only include Codex models (family contains "gpt-codex")
		if !strings.Contains(model.Family, "gpt-codex") {
			continue
		}

		capabilities := []string{"chat", "code"}
		for _, input := range model.Modalities.Input {
			if input == "image" {
				capabilities = append(capabilities, "vision")
				break
			}
		}
		if model.Reasoning {
			capabilities = append(capabilities, "reasoning")
		}

		models = append(models, &pb.ModelInfo{
			Id:           model.ID,
			Name:         model.Name,
			Provider:     strPtr("OpenAI"),
			Capabilities: capabilities,
		})
	}

	slog.Info("Fetched Codex models from models.dev", "count", len(models))
	return models
}

// getCodexBaseModels returns fallback base Codex models when models.dev is unavailable
// These are base models without auth mode suffix - the suffix is added by fetchCodexModels
func (m *Manager) getCodexBaseModels() []*pb.ModelInfo {
	return []*pb.ModelInfo{
		{Id: "gpt-5.2-codex", Name: "GPT-5.2 Codex", Provider: strPtr("OpenAI"), Capabilities: []string{"chat", "code", "reasoning"}},
		{Id: "gpt-5.1-codex", Name: "GPT-5.1 Codex", Provider: strPtr("OpenAI"), Capabilities: []string{"chat", "code", "reasoning"}},
		{Id: "codex-mini-latest", Name: "Codex Mini", Provider: strPtr("OpenAI"), Capabilities: []string{"chat", "code"}},
	}
}

// ============================================================================
// Snapshot Operations
// ============================================================================

// CreateSnapshot creates a snapshot of the session's PVC using Kubernetes VolumeSnapshots.
func (m *Manager) CreateSnapshot(ctx context.Context, sessionID string, name string) (*pb.Snapshot, error) {
	state := m.getState(sessionID)
	if state == nil {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}

	// Get message count for the snapshot
	msgCount, _ := m.storage.GetMessageCount(ctx, sessionID)

	// Count assistant messages (turns)
	messages, _ := m.storage.GetMessages(ctx, sessionID, nil)
	turnNumber := 0
	for _, msg := range messages {
		if msg.Role == pb.MessageRole_MESSAGE_ROLE_ASSISTANT {
			turnNumber++
		}
	}

	// Get current event stream ID for snapshot point
	eventStreamID, _ := m.storage.GetLastEventStreamID(ctx, sessionID)

	// Generate snapshot ID
	snapshotID := uuid.NewString()[:12]

	// Create K8s VolumeSnapshot
	if err := m.k8s.CreateVolumeSnapshot(ctx, sessionID, snapshotID); err != nil {
		return nil, fmt.Errorf("failed to create volume snapshot: %w", err)
	}

	// Wait for snapshot to be ready (with timeout)
	if err := m.k8s.WaitForSnapshotReady(ctx, sessionID, snapshotID, 60*time.Second); err != nil {
		// Cleanup failed snapshot
		_ = m.k8s.DeleteVolumeSnapshot(ctx, sessionID, snapshotID)
		return nil, fmt.Errorf("snapshot not ready: %w", err)
	}

	// Create snapshot record
	snapshot := &pb.Snapshot{
		Id:            snapshotID,
		SessionId:     sessionID,
		Name:          name,
		CreatedAt:     timestamppb.Now(),
		SizeBytes:     0, // K8s VolumeSnapshots don't report size synchronously
		TurnNumber:    int32(turnNumber),
		MessageCount:  int32(msgCount),
		EventStreamId: eventStreamID,
	}

	// Save to storage
	if err := m.storage.SaveSnapshot(ctx, snapshot); err != nil {
		return nil, fmt.Errorf("failed to save snapshot: %w", err)
	}

	slog.Info("Snapshot created", "sessionID", sessionID, "snapshotID", snapshotID, "name", name)
	return snapshot, nil
}

// RestoreSnapshot restores a session from a snapshot.
// This involves:
// 1. Setting session status to "restoring"
// 2. Deleting the sandbox/pod and old PVC
// 3. Creating new PVC from the VolumeSnapshot
// 4. Truncating messages/events in Redis
// 5. Session will be recreated when client reconnects
func (m *Manager) RestoreSnapshot(ctx context.Context, sessionID, snapshotID string) (int32, error) {
	state := m.getState(sessionID)
	if state == nil {
		return 0, fmt.Errorf("session %s not found", sessionID)
	}

	// Must be ready or interrupted (not running) to restore
	if state.Session.Status != pb.SessionStatus_SESSION_STATUS_READY &&
		state.Session.Status != pb.SessionStatus_SESSION_STATUS_INTERRUPTED {
		return 0, fmt.Errorf("cannot restore while session is %s", state.Session.Status.String())
	}

	// Get snapshot metadata
	snapshot, err := m.storage.GetSnapshot(ctx, sessionID, snapshotID)
	if err != nil {
		return 0, fmt.Errorf("snapshot not found: %w", err)
	}

	slog.Info("Starting restore from snapshot", "sessionID", sessionID, "snapshotID", snapshotID)

	// Update session status to indicate restore in progress
	state.Session.Status = pb.SessionStatus_SESSION_STATUS_PAUSED
	_ = m.storage.UpdateSessionStatus(ctx, sessionID, pb.SessionStatus_SESSION_STATUS_PAUSED)

	// Disconnect the agent (if connected)
	m.mu.Lock()
	if _, ok := m.agents[sessionID]; ok {
		delete(m.agents, sessionID)
		// The agent connection will be closed when the pod is deleted
	}
	m.mu.Unlock()

	// Clean up K8s resources (sandbox, PVC) - returns old PVC name for cleanup after restore
	oldPVCName, err := m.k8s.RestoreFromSnapshot(ctx, sessionID, snapshotID)
	if err != nil {
		slog.Error("Restore cleanup failed", "sessionID", sessionID, "snapshotID", snapshotID, "error", err)
		state.Session.Status = pb.SessionStatus_SESSION_STATUS_READY
		_ = m.storage.UpdateSessionStatus(ctx, sessionID, pb.SessionStatus_SESSION_STATUS_READY)
		return 0, fmt.Errorf("restore failed: %w", err)
	}

	// Store snapshot ID for next sandbox creation (bypasses warm pool)
	state.RestoreSnapshotID = snapshotID
	if err := m.storage.SetRestoreSnapshotID(ctx, sessionID, snapshotID); err != nil {
		slog.Warn("Failed to persist restore snapshot ID", "sessionID", sessionID, "error", err)
	}

	// Store old PVC name so it can be deleted after restore completes
	if oldPVCName != "" {
		if err := m.storage.SetOldPVCName(ctx, sessionID, oldPVCName); err != nil {
			slog.Warn("Failed to persist old PVC name", "sessionID", sessionID, "pvc", oldPVCName, "error", err)
		}
	}

	// Truncate messages, events, and notifications in parallel (independent operations)
	var truncateWg sync.WaitGroup
	truncateWg.Add(3)
	go func() {
		defer truncateWg.Done()
		if err := m.storage.TruncateMessages(ctx, sessionID, int(snapshot.MessageCount)); err != nil {
			slog.Warn("Failed to truncate messages", "sessionID", sessionID, "error", err)
		}
	}()
	go func() {
		defer truncateWg.Done()
		if err := m.storage.TruncateEventsAfter(ctx, sessionID, snapshot.EventStreamId); err != nil {
			slog.Warn("Failed to truncate events", "sessionID", sessionID, "error", err)
		}
	}()
	go func() {
		defer truncateWg.Done()
		if err := m.storage.TruncateNotificationsAfter(ctx, sessionID, snapshot.EventStreamId); err != nil {
			slog.Warn("Failed to truncate notifications", "sessionID", sessionID, "error", err)
		}
	}()
	truncateWg.Wait()

	// Delete snapshots newer than the restored one (destructive restore)
	m.deleteSnapshotsAfter(ctx, sessionID, snapshot)

	// Delete the K8s VolumeSnapshots that are newer than the restored one
	m.deleteK8sSnapshotsAfter(ctx, sessionID, snapshot)

	slog.Info("Snapshot restored", "sessionID", sessionID, "snapshotID", snapshotID, "messagesRestored", snapshot.MessageCount)

	// Resume immediately to create sandbox from snapshot
	// This runs async so we don't block the restore response
	go func() {
		if _, err := m.Resume(context.Background(), sessionID); err != nil {
			slog.Error("Failed to resume after restore", "sessionID", sessionID, "error", err)
		}
	}()

	return snapshot.MessageCount, nil
}

// ListSnapshots returns all snapshots for a session.
func (m *Manager) ListSnapshots(ctx context.Context, sessionID string) ([]*pb.Snapshot, error) {
	return m.storage.ListSnapshots(ctx, sessionID)
}

// AutoSnapshot creates an auto-snapshot after an agent turn completes.
// This runs asynchronously and doesn't block the main flow.
func (m *Manager) AutoSnapshot(ctx context.Context, sessionID string, turnNumber int, promptSummary string) {
	// Truncate prompt for name
	name := promptSummary
	if len(name) > 50 {
		name = name[:47] + "..."
	}
	name = fmt.Sprintf("Turn %d: %s", turnNumber, name)

	snapshot, err := m.CreateSnapshot(ctx, sessionID, name)
	if err != nil {
		slog.Warn("Auto-snapshot failed", "sessionID", sessionID, "error", err)
		return
	}

	// Enforce retention (keep last 10 snapshots)
	m.enforceSnapshotRetention(ctx, sessionID, 10)

	// Emit snapshot created notification to clients
	m.emitSnapshotCreated(ctx, sessionID, snapshot)

	slog.Info("Auto-snapshot created", "sessionID", sessionID, "snapshotID", snapshot.Id, "turn", turnNumber)
}

// enforceSnapshotRetention deletes old snapshots beyond the limit.
func (m *Manager) enforceSnapshotRetention(ctx context.Context, sessionID string, maxSnapshots int) {
	snapshots, err := m.storage.ListSnapshots(ctx, sessionID)
	if err != nil || len(snapshots) <= maxSnapshots {
		return
	}

	// Delete oldest snapshots beyond limit (list is already newest-first)
	for i := maxSnapshots; i < len(snapshots); i++ {
		if err := m.storage.DeleteSnapshot(ctx, sessionID, snapshots[i].Id); err != nil {
			slog.Warn("Failed to delete old snapshot", "sessionID", sessionID, "snapshotID", snapshots[i].Id, "error", err)
		} else {
			slog.Debug("Deleted old snapshot", "sessionID", sessionID, "snapshotID", snapshots[i].Id)
		}
	}
}

// deleteSnapshotsAfter deletes all snapshots created after the given snapshot (destructive restore).
func (m *Manager) deleteSnapshotsAfter(ctx context.Context, sessionID string, restoredSnapshot *pb.Snapshot) {
	snapshots, err := m.storage.ListSnapshots(ctx, sessionID)
	if err != nil {
		slog.Warn("Failed to list snapshots for cleanup", "sessionID", sessionID, "error", err)
		return
	}

	restoredTime := restoredSnapshot.CreatedAt.AsTime()
	deleted := 0

	// Delete snapshots newer than the restored one (list is newest-first)
	for _, s := range snapshots {
		if s.CreatedAt.AsTime().After(restoredTime) {
			if err := m.storage.DeleteSnapshot(ctx, sessionID, s.Id); err != nil {
				slog.Warn("Failed to delete newer snapshot", "sessionID", sessionID, "snapshotID", s.Id, "error", err)
			} else {
				deleted++
				slog.Debug("Deleted newer snapshot", "sessionID", sessionID, "snapshotID", s.Id)
			}
		}
	}

	if deleted > 0 {
		slog.Info("Deleted snapshots after restore point", "sessionID", sessionID, "count", deleted)
		// Emit updated snapshot list to clients
		m.emitSnapshotList(ctx, sessionID)
	}
}

// deleteK8sSnapshotsAfter deletes K8s VolumeSnapshots created after the restored snapshot.
func (m *Manager) deleteK8sSnapshotsAfter(ctx context.Context, sessionID string, restoredSnapshot *pb.Snapshot) {
	snapshots, err := m.storage.ListSnapshots(ctx, sessionID)
	if err != nil {
		slog.Warn("Failed to list snapshots for K8s cleanup", "sessionID", sessionID, "error", err)
		return
	}

	restoredTime := restoredSnapshot.CreatedAt.AsTime()

	for _, s := range snapshots {
		if s.CreatedAt.AsTime().After(restoredTime) {
			if err := m.k8s.DeleteVolumeSnapshot(ctx, sessionID, s.Id); err != nil {
				slog.Warn("Failed to delete K8s VolumeSnapshot", "sessionID", sessionID, "snapshotID", s.Id, "error", err)
			} else {
				slog.Debug("Deleted K8s VolumeSnapshot", "sessionID", sessionID, "snapshotID", s.Id)
			}
		}
	}
}

// emitSnapshotList broadcasts updated snapshot list to clients.
func (m *Manager) emitSnapshotList(ctx context.Context, sessionID string) {
	snapshots, err := m.storage.ListSnapshots(ctx, sessionID)
	if err != nil {
		return
	}
	payload, _ := protoJsonOpts.Marshal(&pb.SnapshotListResponse{
		SessionId: sessionID,
		Snapshots: snapshots,
	})
	m.publishNotification(ctx, sessionID, &storage.Notification{
		Type:      "snapshot_list",
		Payload:   payload,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// emitSnapshotCreated broadcasts snapshot created event to clients.
func (m *Manager) emitSnapshotCreated(ctx context.Context, sessionID string, snapshot *pb.Snapshot) {
	payload, _ := protoJsonOpts.Marshal(&pb.SnapshotCreatedResponse{
		SessionId: sessionID,
		Snapshot:  snapshot,
	})
	m.publishNotification(ctx, sessionID, &storage.Notification{
		Type:      "snapshot_created",
		Payload:   payload,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// GetCopilotStatus returns GitHub Copilot authentication status and quota.
// Note: This is currently a placeholder - actual implementation would require
// calling the GitHub Copilot API which needs the agent to be running.
func (m *Manager) GetCopilotStatus(ctx context.Context) *pb.CopilotStatusResponse {
	// Check if we have a GitHub Copilot token configured
	hasGitHubCopilotToken := m.config.GitHubCopilotToken != ""

	return &pb.CopilotStatusResponse{
		Auth: &pb.CopilotAuthStatus{
			IsAuthenticated: hasGitHubCopilotToken,
			AuthType:        strPtr("env"),
		},
		// Quota is not available without calling the GitHub API
		// which would require an active agent session
		Quota: nil,
	}
}

// Helper for optional string pointers
func strPtr(s string) *string {
	return &s
}
