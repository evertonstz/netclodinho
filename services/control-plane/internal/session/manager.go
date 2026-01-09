package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/angristan/netclode/services/control-plane/internal/config"
	"github.com/angristan/netclode/services/control-plane/internal/k8s"
	"github.com/angristan/netclode/services/control-plane/internal/protocol"
	"github.com/angristan/netclode/services/control-plane/internal/storage"
	"github.com/google/uuid"
)

const (
	sandboxReadyTimeout = 120 * time.Second
)

// Manager handles session lifecycle and agent communication.
type Manager struct {
	storage storage.Storage
	k8s     k8s.Runtime
	config  *config.Config

	sessions map[string]*SessionState
	mu       sync.RWMutex
}

// NewManager creates a new session manager.
func NewManager(store storage.Storage, k8sRuntime k8s.Runtime, cfg *config.Config) *Manager {
	return &Manager{
		storage:  store,
		k8s:      k8sRuntime,
		config:   cfg,
		sessions: make(map[string]*SessionState),
	}
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
		m.sessions[s.ID] = NewSessionState(s)
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
			if state.Session.Status == protocol.StatusRunning || state.Session.Status == protocol.StatusCreating {
				slog.Info("Session has no sandbox, marking as paused", "sessionID", id)
				state.Session.Status = protocol.StatusPaused
				_ = m.storage.UpdateSessionStatus(ctx, id, protocol.StatusPaused)
			}
		} else if sb.Ready {
			state.ServiceFQDN = sb.ServiceFQDN
			if state.Session.Status == protocol.StatusCreating {
				state.Session.Status = protocol.StatusReady
				_ = m.storage.UpdateSessionStatus(ctx, id, protocol.StatusReady)
			}
		}
	}

	slog.Info("Session manager initialized", "sessions", len(m.sessions), "sandboxes", len(sandboxes))
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
func (m *Manager) Create(ctx context.Context, name string, repo *string) (*protocol.Session, error) {
	// Ensure we have a slot for a new active session
	m.ensureActiveSlot(ctx, "")

	id := generateID()
	now := time.Now().UTC().Format(time.RFC3339)

	if name == "" {
		name = "Session " + id[:6]
	}

	session := &protocol.Session{
		ID:           id,
		Name:         name,
		Status:       protocol.StatusCreating,
		Repo:         repo,
		CreatedAt:    now,
		LastActiveAt: now,
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
	go m.createSandbox(context.Background(), id, repo)

	return session, nil
}

func (m *Manager) createSandbox(ctx context.Context, sessionID string, repo *string) {
	if m.config.UseWarmPool {
		m.createSandboxViaClaim(ctx, sessionID, repo)
	} else {
		m.createSandboxDirect(ctx, sessionID, repo)
	}
}

// createSandboxDirect creates a sandbox directly (legacy mode)
func (m *Manager) createSandboxDirect(ctx context.Context, sessionID string, repo *string) {
	env := map[string]string{
		"SESSION_ID":        sessionID,
		"ANTHROPIC_API_KEY": m.config.AnthropicAPIKey,
	}
	if repo != nil && *repo != "" {
		env["GIT_REPO"] = *repo
	}

	// Create sandbox
	if err := m.k8s.CreateSandbox(ctx, sessionID, env); err != nil {
		slog.Error("Failed to create sandbox", "sessionID", sessionID, "error", err)
		m.updateSessionStatus(ctx, sessionID, protocol.StatusError)
		m.emit(ctx, sessionID, protocol.NewSessionError(sessionID, err.Error()))
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
		m.updateSessionStatus(ctx, sessionID, protocol.StatusError)
		m.emit(ctx, sessionID, protocol.NewSessionError(sessionID, err.Error()))
		return
	}

	// Create Tailscale-exposed service for preview URLs
	if err := m.k8s.CreateSandboxService(ctx, sessionID); err != nil {
		slog.Warn("Failed to create sandbox service", "sessionID", sessionID, "error", err)
		// Non-fatal: sandbox still works, just no preview URLs
	}

	// Update state
	m.mu.Lock()
	if state, ok := m.sessions[sessionID]; ok {
		state.ServiceFQDN = fqdn
		state.Session.Status = protocol.StatusRunning
	}
	m.mu.Unlock()

	if err := m.storage.UpdateSessionStatus(ctx, sessionID, protocol.StatusRunning); err != nil {
		slog.Error("Failed to update session status", "sessionID", sessionID, "error", err)
	}

	// Emit update
	if session := m.getSession(sessionID); session != nil {
		m.emit(ctx, sessionID, protocol.NewSessionUpdated(session))
	}

	slog.Info("Session created and running", "sessionID", sessionID, "fqdn", fqdn)
}

// createSandboxViaClaim uses SandboxClaim for warm pool allocation
func (m *Manager) createSandboxViaClaim(ctx context.Context, sessionID string, repo *string) {
	// Create SandboxClaim to request from warm pool
	if err := m.k8s.CreateSandboxClaim(ctx, sessionID); err != nil {
		slog.Error("Failed to create sandbox claim", "sessionID", sessionID, "error", err)
		m.updateSessionStatus(ctx, sessionID, protocol.StatusError)
		m.emit(ctx, sessionID, protocol.NewSessionError(sessionID, err.Error()))
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
		m.updateSessionStatus(ctx, sessionID, protocol.StatusError)
		m.emit(ctx, sessionID, protocol.NewSessionError(sessionID, err.Error()))
		return
	}

	slog.Info("Claim bound to sandbox", "sessionID", sessionID, "sandbox", sandboxName)

	// Label the sandbox so the informer can track it
	if err := m.k8s.LabelSandbox(ctx, sandboxName, sessionID); err != nil {
		slog.Warn("Failed to label sandbox", "sessionID", sessionID, "sandbox", sandboxName, "error", err)
		// Continue anyway - labeling is for informer optimization
	}

	// Get sandbox to retrieve serviceFQDN
	sandbox, err := m.k8s.GetSandboxByName(ctx, sandboxName)
	if err != nil {
		slog.Error("Failed to get bound sandbox", "sessionID", sessionID, "sandbox", sandboxName, "error", err)
		// Cleanup: delete the sandbox (claim was already bound)
		if delErr := m.k8s.DeleteSandbox(ctx, sessionID); delErr != nil {
			slog.Error("Failed to cleanup sandbox", "sessionID", sessionID, "error", delErr)
		}
		m.updateSessionStatus(ctx, sessionID, protocol.StatusError)
		m.emit(ctx, sessionID, protocol.NewSessionError(sessionID, err.Error()))
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
			// Cleanup: delete the sandbox to avoid resource leak
			if delErr := m.k8s.DeleteSandbox(ctx, sessionID); delErr != nil {
				slog.Error("Failed to cleanup sandbox after timeout", "sessionID", sessionID, "error", delErr)
			} else {
				slog.Info("Cleaned up sandbox after timeout", "sessionID", sessionID)
			}
			m.updateSessionStatus(ctx, sessionID, protocol.StatusError)
			m.emit(ctx, sessionID, protocol.NewSessionError(sessionID, err.Error()))
			return
		}
	}

	// Create Tailscale-exposed service for preview URLs
	if err := m.k8s.CreateSandboxService(ctx, sessionID); err != nil {
		slog.Warn("Failed to create sandbox service", "sessionID", sessionID, "error", err)
		// Non-fatal: sandbox still works, just no preview URLs
	}

	// Update state
	m.mu.Lock()
	if state, ok := m.sessions[sessionID]; ok {
		state.ServiceFQDN = fqdn
		state.Session.Status = protocol.StatusRunning
	}
	m.mu.Unlock()

	if err := m.storage.UpdateSessionStatus(ctx, sessionID, protocol.StatusRunning); err != nil {
		slog.Error("Failed to update session status", "sessionID", sessionID, "error", err)
	}

	// Emit update
	if session := m.getSession(sessionID); session != nil {
		m.emit(ctx, sessionID, protocol.NewSessionUpdated(session))
	}

	slog.Info("Session created via warm pool", "sessionID", sessionID, "fqdn", fqdn)
}

func (m *Manager) updateSessionStatus(ctx context.Context, sessionID string, status protocol.SessionStatus) {
	m.mu.Lock()
	if state, ok := m.sessions[sessionID]; ok {
		state.Session.Status = status
	}
	m.mu.Unlock()

	_ = m.storage.UpdateSessionStatus(ctx, sessionID, status)

	if session := m.getSession(sessionID); session != nil {
		m.emit(ctx, sessionID, protocol.NewSessionUpdated(session))
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
		m.emit(ctx, sessionID, protocol.NewSessionUpdated(session))
	}
}

// waitForSandbox waits for an existing sandbox to become ready (used when sandbox already exists).
func (m *Manager) waitForSandbox(ctx context.Context, sessionID string) {
	fqdn, err := m.k8s.WaitForReady(ctx, sessionID, sandboxReadyTimeout)
	if err != nil {
		slog.Error("Sandbox failed to become ready", "sessionID", sessionID, "error", err)
		m.updateSessionStatus(ctx, sessionID, protocol.StatusError)
		m.emit(ctx, sessionID, protocol.NewSessionError(sessionID, err.Error()))
		return
	}

	// Update state
	m.mu.Lock()
	if state, ok := m.sessions[sessionID]; ok {
		state.ServiceFQDN = fqdn
		state.Session.Status = protocol.StatusRunning
	}
	m.mu.Unlock()

	if err := m.storage.UpdateSessionStatus(ctx, sessionID, protocol.StatusRunning); err != nil {
		slog.Error("Failed to update session status", "sessionID", sessionID, "error", err)
	}

	// Emit update
	if session := m.getSession(sessionID); session != nil {
		m.emit(ctx, sessionID, protocol.NewSessionUpdated(session))
	}

	slog.Info("Sandbox ready", "sessionID", sessionID, "fqdn", fqdn)
}

// Resume resumes a paused session.
func (m *Manager) Resume(ctx context.Context, id string) (*protocol.Session, error) {
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
		// Sandbox already running
		m.mu.Lock()
		state.ServiceFQDN = status.ServiceFQDN
		state.Session.Status = protocol.StatusRunning
		m.mu.Unlock()

		_ = m.storage.UpdateSessionStatus(ctx, id, protocol.StatusRunning)
		return state.Session, nil
	}

	if status.Exists {
		// Sandbox exists but not ready yet - just wait for it
		m.mu.Lock()
		state.Session.Status = protocol.StatusCreating
		m.mu.Unlock()

		_ = m.storage.UpdateSessionStatus(ctx, id, protocol.StatusCreating)

		// Wait for existing sandbox to become ready
		go m.waitForSandbox(context.Background(), id)

		return state.Session, nil
	}

	// No sandbox exists - create new one
	m.mu.Lock()
	state.Session.Status = protocol.StatusCreating
	m.mu.Unlock()

	_ = m.storage.UpdateSessionStatus(ctx, id, protocol.StatusCreating)

	// Start sandbox creation in background
	go m.createSandbox(context.Background(), id, state.Session.Repo)

	return state.Session, nil
}

// Pause pauses a running session.
func (m *Manager) Pause(ctx context.Context, id string) (*protocol.Session, error) {
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
	state.Session.Status = protocol.StatusPaused
	m.mu.Unlock()

	_ = m.storage.UpdateSessionStatus(ctx, id, protocol.StatusPaused)

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

// ExposePort exposes a port for a session via Tailscale and persists the event.
func (m *Manager) ExposePort(ctx context.Context, sessionID string, port int) (string, error) {
	if err := m.k8s.ExposePort(ctx, sessionID, port); err != nil {
		return "", err
	}

	previewURL := fmt.Sprintf("http://sandbox-%s:%d", sessionID, port)

	// Create and persist the port_exposed event
	event := protocol.AgentEvent{
		Kind:       protocol.EventKindPortExposed,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Port:       port,
		PreviewURL: &previewURL,
	}

	persistedEvent := &protocol.PersistedEvent{
		ID:        "evt_" + uuid.NewString()[:12],
		SessionID: sessionID,
		Event:     event,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	if err := m.storage.AppendEvent(ctx, persistedEvent); err != nil {
		slog.Warn("Failed to persist port exposed event", "sessionID", sessionID, "error", err)
	}

	// Emit to all connected clients
	m.emit(ctx, sessionID, protocol.NewAgentEvent(sessionID, &event))

	return previewURL, nil
}

// List returns all sessions, reconciling with K8s state.
func (m *Manager) List(ctx context.Context) ([]protocol.Session, error) {
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

	sessions := make([]protocol.Session, 0, len(m.sessions))
	for id, state := range m.sessions {
		sb, exists := sandboxMap[id]
		if !exists && (state.Session.Status == protocol.StatusRunning || state.Session.Status == protocol.StatusCreating) {
			state.Session.Status = protocol.StatusPaused
			_ = m.storage.UpdateSessionStatus(ctx, id, protocol.StatusPaused)
		} else if exists && sb.Ready {
			state.ServiceFQDN = sb.ServiceFQDN
		}
		sessions = append(sessions, *state.Session)
	}

	return sessions, nil
}

// Get returns a session by ID.
func (m *Manager) Get(ctx context.Context, id string) (*protocol.Session, error) {
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
func (m *Manager) GetWithHistory(ctx context.Context, id string, messageLimit int) (*protocol.Session, []protocol.PersistedMessage, []protocol.PersistedEvent, bool, string, error) {
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
	msgSlice := make([]protocol.PersistedMessage, len(messages))
	for i, msg := range messages {
		msgSlice[i] = *msg
	}

	evtSlice := make([]protocol.PersistedEvent, len(events))
	for i, e := range events {
		evtSlice[i] = *e
	}

	hasMore := len(messages) >= messageLimit

	return session, msgSlice, evtSlice, hasMore, lastNotificationID, nil
}

// GetAllWithMeta returns all sessions with metadata.
func (m *Manager) GetAllWithMeta(ctx context.Context) ([]protocol.SessionWithMeta, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]protocol.SessionWithMeta, 0, len(m.sessions))
	for id, state := range m.sessions {
		meta := protocol.SessionWithMeta{Session: *state.Session}

		count, err := m.storage.GetMessageCount(ctx, id)
		if err == nil {
			meta.MessageCount = &count
		}

		lastMsg, err := m.storage.GetLastMessage(ctx, id)
		if err == nil && lastMsg != nil {
			meta.LastMessageID = &lastMsg.ID
		}

		result = append(result, meta)
	}

	return result, nil
}

func (m *Manager) getSession(id string) *protocol.Session {
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
func isActiveStatus(status protocol.SessionStatus) bool {
	return status == protocol.StatusCreating ||
		status == protocol.StatusReady ||
		status == protocol.StatusRunning
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

		lastActive, err := time.Parse(time.RFC3339, state.Session.LastActiveAt)
		if err != nil {
			continue
		}

		if oldest == nil || lastActive.Before(oldestTime) {
			oldest = state
			oldestTime = lastActive
		}
	}

	return oldest
}

// ensureActiveSlot ensures there's room for a new active session.
// If at the limit, it pauses the oldest active session (excluding excludeID).
// Returns the paused session ID (if any) or empty string.
func (m *Manager) ensureActiveSlot(ctx context.Context, excludeID string) string {
	m.mu.Lock()
	activeCount := m.countActiveSessionsLocked()
	maxActive := m.config.MaxActiveSessions

	if maxActive <= 0 || activeCount < maxActive {
		m.mu.Unlock()
		return ""
	}

	// Find oldest active session to pause
	oldest := m.findOldestActiveSessionLocked(excludeID)
	if oldest == nil {
		m.mu.Unlock()
		return ""
	}

	oldestID := oldest.Session.ID
	m.mu.Unlock()

	// Pause the oldest session
	slog.Info("Auto-pausing session to make room", "sessionID", oldestID, "activeCount", activeCount, "maxActive", maxActive)
	if _, err := m.Pause(ctx, oldestID); err != nil {
		slog.Error("Failed to auto-pause session", "sessionID", oldestID, "error", err)
		return ""
	}

	return oldestID
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

// GetSessionConfig returns session configuration for the agent.
// This is used by agents to get session-specific config when using warm pools.
func (m *Manager) GetSessionConfig(ctx context.Context, sessionID string) (map[string]string, error) {
	m.mu.RLock()
	state, ok := m.sessions[sessionID]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}

	config := map[string]string{
		"SESSION_ID":        sessionID,
		"ANTHROPIC_API_KEY": m.config.AnthropicAPIKey,
	}

	if state.Session.Repo != nil && *state.Session.Repo != "" {
		config["GIT_REPO"] = *state.Session.Repo
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

// emit is a helper to convert a ServerMessage to a Notification and publish it.
// This maintains the existing API while using Redis Streams underneath.
func (m *Manager) emit(ctx context.Context, sessionID string, msg protocol.ServerMessage) {
	notification := serverMessageToNotification(sessionID, msg)
	if notification != nil {
		m.publishNotification(ctx, sessionID, notification)
	}
}

// serverMessageToNotification converts a protocol.ServerMessage to a storage.Notification.
func serverMessageToNotification(sessionID string, msg protocol.ServerMessage) *storage.Notification {
	timestamp := time.Now().UTC().Format(time.RFC3339)

	switch msg.Type {
	case protocol.MsgTypeAgentEvent:
		if msg.Event == nil {
			return nil
		}
		payload, _ := json.Marshal(msg.Event)
		return &storage.Notification{
			Type:      "event",
			Payload:   payload,
			Timestamp: timestamp,
		}

	case protocol.MsgTypeAgentMessage:
		payload, _ := json.Marshal(map[string]interface{}{
			"id":        msg.MessageID,
			"sessionId": sessionID,
			"role":      "assistant",
			"content":   msg.Content,
			"partial":   msg.Partial,
		})
		return &storage.Notification{
			Type:      "message",
			Payload:   payload,
			Timestamp: timestamp,
		}

	case protocol.MsgTypeSessionUpdated:
		if msg.Session == nil {
			return nil
		}
		payload, _ := json.Marshal(msg.Session)
		return &storage.Notification{
			Type:      "session_update",
			Payload:   payload,
			Timestamp: timestamp,
		}

	case protocol.MsgTypeUserMessage:
		payload, _ := json.Marshal(map[string]string{
			"text":      msg.Content,
			"sessionId": sessionID,
		})
		return &storage.Notification{
			Type:      "user_message",
			Payload:   payload,
			Timestamp: timestamp,
		}

	case protocol.MsgTypeAgentDone:
		return &storage.Notification{
			Type:      "agent_done",
			Payload:   json.RawMessage(`{}`),
			Timestamp: timestamp,
		}

	case protocol.MsgTypeAgentError:
		payload, _ := json.Marshal(map[string]string{
			"error": msg.Error,
		})
		return &storage.Notification{
			Type:      "agent_error",
			Payload:   payload,
			Timestamp: timestamp,
		}

	case protocol.MsgTypeSessionError:
		payload, _ := json.Marshal(map[string]string{
			"error":     msg.Error,
			"sessionId": msg.ID,
		})
		return &storage.Notification{
			Type:      "session_error",
			Payload:   payload,
			Timestamp: timestamp,
		}

	default:
		return nil
	}
}
