package session

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/angristan/netclode/apps/control-plane/internal/protocol"
	"github.com/google/uuid"
)

const (
	interruptTimeout = 5 * time.Second
)

// SendPrompt sends a prompt to the agent and streams the response.
func (m *Manager) SendPrompt(ctx context.Context, sessionID, text string) error {
	state := m.getState(sessionID)
	if state == nil {
		return fmt.Errorf("session %s not found", sessionID)
	}

	if state.ServiceFQDN == "" {
		return fmt.Errorf("session %s is not running", sessionID)
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

	// Call agent in background to not block
	go m.callAgentPrompt(context.Background(), sessionID, state.ServiceFQDN, text)

	return nil
}

func (m *Manager) callAgentPrompt(ctx context.Context, sessionID, fqdn, text string) {
	url := fmt.Sprintf("http://%s:3002/prompt", fqdn)

	body, _ := json.Marshal(map[string]string{
		"sessionId": sessionID,
		"text":      text,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		m.handleAgentError(ctx, sessionID, fmt.Errorf("create request: %w", err))
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		m.handleAgentError(ctx, sessionID, fmt.Errorf("call agent: %w", err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		m.handleAgentError(ctx, sessionID, fmt.Errorf("agent returned %d: %s", resp.StatusCode, string(bodyBytes)))
		return
	}

	// Stream SSE response
	if err := m.streamSSE(ctx, sessionID, resp.Body); err != nil {
		m.handleAgentError(ctx, sessionID, err)
		return
	}

	// Mark session as ready
	m.updateSessionStatus(ctx, sessionID, protocol.StatusReady)

	// Emit done
	m.emit(ctx, sessionID, protocol.NewAgentDone(sessionID))
}

func (m *Manager) handleAgentError(ctx context.Context, sessionID string, err error) {
	slog.Error("Agent error", "sessionID", sessionID, "error", err)
	m.emit(ctx, sessionID, protocol.NewAgentError(sessionID, err.Error()))
	m.updateSessionStatus(ctx, sessionID, protocol.StatusReady)
}

func (m *Manager) streamSSE(ctx context.Context, sessionID string, body io.Reader) error {
	scanner := bufio.NewScanner(body)
	var contentBuilder strings.Builder
	messageID := "msg_" + uuid.NewString()[:12]

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "" {
			continue
		}

		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			// If not valid JSON, emit as plain message
			m.emit(ctx, sessionID, protocol.NewAgentMessage(sessionID, data, false, messageID))
			continue
		}

		msgType, _ := payload["type"].(string)

		switch msgType {
		case "agent.message":
			content, _ := payload["content"].(string)
			partial, _ := payload["partial"].(bool)

			if partial {
				contentBuilder.WriteString(content)
				m.emit(ctx, sessionID, protocol.NewAgentMessage(sessionID, content, true, messageID))
			} else {
				if content != "" {
					contentBuilder.Reset()
					contentBuilder.WriteString(content)
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

				// Reset for next message
				contentBuilder.Reset()
				messageID = "msg_" + uuid.NewString()[:12]
			}

		case "agent.event":
			eventData, ok := payload["event"].(map[string]interface{})
			if !ok {
				continue
			}

			event := parseAgentEvent(eventData)

			// Inject preview URL for port_detected events (uses Tailscale MagicDNS short hostname)
			if event.Kind == protocol.EventKindPortDetected && event.Port > 0 {
				previewURL := fmt.Sprintf("http://sandbox-%s:%d", sessionID, event.Port)
				event.PreviewURL = &previewURL

				// Dynamically expose the port via Tailscale service and NetworkPolicy
				if err := m.k8s.ExposePort(ctx, sessionID, event.Port); err != nil {
					slog.Warn("Failed to expose port", "sessionID", sessionID, "port", event.Port, "error", err)
				}
			}

			// Persist event
			persistedEvent := &protocol.PersistedEvent{
				ID:        "evt_" + uuid.NewString()[:12],
				SessionID: sessionID,
				Event:     event,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			}
			if err := m.storage.AppendEvent(ctx, persistedEvent); err != nil {
				slog.Warn("Failed to persist event", "sessionID", sessionID, "error", err)
			}

			m.emit(ctx, sessionID, protocol.NewAgentEvent(sessionID, &event))

		case "error", "agent.error":
			errMsg, _ := payload["error"].(string)
			if errMsg == "" {
				errMsg, _ = payload["message"].(string)
			}
			if errMsg != "" {
				m.emit(ctx, sessionID, protocol.NewAgentError(sessionID, errMsg))
			}

		case "agent.system", "agent.result", "start", "done":
			// Ignored

		default:
			// Ignore unknown types
		}
	}

	return scanner.Err()
}

func parseAgentEvent(data map[string]interface{}) protocol.AgentEvent {
	event := protocol.AgentEvent{}

	if kind, ok := data["kind"].(string); ok {
		event.Kind = protocol.AgentEventKind(kind)
	}
	if ts, ok := data["timestamp"].(string); ok {
		event.Timestamp = ts
	} else {
		event.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}

	// Tool events
	if tool, ok := data["tool"].(string); ok {
		event.Tool = tool
	}
	if toolUseID, ok := data["toolUseId"].(string); ok {
		event.ToolUseID = toolUseID
	}
	if input, ok := data["input"].(map[string]interface{}); ok {
		event.Input = input
	}
	if inputDelta, ok := data["inputDelta"].(string); ok {
		event.InputDelta = inputDelta
	}
	if result, ok := data["result"].(string); ok {
		event.Result = &result
	}
	if errStr, ok := data["error"].(string); ok {
		event.Error = &errStr
	}

	// File change events
	if path, ok := data["path"].(string); ok {
		event.Path = path
	}
	if action, ok := data["action"].(string); ok {
		event.Action = action
	}
	if linesAdded, ok := data["linesAdded"].(float64); ok {
		i := int(linesAdded)
		event.LinesAdded = &i
	}
	if linesRemoved, ok := data["linesRemoved"].(float64); ok {
		i := int(linesRemoved)
		event.LinesRemoved = &i
	}

	// Command events
	if cmd, ok := data["command"].(string); ok {
		event.Command = cmd
	}
	if cwd, ok := data["cwd"].(string); ok {
		event.Cwd = &cwd
	}
	if exitCode, ok := data["exitCode"].(float64); ok {
		i := int(exitCode)
		event.ExitCode = &i
	}
	if output, ok := data["output"].(string); ok {
		event.Output = &output
	}

	// Thinking events
	if content, ok := data["content"].(string); ok {
		event.Content = content
	}

	// Port detected events
	if port, ok := data["port"].(float64); ok {
		event.Port = int(port)
	}
	if process, ok := data["process"].(string); ok {
		event.Process = &process
	}
	if previewURL, ok := data["previewUrl"].(string); ok {
		event.PreviewURL = &previewURL
	}

	return event
}

// Interrupt sends an interrupt signal to the agent.
func (m *Manager) Interrupt(ctx context.Context, sessionID string) error {
	state := m.getState(sessionID)
	if state == nil {
		return fmt.Errorf("session %s not found", sessionID)
	}

	if state.ServiceFQDN == "" {
		return nil // Not running, nothing to interrupt
	}

	url := fmt.Sprintf("http://%s:3002/interrupt", state.ServiceFQDN)

	ctx, cancel := context.WithTimeout(ctx, interruptTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// Fire and forget, don't return error
		slog.Warn("Failed to interrupt agent", "sessionID", sessionID, "error", err)
		return nil
	}
	defer resp.Body.Close()

	return nil
}
