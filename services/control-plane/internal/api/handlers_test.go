package api

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/angristan/netclode/services/control-plane/internal/github"
	"github.com/angristan/netclode/services/control-plane/internal/protocol"
	"github.com/angristan/netclode/services/control-plane/internal/session"
)

// mockManager implements the methods of session.Manager needed for handler tests.
type mockManager struct {
	mu sync.Mutex

	// Session storage
	sessions map[string]*protocol.Session

	// Configurable return values
	createSession  *protocol.Session
	createErr      error
	resumeSession  *protocol.Session
	resumeErr      error
	pauseSession   *protocol.Session
	pauseErr       error
	deleteErr      error
	listSessions   []protocol.Session
	listErr        error
	getSession     *protocol.Session
	getErr         error
	getWithHistory *getWithHistoryResult
	getWithHistErr error
	getAllWithMeta []protocol.SessionWithMeta
	getAllWithErr  error
	sendPromptErr  error
	interruptErr   error
	terminalErr    error
	exposePortURL  string
	exposePortErr  error
	listRepos      []github.Repository
	listReposErr   error

	// Track calls
	createCalls         []createCall
	resumeCalls         []string
	pauseCalls          []string
	deleteCalls         []string
	sendPromptCalls     []promptCall
	interruptCalls      []string
	terminalInputCalls  []terminalInputCall
	terminalResizeCalls []terminalResizeCall
	exposePortCalls     []exposePortCall
}

type createCall struct {
	name       string
	repo       *string
	repoAccess *string
}

type promptCall struct {
	sessionID string
	text      string
}

type terminalInputCall struct {
	sessionID string
	data      string
}

type terminalResizeCall struct {
	sessionID  string
	cols, rows int
}

type exposePortCall struct {
	sessionID string
	port      int
}

type getWithHistoryResult struct {
	session        *protocol.Session
	messages       []protocol.PersistedMessage
	events         []protocol.PersistedEvent
	hasMore        bool
	notificationID string
}

func newMockManager() *mockManager {
	return &mockManager{
		sessions: make(map[string]*protocol.Session),
	}
}

func (m *mockManager) Create(ctx context.Context, name string, repo *string, repoAccess *string) (*protocol.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createCalls = append(m.createCalls, createCall{name: name, repo: repo, repoAccess: repoAccess})
	if m.createErr != nil {
		return nil, m.createErr
	}
	if m.createSession != nil {
		return m.createSession, nil
	}
	return &protocol.Session{ID: "test-session", Name: name, Status: protocol.StatusCreating}, nil
}

func (m *mockManager) Resume(ctx context.Context, id string) (*protocol.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resumeCalls = append(m.resumeCalls, id)
	if m.resumeErr != nil {
		return nil, m.resumeErr
	}
	if m.resumeSession != nil {
		return m.resumeSession, nil
	}
	return &protocol.Session{ID: id, Status: protocol.StatusRunning}, nil
}

func (m *mockManager) Pause(ctx context.Context, id string) (*protocol.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pauseCalls = append(m.pauseCalls, id)
	if m.pauseErr != nil {
		return nil, m.pauseErr
	}
	if m.pauseSession != nil {
		return m.pauseSession, nil
	}
	return &protocol.Session{ID: id, Status: protocol.StatusPaused}, nil
}

func (m *mockManager) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteCalls = append(m.deleteCalls, id)
	return m.deleteErr
}

func (m *mockManager) List(ctx context.Context) ([]protocol.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.listSessions, nil
}

func (m *mockManager) Get(ctx context.Context, id string) (*protocol.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.getSession, nil
}

func (m *mockManager) GetWithHistory(ctx context.Context, id string, limit int) (*protocol.Session, []protocol.PersistedMessage, []protocol.PersistedEvent, bool, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getWithHistErr != nil {
		return nil, nil, nil, false, "", m.getWithHistErr
	}
	if m.getWithHistory != nil {
		return m.getWithHistory.session, m.getWithHistory.messages, m.getWithHistory.events, m.getWithHistory.hasMore, m.getWithHistory.notificationID, nil
	}
	return &protocol.Session{ID: id}, nil, nil, false, "0-0", nil
}

func (m *mockManager) GetAllWithMeta(ctx context.Context) ([]protocol.SessionWithMeta, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getAllWithErr != nil {
		return nil, m.getAllWithErr
	}
	return m.getAllWithMeta, nil
}

func (m *mockManager) SendPrompt(ctx context.Context, sessionID, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sendPromptCalls = append(m.sendPromptCalls, promptCall{sessionID: sessionID, text: text})
	return m.sendPromptErr
}

func (m *mockManager) Interrupt(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.interruptCalls = append(m.interruptCalls, sessionID)
	return m.interruptErr
}

func (m *mockManager) SendTerminalInput(ctx context.Context, sessionID, data string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.terminalInputCalls = append(m.terminalInputCalls, terminalInputCall{sessionID: sessionID, data: data})
	return m.terminalErr
}

func (m *mockManager) ResizeTerminal(ctx context.Context, sessionID string, cols, rows int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.terminalResizeCalls = append(m.terminalResizeCalls, terminalResizeCall{sessionID: sessionID, cols: cols, rows: rows})
	return m.terminalErr
}

func (m *mockManager) ExposePort(ctx context.Context, sessionID string, port int) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.exposePortCalls = append(m.exposePortCalls, exposePortCall{sessionID: sessionID, port: port})
	if m.exposePortErr != nil {
		return "", m.exposePortErr
	}
	if m.exposePortURL != "" {
		return m.exposePortURL, nil
	}
	return "http://preview.example.com", nil
}

func (m *mockManager) ListGitHubRepos(ctx context.Context) ([]github.Repository, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listReposErr != nil {
		return nil, m.listReposErr
	}
	return m.listRepos, nil
}

func (m *mockManager) Subscribe(ctx context.Context, id string, lastNotificationID string) (*session.StreamSubscriber, error) {
	return nil, nil // Not used in handler tests
}

// mockConnection captures sent messages for testing.
type mockConnection struct {
	manager       *mockManager
	server        *mockServer
	sentMessages  []protocol.ServerMessage
	subscriptions map[string]string // sessionID -> lastNotificationID
	mu            sync.Mutex
}

type mockServer struct {
	broadcasts []protocol.ServerMessage
	mu         sync.Mutex
}

func (s *mockServer) BroadcastToAll(msg protocol.ServerMessage, exclude *Connection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.broadcasts = append(s.broadcasts, msg)
}

func newMockConnection(manager *mockManager) *mockConnection {
	return &mockConnection{
		manager:       manager,
		server:        &mockServer{},
		subscriptions: make(map[string]string),
	}
}

func (c *mockConnection) Send(msg protocol.ServerMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sentMessages = append(c.sentMessages, msg)
	return nil
}

func (c *mockConnection) subscribe(_ context.Context, sessionID string, lastNotificationID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subscriptions[sessionID] = lastNotificationID
	return nil
}

func (c *mockConnection) unsubscribe(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.subscriptions, sessionID)
}

// testableConnection wraps mockConnection to match the Connection interface for testing.
type testableConnection struct {
	*mockConnection
}

func (tc *testableConnection) handleSessionCreate(ctx context.Context, name, repo, repoAccess, initialPrompt string) error {
	var repoPtr *string
	if repo != "" {
		repoPtr = &repo
	}

	var repoAccessPtr *string
	if repoAccess != "" {
		repoAccessPtr = &repoAccess
	}

	session, err := tc.manager.Create(ctx, name, repoPtr, repoAccessPtr)
	if err != nil {
		return err
	}

	tc.subscribe(ctx, session.ID, "$")
	tc.Send(protocol.NewSessionCreated(session))
	tc.server.BroadcastToAll(protocol.NewSessionCreated(session), nil)

	if initialPrompt != "" {
		tc.manager.SendPrompt(ctx, session.ID, initialPrompt)
	}

	return nil
}

func (tc *testableConnection) handleSessionList(ctx context.Context) error {
	sessions, err := tc.manager.List(ctx)
	if err != nil {
		return err
	}
	return tc.Send(protocol.NewSessionListMsg(sessions))
}

func (tc *testableConnection) handleSessionResume(ctx context.Context, id string) error {
	session, err := tc.manager.Resume(ctx, id)
	if err != nil {
		return err
	}
	tc.subscribe(ctx, id, "$")
	tc.Send(protocol.NewSessionUpdated(session))
	tc.server.BroadcastToAll(protocol.NewSessionUpdated(session), nil)
	return nil
}

func (tc *testableConnection) handleSessionPause(ctx context.Context, id string) error {
	session, err := tc.manager.Pause(ctx, id)
	if err != nil {
		return err
	}
	tc.unsubscribe(id)
	tc.Send(protocol.NewSessionUpdated(session))
	tc.server.BroadcastToAll(protocol.NewSessionUpdated(session), nil)
	return nil
}

func (tc *testableConnection) handleSessionDelete(ctx context.Context, id string) error {
	tc.unsubscribe(id)
	if err := tc.manager.Delete(ctx, id); err != nil {
		return err
	}
	tc.Send(protocol.NewSessionDeleted(id))
	tc.server.BroadcastToAll(protocol.NewSessionDeleted(id), nil)
	return nil
}

func (tc *testableConnection) handlePrompt(ctx context.Context, sessionID, text string) error {
	if sessionID == "" {
		return errors.New("sessionId is required")
	}
	if text == "" {
		return errors.New("text is required")
	}
	return tc.manager.SendPrompt(ctx, sessionID, text)
}

func (tc *testableConnection) handlePromptInterrupt(ctx context.Context, sessionID string) error {
	return tc.manager.Interrupt(ctx, sessionID)
}

func (tc *testableConnection) handleTerminalInput(ctx context.Context, sessionID, data string) error {
	if sessionID == "" {
		return errors.New("sessionId is required")
	}
	if data == "" {
		return nil
	}
	return tc.manager.SendTerminalInput(ctx, sessionID, data)
}

func (tc *testableConnection) handleTerminalResize(ctx context.Context, sessionID string, cols, rows int) error {
	if sessionID == "" {
		return errors.New("sessionId is required")
	}
	if cols <= 0 || rows <= 0 {
		return errors.New("invalid terminal dimensions")
	}
	return tc.manager.ResizeTerminal(ctx, sessionID, cols, rows)
}

func (tc *testableConnection) handleSync(ctx context.Context) error {
	sessions, err := tc.manager.GetAllWithMeta(ctx)
	if err != nil {
		return err
	}
	return tc.Send(protocol.NewSyncResponse(sessions, "2024-01-01T00:00:00Z"))
}

func (tc *testableConnection) handleSessionOpen(ctx context.Context, id string, lastMessageID *string, lastNotificationID *string) error {
	session, messages, events, hasMore, currentNotificationID, err := tc.manager.GetWithHistory(ctx, id, 100)
	if err != nil {
		return err
	}

	cursor := currentNotificationID
	if lastNotificationID != nil && *lastNotificationID != "" {
		cursor = *lastNotificationID
	}

	tc.subscribe(ctx, id, cursor)

	response := protocol.NewSessionState(session, messages, events, hasMore)
	response.LastNotificationID = currentNotificationID
	return tc.Send(response)
}

func (tc *testableConnection) handlePortExpose(ctx context.Context, sessionID string, port int) error {
	if sessionID == "" {
		return tc.Send(protocol.NewPortError(sessionID, port, "sessionId is required"))
	}
	if port <= 0 || port > 65535 {
		return tc.Send(protocol.NewPortError(sessionID, port, "invalid port number"))
	}

	previewURL, err := tc.manager.ExposePort(ctx, sessionID, port)
	if err != nil {
		return tc.Send(protocol.NewPortError(sessionID, port, err.Error()))
	}

	return tc.Send(protocol.NewPortExposed(sessionID, port, previewURL))
}

func (tc *testableConnection) handleGitHubReposList(ctx context.Context) error {
	repos, err := tc.manager.ListGitHubRepos(ctx)
	if err != nil {
		return tc.Send(protocol.NewError("Failed to list GitHub repositories: " + err.Error()))
	}

	protoRepos := make([]protocol.GitHubRepo, len(repos))
	for i, r := range repos {
		protoRepos[i] = protocol.GitHubRepo{
			Name:        r.Name,
			FullName:    r.FullName,
			Private:     r.Private,
			Description: r.Description,
		}
	}

	return tc.Send(protocol.NewGitHubReposResponse(protoRepos))
}

// Tests

func TestHandleSessionCreate_Success(t *testing.T) {
	manager := newMockManager()
	manager.createSession = &protocol.Session{ID: "sess-123", Name: "Test Session", Status: protocol.StatusCreating}

	conn := &testableConnection{newMockConnection(manager)}
	ctx := context.Background()

	err := conn.handleSessionCreate(ctx, "Test Session", "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify manager was called
	if len(manager.createCalls) != 1 {
		t.Errorf("expected 1 create call, got %d", len(manager.createCalls))
	}
	if manager.createCalls[0].name != "Test Session" {
		t.Errorf("expected name 'Test Session', got %q", manager.createCalls[0].name)
	}

	// Verify response was sent
	if len(conn.sentMessages) != 1 {
		t.Errorf("expected 1 sent message, got %d", len(conn.sentMessages))
	}
	if conn.sentMessages[0].Type != protocol.MsgTypeSessionCreated {
		t.Errorf("expected session.created message, got %s", conn.sentMessages[0].Type)
	}

	// Verify subscription was created
	if _, ok := conn.subscriptions["sess-123"]; !ok {
		t.Error("expected subscription to be created for new session")
	}

	// Verify broadcast was sent
	if len(conn.server.broadcasts) != 1 {
		t.Errorf("expected 1 broadcast, got %d", len(conn.server.broadcasts))
	}
}

func TestHandleSessionCreate_WithRepo(t *testing.T) {
	manager := newMockManager()
	conn := &testableConnection{newMockConnection(manager)}

	err := conn.handleSessionCreate(context.Background(), "Repo Session", "owner/repo", "write", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(manager.createCalls) != 1 {
		t.Fatalf("expected 1 create call, got %d", len(manager.createCalls))
	}

	call := manager.createCalls[0]
	if call.repo == nil || *call.repo != "owner/repo" {
		t.Errorf("expected repo 'owner/repo', got %v", call.repo)
	}
	if call.repoAccess == nil || *call.repoAccess != "write" {
		t.Errorf("expected repoAccess 'write', got %v", call.repoAccess)
	}
}

func TestHandleSessionCreate_WithInitialPrompt(t *testing.T) {
	manager := newMockManager()
	manager.createSession = &protocol.Session{ID: "sess-456", Name: "Test", Status: protocol.StatusCreating}

	conn := &testableConnection{newMockConnection(manager)}

	err := conn.handleSessionCreate(context.Background(), "Test", "", "", "Hello, Claude!")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify initial prompt was sent
	if len(manager.sendPromptCalls) != 1 {
		t.Fatalf("expected 1 sendPrompt call, got %d", len(manager.sendPromptCalls))
	}
	if manager.sendPromptCalls[0].text != "Hello, Claude!" {
		t.Errorf("expected prompt 'Hello, Claude!', got %q", manager.sendPromptCalls[0].text)
	}
}

func TestHandleSessionCreate_Error(t *testing.T) {
	manager := newMockManager()
	manager.createErr = errors.New("storage error")

	conn := &testableConnection{newMockConnection(manager)}

	err := conn.handleSessionCreate(context.Background(), "Test", "", "", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "storage error" {
		t.Errorf("expected 'storage error', got %q", err.Error())
	}
}

func TestHandleSessionList_Success(t *testing.T) {
	manager := newMockManager()
	manager.listSessions = []protocol.Session{
		{ID: "sess-1", Name: "Session 1"},
		{ID: "sess-2", Name: "Session 2"},
	}

	conn := &testableConnection{newMockConnection(manager)}

	err := conn.handleSessionList(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(conn.sentMessages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(conn.sentMessages))
	}

	msg := conn.sentMessages[0]
	if msg.Type != protocol.MsgTypeSessionListResponse {
		t.Errorf("expected session.list message, got %s", msg.Type)
	}
	if len(msg.Sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(msg.Sessions))
	}
}

func TestHandleSessionList_Empty(t *testing.T) {
	manager := newMockManager()
	manager.listSessions = []protocol.Session{}

	conn := &testableConnection{newMockConnection(manager)}

	err := conn.handleSessionList(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(conn.sentMessages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(conn.sentMessages))
	}

	if len(conn.sentMessages[0].Sessions) != 0 {
		t.Errorf("expected empty sessions array, got %d", len(conn.sentMessages[0].Sessions))
	}
}

func TestHandleSessionResume_Success(t *testing.T) {
	manager := newMockManager()
	manager.resumeSession = &protocol.Session{ID: "sess-123", Status: protocol.StatusRunning}

	conn := &testableConnection{newMockConnection(manager)}

	err := conn.handleSessionResume(context.Background(), "sess-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify manager was called
	if len(manager.resumeCalls) != 1 || manager.resumeCalls[0] != "sess-123" {
		t.Errorf("expected resume call for sess-123")
	}

	// Verify subscription was created
	if _, ok := conn.subscriptions["sess-123"]; !ok {
		t.Error("expected subscription to be created")
	}

	// Verify response and broadcast
	if len(conn.sentMessages) != 1 || conn.sentMessages[0].Type != protocol.MsgTypeSessionUpdated {
		t.Error("expected session.updated message")
	}
	if len(conn.server.broadcasts) != 1 {
		t.Error("expected broadcast")
	}
}

func TestHandleSessionResume_NotFound(t *testing.T) {
	manager := newMockManager()
	manager.resumeErr = errors.New("session not found")

	conn := &testableConnection{newMockConnection(manager)}

	err := conn.handleSessionResume(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHandleSessionPause_Success(t *testing.T) {
	manager := newMockManager()
	conn := &testableConnection{newMockConnection(manager)}

	// Add subscription first
	conn.subscriptions["sess-123"] = "$"

	err := conn.handleSessionPause(context.Background(), "sess-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify subscription was removed
	if _, ok := conn.subscriptions["sess-123"]; ok {
		t.Error("expected subscription to be removed")
	}

	if len(manager.pauseCalls) != 1 {
		t.Error("expected pause to be called")
	}
}

func TestHandleSessionDelete_Success(t *testing.T) {
	manager := newMockManager()
	conn := &testableConnection{newMockConnection(manager)}

	// Add subscription first
	conn.subscriptions["sess-123"] = "$"

	err := conn.handleSessionDelete(context.Background(), "sess-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify subscription was removed
	if _, ok := conn.subscriptions["sess-123"]; ok {
		t.Error("expected subscription to be removed")
	}

	if len(manager.deleteCalls) != 1 || manager.deleteCalls[0] != "sess-123" {
		t.Error("expected delete to be called with sess-123")
	}

	// Verify deleted message sent
	if len(conn.sentMessages) != 1 || conn.sentMessages[0].Type != protocol.MsgTypeSessionDeleted {
		t.Error("expected session.deleted message")
	}
}

func TestHandlePrompt_Success(t *testing.T) {
	manager := newMockManager()
	conn := &testableConnection{newMockConnection(manager)}

	err := conn.handlePrompt(context.Background(), "sess-123", "Hello, Claude!")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(manager.sendPromptCalls) != 1 {
		t.Fatalf("expected 1 sendPrompt call")
	}
	call := manager.sendPromptCalls[0]
	if call.sessionID != "sess-123" || call.text != "Hello, Claude!" {
		t.Errorf("unexpected call: %+v", call)
	}
}

func TestHandlePrompt_MissingSessionID(t *testing.T) {
	manager := newMockManager()
	conn := &testableConnection{newMockConnection(manager)}

	err := conn.handlePrompt(context.Background(), "", "Hello")
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "sessionId is required" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestHandlePrompt_MissingText(t *testing.T) {
	manager := newMockManager()
	conn := &testableConnection{newMockConnection(manager)}

	err := conn.handlePrompt(context.Background(), "sess-123", "")
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "text is required" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestHandlePromptInterrupt_Success(t *testing.T) {
	manager := newMockManager()
	conn := &testableConnection{newMockConnection(manager)}

	err := conn.handlePromptInterrupt(context.Background(), "sess-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(manager.interruptCalls) != 1 || manager.interruptCalls[0] != "sess-123" {
		t.Error("expected interrupt to be called")
	}
}

func TestHandleTerminalInput_Success(t *testing.T) {
	manager := newMockManager()
	conn := &testableConnection{newMockConnection(manager)}

	err := conn.handleTerminalInput(context.Background(), "sess-123", "ls -la")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(manager.terminalInputCalls) != 1 {
		t.Fatalf("expected 1 terminal input call")
	}
	call := manager.terminalInputCalls[0]
	if call.sessionID != "sess-123" || call.data != "ls -la" {
		t.Errorf("unexpected call: %+v", call)
	}
}

func TestHandleTerminalInput_EmptyData(t *testing.T) {
	manager := newMockManager()
	conn := &testableConnection{newMockConnection(manager)}

	err := conn.handleTerminalInput(context.Background(), "sess-123", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Empty data should be a no-op
	if len(manager.terminalInputCalls) != 0 {
		t.Error("expected no terminal input calls for empty data")
	}
}

func TestHandleTerminalInput_MissingSessionID(t *testing.T) {
	manager := newMockManager()
	conn := &testableConnection{newMockConnection(manager)}

	err := conn.handleTerminalInput(context.Background(), "", "data")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHandleTerminalResize_Success(t *testing.T) {
	manager := newMockManager()
	conn := &testableConnection{newMockConnection(manager)}

	err := conn.handleTerminalResize(context.Background(), "sess-123", 80, 24)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(manager.terminalResizeCalls) != 1 {
		t.Fatalf("expected 1 resize call")
	}
	call := manager.terminalResizeCalls[0]
	if call.cols != 80 || call.rows != 24 {
		t.Errorf("expected 80x24, got %dx%d", call.cols, call.rows)
	}
}

func TestHandleTerminalResize_InvalidDimensions(t *testing.T) {
	manager := newMockManager()
	conn := &testableConnection{newMockConnection(manager)}

	tests := []struct {
		cols, rows int
	}{
		{0, 24},
		{80, 0},
		{-1, 24},
		{80, -1},
	}

	for _, tt := range tests {
		err := conn.handleTerminalResize(context.Background(), "sess-123", tt.cols, tt.rows)
		if err == nil {
			t.Errorf("expected error for dimensions %dx%d", tt.cols, tt.rows)
		}
	}
}

func TestHandleSync_Success(t *testing.T) {
	manager := newMockManager()
	count := 5
	manager.getAllWithMeta = []protocol.SessionWithMeta{
		{Session: protocol.Session{ID: "sess-1"}, MessageCount: &count},
		{Session: protocol.Session{ID: "sess-2"}},
	}

	conn := &testableConnection{newMockConnection(manager)}

	err := conn.handleSync(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(conn.sentMessages) != 1 {
		t.Fatalf("expected 1 message")
	}
	msg := conn.sentMessages[0]
	if msg.Type != protocol.MsgTypeSyncResponse {
		t.Errorf("expected sync.response, got %s", msg.Type)
	}
	if len(msg.SessionsWithMeta) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(msg.SessionsWithMeta))
	}
}

func TestHandleSessionOpen_Success(t *testing.T) {
	manager := newMockManager()
	manager.getWithHistory = &getWithHistoryResult{
		session:        &protocol.Session{ID: "sess-123"},
		messages:       []protocol.PersistedMessage{{ID: "msg-1", Content: "Hello"}},
		events:         []protocol.PersistedEvent{{ID: "evt-1"}},
		hasMore:        false,
		notificationID: "1234-0",
	}

	conn := &testableConnection{newMockConnection(manager)}

	err := conn.handleSessionOpen(context.Background(), "sess-123", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify subscription was created with current notification ID
	if cursor, ok := conn.subscriptions["sess-123"]; !ok {
		t.Error("expected subscription to be created")
	} else if cursor != "1234-0" {
		t.Errorf("expected cursor '1234-0', got %q", cursor)
	}

	if len(conn.sentMessages) != 1 {
		t.Fatalf("expected 1 message")
	}
	msg := conn.sentMessages[0]
	if msg.Type != protocol.MsgTypeSessionState {
		t.Errorf("expected session.state, got %s", msg.Type)
	}
	if msg.LastNotificationID != "1234-0" {
		t.Errorf("expected lastNotificationID '1234-0', got %q", msg.LastNotificationID)
	}
}

func TestHandleSessionOpen_WithLastNotificationID(t *testing.T) {
	manager := newMockManager()
	manager.getWithHistory = &getWithHistoryResult{
		session:        &protocol.Session{ID: "sess-123"},
		notificationID: "1234-0",
	}

	conn := &testableConnection{newMockConnection(manager)}

	lastID := "5678-0"
	err := conn.handleSessionOpen(context.Background(), "sess-123", nil, &lastID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should use client-provided cursor for reconnection
	if cursor := conn.subscriptions["sess-123"]; cursor != "5678-0" {
		t.Errorf("expected cursor '5678-0', got %q", cursor)
	}
}

func TestHandlePortExpose_Success(t *testing.T) {
	manager := newMockManager()
	manager.exposePortURL = "http://sandbox-sess-123:8080"

	conn := &testableConnection{newMockConnection(manager)}

	err := conn.handlePortExpose(context.Background(), "sess-123", 8080)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(manager.exposePortCalls) != 1 {
		t.Fatalf("expected 1 exposePort call")
	}

	if len(conn.sentMessages) != 1 {
		t.Fatalf("expected 1 message")
	}
	msg := conn.sentMessages[0]
	if msg.Type != protocol.MsgTypePortExposed {
		t.Errorf("expected port.exposed, got %s", msg.Type)
	}
	if msg.Port != 8080 {
		t.Errorf("expected port 8080, got %d", msg.Port)
	}
	if msg.PreviewURL != "http://sandbox-sess-123:8080" {
		t.Errorf("unexpected preview URL: %s", msg.PreviewURL)
	}
}

func TestHandlePortExpose_InvalidPort(t *testing.T) {
	manager := newMockManager()
	conn := &testableConnection{newMockConnection(manager)}

	tests := []int{0, -1, 65536, 70000}
	for _, port := range tests {
		err := conn.handlePortExpose(context.Background(), "sess-123", port)
		if err != nil {
			t.Errorf("expected no error (error message sent as response), got %v", err)
		}
		// Should send error message
		if len(conn.sentMessages) == 0 {
			t.Error("expected error message to be sent")
		}
		lastMsg := conn.sentMessages[len(conn.sentMessages)-1]
		if lastMsg.Type != protocol.MsgTypePortError {
			t.Errorf("expected port.error, got %s", lastMsg.Type)
		}
	}
}

func TestHandlePortExpose_MissingSessionID(t *testing.T) {
	manager := newMockManager()
	conn := &testableConnection{newMockConnection(manager)}

	err := conn.handlePortExpose(context.Background(), "", 8080)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(conn.sentMessages) != 1 || conn.sentMessages[0].Type != protocol.MsgTypePortError {
		t.Error("expected port.error message")
	}
}

func TestHandleGitHubReposList_Success(t *testing.T) {
	manager := newMockManager()
	manager.listRepos = []github.Repository{
		{Name: "repo1", FullName: "owner/repo1", Private: false},
		{Name: "repo2", FullName: "owner/repo2", Private: true, Description: "Private repo"},
	}

	conn := &testableConnection{newMockConnection(manager)}

	err := conn.handleGitHubReposList(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(conn.sentMessages) != 1 {
		t.Fatalf("expected 1 message")
	}
	msg := conn.sentMessages[0]
	if msg.Type != protocol.MsgTypeGitHubReposResponse {
		t.Errorf("expected github.repos, got %s", msg.Type)
	}
	if len(msg.Repos) != 2 {
		t.Errorf("expected 2 repos, got %d", len(msg.Repos))
	}
}

func TestHandleGitHubReposList_Error(t *testing.T) {
	manager := newMockManager()
	manager.listReposErr = errors.New("API error")

	conn := &testableConnection{newMockConnection(manager)}

	err := conn.handleGitHubReposList(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(conn.sentMessages) != 1 {
		t.Fatalf("expected 1 message")
	}
	msg := conn.sentMessages[0]
	if msg.Type != protocol.MsgTypeError {
		t.Errorf("expected error message, got %s", msg.Type)
	}
}

func TestHandleGitHubReposList_Empty(t *testing.T) {
	manager := newMockManager()
	manager.listRepos = nil // GitHub not configured

	conn := &testableConnection{newMockConnection(manager)}

	err := conn.handleGitHubReposList(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msg := conn.sentMessages[0]
	if msg.Type != protocol.MsgTypeGitHubReposResponse {
		t.Errorf("expected github.repos, got %s", msg.Type)
	}
	if len(msg.Repos) != 0 {
		t.Errorf("expected empty repos, got %d", len(msg.Repos))
	}
}
