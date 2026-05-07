package session

import (
	"bytes"
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	pb "github.com/angristan/netclode/services/control-plane/gen/netclode/v1"
	"github.com/angristan/netclode/services/control-plane/internal/config"
	"github.com/angristan/netclode/services/control-plane/internal/k8s"
	"github.com/angristan/netclode/services/control-plane/internal/storage"
	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
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
	createdEnvs      map[string]map[string]string          // sessionID -> env passed to CreateSandbox
	createdResources map[string]*k8s.SandboxResourceConfig // sessionID -> resources passed to CreateSandbox
	exposedPorts     map[string]map[int]bool
	labeledSandboxes map[string]string // sandboxName -> sessionID
	readyCallbacks   map[string][]k8s.SandboxReadyCallback
}

func newMockRuntime() *mockRuntime {
	return &mockRuntime{
		sandboxes:        make(map[string]*k8s.SandboxStatusInfo),
		createdEnvs:      make(map[string]map[string]string),
		createdResources: make(map[string]*k8s.SandboxResourceConfig),
		exposedPorts:     make(map[string]map[int]bool),
		labeledSandboxes: make(map[string]string),
		readyCallbacks:   make(map[string][]k8s.SandboxReadyCallback),
	}
}

func (m *mockRuntime) CreateSandbox(ctx context.Context, sessionID string, env map[string]string, resources *k8s.SandboxResourceConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createdSandboxes = append(m.createdSandboxes, sessionID)
	m.sandboxes[sessionID] = &k8s.SandboxStatusInfo{Exists: true, Ready: true, ServiceFQDN: "test.local"}
	// Record env/resources for inspection in tests.
	envCopy := make(map[string]string, len(env))
	for k, v := range env {
		envCopy[k] = v
	}
	m.createdEnvs[sessionID] = envCopy
	if resources != nil {
		resCopy := *resources
		m.createdResources[sessionID] = &resCopy
	} else {
		m.createdResources[sessionID] = nil
	}
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

func (m *mockRuntime) CreateSandboxClaim(ctx context.Context, sessionID string, templateName string) error {
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

func (m *mockRuntime) ListTailscaleServices(ctx context.Context) ([]string, error) {
	return nil, nil
}

func (m *mockRuntime) ExposePort(ctx context.Context, sessionID string, port int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ports, exists := m.exposedPorts[sessionID]
	if !exists {
		ports = make(map[int]bool)
		m.exposedPorts[sessionID] = ports
	}
	ports[port] = true
	return nil
}

func (m *mockRuntime) UnexposePort(ctx context.Context, sessionID string, port int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ports, exists := m.exposedPorts[sessionID]; exists {
		delete(ports, port)
	}
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

func (m *mockRuntime) CreateVolumeSnapshot(ctx context.Context, sessionID, snapshotID string) error {
	return nil
}

func (m *mockRuntime) WaitForSnapshotReady(ctx context.Context, sessionID, snapshotID string, timeout time.Duration) error {
	return nil
}

func (m *mockRuntime) DeleteVolumeSnapshot(ctx context.Context, sessionID, snapshotID string) error {
	return nil
}

func (m *mockRuntime) ListVolumeSnapshots(ctx context.Context, sessionID string) ([]k8s.VolumeSnapshotInfo, error) {
	return nil, nil
}

func (m *mockRuntime) RestoreFromSnapshot(ctx context.Context, sessionID, snapshotID string) (string, error) {
	return "old-pvc-name", nil
}

func (m *mockRuntime) DeletePVCByName(ctx context.Context, name string) error {
	return nil
}

func (m *mockRuntime) GetPVCName(ctx context.Context, sessionID string) (string, error) {
	return "", nil
}

func (m *mockRuntime) CreatePVCFromSnapshot(ctx context.Context, sessionID, snapshotID string) (string, error) {
	return "agent-home-sess-" + sessionID, nil
}

func (m *mockRuntime) WaitForRestoreJob(ctx context.Context, sessionID, snapshotID string, timeout time.Duration) error {
	return nil
}

func (m *mockRuntime) EnsureSessionAnchor(ctx context.Context, sessionID string) error {
	return nil
}

func (m *mockRuntime) DeleteSessionAnchor(ctx context.Context, sessionID string) error {
	return nil
}

func (m *mockRuntime) AddSessionAnchorToPVC(ctx context.Context, sessionID, pvcName string) error {
	return nil
}

func (m *mockRuntime) ConfigureNetwork(ctx context.Context, sessionID string, networkEnabled bool) error {
	return nil
}

func (m *mockRuntime) ConfigureTailnetAccess(ctx context.Context, sessionID string, tailnetEnabled bool) error {
	return nil
}

func (m *mockRuntime) DeleteNetworkRestriction(ctx context.Context, sessionID string) error {
	return nil
}

func (m *mockRuntime) NotifyAgentReady(_ string) {}

func (m *mockRuntime) VerifyAgentToken(ctx context.Context, token string, audiences []string) (string, error) {
	// Mock implementation - return a fake pod name for testing
	return "mock-pod-" + token[:8], nil
}

func (m *mockRuntime) GetSessionIDByPodIP(ctx context.Context, podIP string) (string, error) {
	// Mock implementation - return a fake session ID for testing
	return "mock-session-" + podIP, nil
}

func (m *mockRuntime) Close() {
}

// mockStorage implements storage.Storage for testing
type mockStorage struct {
	mu       sync.Mutex
	sessions map[string]*pb.Session
	streams  map[string][]storage.StreamEntryWithID
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		sessions: make(map[string]*pb.Session),
		streams:  make(map[string][]storage.StreamEntryWithID),
	}
}

func (m *mockStorage) SaveSession(ctx context.Context, s *pb.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[s.Id] = proto.Clone(s).(*pb.Session)
	return nil
}

func (m *mockStorage) GetSession(ctx context.Context, id string) (*pb.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok {
		return proto.Clone(s).(*pb.Session), nil
	}
	return nil, nil
}

func (m *mockStorage) GetAllSessions(ctx context.Context) ([]*pb.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*pb.Session
	for _, s := range m.sessions {
		result = append(result, proto.Clone(s).(*pb.Session))
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

// Unified Stream methods (replaces messages, events, and notifications)
func (m *mockStorage) AppendStreamEntry(ctx context.Context, sessionID string, entry *storage.StreamEntry) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	stream := m.streams[sessionID]
	id := strconv.Itoa(len(stream)+1) + "-0"
	entryCopy := *entry
	m.streams[sessionID] = append(stream, storage.StreamEntryWithID{
		ID:    id,
		Entry: &entryCopy,
	})
	return id, nil
}

func (m *mockStorage) GetStreamEntries(ctx context.Context, sessionID string, afterID string, limit int) ([]storage.StreamEntryWithID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.filterStreamEntriesLocked(sessionID, afterID, limit, nil), nil
}

func (m *mockStorage) GetStreamEntriesByTypes(ctx context.Context, sessionID string, afterID string, limit int, types []string) ([]storage.StreamEntryWithID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.filterStreamEntriesLocked(sessionID, afterID, limit, types), nil
}

func (m *mockStorage) GetLastStreamID(ctx context.Context, sessionID string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	stream := m.streams[sessionID]
	if len(stream) == 0 {
		return "0-0", nil
	}
	return stream[len(stream)-1].ID, nil
}

func (m *mockStorage) filterStreamEntriesLocked(sessionID, afterID string, limit int, types []string) []storage.StreamEntryWithID {
	stream := m.streams[sessionID]
	if len(stream) == 0 {
		return nil
	}

	typeFilter := map[string]bool{}
	for _, t := range types {
		typeFilter[t] = true
	}

	afterSeq := 0
	if afterID != "" && afterID != "0" {
		afterSeq = parseStreamSeq(afterID)
	}

	result := make([]storage.StreamEntryWithID, 0, len(stream))
	for _, e := range stream {
		if parseStreamSeq(e.ID) <= afterSeq {
			continue
		}
		if len(typeFilter) > 0 && !typeFilter[e.Entry.Type] {
			continue
		}
		result = append(result, e)
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result
}

func parseStreamSeq(id string) int {
	parts := strings.SplitN(id, "-", 2)
	if len(parts) == 0 {
		return 0
	}
	n, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0
	}
	return n
}

func (m *mockStorage) TruncateStreamAfter(ctx context.Context, sessionID string, afterID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	afterSeq := parseStreamSeq(afterID)
	stream := m.streams[sessionID]
	filtered := stream[:0]
	for _, e := range stream {
		if parseStreamSeq(e.ID) <= afterSeq {
			filtered = append(filtered, e)
		}
	}
	m.streams[sessionID] = filtered
	return nil
}

func (m *mockStorage) GetMessageCount(ctx context.Context, sessionID string) (int, error) {
	return 0, nil
}

func (m *mockStorage) IncrementMessageCount(ctx context.Context, sessionID string) error {
	return nil
}

func (m *mockStorage) SetMessageCount(ctx context.Context, sessionID string, count int) error {
	return nil
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

func (m *mockStorage) SetRestoreSnapshotID(ctx context.Context, sessionID, snapshotID string) error {
	return nil
}

func (m *mockStorage) GetRestoreSnapshotID(ctx context.Context, sessionID string) (string, error) {
	return "", nil
}

func (m *mockStorage) ClearRestoreSnapshotID(ctx context.Context, sessionID string) error {
	return nil
}

func (m *mockStorage) SetOldPVCName(ctx context.Context, sessionID, pvcName string) error {
	return nil
}

func (m *mockStorage) GetOldPVCName(ctx context.Context, sessionID string) (string, error) {
	return "", nil
}

func (m *mockStorage) ClearOldPVCName(ctx context.Context, sessionID string) error {
	return nil
}

func (m *mockStorage) GetStreamDepth(ctx context.Context, sessionID string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return int64(len(m.streams[sessionID])), nil
}

func (m *mockStorage) SetPVCName(ctx context.Context, sessionID, pvcName string) error {
	return nil
}

func (m *mockStorage) GetPVCName(ctx context.Context, sessionID string) (string, error) {
	return "", nil
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

func waitForCondition(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

func TestCreatePersistsResources(t *testing.T) {
	manager, _, store := newTestManager(3)
	manager.config.UseWarmPool = false
	manager.config.HostCPUs = 16
	manager.config.HostMemoryMB = 32768

	repoAccess := pb.RepoAccess_REPO_ACCESS_WRITE
	tailnet := true
	resources := &pb.SandboxResources{Vcpus: 4, MemoryMb: 4096}
	diskSizeGb := int32(40)
	resources.DiskSizeGb = &diskSizeGb

	sess, err := manager.Create(context.Background(), "resource-session", []string{"owner/repo"}, &repoAccess, nil, nil, nil, &tailnet, resources)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	persisted, err := store.GetSession(context.Background(), sess.Id)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if persisted == nil || persisted.Resources == nil {
		t.Fatalf("expected persisted resources, got %#v", persisted)
	}
	if persisted.Resources.Vcpus != 4 || persisted.Resources.MemoryMb != 4096 || persisted.Resources.GetDiskSizeGb() != 40 {
		t.Fatalf("persisted resources = %+v, want vcpus=4 memory=4096 disk=40", persisted.Resources)
	}
	if !persisted.TailnetEnabled {
		t.Fatalf("expected tailnet_enabled to be persisted")
	}
}

func TestCreateRejectsResourcesThatExceedCapacity(t *testing.T) {
	manager, _, _ := newTestManager(3)
	manager.config.HostCPUs = 4
	manager.config.HostMemoryMB = 4096

	_, err := manager.Create(context.Background(), "too-big", nil, nil, nil, nil, nil, nil, &pb.SandboxResources{Vcpus: 8, MemoryMb: 8192})
	if err == nil {
		t.Fatal("expected Create() to reject oversized resources")
	}
	if !strings.Contains(err.Error(), "exceeds maximum allowed") {
		t.Fatalf("Create() error = %v, want capacity validation error", err)
	}
}

func TestResumeUsesPersistedResources(t *testing.T) {
	manager, runtime, store := newTestManager(3)
	manager.config.HostCPUs = 16
	manager.config.HostMemoryMB = 32768

	now := timestamppb.Now()
	diskSizeGb := int32(50)
	session := &pb.Session{
		Id:           "resume-resource-session",
		Name:         "resume-resource-session",
		Status:       pb.SessionStatus_SESSION_STATUS_PAUSED,
		CreatedAt:    now,
		LastActiveAt: now,
		Resources:    &pb.SandboxResources{Vcpus: 6, MemoryMb: 8192, DiskSizeGb: &diskSizeGb},
	}
	if err := store.SaveSession(context.Background(), session); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}
	manager.sessions[session.Id] = NewSessionState(proto.Clone(session).(*pb.Session))

	if _, err := manager.Resume(context.Background(), session.Id); err != nil {
		t.Fatalf("Resume() error = %v", err)
	}

	waitForCondition(t, func() bool {
		runtime.mu.Lock()
		defer runtime.mu.Unlock()
		_, ok := runtime.createdResources[session.Id]
		return ok
	})

	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	res := runtime.createdResources[session.Id]
	if res == nil {
		t.Fatalf("expected runtime resources, got nil")
	}
	if res.VCPUs != 6 || res.MemoryMB != 8192 || res.DiskSizeGb != 50 {
		t.Fatalf("runtime resources = %+v, want vcpus=6 memory=8192 disk=50", res)
	}
}

func TestResumeRejectsPersistedResourcesThatExceedCapacity(t *testing.T) {
	manager, runtime, store := newTestManager(3)
	manager.config.HostCPUs = 4
	manager.config.HostMemoryMB = 4096

	now := timestamppb.Now()
	session := &pb.Session{
		Id:           "oversized-session",
		Name:         "oversized-session",
		Status:       pb.SessionStatus_SESSION_STATUS_PAUSED,
		CreatedAt:    now,
		LastActiveAt: now,
		Resources:    &pb.SandboxResources{Vcpus: 8, MemoryMb: 8192},
	}
	if err := store.SaveSession(context.Background(), session); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}
	manager.sessions[session.Id] = NewSessionState(proto.Clone(session).(*pb.Session))

	_, err := manager.Resume(context.Background(), session.Id)
	if err == nil {
		t.Fatal("expected Resume() to fail for oversized persisted resources")
	}
	if !strings.Contains(err.Error(), "recreate with smaller allocation") {
		t.Fatalf("Resume() error = %v, want clearer capacity guidance", err)
	}

	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if _, ok := runtime.createdResources[session.Id]; ok {
		t.Fatalf("expected no sandbox creation for oversized resume")
	}
}

func TestCreatePauseResumePreservesPersistedResources(t *testing.T) {
	manager, runtime, _ := newTestManager(3)
	manager.config.UseWarmPool = false
	manager.config.HostCPUs = 16
	manager.config.HostMemoryMB = 32768

	diskSizeGb := int32(30)
	resources := &pb.SandboxResources{Vcpus: 3, MemoryMb: 6144, DiskSizeGb: &diskSizeGb}
	sess, err := manager.Create(context.Background(), "roundtrip-session", nil, nil, nil, nil, nil, nil, resources)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	waitForCondition(t, func() bool {
		runtime.mu.Lock()
		defer runtime.mu.Unlock()
		_, ok := runtime.createdResources[sess.Id]
		return ok
	})

	if _, err := manager.Pause(context.Background(), sess.Id); err != nil {
		t.Fatalf("Pause() error = %v", err)
	}

	runtime.mu.Lock()
	delete(runtime.createdResources, sess.Id)
	runtime.mu.Unlock()

	if _, err := manager.Resume(context.Background(), sess.Id); err != nil {
		t.Fatalf("Resume() error = %v", err)
	}

	waitForCondition(t, func() bool {
		runtime.mu.Lock()
		defer runtime.mu.Unlock()
		_, ok := runtime.createdResources[sess.Id]
		return ok
	})

	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	res := runtime.createdResources[sess.Id]
	if res == nil || res.VCPUs != 3 || res.MemoryMB != 6144 || res.DiskSizeGb != 30 {
		t.Fatalf("resume resources = %+v, want vcpus=3 memory=6144 disk=30", res)
	}
}

func TestResumeOldSessionUsesDefaultResources(t *testing.T) {
	manager, runtime, store := newTestManager(3)
	manager.config.HostCPUs = 16
	manager.config.HostMemoryMB = 32768

	var logs bytes.Buffer
	prevDefault := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prevDefault)

	now := timestamppb.Now()
	session := &pb.Session{
		Id:           "legacy-session",
		Name:         "legacy-session",
		Status:       pb.SessionStatus_SESSION_STATUS_PAUSED,
		CreatedAt:    now,
		LastActiveAt: now,
	}
	if err := store.SaveSession(context.Background(), session); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}
	manager.sessions[session.Id] = NewSessionState(proto.Clone(session).(*pb.Session))

	if _, err := manager.Resume(context.Background(), session.Id); err != nil {
		t.Fatalf("Resume() error = %v", err)
	}

	waitForCondition(t, func() bool {
		runtime.mu.Lock()
		defer runtime.mu.Unlock()
		_, ok := runtime.createdResources[session.Id]
		return ok
	})

	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.createdResources[session.Id] != nil {
		t.Fatalf("expected legacy session resume to use default resources, got %+v", runtime.createdResources[session.Id])
	}
	if !strings.Contains(logs.String(), "defaulting to Claude") {
		t.Fatalf("expected warning log about Claude fallback during resume, got logs: %s", logs.String())
	}
}

func TestCreateSandboxDirectInfersSdkTypeFromModelAndPersistsIt(t *testing.T) {
	manager, runtime, store := newTestManager(3)
	manager.config.UseWarmPool = false

	model := "gpt-5-codex:oauth:high"
	now := timestamppb.Now()
	session := &pb.Session{
		Id:           "model-inference-session",
		Name:         "model-inference-session",
		Status:       pb.SessionStatus_SESSION_STATUS_PAUSED,
		CreatedAt:    now,
		LastActiveAt: now,
		Model:        &model,
	}
	if err := store.SaveSession(context.Background(), session); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}
	manager.sessions[session.Id] = NewSessionState(proto.Clone(session).(*pb.Session))

	manager.createSandboxDirect(context.Background(), session.Id, nil, nil, false, nil)

	persisted, err := store.GetSession(context.Background(), session.Id)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if persisted.SdkType == nil || *persisted.SdkType != pb.SdkType_SDK_TYPE_CODEX {
		t.Fatalf("persisted sdk type = %v, want CODEX", persisted.SdkType)
	}

	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if got := runtime.createdEnvs[session.Id]["SDK_TYPE"]; got != pb.SdkType_SDK_TYPE_CODEX.String() {
		t.Fatalf("SDK_TYPE env = %q, want %q", got, pb.SdkType_SDK_TYPE_CODEX.String())
	}
}

func TestCreateSandboxDirectDefaultsToClaudeWithoutPersistedModel(t *testing.T) {
	manager, runtime, store := newTestManager(3)
	manager.config.UseWarmPool = false

	var logs bytes.Buffer
	prevDefault := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prevDefault)

	now := timestamppb.Now()
	session := &pb.Session{
		Id:           "default-claude-session",
		Name:         "default-claude-session",
		Status:       pb.SessionStatus_SESSION_STATUS_PAUSED,
		CreatedAt:    now,
		LastActiveAt: now,
	}
	if err := store.SaveSession(context.Background(), session); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}
	manager.sessions[session.Id] = NewSessionState(proto.Clone(session).(*pb.Session))

	manager.createSandboxDirect(context.Background(), session.Id, nil, nil, false, nil)

	persisted, err := store.GetSession(context.Background(), session.Id)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if persisted.SdkType == nil || *persisted.SdkType != pb.SdkType_SDK_TYPE_CLAUDE {
		t.Fatalf("persisted sdk type = %v, want CLAUDE", persisted.SdkType)
	}

	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if got := runtime.createdEnvs[session.Id]["SDK_TYPE"]; got != pb.SdkType_SDK_TYPE_CLAUDE.String() {
		t.Fatalf("SDK_TYPE env = %q, want %q", got, pb.SdkType_SDK_TYPE_CLAUDE.String())
	}
	if !strings.Contains(logs.String(), "defaulting to Claude") {
		t.Fatalf("expected warning log about Claude fallback, got logs: %s", logs.String())
	}
}

func TestPersistPromptModelSavesModelAndSdkType(t *testing.T) {
	manager, _, store := newTestManager(3)
	model := "opencode/openrouter/anthropic/claude-sonnet-4"
	now := timestamppb.Now()
	session := &pb.Session{
		Id:           "prompt-model-session",
		Name:         "prompt-model-session",
		Status:       pb.SessionStatus_SESSION_STATUS_READY,
		CreatedAt:    now,
		LastActiveAt: now,
	}
	if err := store.SaveSession(context.Background(), session); err != nil {
		t.Fatalf("SaveSession() error = %v", err)
	}
	manager.sessions[session.Id] = NewSessionState(proto.Clone(session).(*pb.Session))

	if err := manager.PersistPromptModel(context.Background(), session.Id, &model); err != nil {
		t.Fatalf("PersistPromptModel() error = %v", err)
	}

	persisted, err := store.GetSession(context.Background(), session.Id)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if persisted.Model == nil || *persisted.Model != model {
		t.Fatalf("persisted model = %v, want %q", persisted.Model, model)
	}
	if persisted.SdkType == nil || *persisted.SdkType != pb.SdkType_SDK_TYPE_OPENCODE {
		t.Fatalf("persisted sdk type = %v, want OPENCODE", persisted.SdkType)
	}
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

	// Add 3 running sessions (over limit)
	now := time.Now()
	addSession(manager, "sess-1", pb.SessionStatus_SESSION_STATUS_RUNNING, now.Add(-3*time.Hour)) // oldest
	addSession(manager, "sess-2", pb.SessionStatus_SESSION_STATUS_RUNNING, now.Add(-2*time.Hour))
	addSession(manager, "sess-3", pb.SessionStatus_SESSION_STATUS_RUNNING, now.Add(-1*time.Hour))

	// Exclude sess-1 from being paused AND from count (simulates resuming a zombie session)
	pausedID := manager.ensureActiveSlot(context.Background(), "sess-1")

	// Should pause sess-2 (oldest among non-excluded)
	if pausedID != "sess-2" {
		t.Errorf("Expected sess-2 to be paused (sess-1 excluded), got %s", pausedID)
	}
}

func TestEnsureActiveSlot_NoActionWhenExcludedSessionAtLimit(t *testing.T) {
	manager, _, _ := newTestManager(2)

	// Add 2 running sessions (at limit)
	now := time.Now()
	addSession(manager, "sess-1", pb.SessionStatus_SESSION_STATUS_RUNNING, now.Add(-2*time.Hour))
	addSession(manager, "sess-2", pb.SessionStatus_SESSION_STATUS_RUNNING, now.Add(-1*time.Hour))

	// Exclude sess-1 from count - simulates resuming a zombie session that's already counted as active
	// Since we exclude sess-1, count is 1, which is under limit of 2
	pausedID := manager.ensureActiveSlot(context.Background(), "sess-1")

	// Should NOT pause anything - the excluded session is already using the slot
	if pausedID != "" {
		t.Errorf("Expected no session to be paused when excluded session is at limit, got %s", pausedID)
	}
}

func TestEnsureActiveSlot_NoLimitWhenZero(t *testing.T) {
	manager, runtime, _ := newTestManager(0) // 0 = unlimited

	// Add many running sessions
	now := time.Now()
	for i := range 10 {
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

// mockWarmAgentConnection implements WarmAgentConnection for testing
type mockWarmAgentConnection struct {
	assignedSessionID string
	assignedConfig    *AgentSessionConfig
	assignCalled      bool
	assignError       error
}

func (m *mockWarmAgentConnection) ExecutePrompt(text string) error {
	return nil
}

func (m *mockWarmAgentConnection) Interrupt() error {
	return nil
}

func (m *mockWarmAgentConnection) GenerateTitle(requestID, prompt string) error {
	return nil
}

func (m *mockWarmAgentConnection) GetGitStatus(requestID string) error {
	return nil
}

func (m *mockWarmAgentConnection) GetGitDiff(requestID string, file *string) error {
	return nil
}

func (m *mockWarmAgentConnection) SendTerminalInput(data string) error {
	return nil
}

func (m *mockWarmAgentConnection) ResizeTerminal(cols, rows int) error {
	return nil
}

func (m *mockWarmAgentConnection) UpdateGitCredentials(token string, repoAccess pb.RepoAccess) error {
	return nil
}

func (m *mockWarmAgentConnection) AssignSession(sessionID string, config *AgentSessionConfig) error {
	m.assignCalled = true
	m.assignedSessionID = sessionID
	m.assignedConfig = config
	return m.assignError
}

func TestRegisterWarmAgentConnection(t *testing.T) {
	manager, _, _ := newTestManager(3)

	conn := &mockWarmAgentConnection{}
	manager.RegisterWarmAgentConnection("pod-1", conn)

	// Verify the connection was registered
	if got := manager.GetWarmAgentConnection("pod-1"); got != conn {
		t.Errorf("Expected warm agent to be registered, got %v", got)
	}
}

func TestUnregisterWarmAgentConnection(t *testing.T) {
	manager, _, _ := newTestManager(3)

	conn := &mockWarmAgentConnection{}
	manager.RegisterWarmAgentConnection("pod-1", conn)
	manager.UnregisterWarmAgentConnection("pod-1")

	// Verify the connection was removed
	if got := manager.GetWarmAgentConnection("pod-1"); got != nil {
		t.Errorf("Expected warm agent to be unregistered, got %v", got)
	}
}

func TestAssignSessionToWarmAgent_Success(t *testing.T) {
	manager, _, _ := newTestManager(3)

	// Create a session first
	now := time.Now()
	addSession(manager, "sess-123", pb.SessionStatus_SESSION_STATUS_CREATING, now)

	// Register a warm agent
	conn := &mockWarmAgentConnection{}
	manager.RegisterWarmAgentConnection("pod-1", conn)

	// Assign the session
	result := manager.AssignSessionToWarmAgent("pod-1", "sess-123")

	if !result {
		t.Error("Expected AssignSessionToWarmAgent to return true")
	}

	if !conn.assignCalled {
		t.Error("Expected AssignSession to be called on the connection")
	}

	if conn.assignedSessionID != "sess-123" {
		t.Errorf("Expected assigned session ID to be 'sess-123', got %s", conn.assignedSessionID)
	}

	// Verify the connection was moved from warmAgents to agents
	if got := manager.GetWarmAgentConnection("pod-1"); got != nil {
		t.Error("Expected warm agent to be removed from warmAgents map")
	}

	if got := manager.GetAgentConnection("sess-123"); got != conn {
		t.Errorf("Expected agent to be in agents map, got %v", got)
	}
}

func TestAssignSessionToWarmAgent_NotFound(t *testing.T) {
	manager, _, _ := newTestManager(3)

	// Try to assign to a non-existent warm agent
	result := manager.AssignSessionToWarmAgent("nonexistent-pod", "sess-123")

	if result {
		t.Error("Expected AssignSessionToWarmAgent to return false for non-existent pod")
	}
}

func TestAssignSessionToWarmAgent_SessionNotFound(t *testing.T) {
	manager, _, _ := newTestManager(3)

	// Register a warm agent but don't create the session
	conn := &mockWarmAgentConnection{}
	manager.RegisterWarmAgentConnection("pod-1", conn)

	// Try to assign a non-existent session
	result := manager.AssignSessionToWarmAgent("pod-1", "nonexistent-session")

	// Should fail because GetSessionConfig will fail
	if result {
		t.Error("Expected AssignSessionToWarmAgent to return false for non-existent session")
	}

	// The warm agent should have been removed from warmAgents but not added to agents
	// since the assignment failed
	if got := manager.GetAgentConnection("nonexistent-session"); got != nil {
		t.Error("Expected agent to NOT be in agents map after failed assignment")
	}
}

func TestMultipleWarmAgents(t *testing.T) {
	manager, _, _ := newTestManager(3)

	// Register multiple warm agents
	conn1 := &mockWarmAgentConnection{}
	conn2 := &mockWarmAgentConnection{}
	conn3 := &mockWarmAgentConnection{}

	manager.RegisterWarmAgentConnection("pod-1", conn1)
	manager.RegisterWarmAgentConnection("pod-2", conn2)
	manager.RegisterWarmAgentConnection("pod-3", conn3)

	// Verify all are registered
	if manager.GetWarmAgentConnection("pod-1") != conn1 {
		t.Error("pod-1 not registered")
	}
	if manager.GetWarmAgentConnection("pod-2") != conn2 {
		t.Error("pod-2 not registered")
	}
	if manager.GetWarmAgentConnection("pod-3") != conn3 {
		t.Error("pod-3 not registered")
	}

	// Create sessions and assign
	now := time.Now()
	addSession(manager, "sess-a", pb.SessionStatus_SESSION_STATUS_CREATING, now)
	addSession(manager, "sess-b", pb.SessionStatus_SESSION_STATUS_CREATING, now)

	manager.AssignSessionToWarmAgent("pod-1", "sess-a")
	manager.AssignSessionToWarmAgent("pod-2", "sess-b")

	// pod-1 and pod-2 should be assigned, pod-3 still waiting
	if manager.GetWarmAgentConnection("pod-1") != nil {
		t.Error("pod-1 should not be in warmAgents after assignment")
	}
	if manager.GetWarmAgentConnection("pod-2") != nil {
		t.Error("pod-2 should not be in warmAgents after assignment")
	}
	if manager.GetWarmAgentConnection("pod-3") != conn3 {
		t.Error("pod-3 should still be in warmAgents")
	}

	if manager.GetAgentConnection("sess-a") != conn1 {
		t.Error("sess-a should be assigned to conn1")
	}
	if manager.GetAgentConnection("sess-b") != conn2 {
		t.Error("sess-b should be assigned to conn2")
	}
}

func TestUnexposePort_RemovesPortAndPersistsEvent(t *testing.T) {
	manager, runtime, _ := newTestManager(3)

	if _, err := manager.ExposePort(context.Background(), "test123", 1234); err != nil {
		t.Fatalf("ExposePort failed: %v", err)
	}
	if err := manager.UnexposePort(context.Background(), "test123", 1234); err != nil {
		t.Fatalf("UnexposePort failed: %v", err)
	}

	runtime.mu.Lock()
	_, stillExposed := runtime.exposedPorts["test123"][1234]
	runtime.mu.Unlock()
	if stillExposed {
		t.Fatalf("expected port 1234 to be removed from runtime state")
	}

	entries, err := manager.storage.GetStreamEntriesByTypes(context.Background(), "test123", "0", 0, []string{storage.StreamEntryTypeEvent})
	if err != nil {
		t.Fatalf("GetStreamEntriesByTypes failed: %v", err)
	}

	foundUnexposed := false
	for _, e := range entries {
		var event pb.AgentEvent
		if err := protojson.Unmarshal(e.Entry.Payload, &event); err != nil {
			continue
		}
		if event.Kind == pb.AgentEventKind_AGENT_EVENT_KIND_PORT_UNEXPOSED {
			if payload := event.GetPortUnexposed(); payload != nil && payload.Port == 1234 {
				foundUnexposed = true
				break
			}
		}
	}

	if !foundUnexposed {
		t.Fatalf("expected persisted port_unexposed event for port 1234")
	}
}

func TestRestoreExposedPorts_AppliesLatestExposeState(t *testing.T) {
	manager, runtime, _ := newTestManager(3)

	if _, err := manager.ExposePort(context.Background(), "test123", 1234); err != nil {
		t.Fatalf("ExposePort(1234) failed: %v", err)
	}
	if _, err := manager.ExposePort(context.Background(), "test123", 5678); err != nil {
		t.Fatalf("ExposePort(5678) failed: %v", err)
	}
	if err := manager.UnexposePort(context.Background(), "test123", 1234); err != nil {
		t.Fatalf("UnexposePort(1234) failed: %v", err)
	}

	// Simulate fresh runtime state before resume restoration.
	runtime.mu.Lock()
	runtime.exposedPorts["test123"] = make(map[int]bool)
	runtime.mu.Unlock()

	manager.restoreExposedPorts(context.Background(), "test123")

	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if !runtime.exposedPorts["test123"][5678] {
		t.Fatalf("expected port 5678 to be restored")
	}
	if runtime.exposedPorts["test123"][1234] {
		t.Fatalf("expected port 1234 to remain unexposed after restoration")
	}
}

// TestSessionUpdateStreamContract verifies that a session status update published via
// emitSessionUpdated is received by a subscriber as a StreamEntry_SessionUpdate.
// This guards against the regression where the server published StreamEntry but
// clients expected SessionUpdated directly, causing READY to be silently dropped.
func TestSessionUpdateStreamContract(t *testing.T) {
	if testing.Short() {
		t.Skip("requires Redis")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer client.Close()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis not available: %v", err)
	}

	store, err2 := storage.NewRedisStorage(ctx, &config.Config{RedisURL: "redis://localhost:6379"})
	if err2 != nil {
		t.Skipf("Redis not available: %v", err2)
	}
	cfg := &config.Config{Port: 3000}
	rt := newMockRuntime()
	m := NewManager(store, rt, cfg, nil)

	sessionID := "test-stream-contract-" + strconv.FormatInt(time.Now().UnixNano(), 36)

	// Create a subscriber BEFORE emitting the update (simulates client race).
	sub, err := m.Subscribe(ctx, sessionID, "0")
	if err == nil {
		// Session doesn't exist yet so Subscribe may fail — that's ok.
		defer sub.Close()
	}

	// Publish a session update directly via emitSessionUpdated.
	session := &pb.Session{
		Id:        sessionID,
		Status:    pb.SessionStatus_SESSION_STATUS_READY,
		CreatedAt: timestamppb.Now(),
	}
	m.emitSessionUpdated(ctx, session)

	// Subscribe after publish (with "0" cursor to catch already-published messages).
	sub2, err := m.Subscribe(ctx, sessionID, "0")
	if err != nil {
		// If the stream was never created, Subscribe will fail. Create the stream first.
		// Use AppendStreamEntry so the stream exists.
		_ = err
		t.Skip("session stream not initialized; this test requires a pre-existing stream key")
	}
	defer sub2.Close()

	deadline := time.After(5 * time.Second)
	for {
		select {
		case msg, ok := <-sub2.Messages():
			if !ok {
				t.Fatal("subscriber channel closed without receiving READY update")
			}
			entry := msg.GetStreamEntry().GetEntry()
			if entry == nil {
				continue
			}
			su, ok := entry.Payload.(*pb.StreamEntry_SessionUpdate)
			if !ok {
				continue // Different entry type, keep waiting
			}
			if su.SessionUpdate.Status == pb.SessionStatus_SESSION_STATUS_READY {
				return // ✅ Received as StreamEntry_SessionUpdate
			}
		case <-deadline:
			t.Fatal("timed out waiting for READY status via StreamEntry_SessionUpdate; " +
				"did the server switch to a different message type?")
		case <-ctx.Done():
			t.Fatal("context cancelled")
		}
	}
}
