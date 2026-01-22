package session

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/angristan/netclode/services/control-plane/internal/protocol"
	"github.com/google/uuid"
)

const (
	interruptTimeout = 5 * time.Second
)

// SendPrompt sends a prompt to the agent and streams the response.
// If the sandbox isn't ready yet, queues the prompt to be sent when ready.
func (m *Manager) SendPrompt(ctx context.Context, sessionID, text string) error {
	state := m.getState(sessionID)
	if state == nil {
		return fmt.Errorf("session %s not found", sessionID)
	}

	if state.ServiceFQDN == "" {
		// Sandbox not ready yet - queue the prompt
		slog.Info("Queueing prompt until sandbox is ready", "sessionID", sessionID)
		state.PendingPrompt = text
		// Still set status to running so UI shows activity
		m.updateSessionStatus(ctx, sessionID, protocol.StatusRunning)
		return nil
	}

	// Persist user message
	userMsg := &protocol.PersistedMessage{
		ID:        "msg_" + uuid.NewString()[:12],
		SessionID: sessionID,
		Role:      protocol.RoleUser,
		Content:   text,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	if err := m.storage.AppendMessage(ctx, userMsg); err != nil {
		slog.Warn("Failed to persist user message", "sessionID", sessionID, "error", err)
	}

	// Broadcast user message to all subscribers (for cross-client sync)
	m.emit(ctx, sessionID, protocol.NewUserMessage(sessionID, text))

	// Update session status to running
	m.updateSessionStatus(ctx, sessionID, protocol.StatusRunning)

	// Call agent in background using Connect protocol
	go m.callAgentPromptConnect(context.Background(), sessionID, state.ServiceFQDN, text)

	return nil
}

func (m *Manager) handleAgentError(ctx context.Context, sessionID string, err error) {
	slog.Error("Agent error", "sessionID", sessionID, "error", err)
	m.emit(ctx, sessionID, protocol.NewAgentError(sessionID, err.Error()))
	m.updateSessionStatus(ctx, sessionID, protocol.StatusReady)
}

// GetGitStatus fetches git status from the agent.
func (m *Manager) GetGitStatus(ctx context.Context, sessionID string) ([]protocol.GitFileChange, error) {
	return m.GetGitStatusConnect(ctx, sessionID)
}

// GetGitDiff fetches git diff for a file from the agent.
func (m *Manager) GetGitDiff(ctx context.Context, sessionID, file string) (string, error) {
	return m.GetGitDiffConnect(ctx, sessionID, file)
}

// Interrupt sends an interrupt signal to the agent.
func (m *Manager) Interrupt(ctx context.Context, sessionID string) error {
	return m.InterruptConnect(ctx, sessionID)
}
