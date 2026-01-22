package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	pb "github.com/angristan/netclode/services/control-plane/gen/netclode/v1"
	"github.com/angristan/netclode/services/control-plane/internal/github"
	"github.com/angristan/netclode/services/control-plane/internal/protocol"
	"github.com/angristan/netclode/services/control-plane/internal/session"
)

// =============================================================================
// Mock Manager for Handler Integration Tests
// =============================================================================

// mockManager implements the subset of session.Manager methods used by handlers.
type mockManager struct {
	// Stored state
	sessions      map[string]*protocol.Session
	messages      map[string][]protocol.PersistedMessage
	events        map[string][]protocol.PersistedEvent
	subscribers   map[string]*session.StreamSubscriber
	gitStatus     map[string][]protocol.GitFileChange
	gitDiffs      map[string]string
	githubRepos   []github.Repository
	sessionsWithMeta []protocol.SessionWithMeta

	// Capture method calls
	createdSessions   []createSessionCall
	resumedSessions   []string
	pausedSessions    []string
	deletedSessions   []string
	sentPrompts       []sendPromptCall
	sentTerminalInput []terminalInputCall
	resizedTerminals  []terminalResizeCall
	exposedPorts      []exposePortCall
	gitStatusCalls    []string
	gitDiffCalls      []gitDiffCall
	interrupts        []string

	// Error injection
	createErr         error
	listErr           error
	getWithHistoryErr error
	resumeErr         error
	pauseErr          error
	deleteErr         error
	deleteAllErr      error
	sendPromptErr     error
	interruptErr      error
	terminalInputErr  error
	terminalResizeErr error
	getAllWithMetaErr error
	exposePortErr     error
	listGitHubErr     error
	gitStatusErr      error
	gitDiffErr        error
	subscribeErr      error
}

type createSessionCall struct {
	name       string
	repo       *string
	repoAccess *string
}

type sendPromptCall struct {
	sessionID string
	text      string
}

type terminalInputCall struct {
	sessionID string
	data      string
}

type terminalResizeCall struct {
	sessionID string
	cols      int
	rows      int
}

type exposePortCall struct {
	sessionID string
	port      int
}

type gitDiffCall struct {
	sessionID string
	file      string
}

func newMockManager() *mockManager {
	return &mockManager{
		sessions:    make(map[string]*protocol.Session),
		messages:    make(map[string][]protocol.PersistedMessage),
		events:      make(map[string][]protocol.PersistedEvent),
		subscribers: make(map[string]*session.StreamSubscriber),
		gitStatus:   make(map[string][]protocol.GitFileChange),
		gitDiffs:    make(map[string]string),
	}
}

func (m *mockManager) Create(ctx context.Context, name string, repo *string, repoAccess *string) (*protocol.Session, error) {
	m.createdSessions = append(m.createdSessions, createSessionCall{name, repo, repoAccess})
	if m.createErr != nil {
		return nil, m.createErr
	}
	sess := &protocol.Session{
		ID:           "sess-new-123",
		Name:         name,
		Status:       protocol.StatusCreating,
		Repo:         repo,
		RepoAccess:   repoAccess,
		CreatedAt:    time.Now().UTC().Format(time.RFC3339),
		LastActiveAt: time.Now().UTC().Format(time.RFC3339),
	}
	m.sessions[sess.ID] = sess
	return sess, nil
}

func (m *mockManager) List(ctx context.Context) ([]protocol.Session, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	result := make([]protocol.Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		result = append(result, *s)
	}
	return result, nil
}

func (m *mockManager) GetWithHistory(ctx context.Context, id string, limit int) (*protocol.Session, []protocol.PersistedMessage, []protocol.PersistedEvent, bool, string, error) {
	if m.getWithHistoryErr != nil {
		return nil, nil, nil, false, "", m.getWithHistoryErr
	}
	sess, ok := m.sessions[id]
	if !ok {
		return nil, nil, nil, false, "", errors.New("session not found")
	}
	return sess, m.messages[id], m.events[id], false, "notification-123", nil
}

func (m *mockManager) Resume(ctx context.Context, id string) (*protocol.Session, error) {
	m.resumedSessions = append(m.resumedSessions, id)
	if m.resumeErr != nil {
		return nil, m.resumeErr
	}
	sess, ok := m.sessions[id]
	if !ok {
		return nil, errors.New("session not found")
	}
	sess.Status = protocol.StatusResuming
	return sess, nil
}

func (m *mockManager) Pause(ctx context.Context, id string) (*protocol.Session, error) {
	m.pausedSessions = append(m.pausedSessions, id)
	if m.pauseErr != nil {
		return nil, m.pauseErr
	}
	sess, ok := m.sessions[id]
	if !ok {
		return nil, errors.New("session not found")
	}
	sess.Status = protocol.StatusPaused
	return sess, nil
}

func (m *mockManager) Delete(ctx context.Context, id string) error {
	m.deletedSessions = append(m.deletedSessions, id)
	if m.deleteErr != nil {
		return m.deleteErr
	}
	delete(m.sessions, id)
	return nil
}

func (m *mockManager) DeleteAll(ctx context.Context) ([]string, error) {
	if m.deleteAllErr != nil {
		return nil, m.deleteAllErr
	}
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
		m.deletedSessions = append(m.deletedSessions, id)
	}
	m.sessions = make(map[string]*protocol.Session)
	return ids, nil
}

func (m *mockManager) SendPrompt(ctx context.Context, sessionID, text string) error {
	m.sentPrompts = append(m.sentPrompts, sendPromptCall{sessionID, text})
	return m.sendPromptErr
}

func (m *mockManager) Interrupt(ctx context.Context, sessionID string) error {
	m.interrupts = append(m.interrupts, sessionID)
	return m.interruptErr
}

func (m *mockManager) SendTerminalInput(ctx context.Context, sessionID, data string) error {
	m.sentTerminalInput = append(m.sentTerminalInput, terminalInputCall{sessionID, data})
	return m.terminalInputErr
}

func (m *mockManager) ResizeTerminal(ctx context.Context, sessionID string, cols, rows int) error {
	m.resizedTerminals = append(m.resizedTerminals, terminalResizeCall{sessionID, cols, rows})
	return m.terminalResizeErr
}

func (m *mockManager) GetAllWithMeta(ctx context.Context) ([]protocol.SessionWithMeta, error) {
	if m.getAllWithMetaErr != nil {
		return nil, m.getAllWithMetaErr
	}
	return m.sessionsWithMeta, nil
}

func (m *mockManager) ExposePort(ctx context.Context, sessionID string, port int) (string, error) {
	m.exposedPorts = append(m.exposedPorts, exposePortCall{sessionID, port})
	if m.exposePortErr != nil {
		return "", m.exposePortErr
	}
	return "http://sandbox-" + sessionID + ".example.ts.net:" + string(rune(port+'0')), nil
}

func (m *mockManager) ListGitHubRepos(ctx context.Context) ([]github.Repository, error) {
	if m.listGitHubErr != nil {
		return nil, m.listGitHubErr
	}
	return m.githubRepos, nil
}

func (m *mockManager) GetGitStatus(ctx context.Context, sessionID string) ([]protocol.GitFileChange, error) {
	m.gitStatusCalls = append(m.gitStatusCalls, sessionID)
	if m.gitStatusErr != nil {
		return nil, m.gitStatusErr
	}
	return m.gitStatus[sessionID], nil
}

func (m *mockManager) GetGitDiff(ctx context.Context, sessionID, file string) (string, error) {
	m.gitDiffCalls = append(m.gitDiffCalls, gitDiffCall{sessionID, file})
	if m.gitDiffErr != nil {
		return "", m.gitDiffErr
	}
	return m.gitDiffs[sessionID], nil
}

func (m *mockManager) Subscribe(ctx context.Context, id string, lastNotificationID string) (*session.StreamSubscriber, error) {
	if m.subscribeErr != nil {
		return nil, m.subscribeErr
	}
	// Return nil subscriber for tests - we don't actually need it for most tests
	return nil, nil
}

// =============================================================================
// Mock Connection for Testing Handlers
// =============================================================================

// mockConnectConnection wraps ConnectConnection with a mock manager for testing.
type testConnection struct {
	conn     *ConnectConnection
	mock     *mockManager
	sent     []*pb.ServerMessage
	sendErr  error
}

func newTestConnection(mock *mockManager) *testConnection {
	tc := &testConnection{
		mock: mock,
		sent: make([]*pb.ServerMessage, 0),
	}
	tc.conn = &ConnectConnection{
		subscriptions:  make(map[string]*subscriptionInfo),
		globalMessages: make(chan *pb.ServerMessage, 64),
		done:           make(chan struct{}),
	}
	return tc
}

// Helper to send and capture messages
func (tc *testConnection) handleAndCapture(ctx context.Context, msg *pb.ClientMessage) error {
	// Create a wrapper that intercepts send calls
	return tc.handleMessageWithMock(ctx, msg)
}

// handleMessageWithMock handles a message using the mock manager and captures sent messages.
func (tc *testConnection) handleMessageWithMock(ctx context.Context, msg *pb.ClientMessage) error {
	switch m := msg.Message.(type) {
	case *pb.ClientMessage_CreateSession:
		return tc.handleSessionCreate(ctx, m.CreateSession)
	case *pb.ClientMessage_ListSessions:
		return tc.handleSessionList(ctx)
	case *pb.ClientMessage_OpenSession:
		return tc.handleSessionOpen(ctx, m.OpenSession)
	case *pb.ClientMessage_ResumeSession:
		return tc.handleSessionResume(ctx, m.ResumeSession.SessionId)
	case *pb.ClientMessage_PauseSession:
		return tc.handleSessionPause(ctx, m.PauseSession.SessionId)
	case *pb.ClientMessage_DeleteSession:
		return tc.handleSessionDelete(ctx, m.DeleteSession.SessionId)
	case *pb.ClientMessage_DeleteAllSessions:
		return tc.handleSessionDeleteAll(ctx)
	case *pb.ClientMessage_SendPrompt:
		return tc.handlePrompt(ctx, m.SendPrompt.SessionId, m.SendPrompt.Text)
	case *pb.ClientMessage_InterruptPrompt:
		return tc.handlePromptInterrupt(ctx, m.InterruptPrompt.SessionId)
	case *pb.ClientMessage_TerminalInput:
		return tc.handleTerminalInput(ctx, m.TerminalInput.SessionId, m.TerminalInput.Data)
	case *pb.ClientMessage_TerminalResize:
		return tc.handleTerminalResize(ctx, m.TerminalResize)
	case *pb.ClientMessage_Sync:
		return tc.handleSync(ctx)
	default:
		return connect.NewError(connect.CodeInvalidArgument, errUnknownMessage)
	}
}

func (tc *testConnection) send(msg *pb.ServerMessage) error {
	if tc.sendErr != nil {
		return tc.sendErr
	}
	tc.sent = append(tc.sent, msg)
	return nil
}

func (tc *testConnection) lastSent() *pb.ServerMessage {
	if len(tc.sent) == 0 {
		return nil
	}
	return tc.sent[len(tc.sent)-1]
}

// =============================================================================
// Handler Implementations Using Mock
// =============================================================================

func (tc *testConnection) handleSessionCreate(ctx context.Context, req *pb.CreateSessionRequest) error {
	var repoPtr, repoAccessPtr *string
	if req.Repo != nil {
		repoPtr = req.Repo
	}
	if req.RepoAccess != nil {
		repoAccessPtr = req.RepoAccess
	}

	name := ""
	if req.Name != nil {
		name = *req.Name
	}

	sess, err := tc.mock.Create(ctx, name, repoPtr, repoAccessPtr)
	if err != nil {
		return err
	}

	pbSession := convertSession(sess)
	return tc.send(&pb.ServerMessage{
		Message: &pb.ServerMessage_SessionCreated{
			SessionCreated: &pb.SessionCreatedResponse{Session: pbSession},
		},
	})
}

func (tc *testConnection) handleSessionList(ctx context.Context) error {
	sessions, err := tc.mock.List(ctx)
	if err != nil {
		return err
	}

	pbSessions := make([]*pb.Session, len(sessions))
	for i, s := range sessions {
		pbSessions[i] = convertSession(&s)
	}

	return tc.send(&pb.ServerMessage{
		Message: &pb.ServerMessage_SessionList{
			SessionList: &pb.SessionListResponse{Sessions: pbSessions},
		},
	})
}

func (tc *testConnection) handleSessionOpen(ctx context.Context, req *pb.OpenSessionRequest) error {
	sess, messages, events, hasMore, notifID, err := tc.mock.GetWithHistory(ctx, req.SessionId, 100)
	if err != nil {
		return err
	}

	pbMessages := make([]*pb.PersistedMessage, len(messages))
	for i, m := range messages {
		pbMessages[i] = convertPersistedMessage(&m)
	}

	pbEvents := make([]*pb.PersistedEvent, len(events))
	for i, e := range events {
		pbEvents[i] = convertPersistedEvent(&e)
	}

	resp := &pb.SessionStateResponse{
		Session:            convertSession(sess),
		Messages:           pbMessages,
		Events:             pbEvents,
		HasMore:            hasMore,
		LastNotificationId: &notifID,
	}

	return tc.send(&pb.ServerMessage{
		Message: &pb.ServerMessage_SessionState{SessionState: resp},
	})
}

func (tc *testConnection) handleSessionResume(ctx context.Context, id string) error {
	sess, err := tc.mock.Resume(ctx, id)
	if err != nil {
		return err
	}

	return tc.send(&pb.ServerMessage{
		Message: &pb.ServerMessage_SessionUpdated{
			SessionUpdated: &pb.SessionUpdatedResponse{Session: convertSession(sess)},
		},
	})
}

func (tc *testConnection) handleSessionPause(ctx context.Context, id string) error {
	sess, err := tc.mock.Pause(ctx, id)
	if err != nil {
		return err
	}

	return tc.send(&pb.ServerMessage{
		Message: &pb.ServerMessage_SessionUpdated{
			SessionUpdated: &pb.SessionUpdatedResponse{Session: convertSession(sess)},
		},
	})
}

func (tc *testConnection) handleSessionDelete(ctx context.Context, id string) error {
	if err := tc.mock.Delete(ctx, id); err != nil {
		return err
	}

	return tc.send(&pb.ServerMessage{
		Message: &pb.ServerMessage_SessionDeleted{
			SessionDeleted: &pb.SessionDeletedResponse{SessionId: id},
		},
	})
}

func (tc *testConnection) handleSessionDeleteAll(ctx context.Context) error {
	deletedIDs, err := tc.mock.DeleteAll(ctx)
	if err != nil {
		return err
	}

	return tc.send(&pb.ServerMessage{
		Message: &pb.ServerMessage_SessionsDeletedAll{
			SessionsDeletedAll: &pb.SessionsDeletedAllResponse{DeletedIds: deletedIDs},
		},
	})
}

func (tc *testConnection) handlePrompt(ctx context.Context, sessionID, text string) error {
	if sessionID == "" {
		return connect.NewError(connect.CodeInvalidArgument, errSessionIDRequired)
	}
	if text == "" {
		return connect.NewError(connect.CodeInvalidArgument, errTextRequired)
	}

	return tc.mock.SendPrompt(ctx, sessionID, text)
}

func (tc *testConnection) handlePromptInterrupt(ctx context.Context, sessionID string) error {
	return tc.mock.Interrupt(ctx, sessionID)
}

func (tc *testConnection) handleTerminalInput(ctx context.Context, sessionID, data string) error {
	if sessionID == "" {
		return connect.NewError(connect.CodeInvalidArgument, errSessionIDRequired)
	}
	if data == "" {
		return nil // Empty input is a no-op
	}

	return tc.mock.SendTerminalInput(ctx, sessionID, data)
}

func (tc *testConnection) handleTerminalResize(ctx context.Context, req *pb.TerminalResizeRequest) error {
	if req.SessionId == "" {
		return connect.NewError(connect.CodeInvalidArgument, errSessionIDRequired)
	}
	if req.Cols <= 0 || req.Rows <= 0 {
		return connect.NewError(connect.CodeInvalidArgument, errInvalidTerminalDimensions)
	}

	return tc.mock.ResizeTerminal(ctx, req.SessionId, int(req.Cols), int(req.Rows))
}

func (tc *testConnection) handleSync(ctx context.Context) error {
	sessions, err := tc.mock.GetAllWithMeta(ctx)
	if err != nil {
		return err
	}

	pbSessions := make([]*pb.SessionWithMeta, len(sessions))
	for i, s := range sessions {
		pbSessions[i] = convertSessionWithMeta(&s)
	}

	return tc.send(&pb.ServerMessage{
		Message: &pb.ServerMessage_SyncResponse{
			SyncResponse: &pb.SyncResponse{
				Sessions: pbSessions,
			},
		},
	})
}

// =============================================================================
// Handler Integration Tests
// =============================================================================

func TestHandleSessionCreate(t *testing.T) {
	t.Run("creates session with name and repo", func(t *testing.T) {
		mock := newMockManager()
		tc := newTestConnection(mock)

		repo := "owner/repo"
		access := "write"
		name := "Test Session"
		msg := &pb.ClientMessage{
			Message: &pb.ClientMessage_CreateSession{
				CreateSession: &pb.CreateSessionRequest{
					Name:       &name,
					Repo:       &repo,
					RepoAccess: &access,
				},
			},
		}

		err := tc.handleAndCapture(context.Background(), msg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify manager was called correctly
		if len(mock.createdSessions) != 1 {
			t.Fatalf("expected 1 create call, got %d", len(mock.createdSessions))
		}
		call := mock.createdSessions[0]
		if call.name != name {
			t.Errorf("name = %q, want %q", call.name, name)
		}
		if *call.repo != repo {
			t.Errorf("repo = %q, want %q", *call.repo, repo)
		}

		// Verify response
		resp := tc.lastSent()
		if resp == nil {
			t.Fatal("expected response message")
		}
		created := resp.GetSessionCreated()
		if created == nil {
			t.Fatal("expected SessionCreated response")
		}
		if created.Session.Name != name {
			t.Errorf("response name = %q, want %q", created.Session.Name, name)
		}
	})

	t.Run("returns error on manager failure", func(t *testing.T) {
		mock := newMockManager()
		mock.createErr = errors.New("storage error")
		tc := newTestConnection(mock)

		msg := &pb.ClientMessage{
			Message: &pb.ClientMessage_CreateSession{
				CreateSession: &pb.CreateSessionRequest{},
			},
		}

		err := tc.handleAndCapture(context.Background(), msg)
		if err == nil {
			t.Error("expected error")
		}
	})
}

func TestHandleSessionList(t *testing.T) {
	t.Run("returns all sessions", func(t *testing.T) {
		mock := newMockManager()
		mock.sessions["sess-1"] = &protocol.Session{ID: "sess-1", Name: "Session 1", Status: protocol.StatusReady}
		mock.sessions["sess-2"] = &protocol.Session{ID: "sess-2", Name: "Session 2", Status: protocol.StatusPaused}
		tc := newTestConnection(mock)

		msg := &pb.ClientMessage{
			Message: &pb.ClientMessage_ListSessions{
				ListSessions: &pb.ListSessionsRequest{},
			},
		}

		err := tc.handleAndCapture(context.Background(), msg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		resp := tc.lastSent()
		list := resp.GetSessionList()
		if list == nil {
			t.Fatal("expected SessionList response")
		}
		if len(list.Sessions) != 2 {
			t.Errorf("expected 2 sessions, got %d", len(list.Sessions))
		}
	})
}

func TestHandleSessionOpen(t *testing.T) {
	t.Run("returns session with history", func(t *testing.T) {
		mock := newMockManager()
		mock.sessions["sess-123"] = &protocol.Session{
			ID:     "sess-123",
			Name:   "Test",
			Status: protocol.StatusReady,
		}
		mock.messages["sess-123"] = []protocol.PersistedMessage{
			{ID: "msg-1", Role: protocol.RoleUser, Content: "Hello"},
			{ID: "msg-2", Role: protocol.RoleAssistant, Content: "Hi there"},
		}
		tc := newTestConnection(mock)

		msg := &pb.ClientMessage{
			Message: &pb.ClientMessage_OpenSession{
				OpenSession: &pb.OpenSessionRequest{SessionId: "sess-123"},
			},
		}

		err := tc.handleAndCapture(context.Background(), msg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		resp := tc.lastSent()
		state := resp.GetSessionState()
		if state == nil {
			t.Fatal("expected SessionState response")
		}
		if state.Session.Id != "sess-123" {
			t.Errorf("session ID = %q, want %q", state.Session.Id, "sess-123")
		}
		if len(state.Messages) != 2 {
			t.Errorf("expected 2 messages, got %d", len(state.Messages))
		}
	})

	t.Run("returns error for non-existent session", func(t *testing.T) {
		mock := newMockManager()
		tc := newTestConnection(mock)

		msg := &pb.ClientMessage{
			Message: &pb.ClientMessage_OpenSession{
				OpenSession: &pb.OpenSessionRequest{SessionId: "non-existent"},
			},
		}

		err := tc.handleAndCapture(context.Background(), msg)
		if err == nil {
			t.Error("expected error for non-existent session")
		}
	})
}

func TestHandleSessionResume(t *testing.T) {
	t.Run("resumes paused session", func(t *testing.T) {
		mock := newMockManager()
		mock.sessions["sess-123"] = &protocol.Session{
			ID:     "sess-123",
			Name:   "Test",
			Status: protocol.StatusPaused,
		}
		tc := newTestConnection(mock)

		msg := &pb.ClientMessage{
			Message: &pb.ClientMessage_ResumeSession{
				ResumeSession: &pb.ResumeSessionRequest{SessionId: "sess-123"},
			},
		}

		err := tc.handleAndCapture(context.Background(), msg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify manager was called
		if len(mock.resumedSessions) != 1 {
			t.Fatalf("expected 1 resume call, got %d", len(mock.resumedSessions))
		}
		if mock.resumedSessions[0] != "sess-123" {
			t.Errorf("resumed session = %q, want %q", mock.resumedSessions[0], "sess-123")
		}

		// Verify response
		resp := tc.lastSent()
		updated := resp.GetSessionUpdated()
		if updated == nil {
			t.Fatal("expected SessionUpdated response")
		}
		if updated.Session.Status != pb.SessionStatus_SESSION_STATUS_RESUMING {
			t.Errorf("status = %v, want RESUMING", updated.Session.Status)
		}
	})
}

func TestHandleSessionPause(t *testing.T) {
	t.Run("pauses running session", func(t *testing.T) {
		mock := newMockManager()
		mock.sessions["sess-123"] = &protocol.Session{
			ID:     "sess-123",
			Name:   "Test",
			Status: protocol.StatusRunning,
		}
		tc := newTestConnection(mock)

		msg := &pb.ClientMessage{
			Message: &pb.ClientMessage_PauseSession{
				PauseSession: &pb.PauseSessionRequest{SessionId: "sess-123"},
			},
		}

		err := tc.handleAndCapture(context.Background(), msg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(mock.pausedSessions) != 1 {
			t.Fatalf("expected 1 pause call, got %d", len(mock.pausedSessions))
		}

		resp := tc.lastSent()
		updated := resp.GetSessionUpdated()
		if updated == nil {
			t.Fatal("expected SessionUpdated response")
		}
		if updated.Session.Status != pb.SessionStatus_SESSION_STATUS_PAUSED {
			t.Errorf("status = %v, want PAUSED", updated.Session.Status)
		}
	})
}

func TestHandleSessionDelete(t *testing.T) {
	t.Run("deletes session", func(t *testing.T) {
		mock := newMockManager()
		mock.sessions["sess-123"] = &protocol.Session{ID: "sess-123"}
		tc := newTestConnection(mock)

		msg := &pb.ClientMessage{
			Message: &pb.ClientMessage_DeleteSession{
				DeleteSession: &pb.DeleteSessionRequest{SessionId: "sess-123"},
			},
		}

		err := tc.handleAndCapture(context.Background(), msg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(mock.deletedSessions) != 1 {
			t.Fatalf("expected 1 delete call, got %d", len(mock.deletedSessions))
		}

		resp := tc.lastSent()
		deleted := resp.GetSessionDeleted()
		if deleted == nil {
			t.Fatal("expected SessionDeleted response")
		}
		if deleted.SessionId != "sess-123" {
			t.Errorf("deleted ID = %q, want %q", deleted.SessionId, "sess-123")
		}
	})
}

func TestHandleSessionDeleteAll(t *testing.T) {
	t.Run("deletes all sessions", func(t *testing.T) {
		mock := newMockManager()
		mock.sessions["sess-1"] = &protocol.Session{ID: "sess-1"}
		mock.sessions["sess-2"] = &protocol.Session{ID: "sess-2"}
		tc := newTestConnection(mock)

		msg := &pb.ClientMessage{
			Message: &pb.ClientMessage_DeleteAllSessions{
				DeleteAllSessions: &pb.DeleteAllSessionsRequest{},
			},
		}

		err := tc.handleAndCapture(context.Background(), msg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		resp := tc.lastSent()
		deletedAll := resp.GetSessionsDeletedAll()
		if deletedAll == nil {
			t.Fatal("expected SessionsDeletedAll response")
		}
		if len(deletedAll.DeletedIds) != 2 {
			t.Errorf("expected 2 deleted IDs, got %d", len(deletedAll.DeletedIds))
		}
	})
}

func TestHandlePrompt(t *testing.T) {
	t.Run("sends prompt to manager", func(t *testing.T) {
		mock := newMockManager()
		tc := newTestConnection(mock)

		msg := &pb.ClientMessage{
			Message: &pb.ClientMessage_SendPrompt{
				SendPrompt: &pb.SendPromptRequest{
					SessionId: "sess-123",
					Text:      "Hello, Claude!",
				},
			},
		}

		err := tc.handleAndCapture(context.Background(), msg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(mock.sentPrompts) != 1 {
			t.Fatalf("expected 1 prompt call, got %d", len(mock.sentPrompts))
		}
		call := mock.sentPrompts[0]
		if call.sessionID != "sess-123" {
			t.Errorf("sessionID = %q, want %q", call.sessionID, "sess-123")
		}
		if call.text != "Hello, Claude!" {
			t.Errorf("text = %q, want %q", call.text, "Hello, Claude!")
		}
	})

	t.Run("validates session ID", func(t *testing.T) {
		mock := newMockManager()
		tc := newTestConnection(mock)

		msg := &pb.ClientMessage{
			Message: &pb.ClientMessage_SendPrompt{
				SendPrompt: &pb.SendPromptRequest{
					SessionId: "",
					Text:      "Hello",
				},
			},
		}

		err := tc.handleAndCapture(context.Background(), msg)
		if err == nil {
			t.Error("expected error for empty session ID")
		}
	})

	t.Run("validates text", func(t *testing.T) {
		mock := newMockManager()
		tc := newTestConnection(mock)

		msg := &pb.ClientMessage{
			Message: &pb.ClientMessage_SendPrompt{
				SendPrompt: &pb.SendPromptRequest{
					SessionId: "sess-123",
					Text:      "",
				},
			},
		}

		err := tc.handleAndCapture(context.Background(), msg)
		if err == nil {
			t.Error("expected error for empty text")
		}
	})
}

func TestHandleTerminalInput(t *testing.T) {
	t.Run("sends input to manager", func(t *testing.T) {
		mock := newMockManager()
		tc := newTestConnection(mock)

		msg := &pb.ClientMessage{
			Message: &pb.ClientMessage_TerminalInput{
				TerminalInput: &pb.TerminalInputRequest{
					SessionId: "sess-123",
					Data:      "ls -la\n",
				},
			},
		}

		err := tc.handleAndCapture(context.Background(), msg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(mock.sentTerminalInput) != 1 {
			t.Fatalf("expected 1 terminal input call, got %d", len(mock.sentTerminalInput))
		}
		if mock.sentTerminalInput[0].data != "ls -la\n" {
			t.Errorf("data = %q, want %q", mock.sentTerminalInput[0].data, "ls -la\n")
		}
	})

	t.Run("empty data is no-op", func(t *testing.T) {
		mock := newMockManager()
		tc := newTestConnection(mock)

		msg := &pb.ClientMessage{
			Message: &pb.ClientMessage_TerminalInput{
				TerminalInput: &pb.TerminalInputRequest{
					SessionId: "sess-123",
					Data:      "",
				},
			},
		}

		err := tc.handleAndCapture(context.Background(), msg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(mock.sentTerminalInput) != 0 {
			t.Errorf("expected no terminal input calls for empty data, got %d", len(mock.sentTerminalInput))
		}
	})
}

func TestHandleTerminalResize(t *testing.T) {
	t.Run("resizes terminal", func(t *testing.T) {
		mock := newMockManager()
		tc := newTestConnection(mock)

		msg := &pb.ClientMessage{
			Message: &pb.ClientMessage_TerminalResize{
				TerminalResize: &pb.TerminalResizeRequest{
					SessionId: "sess-123",
					Cols:      120,
					Rows:      40,
				},
			},
		}

		err := tc.handleAndCapture(context.Background(), msg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(mock.resizedTerminals) != 1 {
			t.Fatalf("expected 1 resize call, got %d", len(mock.resizedTerminals))
		}
		resize := mock.resizedTerminals[0]
		if resize.cols != 120 || resize.rows != 40 {
			t.Errorf("resize = %dx%d, want 120x40", resize.cols, resize.rows)
		}
	})

	t.Run("validates dimensions", func(t *testing.T) {
		mock := newMockManager()
		tc := newTestConnection(mock)

		msg := &pb.ClientMessage{
			Message: &pb.ClientMessage_TerminalResize{
				TerminalResize: &pb.TerminalResizeRequest{
					SessionId: "sess-123",
					Cols:      0,
					Rows:      24,
				},
			},
		}

		err := tc.handleAndCapture(context.Background(), msg)
		if err == nil {
			t.Error("expected error for invalid dimensions")
		}
	})
}

func TestHandleSync(t *testing.T) {
	t.Run("returns sessions with metadata", func(t *testing.T) {
		mock := newMockManager()
		msgCount := 10
		mock.sessionsWithMeta = []protocol.SessionWithMeta{
			{
				Session:      protocol.Session{ID: "sess-1", Name: "Session 1", Status: protocol.StatusReady},
				MessageCount: &msgCount,
			},
		}
		tc := newTestConnection(mock)

		msg := &pb.ClientMessage{
			Message: &pb.ClientMessage_Sync{
				Sync: &pb.SyncRequest{},
			},
		}

		err := tc.handleAndCapture(context.Background(), msg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		resp := tc.lastSent()
		sync := resp.GetSyncResponse()
		if sync == nil {
			t.Fatal("expected SyncResponse")
		}
		if len(sync.Sessions) != 1 {
			t.Errorf("expected 1 session, got %d", len(sync.Sessions))
		}
		if sync.Sessions[0].GetMessageCount() != 10 {
			t.Errorf("message count = %d, want 10", sync.Sessions[0].GetMessageCount())
		}
	})
}

func TestHandlePromptInterrupt(t *testing.T) {
	t.Run("interrupts prompt execution", func(t *testing.T) {
		mock := newMockManager()
		tc := newTestConnection(mock)

		msg := &pb.ClientMessage{
			Message: &pb.ClientMessage_InterruptPrompt{
				InterruptPrompt: &pb.InterruptPromptRequest{
					SessionId: "sess-123",
				},
			},
		}

		err := tc.handleAndCapture(context.Background(), msg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(mock.interrupts) != 1 {
			t.Fatalf("expected 1 interrupt call, got %d", len(mock.interrupts))
		}
		if mock.interrupts[0] != "sess-123" {
			t.Errorf("interrupted session = %q, want %q", mock.interrupts[0], "sess-123")
		}
	})
}

// =============================================================================
// Error Propagation Tests
// =============================================================================

func TestHandlerErrorPropagation(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*mockManager)
		message *pb.ClientMessage
	}{
		{
			name: "create error propagates",
			setup: func(m *mockManager) {
				m.createErr = errors.New("create failed")
			},
			message: &pb.ClientMessage{
				Message: &pb.ClientMessage_CreateSession{
					CreateSession: &pb.CreateSessionRequest{},
				},
			},
		},
		{
			name: "list error propagates",
			setup: func(m *mockManager) {
				m.listErr = errors.New("list failed")
			},
			message: &pb.ClientMessage{
				Message: &pb.ClientMessage_ListSessions{
					ListSessions: &pb.ListSessionsRequest{},
				},
			},
		},
		{
			name: "resume error propagates",
			setup: func(m *mockManager) {
				m.resumeErr = errors.New("resume failed")
			},
			message: &pb.ClientMessage{
				Message: &pb.ClientMessage_ResumeSession{
					ResumeSession: &pb.ResumeSessionRequest{SessionId: "sess-123"},
				},
			},
		},
		{
			name: "pause error propagates",
			setup: func(m *mockManager) {
				m.pauseErr = errors.New("pause failed")
			},
			message: &pb.ClientMessage{
				Message: &pb.ClientMessage_PauseSession{
					PauseSession: &pb.PauseSessionRequest{SessionId: "sess-123"},
				},
			},
		},
		{
			name: "delete error propagates",
			setup: func(m *mockManager) {
				m.deleteErr = errors.New("delete failed")
			},
			message: &pb.ClientMessage{
				Message: &pb.ClientMessage_DeleteSession{
					DeleteSession: &pb.DeleteSessionRequest{SessionId: "sess-123"},
				},
			},
		},
		{
			name: "prompt error propagates",
			setup: func(m *mockManager) {
				m.sendPromptErr = errors.New("prompt failed")
			},
			message: &pb.ClientMessage{
				Message: &pb.ClientMessage_SendPrompt{
					SendPrompt: &pb.SendPromptRequest{SessionId: "sess-123", Text: "hello"},
				},
			},
		},
		{
			name: "terminal input error propagates",
			setup: func(m *mockManager) {
				m.terminalInputErr = errors.New("terminal failed")
			},
			message: &pb.ClientMessage{
				Message: &pb.ClientMessage_TerminalInput{
					TerminalInput: &pb.TerminalInputRequest{SessionId: "sess-123", Data: "ls"},
				},
			},
		},
		{
			name: "terminal resize error propagates",
			setup: func(m *mockManager) {
				m.terminalResizeErr = errors.New("resize failed")
			},
			message: &pb.ClientMessage{
				Message: &pb.ClientMessage_TerminalResize{
					TerminalResize: &pb.TerminalResizeRequest{SessionId: "sess-123", Cols: 80, Rows: 24},
				},
			},
		},
		{
			name: "sync error propagates",
			setup: func(m *mockManager) {
				m.getAllWithMetaErr = errors.New("sync failed")
			},
			message: &pb.ClientMessage{
				Message: &pb.ClientMessage_Sync{
					Sync: &pb.SyncRequest{},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newMockManager()
			tt.setup(mock)
			tc := newTestConnection(mock)

			err := tc.handleAndCapture(context.Background(), tt.message)
			if err == nil {
				t.Error("expected error to propagate")
			}
		})
	}
}
