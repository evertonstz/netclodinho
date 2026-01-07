package session

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/angristan/netclode/apps/control-plane/internal/config"
	"github.com/angristan/netclode/apps/control-plane/internal/k8s"
	"github.com/angristan/netclode/apps/control-plane/internal/protocol"
	"github.com/angristan/netclode/apps/control-plane/internal/storage"
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
		m.emit(sessionID, protocol.NewSessionError(sessionID, err.Error()))
		return
	}

	// Wait for ready using informer-based watching
	fqdn, err := m.k8s.WaitForReady(ctx, sessionID, sandboxReadyTimeout)
	if err != nil {
		slog.Error("Sandbox failed to become ready", "sessionID", sessionID, "error", err)
		m.updateSessionStatus(ctx, sessionID, protocol.StatusError)
		m.emit(sessionID, protocol.NewSessionError(sessionID, err.Error()))
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
		m.emit(sessionID, protocol.NewSessionUpdated(session))
	}

	slog.Info("Session created and running", "sessionID", sessionID, "fqdn", fqdn)
}

func (m *Manager) updateSessionStatus(ctx context.Context, sessionID string, status protocol.SessionStatus) {
	m.mu.Lock()
	if state, ok := m.sessions[sessionID]; ok {
		state.Session.Status = status
	}
	m.mu.Unlock()

	_ = m.storage.UpdateSessionStatus(ctx, sessionID, status)

	if session := m.getSession(sessionID); session != nil {
		m.emit(sessionID, protocol.NewSessionUpdated(session))
	}
}

// waitForSandbox waits for an existing sandbox to become ready (used when sandbox already exists).
func (m *Manager) waitForSandbox(ctx context.Context, sessionID string) {
	fqdn, err := m.k8s.WaitForReady(ctx, sessionID, sandboxReadyTimeout)
	if err != nil {
		slog.Error("Sandbox failed to become ready", "sessionID", sessionID, "error", err)
		m.updateSessionStatus(ctx, sessionID, protocol.StatusError)
		m.emit(sessionID, protocol.NewSessionError(sessionID, err.Error()))
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
		m.emit(sessionID, protocol.NewSessionUpdated(session))
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

	// Delete sandbox (but keep PVC)
	if err := m.k8s.DeleteSandbox(ctx, id); err != nil {
		slog.Warn("Failed to delete sandbox", "sessionID", id, "error", err)
	}

	// Delete secret
	if err := m.k8s.DeleteSecret(ctx, id); err != nil {
		slog.Warn("Failed to delete secret", "sessionID", id, "error", err)
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
func (m *Manager) GetWithHistory(ctx context.Context, id string, messageLimit int) (*protocol.Session, []protocol.PersistedMessage, []protocol.PersistedEvent, bool, error) {
	session, err := m.Get(ctx, id)
	if err != nil {
		return nil, nil, nil, false, err
	}

	messages, err := m.storage.GetMessages(ctx, id, nil)
	if err != nil {
		return nil, nil, nil, false, fmt.Errorf("get messages: %w", err)
	}

	events, err := m.storage.GetEvents(ctx, id, m.config.MaxEventsPerSession)
	if err != nil {
		return nil, nil, nil, false, fmt.Errorf("get events: %w", err)
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

	return session, msgSlice, evtSlice, hasMore, nil
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

// Subscribe returns a subscriber for a session.
// The subscriber's Send channel receives messages broadcast to the session.
func (m *Manager) Subscribe(id string) (*Subscriber, error) {
	state := m.getState(id)
	if state == nil {
		return nil, fmt.Errorf("session %s not found", id)
	}
	return state.Subscribe(), nil
}

// Unsubscribe removes a subscriber from a session.
func (m *Manager) Unsubscribe(id string, sub *Subscriber) {
	state := m.getState(id)
	if state != nil {
		state.Unsubscribe(sub)
	}
}

// emit sends a message to all subscribers of a session.
func (m *Manager) emit(id string, msg protocol.ServerMessage) {
	state := m.getState(id)
	if state != nil {
		state.Broadcast(msg)
	}
}
