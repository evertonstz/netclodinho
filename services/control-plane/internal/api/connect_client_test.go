package api

import (
	"testing"
	"time"

	pb "github.com/angristan/netclode/services/control-plane/gen/netclode/v1"
	"github.com/angristan/netclode/services/control-plane/internal/protocol"
)

func TestConvertSession(t *testing.T) {
	tests := []struct {
		name     string
		input    *protocol.Session
		expected *pb.Session
	}{
		{
			name:     "nil session",
			input:    nil,
			expected: nil,
		},
		{
			name: "basic session",
			input: &protocol.Session{
				ID:           "sess-123",
				Name:         "Test Session",
				Status:       protocol.StatusReady,
				CreatedAt:    "2024-01-01T00:00:00Z",
				LastActiveAt: "2024-01-01T01:00:00Z",
			},
			expected: &pb.Session{
				Id:     "sess-123",
				Name:   "Test Session",
				Status: pb.SessionStatus_SESSION_STATUS_READY,
			},
		},
		{
			name: "session with repo",
			input: &protocol.Session{
				ID:           "sess-456",
				Name:         "Repo Session",
				Status:       protocol.StatusRunning,
				Repo:         strPtr("owner/repo"),
				RepoAccess:   strPtr("write"),
				CreatedAt:    "2024-01-01T00:00:00Z",
				LastActiveAt: "2024-01-01T01:00:00Z",
			},
			expected: &pb.Session{
				Id:         "sess-456",
				Name:       "Repo Session",
				Status:     pb.SessionStatus_SESSION_STATUS_RUNNING,
				Repo:       strPtr("owner/repo"),
				RepoAccess: strPtr("write"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertSession(tt.input)
			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
				return
			}
			if result == nil {
				t.Errorf("expected non-nil result")
				return
			}
			if result.Id != tt.expected.Id {
				t.Errorf("Id: got %v, want %v", result.Id, tt.expected.Id)
			}
			if result.Name != tt.expected.Name {
				t.Errorf("Name: got %v, want %v", result.Name, tt.expected.Name)
			}
			if result.Status != tt.expected.Status {
				t.Errorf("Status: got %v, want %v", result.Status, tt.expected.Status)
			}
		})
	}
}

func TestConvertSessionStatus(t *testing.T) {
	tests := []struct {
		name     string
		input    protocol.SessionStatus
		expected pb.SessionStatus
	}{
		{"creating", protocol.StatusCreating, pb.SessionStatus_SESSION_STATUS_CREATING},
		{"resuming", protocol.StatusResuming, pb.SessionStatus_SESSION_STATUS_RESUMING},
		{"ready", protocol.StatusReady, pb.SessionStatus_SESSION_STATUS_READY},
		{"running", protocol.StatusRunning, pb.SessionStatus_SESSION_STATUS_RUNNING},
		{"paused", protocol.StatusPaused, pb.SessionStatus_SESSION_STATUS_PAUSED},
		{"error", protocol.StatusError, pb.SessionStatus_SESSION_STATUS_ERROR},
		{"unknown", protocol.SessionStatus("unknown"), pb.SessionStatus_SESSION_STATUS_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertSessionStatus(tt.input)
			if result != tt.expected {
				t.Errorf("convertSessionStatus(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestConvertMessageRole(t *testing.T) {
	tests := []struct {
		name     string
		input    protocol.MessageRole
		expected pb.MessageRole
	}{
		{"user", protocol.RoleUser, pb.MessageRole_MESSAGE_ROLE_USER},
		{"assistant", protocol.RoleAssistant, pb.MessageRole_MESSAGE_ROLE_ASSISTANT},
		{"unknown", protocol.MessageRole("unknown"), pb.MessageRole_MESSAGE_ROLE_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertMessageRole(tt.input)
			if result != tt.expected {
				t.Errorf("convertMessageRole(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestConvertGitStatus(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected pb.GitFileStatus
	}{
		{"modified", "modified", pb.GitFileStatus_GIT_FILE_STATUS_MODIFIED},
		{"added", "added", pb.GitFileStatus_GIT_FILE_STATUS_ADDED},
		{"deleted", "deleted", pb.GitFileStatus_GIT_FILE_STATUS_DELETED},
		{"renamed", "renamed", pb.GitFileStatus_GIT_FILE_STATUS_RENAMED},
		{"untracked", "untracked", pb.GitFileStatus_GIT_FILE_STATUS_UNTRACKED},
		{"copied", "copied", pb.GitFileStatus_GIT_FILE_STATUS_COPIED},
		{"ignored", "ignored", pb.GitFileStatus_GIT_FILE_STATUS_IGNORED},
		{"unmerged", "unmerged", pb.GitFileStatus_GIT_FILE_STATUS_UNMERGED},
		{"unknown", "unknown", pb.GitFileStatus_GIT_FILE_STATUS_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertGitStatus(tt.input)
			if result != tt.expected {
				t.Errorf("convertGitStatus(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestConvertEventKind(t *testing.T) {
	tests := []struct {
		name     string
		input    protocol.AgentEventKind
		expected pb.AgentEventKind
	}{
		{"tool_start", protocol.EventKindToolStart, pb.AgentEventKind_AGENT_EVENT_KIND_TOOL_START},
		{"tool_input", protocol.EventKindToolInput, pb.AgentEventKind_AGENT_EVENT_KIND_TOOL_INPUT},
		{"tool_input_complete", protocol.EventKindToolInputComplete, pb.AgentEventKind_AGENT_EVENT_KIND_TOOL_INPUT_COMPLETE},
		{"tool_end", protocol.EventKindToolEnd, pb.AgentEventKind_AGENT_EVENT_KIND_TOOL_END},
		{"file_change", protocol.EventKindFileChange, pb.AgentEventKind_AGENT_EVENT_KIND_FILE_CHANGE},
		{"command_start", protocol.EventKindCommandStart, pb.AgentEventKind_AGENT_EVENT_KIND_COMMAND_START},
		{"command_end", protocol.EventKindCommandEnd, pb.AgentEventKind_AGENT_EVENT_KIND_COMMAND_END},
		{"thinking", protocol.EventKindThinking, pb.AgentEventKind_AGENT_EVENT_KIND_THINKING},
		{"port_exposed", protocol.EventKindPortExposed, pb.AgentEventKind_AGENT_EVENT_KIND_PORT_EXPOSED},
		{"repo_clone", protocol.EventKindRepoClone, pb.AgentEventKind_AGENT_EVENT_KIND_REPO_CLONE},
		{"unknown", protocol.AgentEventKind("unknown"), pb.AgentEventKind_AGENT_EVENT_KIND_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertEventKind(tt.input)
			if result != tt.expected {
				t.Errorf("convertEventKind(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestConvertServerMessage(t *testing.T) {
	tests := []struct {
		name        string
		input       protocol.ServerMessage
		checkResult func(t *testing.T, result *pb.ServerMessage)
	}{
		{
			name: "session created",
			input: protocol.ServerMessage{
				Type: protocol.MsgTypeSessionCreated,
				Session: &protocol.Session{
					ID:     "sess-123",
					Name:   "Test",
					Status: protocol.StatusCreating,
				},
			},
			checkResult: func(t *testing.T, result *pb.ServerMessage) {
				msg := result.GetSessionCreated()
				if msg == nil {
					t.Error("expected SessionCreated message")
					return
				}
				if msg.Session.Id != "sess-123" {
					t.Errorf("Session.Id = %v, want sess-123", msg.Session.Id)
				}
			},
		},
		{
			name: "session updated",
			input: protocol.ServerMessage{
				Type: protocol.MsgTypeSessionUpdated,
				Session: &protocol.Session{
					ID:     "sess-456",
					Name:   "Updated",
					Status: protocol.StatusReady,
				},
			},
			checkResult: func(t *testing.T, result *pb.ServerMessage) {
				msg := result.GetSessionUpdated()
				if msg == nil {
					t.Error("expected SessionUpdated message")
					return
				}
				if msg.Session.Id != "sess-456" {
					t.Errorf("Session.Id = %v, want sess-456", msg.Session.Id)
				}
			},
		},
		{
			name: "session deleted",
			input: protocol.ServerMessage{
				Type: protocol.MsgTypeSessionDeleted,
				ID:   "sess-789",
			},
			checkResult: func(t *testing.T, result *pb.ServerMessage) {
				msg := result.GetSessionDeleted()
				if msg == nil {
					t.Error("expected SessionDeleted message")
					return
				}
				if msg.SessionId != "sess-789" {
					t.Errorf("SessionId = %v, want sess-789", msg.SessionId)
				}
			},
		},
		{
			name: "session error",
			input: protocol.ServerMessage{
				Type:  protocol.MsgTypeSessionError,
				ID:    "sess-err",
				Error: "sandbox creation failed",
			},
			checkResult: func(t *testing.T, result *pb.ServerMessage) {
				msg := result.GetSessionError()
				if msg == nil {
					t.Error("expected SessionError message")
					return
				}
				if msg.SessionId != "sess-err" {
					t.Errorf("SessionId = %v, want sess-err", msg.SessionId)
				}
				if msg.Error != "sandbox creation failed" {
					t.Errorf("Error = %v, want sandbox creation failed", msg.Error)
				}
			},
		},
		{
			name: "agent message",
			input: protocol.ServerMessage{
				Type:      protocol.MsgTypeAgentMessage,
				SessionID: "sess-123",
				Content:   "Hello, world!",
				Partial:   boolPtr(true),
				MessageID: "msg-456",
			},
			checkResult: func(t *testing.T, result *pb.ServerMessage) {
				msg := result.GetAgentMessage()
				if msg == nil {
					t.Error("expected AgentMessage message")
					return
				}
				if msg.SessionId != "sess-123" {
					t.Errorf("SessionId = %v, want sess-123", msg.SessionId)
				}
				if msg.Content != "Hello, world!" {
					t.Errorf("Content = %v, want Hello, world!", msg.Content)
				}
				if !msg.Partial {
					t.Error("Partial should be true")
				}
			},
		},
		{
			name: "agent done",
			input: protocol.ServerMessage{
				Type:      protocol.MsgTypeAgentDone,
				SessionID: "sess-123",
			},
			checkResult: func(t *testing.T, result *pb.ServerMessage) {
				msg := result.GetAgentDone()
				if msg == nil {
					t.Error("expected AgentDone message")
					return
				}
				if msg.SessionId != "sess-123" {
					t.Errorf("SessionId = %v, want sess-123", msg.SessionId)
				}
			},
		},
		{
			name: "agent error",
			input: protocol.ServerMessage{
				Type:      protocol.MsgTypeAgentError,
				SessionID: "sess-123",
				Error:     "something went wrong",
			},
			checkResult: func(t *testing.T, result *pb.ServerMessage) {
				msg := result.GetAgentError()
				if msg == nil {
					t.Error("expected AgentError message")
					return
				}
				if msg.Error != "something went wrong" {
					t.Errorf("Error = %v, want something went wrong", msg.Error)
				}
			},
		},
		{
			name: "terminal output",
			input: protocol.ServerMessage{
				Type:      protocol.MsgTypeTerminalOutput,
				SessionID: "sess-123",
				Data:      "$ ls -la\n",
			},
			checkResult: func(t *testing.T, result *pb.ServerMessage) {
				msg := result.GetTerminalOutput()
				if msg == nil {
					t.Error("expected TerminalOutput message")
					return
				}
				if msg.Data != "$ ls -la\n" {
					t.Errorf("Data = %v, want $ ls -la\\n", msg.Data)
				}
			},
		},
		{
			name: "error message",
			input: protocol.ServerMessage{
				Type:    protocol.MsgTypeError,
				Message: "internal error",
			},
			checkResult: func(t *testing.T, result *pb.ServerMessage) {
				msg := result.GetError()
				if msg == nil {
					t.Error("expected Error message")
					return
				}
				if msg.Message != "internal error" {
					t.Errorf("Message = %v, want internal error", msg.Message)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertServerMessage(tt.input)
			if result == nil {
				t.Error("expected non-nil result")
				return
			}
			tt.checkResult(t, result)
		})
	}
}

func TestConvertAgentEvent(t *testing.T) {
	timestamp := time.Now().UTC().Format(time.RFC3339)

	t.Run("nil event", func(t *testing.T) {
		result := convertAgentEvent(nil)
		if result != nil {
			t.Error("expected nil result for nil input")
		}
	})

	t.Run("tool start event", func(t *testing.T) {
		event := &protocol.AgentEvent{
			Kind:      protocol.EventKindToolStart,
			Timestamp: timestamp,
			Tool:      "Read",
			ToolUseID: "tool-123",
		}
		result := convertAgentEvent(event)
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if result.Kind != pb.AgentEventKind_AGENT_EVENT_KIND_TOOL_START {
			t.Errorf("Kind = %v, want TOOL_START", result.Kind)
		}
		if result.GetTool() != "Read" {
			t.Errorf("Tool = %v, want Read", result.GetTool())
		}
		if result.GetToolUseId() != "tool-123" {
			t.Errorf("ToolUseId = %v, want tool-123", result.GetToolUseId())
		}
	})

	t.Run("file change event", func(t *testing.T) {
		linesAdded := 10
		linesRemoved := 5
		event := &protocol.AgentEvent{
			Kind:         protocol.EventKindFileChange,
			Timestamp:    timestamp,
			Path:         "/src/main.go",
			Action:       "edit",
			LinesAdded:   &linesAdded,
			LinesRemoved: &linesRemoved,
		}
		result := convertAgentEvent(event)
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if result.Kind != pb.AgentEventKind_AGENT_EVENT_KIND_FILE_CHANGE {
			t.Errorf("Kind = %v, want FILE_CHANGE", result.Kind)
		}
		if result.GetPath() != "/src/main.go" {
			t.Errorf("Path = %v, want /src/main.go", result.GetPath())
		}
		if result.GetLinesAdded() != 10 {
			t.Errorf("LinesAdded = %v, want 10", result.GetLinesAdded())
		}
	})

	t.Run("command end event", func(t *testing.T) {
		exitCode := 0
		output := "success"
		event := &protocol.AgentEvent{
			Kind:      protocol.EventKindCommandEnd,
			Timestamp: timestamp,
			Command:   "npm test",
			ExitCode:  &exitCode,
			Output:    &output,
		}
		result := convertAgentEvent(event)
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if result.Kind != pb.AgentEventKind_AGENT_EVENT_KIND_COMMAND_END {
			t.Errorf("Kind = %v, want COMMAND_END", result.Kind)
		}
		if result.GetExitCode() != 0 {
			t.Errorf("ExitCode = %v, want 0", result.GetExitCode())
		}
	})
}

func TestConvertSessionWithMeta(t *testing.T) {
	t.Run("nil input", func(t *testing.T) {
		result := convertSessionWithMeta(nil)
		if result != nil {
			t.Error("expected nil result for nil input")
		}
	})

	t.Run("with message count", func(t *testing.T) {
		msgCount := 42
		lastMsgID := "msg-last"
		input := &protocol.SessionWithMeta{
			Session: protocol.Session{
				ID:     "sess-123",
				Name:   "Test",
				Status: protocol.StatusReady,
			},
			MessageCount:  &msgCount,
			LastMessageID: &lastMsgID,
		}
		result := convertSessionWithMeta(input)
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if result.Session.Id != "sess-123" {
			t.Errorf("Session.Id = %v, want sess-123", result.Session.Id)
		}
		if result.GetMessageCount() != 42 {
			t.Errorf("MessageCount = %v, want 42", result.GetMessageCount())
		}
		if result.GetLastMessageId() != "msg-last" {
			t.Errorf("LastMessageId = %v, want msg-last", result.GetLastMessageId())
		}
	})
}

func TestConvertPersistedMessage(t *testing.T) {
	t.Run("nil input", func(t *testing.T) {
		result := convertPersistedMessage(nil)
		if result != nil {
			t.Error("expected nil result for nil input")
		}
	})

	t.Run("valid message", func(t *testing.T) {
		input := &protocol.PersistedMessage{
			ID:        "msg-123",
			Role:      protocol.RoleUser,
			Content:   "Hello",
			Timestamp: "2024-01-01T00:00:00Z",
		}
		result := convertPersistedMessage(input)
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if result.Id != "msg-123" {
			t.Errorf("Id = %v, want msg-123", result.Id)
		}
		if result.Role != pb.MessageRole_MESSAGE_ROLE_USER {
			t.Errorf("Role = %v, want USER", result.Role)
		}
		if result.Content != "Hello" {
			t.Errorf("Content = %v, want Hello", result.Content)
		}
	})
}

// Helper functions
func strPtr(s string) *string {
	return &s
}

func boolPtr(b bool) *bool {
	return &b
}

// =============================================================================
// Handler Tests with Mock Manager
// =============================================================================

// mockSendCapture captures messages sent by handlers
type mockSendCapture struct {
	messages []*pb.ServerMessage
}

func (m *mockSendCapture) send(msg *pb.ServerMessage) error {
	m.messages = append(m.messages, msg)
	return nil
}

func (m *mockSendCapture) lastMessage() *pb.ServerMessage {
	if len(m.messages) == 0 {
		return nil
	}
	return m.messages[len(m.messages)-1]
}

// TestHandleMessageValidation tests that handleMessage correctly validates input
// before dispatching to handlers. Tests only validation errors since we don't
// have a mock manager - handlers that pass validation will call manager methods.
func TestHandleMessageValidation(t *testing.T) {
	tests := []struct {
		name        string
		message     *pb.ClientMessage
		expectError bool
	}{
		{
			name: "prompt message with empty session",
			message: &pb.ClientMessage{
				Message: &pb.ClientMessage_SendPrompt{
					SendPrompt: &pb.SendPromptRequest{
						SessionId: "",
						Text:      "hello",
					},
				},
			},
			expectError: true,
		},
		{
			name: "prompt message with empty text",
			message: &pb.ClientMessage{
				Message: &pb.ClientMessage_SendPrompt{
					SendPrompt: &pb.SendPromptRequest{
						SessionId: "sess-123",
						Text:      "",
					},
				},
			},
			expectError: true,
		},
		{
			name: "terminal input with empty session",
			message: &pb.ClientMessage{
				Message: &pb.ClientMessage_TerminalInput{
					TerminalInput: &pb.TerminalInputRequest{
						SessionId: "",
						Data:      "ls",
					},
				},
			},
			expectError: true,
		},
		{
			name: "terminal resize with empty session",
			message: &pb.ClientMessage{
				Message: &pb.ClientMessage_TerminalResize{
					TerminalResize: &pb.TerminalResizeRequest{
						SessionId: "",
						Cols:      80,
						Rows:      24,
					},
				},
			},
			expectError: true,
		},
		{
			name: "empty message (no inner message set)",
			message: &pb.ClientMessage{
				Message: nil,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a minimal connection for testing validation
			conn := &ConnectConnection{
				manager:        nil,
				subscriptions:  make(map[string]*subscriptionInfo),
				globalMessages: make(chan *pb.ServerMessage, 64),
				done:           make(chan struct{}),
			}

			err := conn.handleMessage(nil, tt.message)

			if tt.expectError && err == nil {
				t.Error("expected error but got none")
			}
		})
	}
}

// TestPromptValidation tests prompt input validation
func TestPromptValidation(t *testing.T) {
	conn := &ConnectConnection{
		manager:        nil,
		subscriptions:  make(map[string]*subscriptionInfo),
		globalMessages: make(chan *pb.ServerMessage, 64),
		done:           make(chan struct{}),
	}

	t.Run("empty session ID", func(t *testing.T) {
		err := conn.handlePrompt(nil, "", "hello")
		if err == nil {
			t.Error("expected error for empty session ID")
		}
	})

	t.Run("empty text", func(t *testing.T) {
		err := conn.handlePrompt(nil, "sess-123", "")
		if err == nil {
			t.Error("expected error for empty text")
		}
	})
}

// TestTerminalInputValidation tests terminal input validation
func TestTerminalInputValidation(t *testing.T) {
	conn := &ConnectConnection{
		manager:        nil,
		subscriptions:  make(map[string]*subscriptionInfo),
		globalMessages: make(chan *pb.ServerMessage, 64),
		done:           make(chan struct{}),
	}

	t.Run("empty session ID", func(t *testing.T) {
		err := conn.handleTerminalInput(nil, "", "ls")
		if err == nil {
			t.Error("expected error for empty session ID")
		}
	})

	t.Run("empty data is ok", func(t *testing.T) {
		// Empty data should return nil (no-op)
		err := conn.handleTerminalInput(nil, "sess-123", "")
		if err != nil {
			t.Errorf("unexpected error for empty data: %v", err)
		}
	})
}

// TestTerminalResizeValidation tests terminal resize validation
func TestTerminalResizeValidation(t *testing.T) {
	conn := &ConnectConnection{
		manager:        nil,
		subscriptions:  make(map[string]*subscriptionInfo),
		globalMessages: make(chan *pb.ServerMessage, 64),
		done:           make(chan struct{}),
	}

	t.Run("empty session ID", func(t *testing.T) {
		err := conn.handleTerminalResize(nil, &pb.TerminalResizeRequest{
			SessionId: "",
			Cols:      80,
			Rows:      24,
		})
		if err == nil {
			t.Error("expected error for empty session ID")
		}
	})
}

// NOTE: TestPortExposeValidation, TestGitStatusValidation, and TestGitDiffValidation
// are not included because these handlers send error messages via the stream
// (rather than returning errors), which requires a mock stream to test properly.

// TestConnectionLifecycle tests connection setup and teardown
func TestConnectionLifecycle(t *testing.T) {
	t.Run("close cancels subscriptions", func(t *testing.T) {
		canceled := false
		conn := &ConnectConnection{
			subscriptions: map[string]*subscriptionInfo{
				"sess-1": {
					cancel: func() { canceled = true },
				},
			},
			globalMessages: make(chan *pb.ServerMessage, 64),
			done:           make(chan struct{}),
		}

		conn.close()

		if !canceled {
			t.Error("expected subscription to be canceled")
		}

		// Verify done channel is closed
		select {
		case <-conn.done:
			// Good, channel is closed
		default:
			t.Error("expected done channel to be closed")
		}
	})

	t.Run("close is idempotent", func(t *testing.T) {
		conn := &ConnectConnection{
			subscriptions:  make(map[string]*subscriptionInfo),
			globalMessages: make(chan *pb.ServerMessage, 64),
			done:           make(chan struct{}),
		}

		// Should not panic on multiple closes
		conn.close()
		conn.close()
	})
}

// TestUnsubscribe tests subscription removal
func TestUnsubscribe(t *testing.T) {
	canceled := false
	conn := &ConnectConnection{
		subscriptions: map[string]*subscriptionInfo{
			"sess-1": {
				cancel: func() { canceled = true },
			},
			"sess-2": {
				cancel: func() {},
			},
		},
		globalMessages: make(chan *pb.ServerMessage, 64),
		done:           make(chan struct{}),
	}

	conn.unsubscribe("sess-1")

	if !canceled {
		t.Error("expected subscription to be canceled")
	}

	if _, exists := conn.subscriptions["sess-1"]; exists {
		t.Error("expected subscription to be removed")
	}

	if _, exists := conn.subscriptions["sess-2"]; !exists {
		t.Error("expected other subscription to remain")
	}
}

// TestUnsubscribeNonexistent tests unsubscribing from non-existent session
func TestUnsubscribeNonexistent(t *testing.T) {
	conn := &ConnectConnection{
		subscriptions:  make(map[string]*subscriptionInfo),
		globalMessages: make(chan *pb.ServerMessage, 64),
		done:           make(chan struct{}),
	}

	// Should not panic
	conn.unsubscribe("nonexistent")
}
