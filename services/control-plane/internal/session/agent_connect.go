package session

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"
	v1 "github.com/angristan/netclode/services/control-plane/gen/netclode/v1"
	"github.com/angristan/netclode/services/control-plane/gen/netclode/v1/netclodev1connect"
	"github.com/angristan/netclode/services/control-plane/internal/protocol"
	"github.com/google/uuid"
)

// callAgentPromptConnect sends a prompt to the agent using the Connect protocol.
func (m *Manager) callAgentPromptConnect(ctx context.Context, sessionID, fqdn, text string) {
	baseURL := fmt.Sprintf("http://%s:%d", fqdn, m.config.AgentPort)
	client := netclodev1connect.NewAgentServiceClient(http.DefaultClient, baseURL)

	// Build session config
	var config *v1.SessionConfig
	if cfg, err := m.GetSessionConfig(ctx, sessionID); err == nil {
		config = &v1.SessionConfig{}
		if repo := cfg["GIT_REPO"]; repo != "" {
			config.Repo = &repo
		}
		if token := cfg["GITHUB_TOKEN"]; token != "" {
			config.GithubToken = &token
		}
	}

	req := connect.NewRequest(&v1.ExecutePromptRequest{
		SessionId: sessionID,
		Text:      text,
		Config:    config,
	})

	stream, err := client.ExecutePrompt(ctx, req)
	if err != nil {
		m.handleAgentError(ctx, sessionID, fmt.Errorf("connect ExecutePrompt: %w", err))
		return
	}
	defer stream.Close()

	if err := m.streamConnectResponse(ctx, sessionID, fqdn, text, stream); err != nil {
		m.handleAgentError(ctx, sessionID, err)
		return
	}

	// Mark session as ready
	m.updateSessionStatus(ctx, sessionID, protocol.StatusReady)

	// Emit done
	m.emit(ctx, sessionID, protocol.NewAgentDone(sessionID))
}

func (m *Manager) streamConnectResponse(
	ctx context.Context,
	sessionID, fqdn, originalPrompt string,
	stream *connect.ServerStreamForClient[v1.AgentStreamResponse],
) error {
	var contentBuilder strings.Builder
	messageID := "msg_" + uuid.NewString()[:12]
	titleGenerated := false
	thinkingBuffers := make(map[string]string)

	for stream.Receive() {
		msg := stream.Msg()

		switch resp := msg.Response.(type) {
		case *v1.AgentStreamResponse_TextDelta:
			delta := resp.TextDelta
			if delta.Partial {
				contentBuilder.WriteString(delta.Content)
				m.emit(ctx, sessionID, protocol.NewAgentMessage(sessionID, delta.Content, true, messageID))
			} else {
				if delta.Content != "" {
					contentBuilder.Reset()
					contentBuilder.WriteString(delta.Content)
				}
				finalContent := contentBuilder.String()
				m.emit(ctx, sessionID, protocol.NewAgentMessage(sessionID, finalContent, false, messageID))

				// Persist assistant message
				assistantMsg := &protocol.PersistedMessage{
					ID:        messageID,
					SessionID: sessionID,
					Role:      protocol.RoleAssistant,
					Content:   finalContent,
					Timestamp: time.Now().UTC().Format(time.RFC3339),
				}
				if err := m.storage.AppendMessage(ctx, assistantMsg); err != nil {
					slog.Warn("Failed to persist assistant message", "sessionID", sessionID, "error", err)
				}

				// Trigger title generation on first complete message
				if !titleGenerated {
					titleGenerated = true
					go m.generateSessionTitleConnect(context.Background(), sessionID, fqdn, originalPrompt)
				}

				// Reset for next message
				contentBuilder.Reset()
				messageID = "msg_" + uuid.NewString()[:12]
			}

		case *v1.AgentStreamResponse_Event:
			event := convertAgentEvent(resp.Event)

			// Inject preview URL for port_exposed events
			if event.Kind == protocol.EventKindPortExposed && event.Port > 0 {
				previewURL := fmt.Sprintf("http://sandbox-%s:%d", sessionID, event.Port)
				event.PreviewURL = &previewURL

				if err := m.k8s.ExposePort(ctx, sessionID, event.Port); err != nil {
					slog.Warn("Failed to expose port", "sessionID", sessionID, "port", event.Port, "error", err)
				}
			}

			// Handle persistence
			shouldPersist := true
			switch event.Kind {
			case protocol.EventKindToolInput:
				shouldPersist = false
			case protocol.EventKindThinking:
				if event.Partial {
					thinkingBuffers[event.ThinkingID] += event.Content
					shouldPersist = false
				} else {
					if accumulated, ok := thinkingBuffers[event.ThinkingID]; ok {
						event.Content = accumulated
						delete(thinkingBuffers, event.ThinkingID)
					}
				}
			}

			if shouldPersist {
				persistedEvent := &protocol.PersistedEvent{
					ID:        "evt_" + uuid.NewString()[:12],
					SessionID: sessionID,
					Event:     event,
					Timestamp: time.Now().UTC().Format(time.RFC3339),
				}
				if err := m.storage.AppendEvent(ctx, persistedEvent); err != nil {
					slog.Warn("Failed to persist event", "sessionID", sessionID, "kind", event.Kind, "error", err)
				}
			}

			m.emit(ctx, sessionID, protocol.NewAgentEvent(sessionID, &event))

		case *v1.AgentStreamResponse_Error:
			errMsg := resp.Error.Message
			m.emit(ctx, sessionID, protocol.NewAgentError(sessionID, errMsg))

		case *v1.AgentStreamResponse_SystemMessage, *v1.AgentStreamResponse_Result:
			// Ignored
		}
	}

	return stream.Err()
}

func convertAgentEvent(e *v1.AgentEvent) protocol.AgentEvent {
	event := protocol.AgentEvent{
		Kind:      convertEventKind(e.Kind),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	if e.Timestamp != nil {
		event.Timestamp = e.Timestamp.AsTime().Format(time.RFC3339)
	}

	// Tool events
	if e.Tool != nil {
		event.Tool = *e.Tool
	}
	if e.ToolUseId != nil {
		event.ToolUseID = *e.ToolUseId
	}
	if e.ParentToolUseId != nil {
		event.ParentToolUseID = *e.ParentToolUseId
	}
	if e.Input != nil {
		event.Input = e.Input.AsMap()
	}
	if e.InputDelta != nil {
		event.InputDelta = *e.InputDelta
	}
	if e.Result != nil {
		event.Result = e.Result
	}
	if e.Error != nil {
		event.Error = e.Error
	}

	// File change events
	if e.Path != nil {
		event.Path = *e.Path
	}
	if e.Action != nil {
		event.Action = *e.Action
	}
	if e.LinesAdded != nil {
		i := int(*e.LinesAdded)
		event.LinesAdded = &i
	}
	if e.LinesRemoved != nil {
		i := int(*e.LinesRemoved)
		event.LinesRemoved = &i
	}

	// Command events
	if e.Command != nil {
		event.Command = *e.Command
	}
	if e.Cwd != nil {
		event.Cwd = e.Cwd
	}
	if e.ExitCode != nil {
		i := int(*e.ExitCode)
		event.ExitCode = &i
	}
	if e.Output != nil {
		event.Output = e.Output
	}

	// Thinking events
	if e.Content != nil {
		event.Content = *e.Content
	}
	if e.ThinkingId != nil {
		event.ThinkingID = *e.ThinkingId
	}
	if e.Partial != nil {
		event.Partial = *e.Partial
	}

	// Port detected events
	if e.Port != nil {
		event.Port = int(*e.Port)
	}
	if e.Process != nil {
		event.Process = e.Process
	}
	if e.PreviewUrl != nil {
		event.PreviewURL = e.PreviewUrl
	}

	// Repo clone events
	if e.Repo != nil {
		event.Repo = *e.Repo
	}
	if e.Stage != nil {
		event.Stage = *e.Stage
	}
	if e.Message != nil {
		event.Message = *e.Message
	}

	return event
}

func convertEventKind(k v1.AgentEventKind) protocol.AgentEventKind {
	switch k {
	case v1.AgentEventKind_AGENT_EVENT_KIND_TOOL_START:
		return protocol.EventKindToolStart
	case v1.AgentEventKind_AGENT_EVENT_KIND_TOOL_END:
		return protocol.EventKindToolEnd
	case v1.AgentEventKind_AGENT_EVENT_KIND_TOOL_INPUT:
		return protocol.EventKindToolInput
	case v1.AgentEventKind_AGENT_EVENT_KIND_TOOL_INPUT_COMPLETE:
		return protocol.EventKindToolInputComplete
	case v1.AgentEventKind_AGENT_EVENT_KIND_FILE_CHANGE:
		return protocol.EventKindFileChange
	case v1.AgentEventKind_AGENT_EVENT_KIND_COMMAND_START:
		return protocol.EventKindCommandStart
	case v1.AgentEventKind_AGENT_EVENT_KIND_COMMAND_END:
		return protocol.EventKindCommandEnd
	case v1.AgentEventKind_AGENT_EVENT_KIND_THINKING:
		return protocol.EventKindThinking
	case v1.AgentEventKind_AGENT_EVENT_KIND_PORT_EXPOSED:
		return protocol.EventKindPortExposed
	case v1.AgentEventKind_AGENT_EVENT_KIND_REPO_CLONE:
		return protocol.EventKindRepoClone
	default:
		return protocol.AgentEventKind(k.String())
	}
}

// generateSessionTitleConnect calls the agent to generate a session title using Connect.
func (m *Manager) generateSessionTitleConnect(ctx context.Context, sessionID, fqdn, prompt string) {
	baseURL := fmt.Sprintf("http://%s:%d", fqdn, m.config.AgentPort)
	client := netclodev1connect.NewAgentServiceClient(http.DefaultClient, baseURL)

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := client.GenerateTitle(ctx, connect.NewRequest(&v1.GenerateTitleRequest{
		Prompt: prompt,
	}))
	if err != nil {
		slog.Warn("Failed to generate title via Connect", "sessionID", sessionID, "error", err)
		return
	}

	if resp.Msg.Title != "" {
		m.updateSessionName(ctx, sessionID, resp.Msg.Title)
		slog.Info("Session title generated", "sessionID", sessionID, "title", resp.Msg.Title)
	}
}

// GetGitStatusConnect fetches git status from the agent using Connect.
func (m *Manager) GetGitStatusConnect(ctx context.Context, sessionID string) ([]protocol.GitFileChange, error) {
	state := m.getState(sessionID)
	if state == nil {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}

	if state.ServiceFQDN == "" {
		return nil, fmt.Errorf("session %s is not running", sessionID)
	}

	baseURL := fmt.Sprintf("http://%s:%d", state.ServiceFQDN, m.config.AgentPort)
	client := netclodev1connect.NewAgentServiceClient(http.DefaultClient, baseURL)

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := client.GetGitStatus(ctx, connect.NewRequest(&v1.GetGitStatusRequest{}))
	if err != nil {
		return nil, fmt.Errorf("connect GetGitStatus: %w", err)
	}

	files := make([]protocol.GitFileChange, len(resp.Msg.Files))
	for i, f := range resp.Msg.Files {
		files[i] = protocol.GitFileChange{
			Path:   f.Path,
			Status: convertGitStatus(f.Status),
			Staged: f.Staged,
		}
	}

	return files, nil
}

func convertGitStatus(s v1.GitFileStatus) string {
	switch s {
	case v1.GitFileStatus_GIT_FILE_STATUS_MODIFIED:
		return "modified"
	case v1.GitFileStatus_GIT_FILE_STATUS_ADDED:
		return "added"
	case v1.GitFileStatus_GIT_FILE_STATUS_DELETED:
		return "deleted"
	case v1.GitFileStatus_GIT_FILE_STATUS_RENAMED:
		return "renamed"
	case v1.GitFileStatus_GIT_FILE_STATUS_UNTRACKED:
		return "untracked"
	case v1.GitFileStatus_GIT_FILE_STATUS_COPIED:
		return "copied"
	case v1.GitFileStatus_GIT_FILE_STATUS_IGNORED:
		return "ignored"
	case v1.GitFileStatus_GIT_FILE_STATUS_UNMERGED:
		return "unmerged"
	default:
		return "unknown"
	}
}

// GetGitDiffConnect fetches git diff from the agent using Connect.
func (m *Manager) GetGitDiffConnect(ctx context.Context, sessionID, file string) (string, error) {
	state := m.getState(sessionID)
	if state == nil {
		return "", fmt.Errorf("session %s not found", sessionID)
	}

	if state.ServiceFQDN == "" {
		return "", fmt.Errorf("session %s is not running", sessionID)
	}

	baseURL := fmt.Sprintf("http://%s:%d", state.ServiceFQDN, m.config.AgentPort)
	client := netclodev1connect.NewAgentServiceClient(http.DefaultClient, baseURL)

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req := &v1.GetGitDiffRequest{}
	if file != "" {
		req.File = &file
	}

	resp, err := client.GetGitDiff(ctx, connect.NewRequest(req))
	if err != nil {
		return "", fmt.Errorf("connect GetGitDiff: %w", err)
	}

	return resp.Msg.Diff, nil
}

// InterruptConnect sends an interrupt signal to the agent using Connect.
func (m *Manager) InterruptConnect(ctx context.Context, sessionID string) error {
	state := m.getState(sessionID)
	if state == nil {
		return fmt.Errorf("session %s not found", sessionID)
	}

	if state.ServiceFQDN == "" {
		return nil
	}

	baseURL := fmt.Sprintf("http://%s:%d", state.ServiceFQDN, m.config.AgentPort)
	client := netclodev1connect.NewAgentServiceClient(http.DefaultClient, baseURL)

	ctx, cancel := context.WithTimeout(ctx, interruptTimeout)
	defer cancel()

	_, err := client.Interrupt(ctx, connect.NewRequest(&v1.InterruptRequest{}))
	if err != nil {
		slog.Warn("Failed to interrupt agent via Connect", "sessionID", sessionID, "error", err)
		return nil
	}

	return nil
}
