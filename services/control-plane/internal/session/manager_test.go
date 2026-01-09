package session

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/angristan/netclode/services/control-plane/internal/config"
	"github.com/angristan/netclode/services/control-plane/internal/k8s"
	"github.com/angristan/netclode/services/control-plane/internal/protocol"
	"github.com/angristan/netclode/services/control-plane/internal/storage"
	"github.com/redis/go-redis/v9"
)

// mockRuntime implements k8s.Runtime for testing
type mockRuntime struct {
	mu               sync.Mutex
	sandboxes        map[string]*k8s.SandboxStatusInfo
	deletedSandboxes []string
	deletedClaims    []string
	deletedServices  []string
	deletedSecrets   []string
	createdSandboxes []string
	createdClaims    []string
	createdServices  []string
	labeledSandboxes map[string]string // sandboxName -> sessionID
	readyCallbacks   map[string]k8s.SandboxReadyCallback
}

func newMockRuntime() *mockRuntime {
	return &mockRuntime{
		sandboxes:        make(map[string]*k8s.SandboxStatusInfo),
		labeledSandboxes: make(map[string]string),
		readyCallbacks:   make(map[string]k8s.SandboxReadyCallback),
	}
}

func (m *mockRuntime) CreateSandbox(ctx context.Context, sessionID string, env map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createdSandboxes = append(m.createdSandboxes, sessionID)
	m.sandboxes[sessionID] = &k8s.SandboxStatusInfo{Exists: true, Ready: true, ServiceFQDN: "test.local"}
	return nil
}

func (m *mockRuntime) WaitForReady(ctx context.Context, sessionID string, timeout time.Duration) (string, error) {
	return "test.local", nil
}

func (m *mockRuntime) WatchSandboxReady(sessionID string, callback k8s.SandboxReadyCallback) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.readyCallbacks[sessionID] = callback
}

func (m *mockRuntime) GetStatus(ctx context.Context, sessionID string) (*k8s.SandboxStatusInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if status, ok := m.sandboxes[sessionID]; ok {
		return status, nil
	}
	return &k8s.SandboxStatusInfo{Exists: false}, nil
}

func (m *mockRuntime) DeleteSandbox(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deletedSandboxes = append(m.deletedSandboxes, sessionID)
	delete(m.sandboxes, sessionID)
	return nil
}

func (m *mockRuntime) DeletePVC(ctx context.Context, sessionID string) error {
	return nil
}

func (m *mockRuntime) DeleteSecret(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deletedSecrets = append(m.deletedSecrets, sessionID)
	return nil
}

func (m *mockRuntime) ListSandboxes(ctx context.Context) ([]k8s.SandboxInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []k8s.SandboxInfo
	for id, status := range m.sandboxes {
		result = append(result, k8s.SandboxInfo{
			SessionID:   id,
			Ready:       status.Ready,
			ServiceFQDN: status.ServiceFQDN,
		})
	}
	return result, nil
}

func (m *mockRuntime) CreateSandboxClaim(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createdClaims = append(m.createdClaims, sessionID)
	return nil
}

func (m *mockRuntime) WaitForClaimBound(ctx context.Context, sessionID string, timeout time.Duration) (string, error) {
	return "sandbox-" + sessionID, nil
}

func (m *mockRuntime) GetSandboxByName(ctx context.Context, name string) (*k8s.Sandbox, error) {
	return &k8s.Sandbox{
		Status: k8s.SandboxStatus{
			Conditions:  []k8s.SandboxCondition{{Type: "Ready", Status: "True"}},
			ServiceFQDN: "test.local",
		},
	}, nil
}

func (m *mockRuntime) LabelSandbox(ctx context.Context, sandboxName string, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.labeledSandboxes[sandboxName] = sessionID
	return nil
}

func (m *mockRuntime) DeleteSandboxClaim(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deletedClaims = append(m.deletedClaims, sessionID)
	return nil
}

func (m *mockRuntime) ListSandboxClaims(ctx context.Context) ([]k8s.SandboxClaimInfo, error) {
	return nil, nil
}

func (m *mockRuntime) CreateSandboxService(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createdServices = append(m.createdServices, sessionID)
	return nil
}

func (m *mockRuntime) DeleteSandboxService(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deletedServices = append(m.deletedServices, sessionID)
	return nil
}

func (m *mockRuntime) ExposePort(ctx context.Context, sessionID string, port int) error {
	return nil
}

func (m *mockRuntime) Close() {}

// mockStorage implements storage.Storage for testing
type mockStorage struct {
	mu       sync.Mutex
	sessions map[string]*protocol.Session
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		sessions: make(map[string]*protocol.Session),
	}
}

func (m *mockStorage) SaveSession(ctx context.Context, s *protocol.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[s.ID] = s
	return nil
}

func (m *mockStorage) GetSession(ctx context.Context, id string) (*protocol.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id], nil
}

func (m *mockStorage) GetAllSessions(ctx context.Context) ([]*protocol.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*protocol.Session
	for _, s := range m.sessions {
		result = append(result, s)
	}
	return result, nil
}

func (m *mockStorage) UpdateSessionStatus(ctx context.Context, id string, status protocol.SessionStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok {
		s.Status = status
	}
	return nil
}

func (m *mockStorage) UpdateSessionField(ctx context.Context, id, field, value string) error {
	return nil
}

func (m *mockStorage) DeleteSession(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
	return nil
}

func (m *mockStorage) AppendMessage(ctx context.Context, msg *protocol.PersistedMessage) error {
	return nil
}

func (m *mockStorage) GetMessages(ctx context.Context, sessionID string, afterID *string) ([]*protocol.PersistedMessage, error) {
	return nil, nil
}

func (m *mockStorage) GetLastMessage(ctx context.Context, sessionID string) (*protocol.PersistedMessage, error) {
	return nil, nil
}

func (m *mockStorage) GetMessageCount(ctx context.Context, sessionID string) (int, error) {
	return 0, nil
}

func (m *mockStorage) AppendEvent(ctx context.Context, evt *protocol.PersistedEvent) error {
	return nil
}

func (m *mockStorage) GetEvents(ctx context.Context, sessionID string, limit int) ([]*protocol.PersistedEvent, error) {
	return nil, nil
}

func (m *mockStorage) PublishNotification(ctx context.Context, sessionID string, notification *storage.Notification) (string, error) {
	return "0-0", nil
}

func (m *mockStorage) GetNotificationsAfter(ctx context.Context, sessionID string, afterID string, limit int) ([]storage.NotificationWithID, error) {
	return nil, nil
}

func (m *mockStorage) GetLastNotificationID(ctx context.Context, sessionID string) (string, error) {
	return "0", nil
}

func (m *mockStorage) GetRedisClient() *redis.Client {
	return nil
}

func (m *mockStorage) Close() error {
	return nil
}

// Helper to create a test manager with mock dependencies
func newTestManager(maxActiveSessions int) (*Manager, *mockRuntime, *mockStorage) {
	runtime := newMockRuntime()
	store := newMockStorage()
	cfg := &config.Config{
		MaxActiveSessions: maxActiveSessions,
		UseWarmPool:       true,
	}

	manager := NewManager(store, runtime, cfg)
	return manager, runtime, store
}

// Helper to add a session directly to manager state
func addSession(m *Manager, id string, status protocol.SessionStatus, lastActiveAt time.Time) {
	session := &protocol.Session{
		ID:           id,
		Name:         "Session " + id,
		Status:       status,
		CreatedAt:    lastActiveAt.Format(time.RFC3339),
		LastActiveAt: lastActiveAt.Format(time.RFC3339),
	}
	m.sessions[id] = NewSessionState(session)
}

func TestEnsureActiveSlot_NoActionWhenUnderLimit(t *testing.T) {
	manager, runtime, _ := newTestManager(3)

	// Add 2 running sessions (under limit of 3)
	now := time.Now()
	addSession(manager, "sess-1", protocol.StatusRunning, now.Add(-2*time.Hour))
	addSession(manager, "sess-2", protocol.StatusRunning, now.Add(-1*time.Hour))

	// Should not pause anything
	pausedID := manager.ensureActiveSlot(context.Background(), "")

	if pausedID != "" {
		t.Errorf("Expected no session to be paused, got %s", pausedID)
	}

	if len(runtime.deletedSandboxes) > 0 {
		t.Errorf("Expected no sandboxes deleted, got %v", runtime.deletedSandboxes)
	}
}

func TestEnsureActiveSlot_PausesOneWhenAtLimit(t *testing.T) {
	manager, _, _ := newTestManager(2)

	// Add 2 running sessions (at limit)
	now := time.Now()
	addSession(manager, "sess-1", protocol.StatusRunning, now.Add(-2*time.Hour)) // oldest
	addSession(manager, "sess-2", protocol.StatusRunning, now.Add(-1*time.Hour))

	// Should pause the oldest one
	pausedID := manager.ensureActiveSlot(context.Background(), "")

	if pausedID != "sess-1" {
		t.Errorf("Expected sess-1 to be paused, got %s", pausedID)
	}

	// Verify session status changed
	if manager.sessions["sess-1"].Session.Status != protocol.StatusPaused {
		t.Errorf("Expected sess-1 status to be paused, got %s", manager.sessions["sess-1"].Session.Status)
	}
}

func TestEnsureActiveSlot_PausesMultipleWhenOverLimit(t *testing.T) {
	manager, _, _ := newTestManager(2)

	// Add 5 running sessions (way over limit of 2)
	now := time.Now()
	addSession(manager, "sess-1", protocol.StatusRunning, now.Add(-5*time.Hour)) // oldest
	addSession(manager, "sess-2", protocol.StatusRunning, now.Add(-4*time.Hour))
	addSession(manager, "sess-3", protocol.StatusRunning, now.Add(-3*time.Hour))
	addSession(manager, "sess-4", protocol.StatusRunning, now.Add(-2*time.Hour))
	addSession(manager, "sess-5", protocol.StatusRunning, now.Add(-1*time.Hour)) // newest

	// Should pause enough to get under limit (pause 4, leaving 1 active + 1 for new)
	manager.ensureActiveSlot(context.Background(), "")

	// Count active sessions
	activeCount := 0
	for _, state := range manager.sessions {
		if isActiveStatus(state.Session.Status) {
			activeCount++
		}
	}

	// Should have less than MaxActiveSessions (2) active
	if activeCount >= 2 {
		t.Errorf("Expected less than 2 active sessions, got %d", activeCount)
	}

	// Verify oldest sessions were paused first
	if manager.sessions["sess-1"].Session.Status != protocol.StatusPaused {
		t.Errorf("Expected sess-1 (oldest) to be paused")
	}
}

func TestEnsureActiveSlot_ExcludesSpecifiedSession(t *testing.T) {
	manager, _, _ := newTestManager(2)

	// Add 2 running sessions (at limit)
	now := time.Now()
	addSession(manager, "sess-1", protocol.StatusRunning, now.Add(-2*time.Hour)) // oldest
	addSession(manager, "sess-2", protocol.StatusRunning, now.Add(-1*time.Hour))

	// Exclude sess-1 from being paused
	pausedID := manager.ensureActiveSlot(context.Background(), "sess-1")

	// Should pause sess-2 instead (even though sess-1 is older)
	if pausedID != "sess-2" {
		t.Errorf("Expected sess-2 to be paused (sess-1 excluded), got %s", pausedID)
	}
}

func TestEnsureActiveSlot_NoLimitWhenZero(t *testing.T) {
	manager, runtime, _ := newTestManager(0) // 0 = unlimited

	// Add many running sessions
	now := time.Now()
	for i := 0; i < 10; i++ {
		addSession(manager, "sess-"+string(rune('A'+i)), protocol.StatusRunning, now.Add(time.Duration(-i)*time.Hour))
	}

	// Should not pause anything
	pausedID := manager.ensureActiveSlot(context.Background(), "")

	if pausedID != "" {
		t.Errorf("Expected no session to be paused with limit=0, got %s", pausedID)
	}

	if len(runtime.deletedSandboxes) > 0 {
		t.Errorf("Expected no sandboxes deleted with limit=0")
	}
}

func TestEnforceActiveLimit_PausesExcessOnStartup(t *testing.T) {
	manager, _, _ := newTestManager(2)

	// Simulate startup state with 5 running sessions
	now := time.Now()
	addSession(manager, "sess-1", protocol.StatusRunning, now.Add(-5*time.Hour))
	addSession(manager, "sess-2", protocol.StatusRunning, now.Add(-4*time.Hour))
	addSession(manager, "sess-3", protocol.StatusRunning, now.Add(-3*time.Hour))
	addSession(manager, "sess-4", protocol.StatusRunning, now.Add(-2*time.Hour))
	addSession(manager, "sess-5", protocol.StatusRunning, now.Add(-1*time.Hour))

	// Call enforceActiveLimit directly
	manager.enforceActiveLimit(context.Background())

	// Count active sessions
	activeCount := 0
	pausedCount := 0
	for _, state := range manager.sessions {
		if isActiveStatus(state.Session.Status) {
			activeCount++
		}
		if state.Session.Status == protocol.StatusPaused {
			pausedCount++
		}
	}

	// Should have at most MaxActiveSessions active
	if activeCount > 2 {
		t.Errorf("Expected at most 2 active sessions, got %d", activeCount)
	}

	// Should have paused 3 sessions (5 - 2 = 3)
	if pausedCount != 3 {
		t.Errorf("Expected 3 paused sessions, got %d", pausedCount)
	}
}

func TestEnforceActiveLimit_NoActionWhenUnderLimit(t *testing.T) {
	manager, runtime, _ := newTestManager(5)

	// Add 2 running sessions (under limit of 5)
	now := time.Now()
	addSession(manager, "sess-1", protocol.StatusRunning, now.Add(-2*time.Hour))
	addSession(manager, "sess-2", protocol.StatusRunning, now.Add(-1*time.Hour))

	manager.enforceActiveLimit(context.Background())

	// No sandboxes should be deleted
	if len(runtime.deletedSandboxes) > 0 {
		t.Errorf("Expected no sandboxes deleted, got %v", runtime.deletedSandboxes)
	}

	// Both sessions should still be running
	for id, state := range manager.sessions {
		if state.Session.Status != protocol.StatusRunning {
			t.Errorf("Expected session %s to remain running, got %s", id, state.Session.Status)
		}
	}
}

func TestPause_UpdatesStatusAndCleansUpResources(t *testing.T) {
	manager, runtime, store := newTestManager(2)

	// Add a running session
	now := time.Now()
	addSession(manager, "sess-1", protocol.StatusRunning, now)
	store.sessions["sess-1"] = manager.sessions["sess-1"].Session

	// Pause the session
	session, err := manager.Pause(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("Pause failed: %v", err)
	}

	// Verify status changed
	if session.Status != protocol.StatusPaused {
		t.Errorf("Expected status to be paused, got %s", session.Status)
	}

	// Verify K8s resources cleaned up (warm pool mode)
	if len(runtime.deletedClaims) == 0 || runtime.deletedClaims[0] != "sess-1" {
		t.Errorf("Expected sandbox claim to be deleted")
	}

	if len(runtime.deletedServices) == 0 || runtime.deletedServices[0] != "sess-1" {
		t.Errorf("Expected sandbox service to be deleted")
	}

	if len(runtime.deletedSandboxes) == 0 || runtime.deletedSandboxes[0] != "sess-1" {
		t.Errorf("Expected sandbox to be deleted")
	}
}

func TestPause_NotifiesCallback(t *testing.T) {
	manager, _, store := newTestManager(2)

	// Add sessions
	now := time.Now()
	addSession(manager, "sess-1", protocol.StatusRunning, now.Add(-2*time.Hour))
	addSession(manager, "sess-2", protocol.StatusRunning, now.Add(-1*time.Hour))
	store.sessions["sess-1"] = manager.sessions["sess-1"].Session
	store.sessions["sess-2"] = manager.sessions["sess-2"].Session

	// Set up callback to track all calls
	var mu sync.Mutex
	var callbackSessions []*protocol.Session
	manager.SetOnSessionUpdated(func(session *protocol.Session) {
		mu.Lock()
		callbackSessions = append(callbackSessions, session)
		mu.Unlock()
	})

	// Trigger auto-pause by adding a third session when limit is 2
	addSession(manager, "sess-3", protocol.StatusRunning, now)
	store.sessions["sess-3"] = manager.sessions["sess-3"].Session

	// This should auto-pause sessions to get under limit
	manager.ensureActiveSlot(context.Background(), "sess-3")

	mu.Lock()
	defer mu.Unlock()

	if len(callbackSessions) == 0 {
		t.Error("Expected callback to be called for auto-pause")
	}

	// Verify all notified sessions are paused
	for _, s := range callbackSessions {
		if s.Status != protocol.StatusPaused {
			t.Errorf("Expected callback session %s to have paused status, got %s", s.ID, s.Status)
		}
	}

	// The oldest session (sess-1) should be among those paused
	foundSess1 := false
	for _, s := range callbackSessions {
		if s.ID == "sess-1" {
			foundSess1 = true
			break
		}
	}
	if !foundSess1 {
		t.Error("Expected sess-1 (oldest) to be paused and notified")
	}
}

func TestIsActiveStatus(t *testing.T) {
	tests := []struct {
		status   protocol.SessionStatus
		expected bool
	}{
		{protocol.StatusCreating, true},
		{protocol.StatusReady, true},
		{protocol.StatusRunning, true},
		{protocol.StatusPaused, false},
		{protocol.StatusError, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			result := isActiveStatus(tt.status)
			if result != tt.expected {
				t.Errorf("isActiveStatus(%s) = %v, want %v", tt.status, result, tt.expected)
			}
		})
	}
}
