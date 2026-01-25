package session

import (
	"context"
	"sync"
	"testing"
	"time"

	pb "github.com/angristan/netclode/services/control-plane/gen/netclode/v1"
	"github.com/angristan/netclode/services/control-plane/internal/config"
	"github.com/angristan/netclode/services/control-plane/internal/k8s"
	"github.com/angristan/netclode/services/control-plane/internal/storage"
	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/types/known/timestamppb"
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
	readyCallbacks   map[string][]k8s.SandboxReadyCallback
}

func newMockRuntime() *mockRuntime {
	return &mockRuntime{
		sandboxes:        make(map[string]*k8s.SandboxStatusInfo),
		labeledSandboxes: make(map[string]string),
		readyCallbacks:   make(map[string][]k8s.SandboxReadyCallback),
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
	m.readyCallbacks[sessionID] = append(m.readyCallbacks[sessionID], callback)
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

func (m *mockRuntime) DeleteSandboxClaim(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deletedClaims = append(m.deletedClaims, sessionID)
	return nil
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

func (m *mockRuntime) CreateOrLabelPoolSandbox(ctx context.Context, sessionID string, fromPool bool, env map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createdSandboxes = append(m.createdSandboxes, sessionID)
	m.sandboxes[sessionID] = &k8s.SandboxStatusInfo{Exists: true, Ready: true, ServiceFQDN: "test.local"}
	return nil
}

func (m *mockRuntime) GetWarmPool(ctx context.Context) ([]string, error) {
	return nil, nil
}

func (m *mockRuntime) EvictWarmPool(ctx context.Context) error {
	return nil
}

func (m *mockRuntime) WaitForClaimBound(ctx context.Context, sessionID string, timeout time.Duration) (string, error) {
	return "sandbox-" + sessionID, nil
}

func (m *mockRuntime) GetSandboxByName(ctx context.Context, name string) (*k8s.Sandbox, error) {
	return nil, nil
}

func (m *mockRuntime) GetSessionIDByPodName(ctx context.Context, podName string) (string, error) {
	return "", nil
}

func (m *mockRuntime) LabelSandbox(ctx context.Context, sandboxName string, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.labeledSandboxes[sandboxName] = sessionID
	return nil
}

func (m *mockRuntime) ListSandboxClaims(ctx context.Context) ([]k8s.SandboxClaimInfo, error) {
	return nil, nil
}

func (m *mockRuntime) Close() {
}

// mockStorage implements storage.Storage for testing
type mockStorage struct {
	mu       sync.Mutex
	sessions map[string]*pb.Session
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		sessions: make(map[string]*pb.Session),
	}
}

func (m *mockStorage) SaveSession(ctx context.Context, s *pb.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[s.Id] = s
	return nil
}

func (m *mockStorage) GetSession(ctx context.Context, id string) (*pb.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok {
		return s, nil
	}
	return nil, nil
}

func (m *mockStorage) GetAllSessions(ctx context.Context) ([]*pb.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*pb.Session
	for _, s := range m.sessions {
		result = append(result, s)
	}
	return result, nil
}

func (m *mockStorage) UpdateSessionStatus(ctx context.Context, id string, status pb.SessionStatus) error {
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

func (m *mockStorage) AppendMessage(ctx context.Context, sessionID string, msg *pb.Message) error {
	return nil
}

func (m *mockStorage) GetMessages(ctx context.Context, sessionID string, afterID *string) ([]*pb.Message, error) {
	return nil, nil
}

func (m *mockStorage) GetLastMessage(ctx context.Context, sessionID string) (*pb.Message, error) {
	return nil, nil
}

func (m *mockStorage) GetMessageCount(ctx context.Context, sessionID string) (int, error) {
	return 0, nil
}

func (m *mockStorage) AppendEvent(ctx context.Context, sessionID string, evt *pb.Event) error {
	return nil
}

func (m *mockStorage) GetEvents(ctx context.Context, sessionID string, limit int) ([]*pb.Event, error) {
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

// Snapshot methods
func (m *mockStorage) SaveSnapshot(ctx context.Context, s *pb.Snapshot) error {
	return nil
}

func (m *mockStorage) GetSnapshot(ctx context.Context, sessionID, snapshotID string) (*pb.Snapshot, error) {
	return nil, nil
}

func (m *mockStorage) ListSnapshots(ctx context.Context, sessionID string) ([]*pb.Snapshot, error) {
	return nil, nil
}

func (m *mockStorage) DeleteSnapshot(ctx context.Context, sessionID, snapshotID string) error {
	return nil
}

func (m *mockStorage) DeleteAllSnapshots(ctx context.Context, sessionID string) error {
	return nil
}

func (m *mockStorage) TruncateMessages(ctx context.Context, sessionID string, keepCount int) error {
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

	manager := NewManager(store, runtime, cfg, nil)
	return manager, runtime, store
}

// Helper to add a session directly to manager state
func addSession(m *Manager, id string, status pb.SessionStatus, lastActiveAt time.Time) {
	session := &pb.Session{
		Id:           id,
		Name:         "Session " + id,
		Status:       status,
		CreatedAt:    timestamppb.New(lastActiveAt),
		LastActiveAt: timestamppb.New(lastActiveAt),
	}
	m.sessions[id] = NewSessionState(session)
}

func TestEnsureActiveSlot_NoActionWhenUnderLimit(t *testing.T) {
	manager, runtime, _ := newTestManager(3)

	// Add 2 running sessions (under limit of 3)
	now := time.Now()
	addSession(manager, "sess-1", pb.SessionStatus_SESSION_STATUS_RUNNING, now.Add(-2*time.Hour))
	addSession(manager, "sess-2", pb.SessionStatus_SESSION_STATUS_RUNNING, now.Add(-1*time.Hour))

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
	addSession(manager, "sess-1", pb.SessionStatus_SESSION_STATUS_RUNNING, now.Add(-2*time.Hour)) // oldest
	addSession(manager, "sess-2", pb.SessionStatus_SESSION_STATUS_RUNNING, now.Add(-1*time.Hour))

	// Should pause the oldest one
	pausedID := manager.ensureActiveSlot(context.Background(), "")

	if pausedID != "sess-1" {
		t.Errorf("Expected sess-1 to be paused, got %s", pausedID)
	}

	// Verify session status changed
	if manager.sessions["sess-1"].Session.Status != pb.SessionStatus_SESSION_STATUS_PAUSED {
		t.Errorf("Expected sess-1 status to be paused, got %s", manager.sessions["sess-1"].Session.Status)
	}
}

func TestEnsureActiveSlot_PausesMultipleWhenOverLimit(t *testing.T) {
	manager, _, _ := newTestManager(2)

	// Add 5 running sessions (way over limit of 2)
	now := time.Now()
	addSession(manager, "sess-1", pb.SessionStatus_SESSION_STATUS_RUNNING, now.Add(-5*time.Hour)) // oldest
	addSession(manager, "sess-2", pb.SessionStatus_SESSION_STATUS_RUNNING, now.Add(-4*time.Hour))
	addSession(manager, "sess-3", pb.SessionStatus_SESSION_STATUS_RUNNING, now.Add(-3*time.Hour))
	addSession(manager, "sess-4", pb.SessionStatus_SESSION_STATUS_RUNNING, now.Add(-2*time.Hour))
	addSession(manager, "sess-5", pb.SessionStatus_SESSION_STATUS_RUNNING, now.Add(-1*time.Hour)) // newest

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
	if manager.sessions["sess-1"].Session.Status != pb.SessionStatus_SESSION_STATUS_PAUSED {
		t.Errorf("Expected sess-1 (oldest) to be paused")
	}
}

func TestEnsureActiveSlot_ExcludesSpecifiedSession(t *testing.T) {
	manager, _, _ := newTestManager(2)

	// Add 2 running sessions (at limit)
	now := time.Now()
	addSession(manager, "sess-1", pb.SessionStatus_SESSION_STATUS_RUNNING, now.Add(-2*time.Hour)) // oldest
	addSession(manager, "sess-2", pb.SessionStatus_SESSION_STATUS_RUNNING, now.Add(-1*time.Hour))

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
		addSession(manager, "sess-"+string(rune('A'+i)), pb.SessionStatus_SESSION_STATUS_RUNNING, now.Add(time.Duration(-i)*time.Hour))
	}

	// Should not pause anything
	pausedID := manager.ensureActiveSlot(context.Background(), "")

	if pausedID != "" {
		t.Errorf("Expected no session to be paused with limit=0, got %s", pausedID)
	}

	if len(runtime.deletedSandboxes) > 0 {
		t.Errorf("Expected no sandboxes deleted with limit=0, got %v", runtime.deletedSandboxes)
	}
}

func TestEnforceActiveLimit_OnStartup(t *testing.T) {
	manager, _, _ := newTestManager(2)

	// Simulate startup state with 4 running sessions
	now := time.Now()
	addSession(manager, "sess-1", pb.SessionStatus_SESSION_STATUS_RUNNING, now.Add(-4*time.Hour))
	addSession(manager, "sess-2", pb.SessionStatus_SESSION_STATUS_RUNNING, now.Add(-3*time.Hour))
	addSession(manager, "sess-3", pb.SessionStatus_SESSION_STATUS_RUNNING, now.Add(-2*time.Hour))
	addSession(manager, "sess-4", pb.SessionStatus_SESSION_STATUS_RUNNING, now.Add(-1*time.Hour))

	// Enforce limit on startup
	manager.enforceActiveLimit(context.Background())

	// Count active sessions
	activeCount := 0
	for _, state := range manager.sessions {
		if isActiveStatus(state.Session.Status) {
			activeCount++
		}
	}

	// Should be at or under the limit
	if activeCount > 2 {
		t.Errorf("Expected at most 2 active sessions, got %d", activeCount)
	}
}
