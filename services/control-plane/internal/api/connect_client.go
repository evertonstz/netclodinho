package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"

	"connectrpc.com/connect"
	pb "github.com/angristan/netclode/services/control-plane/gen/netclode/v1"
	"github.com/angristan/netclode/services/control-plane/gen/netclode/v1/netclodev1connect"
	"github.com/angristan/netclode/services/control-plane/internal/session"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Validation errors for client requests
var (
	errSessionIDRequired = errors.New("sessionId is required")
	errTextRequired      = errors.New("text is required")
	errUnknownMessage    = errors.New("unknown message type")
)

// makeErrorResponse creates a unified error response.
func makeErrorResponse(sessionID, code, message string) *pb.ServerMessage {
	var sessID *string
	if sessionID != "" {
		sessID = &sessionID
	}
	return &pb.ServerMessage{
		Message: &pb.ServerMessage_Error{
			Error: &pb.ErrorResponse{
				Error: &pb.Error{
					Code:      code,
					Message:   message,
					SessionId: sessID,
				},
			},
		},
	}
}

// Ensure ConnectClientServiceHandler implements the interface
var _ netclodev1connect.ClientServiceHandler = (*ConnectClientServiceHandler)(nil)

// subscriptionInfo holds subscription state for a session.
type subscriptionInfo struct {
	sub    *session.StreamSubscriber
	cancel context.CancelFunc
}

// ConnectClientServiceHandler implements the Connect ClientService.
type ConnectClientServiceHandler struct {
	netclodev1connect.UnimplementedClientServiceHandler
	manager *session.Manager
	server  *Server
}

// NewConnectClientServiceHandler creates a new Connect client service handler.
func NewConnectClientServiceHandler(manager *session.Manager, server *Server) *ConnectClientServiceHandler {
	return &ConnectClientServiceHandler{
		manager: manager,
		server:  server,
	}
}

// ConnectConnection represents a Connect bidirectional stream connection.
type ConnectConnection struct {
	stream  *connect.BidiStream[pb.ClientMessage, pb.ServerMessage]
	manager *session.Manager
	server  *Server

	// Redis Streams-based subscriptions
	subscriptions map[string]*subscriptionInfo // sessionID -> subscription info
	subMu         sync.Mutex

	// Global messages channel (session create/delete events)
	globalMessages chan *pb.ServerMessage

	// For graceful shutdown
	done    chan struct{}
	writeMu sync.Mutex
}

// Connect implements the bidirectional streaming RPC using Connect protocol.
func (h *ConnectClientServiceHandler) Connect(ctx context.Context, stream *connect.BidiStream[pb.ClientMessage, pb.ServerMessage]) error {
	conn := &ConnectConnection{
		stream:         stream,
		manager:        h.manager,
		server:         h.server,
		subscriptions:  make(map[string]*subscriptionInfo),
		globalMessages: make(chan *pb.ServerMessage, 64),
		done:           make(chan struct{}),
	}

	// Track connection
	h.server.connectConnections.Store(conn, struct{}{})
	h.server.connCount.Add(1)
	h.server.wg.Add(1)

	slog.Info("Connect connection opened", "activeConnections", h.server.connCount.Load())

	// Start goroutine to forward global messages
	go conn.forwardGlobalMessages()

	// Handle the connection
	err := conn.run(ctx)

	// Cleanup
	conn.close()
	h.server.connectConnections.Delete(conn)
	h.server.connCount.Add(-1)
	h.server.wg.Done()

	slog.Info("Connect connection closed", "activeConnections", h.server.connCount.Load())

	return err
}

// run handles the Connect connection lifecycle.
func (c *ConnectConnection) run(ctx context.Context) error {
	for {
		// Check for shutdown before blocking on Receive
		select {
		case <-c.done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Continue to receive
		}

		// Receive blocks until a message arrives or the stream closes.
		// We check ctx.Done() after receive errors to handle cancellation.
		msg, err := c.stream.Receive()
		if err != nil {
			// Check for graceful close first
			if err == io.EOF {
				return nil
			}
			// Check if context was cancelled while we were blocked
			select {
			case <-c.done:
				return nil
			case <-ctx.Done():
				return nil
			default:
			}
			slog.Warn("Connect recv error", "error", err)
			return err
		}

		if err := c.handleMessage(ctx, msg); err != nil {
			slog.Warn("Connect handler error", "error", err)
			c.send(makeErrorResponse("", "HANDLER_ERROR", err.Error()))
		}
	}
}

// handleMessage dispatches a client message to the appropriate handler.
func (c *ConnectConnection) handleMessage(ctx context.Context, msg *pb.ClientMessage) error {
	switch m := msg.Message.(type) {
	case *pb.ClientMessage_CreateSession:
		return c.handleSessionCreate(ctx, m.CreateSession)
	case *pb.ClientMessage_ListSessions:
		return c.handleSessionList(ctx)
	case *pb.ClientMessage_OpenSession:
		return c.handleSessionOpen(ctx, m.OpenSession)
	case *pb.ClientMessage_ResumeSession:
		return c.handleSessionResume(ctx, m.ResumeSession.SessionId)
	case *pb.ClientMessage_PauseSession:
		return c.handleSessionPause(ctx, m.PauseSession.SessionId)
	case *pb.ClientMessage_DeleteSession:
		return c.handleSessionDelete(ctx, m.DeleteSession.SessionId)
	case *pb.ClientMessage_DeleteAllSessions:
		return c.handleSessionDeleteAll(ctx)
	case *pb.ClientMessage_SendPrompt:
		return c.handlePrompt(ctx, m.SendPrompt.SessionId, m.SendPrompt.Text)
	case *pb.ClientMessage_InterruptPrompt:
		return c.handlePromptInterrupt(ctx, m.InterruptPrompt.SessionId)
	case *pb.ClientMessage_TerminalInput:
		return c.handleTerminalInput(ctx, m.TerminalInput.SessionId, m.TerminalInput.Data)
	case *pb.ClientMessage_TerminalResize:
		return c.handleTerminalResize(ctx, m.TerminalResize)
	case *pb.ClientMessage_Sync:
		return c.handleSync(ctx)
	case *pb.ClientMessage_ExposePort:
		return c.handlePortExpose(ctx, m.ExposePort.SessionId, int(m.ExposePort.Port))
	case *pb.ClientMessage_ListGithubRepos:
		return c.handleGitHubReposList(ctx)
	case *pb.ClientMessage_GitStatus:
		return c.handleGitStatus(ctx, m.GitStatus.SessionId)
	case *pb.ClientMessage_GitDiff:
		return c.handleGitDiff(ctx, m.GitDiff)
	default:
		return connect.NewError(connect.CodeInvalidArgument, errUnknownMessage)
	}
}

// send sends a message to the client.
func (c *ConnectConnection) send(msg *pb.ServerMessage) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.stream.Send(msg)
}

// close closes the connection and cancels all subscriptions.
func (c *ConnectConnection) close() {
	select {
	case <-c.done:
		return
	default:
		close(c.done)
	}

	c.subMu.Lock()
	for _, info := range c.subscriptions {
		info.cancel()
	}
	c.subscriptions = make(map[string]*subscriptionInfo)
	c.subMu.Unlock()
}

// forwardGlobalMessages reads from the global messages channel and sends to stream.
func (c *ConnectConnection) forwardGlobalMessages() {
	for {
		select {
		case msg, ok := <-c.globalMessages:
			if !ok {
				return
			}
			if err := c.send(msg); err != nil {
				slog.Debug("Failed to forward global message", "error", err)
				return
			}
		case <-c.done:
			return
		}
	}
}

// subscribe adds a subscription for a session.
func (c *ConnectConnection) subscribe(_ context.Context, sessionID string, lastNotificationID string) error {
	c.subMu.Lock()
	defer c.subMu.Unlock()

	if old, ok := c.subscriptions[sessionID]; ok {
		old.cancel()
		delete(c.subscriptions, sessionID)
	}

	subCtx, cancel := context.WithCancel(context.Background())

	sub, err := c.manager.Subscribe(subCtx, sessionID, lastNotificationID)
	if err != nil {
		cancel()
		return err
	}

	c.subscriptions[sessionID] = &subscriptionInfo{
		sub:    sub,
		cancel: cancel,
	}

	go c.forwardMessages(sessionID, sub)

	return nil
}

// forwardMessages reads from the StreamSubscriber and sends to stream.
func (c *ConnectConnection) forwardMessages(sessionID string, sub *session.StreamSubscriber) {
	for {
		select {
		case msg, ok := <-sub.Messages():
			if !ok {
				return
			}
			// Messages are already *pb.ServerMessage from subscriber
			if err := c.send(msg); err != nil {
				slog.Debug("Failed to forward message", "sessionID", sessionID, "error", err)
				return
			}
		case <-c.done:
			return
		}
	}
}

// unsubscribe removes a subscription for a session.
func (c *ConnectConnection) unsubscribe(sessionID string) {
	c.subMu.Lock()
	defer c.subMu.Unlock()

	if info, ok := c.subscriptions[sessionID]; ok {
		info.cancel()
		delete(c.subscriptions, sessionID)
	}
}

// Handler implementations

func (c *ConnectConnection) handleSessionCreate(ctx context.Context, req *pb.CreateSessionRequest) error {
	var repoPtr *string
	var repoAccessPtr *pb.RepoAccess
	var sdkTypePtr *pb.SdkType
	var modelPtr *string

	if req.Repo != nil {
		repoPtr = req.Repo
	}
	if req.RepoAccess != nil {
		repoAccessPtr = req.RepoAccess
	}
	if req.SdkType != nil {
		sdkTypePtr = req.SdkType
	}
	if req.Model != nil {
		modelPtr = req.Model
	}

	name := ""
	if req.Name != nil {
		name = *req.Name
	}

	sess, err := c.manager.Create(ctx, name, repoPtr, repoAccessPtr, sdkTypePtr, modelPtr)
	if err != nil {
		return err
	}

	if err := c.subscribe(ctx, sess.Id, "$"); err != nil {
		slog.Warn("Failed to subscribe to new session", "sessionID", sess.Id, "error", err)
	}

	msg := &pb.ServerMessage{
		Message: &pb.ServerMessage_SessionCreated{
			SessionCreated: &pb.SessionCreatedResponse{Session: sess},
		},
	}

	if err := c.send(msg); err != nil {
		return err
	}

	// Broadcast to other clients
	c.server.BroadcastToAllConnect(msg, c)

	// Send initial prompt if provided
	if req.InitialPrompt != nil && *req.InitialPrompt != "" {
		slog.Info("Sending initial prompt", "sessionID", sess.Id)
		if err := c.manager.SendPrompt(ctx, sess.Id, *req.InitialPrompt); err != nil {
			slog.Warn("Failed to send initial prompt", "sessionID", sess.Id, "error", err)
		}
	}

	return nil
}

func (c *ConnectConnection) handleSessionList(ctx context.Context) error {
	sessions, err := c.manager.List(ctx)
	if err != nil {
		return err
	}

	// Sessions are already pb.Session, just need to convert to pointers
	pbSessions := make([]*pb.Session, len(sessions))
	for i := range sessions {
		pbSessions[i] = &sessions[i]
	}

	return c.send(&pb.ServerMessage{
		Message: &pb.ServerMessage_SessionList{
			SessionList: &pb.SessionListResponse{Sessions: pbSessions},
		},
	})
}

func (c *ConnectConnection) handleSessionOpen(ctx context.Context, req *pb.OpenSessionRequest) error {
	var lastMsgID, lastNotifID *string
	if req.LastMessageId != nil {
		lastMsgID = req.LastMessageId
	}
	if req.LastNotificationId != nil {
		lastNotifID = req.LastNotificationId
	}
	_ = lastMsgID // unused for now

	sess, messages, events, hasMore, currentNotificationID, err := c.manager.GetWithHistory(ctx, req.SessionId, 100)
	if err != nil {
		return err
	}

	cursor := currentNotificationID
	if lastNotifID != nil && *lastNotifID != "" {
		cursor = *lastNotifID
	}

	if err := c.subscribe(ctx, req.SessionId, cursor); err != nil {
		slog.Warn("Failed to subscribe to opened session", "sessionID", req.SessionId, "error", err)
	}

	// Messages and Events are already pb.Message and pb.Event, convert to pointers
	pbMessages := make([]*pb.Message, len(messages))
	for i := range messages {
		pbMessages[i] = &messages[i]
	}

	pbEvents := make([]*pb.Event, len(events))
	for i := range events {
		pbEvents[i] = &events[i]
	}

	resp := &pb.SessionStateResponse{
		Session:            sess,
		Messages:           pbMessages,
		Events:             pbEvents,
		HasMore:            hasMore,
		LastNotificationId: &currentNotificationID,
	}

	return c.send(&pb.ServerMessage{
		Message: &pb.ServerMessage_SessionState{SessionState: resp},
	})
}

func (c *ConnectConnection) handleSessionResume(ctx context.Context, id string) error {
	sess, err := c.manager.Resume(ctx, id)
	if err != nil {
		return err
	}

	if err := c.subscribe(ctx, id, "$"); err != nil {
		slog.Warn("Failed to subscribe to resumed session", "sessionID", id, "error", err)
	}

	msg := &pb.ServerMessage{
		Message: &pb.ServerMessage_SessionUpdated{
			SessionUpdated: &pb.SessionUpdatedResponse{Session: sess},
		},
	}

	if err := c.send(msg); err != nil {
		return err
	}

	c.server.BroadcastToAllConnect(msg, c)
	return nil
}

func (c *ConnectConnection) handleSessionPause(ctx context.Context, id string) error {
	sess, err := c.manager.Pause(ctx, id)
	if err != nil {
		return err
	}

	c.unsubscribe(id)

	msg := &pb.ServerMessage{
		Message: &pb.ServerMessage_SessionUpdated{
			SessionUpdated: &pb.SessionUpdatedResponse{Session: sess},
		},
	}

	if err := c.send(msg); err != nil {
		return err
	}

	c.server.BroadcastToAllConnect(msg, c)
	return nil
}

func (c *ConnectConnection) handleSessionDelete(ctx context.Context, id string) error {
	c.unsubscribe(id)

	if err := c.manager.Delete(ctx, id); err != nil {
		return err
	}

	msg := &pb.ServerMessage{
		Message: &pb.ServerMessage_SessionDeleted{
			SessionDeleted: &pb.SessionDeletedResponse{SessionId: id},
		},
	}

	if err := c.send(msg); err != nil {
		return err
	}

	c.server.BroadcastToAllConnect(msg, c)
	return nil
}

func (c *ConnectConnection) handleSessionDeleteAll(ctx context.Context) error {
	deletedIDs, err := c.manager.DeleteAll(ctx)
	if err != nil {
		slog.Warn("Some sessions failed to delete", "error", err)
	}

	msg := &pb.ServerMessage{
		Message: &pb.ServerMessage_SessionsDeletedAll{
			SessionsDeletedAll: &pb.SessionsDeletedAllResponse{DeletedIds: deletedIDs},
		},
	}

	if err := c.send(msg); err != nil {
		return err
	}

	c.server.BroadcastToAllConnect(msg, c)
	return nil
}

func (c *ConnectConnection) handlePrompt(ctx context.Context, sessionID, text string) error {
	if sessionID == "" {
		return connect.NewError(connect.CodeInvalidArgument, errSessionIDRequired)
	}
	if text == "" {
		return connect.NewError(connect.CodeInvalidArgument, errTextRequired)
	}

	return c.manager.SendPrompt(ctx, sessionID, text)
}

func (c *ConnectConnection) handlePromptInterrupt(ctx context.Context, sessionID string) error {
	return c.manager.Interrupt(ctx, sessionID)
}

func (c *ConnectConnection) handleTerminalInput(ctx context.Context, sessionID, data string) error {
	if sessionID == "" {
		return connect.NewError(connect.CodeInvalidArgument, errSessionIDRequired)
	}
	if data == "" {
		return nil // Empty input is a no-op
	}

	return c.manager.SendTerminalInput(ctx, sessionID, data)
}

var errInvalidTerminalDimensions = errors.New("cols and rows must be positive integers")

func (c *ConnectConnection) handleTerminalResize(ctx context.Context, req *pb.TerminalResizeRequest) error {
	if req.SessionId == "" {
		return connect.NewError(connect.CodeInvalidArgument, errSessionIDRequired)
	}
	if req.Cols <= 0 || req.Rows <= 0 {
		return connect.NewError(connect.CodeInvalidArgument, errInvalidTerminalDimensions)
	}

	return c.manager.ResizeTerminal(ctx, req.SessionId, int(req.Cols), int(req.Rows))
}

func (c *ConnectConnection) handleSync(ctx context.Context) error {
	sessions, err := c.manager.GetAllWithMeta(ctx)
	if err != nil {
		return err
	}

	// Sessions are already pb.SessionSummary, just need to convert to pointers
	pbSessions := make([]*pb.SessionSummary, len(sessions))
	for i := range sessions {
		pbSessions[i] = &sessions[i]
	}

	return c.send(&pb.ServerMessage{
		Message: &pb.ServerMessage_SyncResponse{
			SyncResponse: &pb.SyncResponse{
				Sessions:   pbSessions,
				ServerTime: timestamppb.Now(),
			},
		},
	})
}

func (c *ConnectConnection) handlePortExpose(ctx context.Context, sessionID string, port int) error {
	if sessionID == "" {
		return c.send(makeErrorResponse(sessionID, "PORT_ERROR", "sessionId is required"))
	}
	if port < 1 || port > 65535 {
		return c.send(makeErrorResponse(sessionID, "PORT_ERROR", "port must be between 1 and 65535"))
	}

	previewURL, err := c.manager.ExposePort(ctx, sessionID, port)
	if err != nil {
		return c.send(makeErrorResponse(sessionID, "PORT_ERROR", err.Error()))
	}

	return c.send(&pb.ServerMessage{
		Message: &pb.ServerMessage_PortExposed{
			PortExposed: &pb.PortExposedResponse{
				SessionId:  sessionID,
				Port:       int32(port),
				PreviewUrl: previewURL,
			},
		},
	})
}

func (c *ConnectConnection) handleGitHubReposList(ctx context.Context) error {
	repos, err := c.manager.ListGitHubRepos(ctx)
	if err != nil {
		return c.send(makeErrorResponse("", "GITHUB_ERROR", "Failed to list GitHub repositories: "+err.Error()))
	}

	pbRepos := make([]*pb.GitHubRepo, len(repos))
	for i, r := range repos {
		pbRepos[i] = &pb.GitHubRepo{
			Name:     r.Name,
			FullName: r.FullName,
			Private:  r.Private,
		}
		if r.Description != "" {
			pbRepos[i].Description = &r.Description
		}
	}

	return c.send(&pb.ServerMessage{
		Message: &pb.ServerMessage_GithubRepos{
			GithubRepos: &pb.GitHubReposResponse{Repos: pbRepos},
		},
	})
}

func (c *ConnectConnection) handleGitStatus(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return c.send(makeErrorResponse(sessionID, "GIT_ERROR", "sessionId is required"))
	}

	files, err := c.manager.GetGitStatus(ctx, sessionID)
	if err != nil {
		return c.send(makeErrorResponse(sessionID, "GIT_ERROR", err.Error()))
	}

	// Files are already pb.GitFileChange, convert to pointers
	pbFiles := make([]*pb.GitFileChange, len(files))
	for i := range files {
		pbFiles[i] = &files[i]
	}

	return c.send(&pb.ServerMessage{
		Message: &pb.ServerMessage_GitStatus{
			GitStatus: &pb.GitStatusResponse{SessionId: sessionID, Files: pbFiles},
		},
	})
}

func (c *ConnectConnection) handleGitDiff(ctx context.Context, req *pb.GitDiffRequest) error {
	if req.SessionId == "" {
		return c.send(makeErrorResponse(req.SessionId, "GIT_ERROR", "sessionId is required"))
	}

	file := ""
	if req.File != nil {
		file = *req.File
	}

	diff, err := c.manager.GetGitDiff(ctx, req.SessionId, file)
	if err != nil {
		return c.send(makeErrorResponse(req.SessionId, "GIT_ERROR", err.Error()))
	}

	return c.send(&pb.ServerMessage{
		Message: &pb.ServerMessage_GitDiff{
			GitDiff: &pb.GitDiffResponse{SessionId: req.SessionId, Diff: diff},
		},
	})
}

// All conversion helpers have been removed since we now use proto types (pb.*) directly.
// The Manager now returns *pb.Session, pb.Message, pb.Event, etc.
// The StreamSubscriber now returns *pb.ServerMessage directly.
