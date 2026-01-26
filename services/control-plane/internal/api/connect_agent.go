package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"

	"connectrpc.com/connect"
	v1 "github.com/angristan/netclode/services/control-plane/gen/netclode/v1"
	"github.com/angristan/netclode/services/control-plane/gen/netclode/v1/netclodev1connect"
	"github.com/angristan/netclode/services/control-plane/internal/session"
)

// Ensure ConnectAgentServiceHandler implements the interface
var _ netclodev1connect.AgentServiceHandler = (*ConnectAgentServiceHandler)(nil)

// ConnectAgentServiceHandler implements the Connect AgentService.
type ConnectAgentServiceHandler struct {
	netclodev1connect.UnimplementedAgentServiceHandler
	manager *session.Manager
	server  *Server
}

// NewConnectAgentServiceHandler creates a new Connect agent service handler.
func NewConnectAgentServiceHandler(manager *session.Manager, server *Server) *ConnectAgentServiceHandler {
	return &ConnectAgentServiceHandler{
		manager: manager,
		server:  server,
	}
}

// AgentConnection represents a connected agent's bidirectional stream.
type AgentConnection struct {
	stream    *connect.BidiStream[v1.AgentMessage, v1.ControlPlaneMessage]
	manager   *session.Manager
	server    *Server
	sessionID string

	// For sending messages to agent
	outbound chan *v1.ControlPlaneMessage
	done     chan struct{}
	writeMu  sync.Mutex
}

// Connect implements the bidirectional streaming RPC for agents.
func (h *ConnectAgentServiceHandler) Connect(ctx context.Context, stream *connect.BidiStream[v1.AgentMessage, v1.ControlPlaneMessage]) error {
	conn := &AgentConnection{
		stream:   stream,
		manager:  h.manager,
		server:   h.server,
		outbound: make(chan *v1.ControlPlaneMessage, 64),
		done:     make(chan struct{}),
	}

	// Wait for registration message first
	msg, err := stream.Receive()
	if err != nil {
		slog.Warn("Agent connection failed before registration", "error", err)
		return err
	}

	reg := msg.GetRegister()
	if reg == nil {
		return errors.New("first message must be AgentRegister")
	}

	conn.sessionID = reg.SessionId
	slog.Info("Agent connecting", "sessionID", conn.sessionID, "version", reg.Version)

	// Get session config
	config, err := h.manager.GetSessionConfig(ctx, conn.sessionID)
	if err != nil {
		slog.Warn("Agent registration failed - session not found", "sessionID", conn.sessionID, "error", err)
		conn.send(&v1.ControlPlaneMessage{
			Message: &v1.ControlPlaneMessage_Registered{
				Registered: &v1.AgentRegistered{
					Success: false,
					Error:   strPtr("session not found: " + err.Error()),
				},
			},
		})
		return err
	}

	// Send registration success with config
	sessionConfig := &v1.SessionConfig{
		SessionId:       conn.sessionID,
		WorkspaceDir:    "/workspace",
		ControlPlaneUrl: "http://control-plane.netclode.svc.cluster.local",
		SdkType:         config.SdkType,
		CopilotBackend:  config.CopilotBackend,
	}
	if config.Repo != "" {
		sessionConfig.Repo = &config.Repo
	}
	if config.GitHubToken != "" {
		sessionConfig.GithubToken = &config.GitHubToken
	}
	if config.GitHubCopilotToken != "" {
		sessionConfig.GithubCopilotToken = &config.GitHubCopilotToken
	}
	if config.Model != "" {
		sessionConfig.Model = &config.Model
	}

	if err := conn.send(&v1.ControlPlaneMessage{
		Message: &v1.ControlPlaneMessage_Registered{
			Registered: &v1.AgentRegistered{
				Success: true,
				Config:  sessionConfig,
			},
		},
	}); err != nil {
		return err
	}

	// Register this agent connection with the session manager
	h.manager.RegisterAgentConnection(conn.sessionID, conn)
	defer h.manager.UnregisterAgentConnection(conn.sessionID)

	slog.Info("Agent registered", "sessionID", conn.sessionID)

	// Start outbound message sender
	go conn.sendLoop()

	// Handle incoming messages from agent
	err = conn.receiveLoop(ctx)

	close(conn.done)
	slog.Info("Agent disconnected", "sessionID", conn.sessionID)

	return err
}

// receiveLoop handles incoming messages from the agent.
func (c *AgentConnection) receiveLoop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.done:
			return nil
		default:
		}

		msg, err := c.stream.Receive()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			slog.Warn("Agent recv error", "sessionID", c.sessionID, "error", err)
			return err
		}

		if err := c.handleMessage(ctx, msg); err != nil {
			slog.Warn("Agent message handler error", "sessionID", c.sessionID, "error", err)
		}
	}
}

// handleMessage processes an incoming message from the agent.
func (c *AgentConnection) handleMessage(ctx context.Context, msg *v1.AgentMessage) error {
	switch m := msg.Message.(type) {
	case *v1.AgentMessage_PromptResponse:
		return c.handlePromptResponse(ctx, m.PromptResponse)
	case *v1.AgentMessage_TerminalOutput:
		return c.handleTerminalOutput(ctx, m.TerminalOutput)
	case *v1.AgentMessage_TitleResponse:
		return c.handleTitleResponse(ctx, m.TitleResponse)
	case *v1.AgentMessage_GitStatusResponse:
		return c.handleGitStatusResponse(ctx, m.GitStatusResponse)
	case *v1.AgentMessage_GitDiffResponse:
		return c.handleGitDiffResponse(ctx, m.GitDiffResponse)
	default:
		slog.Warn("Unknown agent message type", "sessionID", c.sessionID)
		return nil
	}
}

// handlePromptResponse forwards agent streaming responses to clients.
func (c *AgentConnection) handlePromptResponse(ctx context.Context, resp *v1.AgentStreamResponse) error {
	return c.manager.HandleAgentResponse(ctx, c.sessionID, resp)
}

// handleTerminalOutput forwards terminal output to clients.
func (c *AgentConnection) handleTerminalOutput(ctx context.Context, output *v1.AgentTerminalOutput) error {
	return c.manager.HandleTerminalOutput(ctx, c.sessionID, output.Data)
}

// handleTitleResponse handles title generation response.
func (c *AgentConnection) handleTitleResponse(ctx context.Context, resp *v1.AgentTitleResponse) error {
	return c.manager.HandleTitleResponse(ctx, c.sessionID, resp.RequestId, resp.Title)
}

// handleGitStatusResponse handles git status response.
func (c *AgentConnection) handleGitStatusResponse(ctx context.Context, resp *v1.AgentGitStatusResponse) error {
	return c.manager.HandleGitStatusResponse(ctx, c.sessionID, resp.RequestId, resp.Files)
}

// handleGitDiffResponse handles git diff response.
func (c *AgentConnection) handleGitDiffResponse(ctx context.Context, resp *v1.AgentGitDiffResponse) error {
	return c.manager.HandleGitDiffResponse(ctx, c.sessionID, resp.RequestId, resp.Diff)
}

// sendLoop sends outbound messages to the agent.
func (c *AgentConnection) sendLoop() {
	for {
		select {
		case <-c.done:
			return
		case msg := <-c.outbound:
			if err := c.send(msg); err != nil {
				slog.Warn("Failed to send to agent", "sessionID", c.sessionID, "error", err)
				return
			}
		}
	}
}

// send sends a message to the agent.
func (c *AgentConnection) send(msg *v1.ControlPlaneMessage) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.stream.Send(msg)
}

// Send queues a message to be sent to the agent (called by session manager).
func (c *AgentConnection) Send(msg *v1.ControlPlaneMessage) error {
	select {
	case <-c.done:
		return errors.New("agent connection closed")
	case c.outbound <- msg:
		return nil
	default:
		return errors.New("agent outbound channel full")
	}
}

// ExecutePrompt sends a prompt execution request to the agent.
func (c *AgentConnection) ExecutePrompt(text string) error {
	return c.Send(&v1.ControlPlaneMessage{
		Message: &v1.ControlPlaneMessage_ExecutePrompt{
			ExecutePrompt: &v1.ExecutePromptRequest{
				Text: text,
			},
		},
	})
}

// Interrupt sends an interrupt request to the agent.
func (c *AgentConnection) Interrupt() error {
	return c.Send(&v1.ControlPlaneMessage{
		Message: &v1.ControlPlaneMessage_Interrupt{
			Interrupt: &v1.InterruptRequest{},
		},
	})
}

// GenerateTitle sends a title generation request to the agent.
func (c *AgentConnection) GenerateTitle(requestID, prompt string) error {
	return c.Send(&v1.ControlPlaneMessage{
		Message: &v1.ControlPlaneMessage_GenerateTitle{
			GenerateTitle: &v1.GenerateTitleRequest{
				RequestId: requestID,
				Prompt:    prompt,
			},
		},
	})
}

// GetGitStatus sends a git status request to the agent.
func (c *AgentConnection) GetGitStatus(requestID string) error {
	return c.Send(&v1.ControlPlaneMessage{
		Message: &v1.ControlPlaneMessage_GetGitStatus{
			GetGitStatus: &v1.GetGitStatusRequest{
				RequestId: requestID,
			},
		},
	})
}

// GetGitDiff sends a git diff request to the agent.
func (c *AgentConnection) GetGitDiff(requestID string, file *string) error {
	return c.Send(&v1.ControlPlaneMessage{
		Message: &v1.ControlPlaneMessage_GetGitDiff{
			GetGitDiff: &v1.GetGitDiffRequest{
				RequestId: requestID,
				File:      file,
			},
		},
	})
}

// SendTerminalInput sends terminal input to the agent.
func (c *AgentConnection) SendTerminalInput(data string) error {
	return c.Send(&v1.ControlPlaneMessage{
		Message: &v1.ControlPlaneMessage_TerminalInput{
			TerminalInput: &v1.AgentTerminalInput{
				Input: &v1.AgentTerminalInput_Data{
					Data: data,
				},
			},
		},
	})
}

// ResizeTerminal sends a terminal resize request to the agent.
func (c *AgentConnection) ResizeTerminal(cols, rows int) error {
	return c.Send(&v1.ControlPlaneMessage{
		Message: &v1.ControlPlaneMessage_TerminalInput{
			TerminalInput: &v1.AgentTerminalInput{
				Input: &v1.AgentTerminalInput_Resize{
					Resize: &v1.AgentTerminalResize{
						Cols: int32(cols),
						Rows: int32(rows),
					},
				},
			},
		},
	})
}

// UpdateGitCredentials sends updated git credentials to the agent.
func (c *AgentConnection) UpdateGitCredentials(token string, repoAccess v1.RepoAccess) error {
	return c.Send(&v1.ControlPlaneMessage{
		Message: &v1.ControlPlaneMessage_UpdateGitCredentials{
			UpdateGitCredentials: &v1.UpdateGitCredentials{
				GithubToken: token,
				RepoAccess:  repoAccess,
			},
		},
	})
}

func strPtr(s string) *string {
	return &s
}
