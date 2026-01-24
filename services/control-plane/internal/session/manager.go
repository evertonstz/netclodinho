package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
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
	SessionID       string
	AnthropicAPIKey string
	GitHubToken     string // For Copilot SDK or repo access
	Repo            string
	RepoAccess      *pb.RepoAccess
	SdkType         *pb.SdkType
	Model           string
	CopilotBackend  *pb.CopilotBackend
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
}

// Manager handles session lifecycle and agent communication.
type Manager struct {
	storage storage.Storage
	k8s     k8s.Runtime
	config  *config.Config
	github  *github.Client // nil if GitHub App not configured

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
func NewManager(store storage.Storage, k8sRuntime k8s.Runtime, cfg *config.Config, githubClient *github.Client) *Manager {
	return &Manager{
		storage:  store,
		k8s:      k8sRuntime,
		config:   cfg,
		github:   githubClient,
		sessions: make(map[string]*SessionState),
		agents:   make(map[string]AgentConnection),
	}
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
		m.sessions[s.Id] = NewSessionState(s)
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
func (m *Manager) Create(ctx context.Context, name string, repo *string, repoAccess *pb.RepoAccess, sdkType *pb.SdkType, model *string, copilotBackend *pb.CopilotBackend) (*pb.Session, error) {
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

	// Start sandbox creation in background
	go m.createSandbox(context.Background(), id, repo, repoAccess)

	return session, nil
}

func (m *Manager) createSandbox(ctx context.Context, sessionID string, repo *string, repoAccess *pb.RepoAccess) {
	if m.config.UseWarmPool {
		m.createSandboxViaClaim(ctx, sessionID, repo, repoAccess)
	} else {
		m.createSandboxDirect(ctx, sessionID, repo, repoAccess)
	}
}

// createSandboxDirect creates a sandbox directly (legacy mode)
func (m *Manager) createSandboxDirect(ctx context.Context, sessionID string, repo *string, repoAccess *pb.RepoAccess) {
	env := map[string]string{
		"SESSION_ID":        sessionID,
		"ANTHROPIC_API_KEY": m.config.AnthropicAPIKey,
	}

	// Pass GitHub token for Copilot SDK if configured
	if m.config.GitHubToken != "" {
		env["GITHUB_TOKEN"] = m.config.GitHubToken
	}

	// Setup GitHub repo access if configured
	if repo != nil && *repo != "" {
		env["GIT_REPO"] = github.NormalizeRepoURL(*repo)

		// Generate scoped GitHub token if GitHub App is configured
		// This overrides the static token with a repo-scoped token
		if m.github != nil {
			access := github.RepoAccessRead
			if repoAccess != nil && *repoAccess == pb.RepoAccess_REPO_ACCESS_WRITE {
				access = github.RepoAccessWrite
			}

			token, err := m.github.CreateInstallationToken(ctx, *repo, access)
			if err != nil {
				slog.Warn("Failed to create GitHub token, proceeding without auth", "sessionID", sessionID, "error", err)
			} else {
				env["GITHUB_TOKEN"] = token.Token
			}
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

	// Create Tailscale-exposed service for preview URLs
	if err := m.k8s.CreateSandboxService(ctx, sessionID); err != nil {
		slog.Warn("Failed to create sandbox service", "sessionID", sessionID, "error", err)
		// Non-fatal: sandbox still works, just no preview URLs
	}

	// Update state and check for pending prompt
	var pendingPrompt string
	m.mu.Lock()
	if state, ok := m.sessions[sessionID]; ok {
		state.ServiceFQDN = fqdn
		state.Session.Status = pb.SessionStatus_SESSION_STATUS_RUNNING
		pendingPrompt = state.PendingPrompt
		state.PendingPrompt = "" // Clear it
	}
	m.mu.Unlock()

	if err := m.storage.UpdateSessionStatus(ctx, sessionID, pb.SessionStatus_SESSION_STATUS_RUNNING); err != nil {
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

	slog.Info("Session created and running", "sessionID", sessionID, "fqdn", fqdn)
}

// createSandboxViaClaim uses SandboxClaim for warm pool allocation
func (m *Manager) createSandboxViaClaim(ctx context.Context, sessionID string, repo *string, repoAccess *pb.RepoAccess) {
	// Create SandboxClaim to request from warm pool
	if err := m.k8s.CreateSandboxClaim(ctx, sessionID); err != nil {
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

	// Create Tailscale-exposed service for preview URLs
	if err := m.k8s.CreateSandboxService(ctx, sessionID); err != nil {
		slog.Warn("Failed to create sandbox service", "sessionID", sessionID, "error", err)
		// Non-fatal: sandbox still works, just no preview URLs
	}

	// Update state and check for pending prompt
	var pendingPrompt string
	m.mu.Lock()
	if state, ok := m.sessions[sessionID]; ok {
		state.ServiceFQDN = fqdn
		state.Session.Status = pb.SessionStatus_SESSION_STATUS_RUNNING
		pendingPrompt = state.PendingPrompt
		state.PendingPrompt = "" // Clear it
	}
	m.mu.Unlock()

	if err := m.storage.UpdateSessionStatus(ctx, sessionID, pb.SessionStatus_SESSION_STATUS_RUNNING); err != nil {
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

	slog.Info("Session created via warm pool", "sessionID", sessionID, "fqdn", fqdn)
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
	m.mu.Unlock()

	if !ok {
		return nil, fmt.Errorf("session %s not found", id)
	}

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
		wasAlreadyReady := state.Session.Status == pb.SessionStatus_SESSION_STATUS_READY
		state.Session.Status = pb.SessionStatus_SESSION_STATUS_READY
		m.mu.Unlock()

		// Only update storage if status actually changed
		if !wasAlreadyReady {
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
	m.mu.Unlock()

	_ = m.storage.UpdateSessionStatus(ctx, id, pb.SessionStatus_SESSION_STATUS_RESUMING)

	// Start sandbox creation in background
	go m.createSandbox(context.Background(), id, state.Session.Repo, state.Session.RepoAccess)

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

	// Delete sandbox (but keep PVC)
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

	// Delete K8s resources
	_ = m.k8s.DeleteSandboxClaim(ctx, id)
	_ = m.k8s.DeleteSandboxService(ctx, id)
	_ = m.k8s.DeleteSandbox(ctx, id)
	_ = m.k8s.DeleteSecret(ctx, id)
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
func (m *Manager) GetWithHistory(ctx context.Context, id string, messageLimit int) (*pb.Session, []pb.Message, []pb.Event, bool, string, error) {
	session, err := m.Get(ctx, id)
	if err != nil {
		return nil, nil, nil, false, "", err
	}

	messages, err := m.storage.GetMessages(ctx, id, nil)
	if err != nil {
		return nil, nil, nil, false, "", fmt.Errorf("get messages: %w", err)
	}

	events, err := m.storage.GetEvents(ctx, id, m.config.MaxEventsPerSession)
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

	hasMore := len(messages) >= messageLimit

	return session, msgSlice, evtSlice, hasMore, lastNotificationID, nil
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

// ListGitHubRepos returns the repositories accessible to the GitHub App installation.
// Returns nil, nil if GitHub App is not configured.
func (m *Manager) ListGitHubRepos(ctx context.Context) ([]github.Repository, error) {
	if m.github == nil {
		slog.Warn("GitHub client is nil, cannot list repos")
		return nil, nil
	}
	slog.Info("Calling GitHub API to list repos")
	return m.github.ListInstallationRepositories(ctx)
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
		SessionID:       sessionID,
		AnthropicAPIKey: m.config.AnthropicAPIKey,
		GitHubToken:     m.config.GitHubToken,
		SdkType:         state.Session.SdkType,
		CopilotBackend:  state.Session.CopilotBackend,
	}

	if state.Session.Model != nil {
		config.Model = *state.Session.Model
	}

	// Setup GitHub repo access if configured
	if state.Session.Repo != nil && *state.Session.Repo != "" {
		config.Repo = github.NormalizeRepoURL(*state.Session.Repo)
		config.RepoAccess = state.Session.RepoAccess

		// Generate scoped GitHub token if GitHub App is configured
		// This overrides the static token with a repo-scoped token
		if m.github != nil {
			access := github.RepoAccessRead
			if state.Session.RepoAccess != nil && *state.Session.RepoAccess == pb.RepoAccess_REPO_ACCESS_WRITE {
				access = github.RepoAccessWrite
			}

			token, err := m.github.CreateInstallationToken(ctx, *state.Session.Repo, access)
			if err != nil {
				slog.Warn("Failed to create GitHub token for session config", "sessionID", sessionID, "error", err)
			} else {
				config.GitHubToken = token.Token
			}
		}
	}

	return config, nil
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
	if state, ok := m.sessions[sessionID]; ok {
		if state.PendingPrompt != "" {
			pendingPrompt = state.PendingPrompt
			state.PendingPrompt = "" // Clear it
		}
		wasInterrupted = (state.Session.Status == pb.SessionStatus_SESSION_STATUS_INTERRUPTED)
	}
	m.mu.Unlock()

	slog.Info("Agent connection registered", "sessionID", sessionID, "wasInterrupted", wasInterrupted)

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
func (m *Manager) ListModels(sdkType pb.SdkType, copilotBackend *pb.CopilotBackend) []*pb.ModelInfo {
	switch sdkType {
	case pb.SdkType_SDK_TYPE_CLAUDE:
		return m.fetchModelsFromModelsDev("anthropic")
	case pb.SdkType_SDK_TYPE_OPENCODE:
		return m.fetchModelsFromModelsDev("opencode")
	case pb.SdkType_SDK_TYPE_COPILOT:
		// Return combined list of GitHub Copilot + Anthropic (BYOK) models
		return m.fetchCopilotModels()
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

		// Calculate billing multiplier from cost (rough approximation)
		var billingMultiplier *float64
		if model.Cost.Input > 0 {
			// Normalize to ~1.0 for standard models ($3/M input)
			mult := model.Cost.Input / 3.0
			billingMultiplier = &mult
		}

		models = append(models, &pb.ModelInfo{
			Id:                model.ID,
			Name:              model.Name,
			Provider:          strPtr(providerData.Name),
			Capabilities:      capabilities,
			BillingMultiplier: billingMultiplier,
		})
	}

	slog.Info("Fetched models from models.dev", "provider", provider, "count", len(models))
	return models
}

// fetchCopilotModels fetches combined GitHub Copilot + Anthropic models
func (m *Manager) fetchCopilotModels() []*pb.ModelInfo {
	// Check cache first (valid for 5 minutes)
	m.copilotModelsMu.RLock()
	if m.copilotModelsCache != nil && time.Since(m.copilotModelsCacheAt) < 5*time.Minute {
		models := m.copilotModelsCache
		m.copilotModelsMu.RUnlock()
		return models
	}
	m.copilotModelsMu.RUnlock()

	// Fetch from models.dev - get both github-copilot and anthropic providers
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://models.dev/api.json")
	if err != nil {
		slog.Error("Failed to fetch from models.dev", "error", err)
		return getCopilotModelsFallback()
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("models.dev API error", "status", resp.StatusCode)
		return getCopilotModelsFallback()
	}

	var data modelsDevResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		slog.Error("Failed to decode models.dev response", "error", err)
		return getCopilotModelsFallback()
	}

	var models []*pb.ModelInfo

	// Add GitHub Copilot models
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

			// Determine provider from model family
			provider := "GitHub Copilot"
			if contains(model.Family, "claude") {
				provider = "Anthropic"
			} else if contains(model.Family, "gpt") || contains(model.Family, "o1") || contains(model.Family, "o3") || contains(model.Family, "o4") {
				provider = "OpenAI"
			} else if contains(model.Family, "gemini") {
				provider = "Google"
			} else if contains(model.Family, "grok") {
				provider = "xAI"
			}

			models = append(models, &pb.ModelInfo{
				Id:           model.ID,
				Name:         model.Name,
				Provider:     strPtr(provider),
				Capabilities: capabilities,
			})
		}
	}

	// Add Anthropic models for BYOK
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

			// Calculate billing multiplier from cost
			var billingMultiplier *float64
			if model.Cost.Input > 0 {
				mult := model.Cost.Input / 3.0
				billingMultiplier = &mult
			}

			models = append(models, &pb.ModelInfo{
				Id:                model.ID,
				Name:              model.Name + " (BYOK)",
				Provider:          strPtr("Anthropic (BYOK)"),
				Capabilities:      capabilities,
				BillingMultiplier: billingMultiplier,
			})
		}
	}

	slog.Info("Fetched Copilot models from models.dev", "count", len(models))

	// Update cache
	m.copilotModelsMu.Lock()
	m.copilotModelsCache = models
	m.copilotModelsCacheAt = time.Now()
	m.copilotModelsMu.Unlock()

	return models
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsLower(s, substr))
}

func containsLower(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// getModelsFallback returns fallback models for a provider when models.dev is unavailable
func getModelsFallback(provider string) []*pb.ModelInfo {
	switch provider {
	case "anthropic":
		return []*pb.ModelInfo{
			{Id: "claude-sonnet-4-20250514", Name: "Claude Sonnet 4", Provider: strPtr("Anthropic"), Capabilities: []string{"chat", "vision", "code"}},
			{Id: "claude-3-5-sonnet-20241022", Name: "Claude 3.5 Sonnet", Provider: strPtr("Anthropic"), Capabilities: []string{"chat", "vision", "code"}},
		}
	case "opencode":
		return []*pb.ModelInfo{
			{Id: "anthropic/claude-sonnet-4-20250514", Name: "Claude Sonnet 4", Provider: strPtr("Anthropic"), Capabilities: []string{"chat", "vision", "code"}},
		}
	default:
		return nil
	}
}

// getCopilotModelsFallback returns fallback models when models.dev is unavailable
func getCopilotModelsFallback() []*pb.ModelInfo {
	return []*pb.ModelInfo{
		// GitHub Copilot models
		{Id: "gpt-4o", Name: "GPT-4o", Provider: strPtr("OpenAI"), Capabilities: []string{"chat", "vision", "code"}},
		{Id: "claude-sonnet-4", Name: "Claude Sonnet 4", Provider: strPtr("Anthropic"), Capabilities: []string{"chat", "vision", "code"}},
		{Id: "gemini-2.5-pro", Name: "Gemini 2.5 Pro", Provider: strPtr("Google"), Capabilities: []string{"chat", "vision", "code"}},
		// Anthropic BYOK models
		{Id: "claude-sonnet-4-20250514", Name: "Claude Sonnet 4 (BYOK)", Provider: strPtr("Anthropic (BYOK)"), Capabilities: []string{"chat", "vision", "code"}},
		{Id: "claude-3-5-sonnet-20241022", Name: "Claude 3.5 Sonnet (BYOK)", Provider: strPtr("Anthropic (BYOK)"), Capabilities: []string{"chat", "vision", "code"}},
	}
}

// GetCopilotStatus returns GitHub Copilot authentication status and quota.
// Note: This is currently a placeholder - actual implementation would require
// calling the GitHub Copilot API which needs the agent to be running.
func (m *Manager) GetCopilotStatus(ctx context.Context) *pb.CopilotStatusResponse {
	// Check if we have a GitHub token configured
	hasGitHubToken := m.config.GitHubToken != ""

	return &pb.CopilotStatusResponse{
		Auth: &pb.CopilotAuthStatus{
			IsAuthenticated: hasGitHubToken,
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
