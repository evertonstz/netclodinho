package session

import (
	"context"
	"log/slog"
	"time"

	pb "github.com/angristan/netclode/services/control-plane/gen/netclode/v1"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// RoleAssistant is the role constant for assistant messages.
const RoleAssistant = pb.MessageRole_MESSAGE_ROLE_ASSISTANT

// HandleAgentResponse processes streaming responses from the agent.
func (m *Manager) HandleAgentResponse(ctx context.Context, sessionID string, resp *pb.AgentStreamResponse) error {
	state := m.getState(sessionID)
	if state == nil {
		return nil
	}

	switch r := resp.Response.(type) {
	case *pb.AgentStreamResponse_TextDelta:
		return m.handleTextDelta(ctx, sessionID, state, r.TextDelta)
	case *pb.AgentStreamResponse_Event:
		return m.handleAgentEvent(ctx, sessionID, state, r.Event)
	case *pb.AgentStreamResponse_SystemMessage:
		slog.Debug("Agent system message", "sessionID", sessionID, "message", r.SystemMessage.Message)
		return nil
	case *pb.AgentStreamResponse_Result:
		return m.handleAgentResult(ctx, sessionID, state, r.Result)
	case *pb.AgentStreamResponse_Error:
		return m.handleAgentStreamError(ctx, sessionID, state, r.Error)
	default:
		slog.Warn("Unknown agent response type", "sessionID", sessionID)
		return nil
	}
}

// handleTextDelta processes text delta from agent streaming.
func (m *Manager) handleTextDelta(ctx context.Context, sessionID string, state *SessionState, delta *pb.AgentTextDelta) error {
	m.mu.Lock()
	// Use message ID from delta if provided, otherwise use current
	messageID := delta.MessageId
	if messageID == "" {
		messageID = state.CurrentMessageID
	}
	if messageID == "" {
		messageID = "msg_" + uuid.NewString()[:12]
		state.CurrentMessageID = messageID
	}

	state.ContentBuilder.WriteString(delta.Content)
	m.mu.Unlock()

	// Emit delta to clients (not accumulated content) - client accumulates
	m.emitAgentMessage(ctx, sessionID, messageID, delta.Content, delta.Partial)

	return nil
}

// handleAgentEvent processes events from agent execution.
func (m *Manager) handleAgentEvent(ctx context.Context, sessionID string, state *SessionState, event *pb.AgentEvent) error {
	if event == nil {
		return nil
	}

	// Persist the event
	persistedEvent := &pb.Event{
		Id:        "evt_" + uuid.NewString()[:12],
		Timestamp: timestamppb.Now(),
		Event:     event,
	}

	if err := m.storage.AppendEvent(ctx, sessionID, persistedEvent); err != nil {
		slog.Warn("Failed to persist agent event", "sessionID", sessionID, "error", err)
	}

	// Emit to clients
	m.emitAgentEvent(ctx, sessionID, event)

	return nil
}

// handleAgentResult processes the final result from agent execution.
func (m *Manager) handleAgentResult(ctx context.Context, sessionID string, state *SessionState, result *pb.AgentResult) error {
	m.mu.Lock()
	content := state.ContentBuilder.String()
	messageID := state.CurrentMessageID
	originalPrompt := state.OriginalPrompt
	titleGenerated := state.TitleGenerated
	m.mu.Unlock()

	// Persist final assistant message if we have content
	if content != "" && messageID != "" {
		msg := &pb.Message{
			Id:        messageID,
			Role:      pb.MessageRole_MESSAGE_ROLE_ASSISTANT,
			Content:   content,
			Timestamp: timestamppb.Now(),
		}
		if err := m.storage.AppendMessage(ctx, sessionID, msg); err != nil {
			slog.Warn("Failed to persist assistant message", "sessionID", sessionID, "error", err)
		}

		// Emit final (non-partial) message
		m.emitAgentMessage(ctx, sessionID, messageID, content, false)
	}

	// Emit agent done
	m.emitAgentDone(ctx, sessionID)

	// Update status back to ready and update last active time
	m.updateSessionStatus(ctx, sessionID, pb.SessionStatus_SESSION_STATUS_READY)
	m.updateLastActiveAt(ctx, sessionID)

	// Generate title if first prompt and not already generated
	if !titleGenerated && originalPrompt != "" {
		m.mu.Lock()
		state.TitleGenerated = true
		m.mu.Unlock()

		// Request title generation from agent
		agent := m.GetAgentConnection(sessionID)
		if agent != nil {
			requestID := uuid.NewString()[:12]
			if err := agent.GenerateTitle(requestID, originalPrompt); err != nil {
				slog.Warn("Failed to request title generation", "sessionID", sessionID, "error", err)
			}
		}
	}

	// Create auto-snapshot after each turn (async with timeout)
	if originalPrompt != "" {
		turnNumber := int(result.TotalTurns)
		go func() {
			snapshotCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			m.AutoSnapshot(snapshotCtx, sessionID, turnNumber, originalPrompt)
		}()
	}

	// Reset streaming state for next prompt
	m.mu.Lock()
	state.CurrentMessageID = ""
	state.ContentBuilder.Reset()
	state.OriginalPrompt = ""
	m.mu.Unlock()

	slog.Info("Agent prompt completed", "sessionID", sessionID,
		"inputTokens", result.InputTokens,
		"outputTokens", result.OutputTokens,
		"turns", result.TotalTurns)

	return nil
}

// handleAgentStreamError processes errors during agent execution.
func (m *Manager) handleAgentStreamError(ctx context.Context, sessionID string, state *SessionState, agentErr *pb.AgentError) error {
	slog.Error("Agent execution error", "sessionID", sessionID, "error", agentErr.Message)

	m.emitAgentError(ctx, sessionID, agentErr.Message)
	m.updateSessionStatus(ctx, sessionID, pb.SessionStatus_SESSION_STATUS_READY)

	// Reset streaming state
	m.mu.Lock()
	state.CurrentMessageID = ""
	state.ContentBuilder.Reset()
	state.OriginalPrompt = ""
	m.mu.Unlock()

	return nil
}

// HandleTerminalOutput broadcasts terminal output to clients.
func (m *Manager) HandleTerminalOutput(ctx context.Context, sessionID string, data string) error {
	m.emitTerminalOutput(ctx, sessionID, data)
	return nil
}

// HandleTitleResponse processes title generation response from agent.
func (m *Manager) HandleTitleResponse(ctx context.Context, sessionID string, requestID string, title string) error {
	if title != "" {
		m.updateSessionName(ctx, sessionID, title)
		slog.Info("Session title generated", "sessionID", sessionID, "title", title)
	}
	return nil
}

// HandleGitStatusResponse processes git status response from agent.
func (m *Manager) HandleGitStatusResponse(ctx context.Context, sessionID string, requestID string, files []*pb.GitFileChange) error {
	// Convert []*pb.GitFileChange to []pb.GitFileChange for gitStatusResult
	result := make([]pb.GitFileChange, len(files))
	for i, f := range files {
		if f != nil {
			result[i] = *f
		}
	}

	// Send to waiting request
	pendingGitMu.Lock()
	if ch, ok := pendingGitStatusRequests[requestID]; ok {
		ch <- gitStatusResult{files: result, err: nil}
	}
	pendingGitMu.Unlock()

	return nil
}

// HandleGitDiffResponse processes git diff response from agent.
func (m *Manager) HandleGitDiffResponse(ctx context.Context, sessionID string, requestID string, diff string) error {
	pendingGitMu.Lock()
	if ch, ok := pendingGitDiffRequests[requestID]; ok {
		ch <- gitDiffResult{diff: diff, err: nil}
	}
	pendingGitMu.Unlock()

	return nil
}
