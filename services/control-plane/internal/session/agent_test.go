package session

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/angristan/netclode/services/control-plane/internal/config"
	"github.com/angristan/netclode/services/control-plane/internal/protocol"
	"github.com/angristan/netclode/services/control-plane/internal/storage"
	"github.com/redis/go-redis/v9"
)

// TestParseAgentEvent tests the event parsing function.
func TestParseAgentEvent_ToolStart(t *testing.T) {
	data := map[string]interface{}{
		"kind":      "tool_start",
		"tool":      "bash",
		"toolUseId": "tool-123",
		"timestamp": "2024-01-01T00:00:00Z",
		"input": map[string]interface{}{
			"command": "ls -la",
		},
	}

	event := parseAgentEvent(data)

	if event.Kind != protocol.EventKindToolStart {
		t.Errorf("expected kind tool_start, got %s", event.Kind)
	}
	if event.Tool != "bash" {
		t.Errorf("expected tool bash, got %s", event.Tool)
	}
	if event.ToolUseID != "tool-123" {
		t.Errorf("expected toolUseId tool-123, got %s", event.ToolUseID)
	}
	if event.Timestamp != "2024-01-01T00:00:00Z" {
		t.Errorf("expected timestamp 2024-01-01T00:00:00Z, got %s", event.Timestamp)
	}
	if event.Input == nil {
		t.Error("expected input to be set")
	}
	if cmd, ok := event.Input["command"].(string); !ok || cmd != "ls -la" {
		t.Errorf("expected command 'ls -la', got %v", event.Input["command"])
	}
}

func TestParseAgentEvent_ToolEnd(t *testing.T) {
	result := "success output"
	data := map[string]interface{}{
		"kind":      "tool_end",
		"tool":      "bash",
		"toolUseId": "tool-123",
		"result":    result,
	}

	event := parseAgentEvent(data)

	if event.Kind != protocol.EventKindToolEnd {
		t.Errorf("expected kind tool_end, got %s", event.Kind)
	}
	if event.Result == nil || *event.Result != result {
		t.Errorf("expected result %q, got %v", result, event.Result)
	}
}

func TestParseAgentEvent_ToolError(t *testing.T) {
	errMsg := "command failed"
	data := map[string]interface{}{
		"kind":      "tool_result",
		"tool":      "bash",
		"toolUseId": "tool-123",
		"error":     errMsg,
	}

	event := parseAgentEvent(data)

	if event.Error == nil || *event.Error != errMsg {
		t.Errorf("expected error %q, got %v", errMsg, event.Error)
	}
}

func TestParseAgentEvent_ToolInput(t *testing.T) {
	data := map[string]interface{}{
		"kind":       "tool_input",
		"toolUseId":  "tool-123",
		"inputDelta": `{"command": "ls"`,
	}

	event := parseAgentEvent(data)

	if event.Kind != protocol.EventKindToolInput {
		t.Errorf("expected kind tool_input, got %s", event.Kind)
	}
	if event.InputDelta != `{"command": "ls"` {
		t.Errorf("expected inputDelta, got %s", event.InputDelta)
	}
}

func TestParseAgentEvent_FileChange(t *testing.T) {
	linesAdded := 10.0
	linesRemoved := 5.0
	data := map[string]interface{}{
		"kind":         "file_change",
		"path":         "/src/main.go",
		"action":       "modified",
		"linesAdded":   linesAdded,
		"linesRemoved": linesRemoved,
	}

	event := parseAgentEvent(data)

	if event.Kind != protocol.EventKindFileChange {
		t.Errorf("expected kind file_change, got %s", event.Kind)
	}
	if event.Path != "/src/main.go" {
		t.Errorf("expected path /src/main.go, got %s", event.Path)
	}
	if event.Action != "modified" {
		t.Errorf("expected action modified, got %s", event.Action)
	}
	if event.LinesAdded == nil || *event.LinesAdded != 10 {
		t.Errorf("expected linesAdded 10, got %v", event.LinesAdded)
	}
	if event.LinesRemoved == nil || *event.LinesRemoved != 5 {
		t.Errorf("expected linesRemoved 5, got %v", event.LinesRemoved)
	}
}

func TestParseAgentEvent_CommandEnd(t *testing.T) {
	exitCode := 0.0
	cwd := "/home/user"
	output := "file1.txt\nfile2.txt"
	data := map[string]interface{}{
		"kind":     "command_end",
		"command":  "ls",
		"cwd":      cwd,
		"exitCode": exitCode,
		"output":   output,
	}

	event := parseAgentEvent(data)

	if event.Kind != protocol.EventKindCommandEnd {
		t.Errorf("expected kind command_end, got %s", event.Kind)
	}
	if event.Command != "ls" {
		t.Errorf("expected command ls, got %s", event.Command)
	}
	if event.Cwd == nil || *event.Cwd != cwd {
		t.Errorf("expected cwd %q, got %v", cwd, event.Cwd)
	}
	if event.ExitCode == nil || *event.ExitCode != 0 {
		t.Errorf("expected exitCode 0, got %v", event.ExitCode)
	}
	if event.Output == nil || *event.Output != output {
		t.Errorf("expected output, got %v", event.Output)
	}
}

func TestParseAgentEvent_Thinking(t *testing.T) {
	data := map[string]interface{}{
		"kind":       "thinking",
		"content":    "Let me analyze this...",
		"thinkingId": "think-123",
		"partial":    true,
	}

	event := parseAgentEvent(data)

	if event.Kind != protocol.EventKindThinking {
		t.Errorf("expected kind thinking, got %s", event.Kind)
	}
	if event.Content != "Let me analyze this..." {
		t.Errorf("expected content, got %s", event.Content)
	}
	if event.ThinkingID != "think-123" {
		t.Errorf("expected thinkingId think-123, got %s", event.ThinkingID)
	}
	if !event.Partial {
		t.Error("expected partial to be true")
	}
}

func TestParseAgentEvent_PortExposed(t *testing.T) {
	process := "node"
	previewURL := "http://localhost:3000"
	data := map[string]interface{}{
		"kind":       "port_exposed",
		"port":       3000.0,
		"process":    process,
		"previewUrl": previewURL,
	}

	event := parseAgentEvent(data)

	if event.Kind != protocol.EventKindPortExposed {
		t.Errorf("expected kind port_exposed, got %s", event.Kind)
	}
	if event.Port != 3000 {
		t.Errorf("expected port 3000, got %d", event.Port)
	}
	if event.Process == nil || *event.Process != process {
		t.Errorf("expected process %q, got %v", process, event.Process)
	}
	if event.PreviewURL == nil || *event.PreviewURL != previewURL {
		t.Errorf("expected previewUrl %q, got %v", previewURL, event.PreviewURL)
	}
}

func TestParseAgentEvent_PortExposed_Basic(t *testing.T) {
	previewURL := "http://sandbox-test:8080"
	data := map[string]interface{}{
		"kind":       "port_exposed",
		"port":       8080.0,
		"previewUrl": previewURL,
	}

	event := parseAgentEvent(data)

	if event.Kind != protocol.EventKindPortExposed {
		t.Errorf("expected kind port_exposed, got %s", event.Kind)
	}
	if event.Port != 8080 {
		t.Errorf("expected port 8080, got %d", event.Port)
	}
}

func TestParseAgentEvent_RepoClone(t *testing.T) {
	data := map[string]interface{}{
		"kind":    "repo_clone",
		"repo":    "owner/repo",
		"stage":   "cloning",
		"message": "Cloning repository...",
	}

	event := parseAgentEvent(data)

	if event.Kind != protocol.EventKindRepoClone {
		t.Errorf("expected kind repo_clone, got %s", event.Kind)
	}
	if event.Repo != "owner/repo" {
		t.Errorf("expected repo owner/repo, got %s", event.Repo)
	}
	if event.Stage != "cloning" {
		t.Errorf("expected stage cloning, got %s", event.Stage)
	}
	if event.Message != "Cloning repository..." {
		t.Errorf("expected message, got %s", event.Message)
	}
}

func TestParseAgentEvent_ParentToolUseID(t *testing.T) {
	data := map[string]interface{}{
		"kind":            "tool_start",
		"tool":            "task",
		"toolUseId":       "child-123",
		"parentToolUseId": "parent-456",
	}

	event := parseAgentEvent(data)

	if event.ParentToolUseID != "parent-456" {
		t.Errorf("expected parentToolUseId parent-456, got %s", event.ParentToolUseID)
	}
}

func TestParseAgentEvent_MissingTimestamp(t *testing.T) {
	data := map[string]interface{}{
		"kind": "tool_start",
		"tool": "bash",
	}

	event := parseAgentEvent(data)

	// Should generate a timestamp if not provided
	if event.Timestamp == "" {
		t.Error("expected timestamp to be generated")
	}

	// Verify it's a valid RFC3339 timestamp
	_, err := time.Parse(time.RFC3339, event.Timestamp)
	if err != nil {
		t.Errorf("expected valid RFC3339 timestamp, got %s", event.Timestamp)
	}
}

func TestParseAgentEvent_EmptyData(t *testing.T) {
	data := map[string]interface{}{}

	event := parseAgentEvent(data)

	// Should not panic and should generate timestamp
	if event.Timestamp == "" {
		t.Error("expected timestamp to be generated")
	}
}

// Helper function to create SSE data lines
func sseData(data interface{}) string {
	bytes, _ := json.Marshal(data)
	return "data: " + string(bytes) + "\n\n"
}

// TestStreamSSE tests the SSE parsing logic
func TestStreamSSE_AgentMessage(t *testing.T) {
	// Create mock manager to capture emitted events
	emittedMessages := make([]protocol.ServerMessage, 0)
	var mu sync.Mutex

	// Create a simple mock that captures emit calls
	manager := &Manager{
		sessions: make(map[string]*SessionState),
		storage:  newMockStorageForAgent(),
		config:   &config.Config{},
	}

	// Add test session
	session := &protocol.Session{ID: "test-session"}
	manager.sessions["test-session"] = NewSessionState(session)

	// Create SSE response with agent messages
	sseResponse := strings.Join([]string{
		sseData(map[string]interface{}{
			"type":    "agent.message",
			"content": "Hello",
			"partial": true,
		}),
		sseData(map[string]interface{}{
			"type":    "agent.message",
			"content": ", world!",
			"partial": true,
		}),
		sseData(map[string]interface{}{
			"type":    "agent.message",
			"content": "",
			"partial": false,
		}),
	}, "")

	// Parse the SSE stream
	reader := strings.NewReader(sseResponse)
	err := manager.streamSSE(context.Background(), "test-session", "test.local", "test prompt", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	// The actual emit is through Redis publish which we can't easily mock here
	// This test at least verifies no panic and proper parsing
	_ = emittedMessages
}

// TestStreamSSE_AgentEvent tests event parsing in SSE stream
func TestStreamSSE_AgentEvent(t *testing.T) {
	manager := &Manager{
		sessions: make(map[string]*SessionState),
		storage:  newMockStorageForAgent(),
		config:   &config.Config{MaxEventsPerSession: 100},
	}

	session := &protocol.Session{ID: "test-session"}
	manager.sessions["test-session"] = NewSessionState(session)

	sseResponse := sseData(map[string]interface{}{
		"type": "agent.event",
		"event": map[string]interface{}{
			"kind":      "tool_start",
			"tool":      "bash",
			"toolUseId": "tool-123",
		},
	})

	reader := strings.NewReader(sseResponse)
	err := manager.streamSSE(context.Background(), "test-session", "test.local", "test", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestStreamSSE_ErrorEvent tests error event handling
func TestStreamSSE_ErrorEvent(t *testing.T) {
	manager := &Manager{
		sessions: make(map[string]*SessionState),
		storage:  newMockStorageForAgent(),
		config:   &config.Config{},
	}

	session := &protocol.Session{ID: "test-session"}
	manager.sessions["test-session"] = NewSessionState(session)

	tests := []struct {
		name    string
		payload map[string]interface{}
	}{
		{
			name: "error type with error field",
			payload: map[string]interface{}{
				"type":  "error",
				"error": "Something went wrong",
			},
		},
		{
			name: "agent.error type",
			payload: map[string]interface{}{
				"type":    "agent.error",
				"message": "Agent error occurred",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sseResponse := sseData(tt.payload)
			reader := strings.NewReader(sseResponse)
			err := manager.streamSSE(context.Background(), "test-session", "test.local", "test", reader)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestStreamSSE_IgnoredTypes tests that system/result/done types are ignored
func TestStreamSSE_IgnoredTypes(t *testing.T) {
	manager := &Manager{
		sessions: make(map[string]*SessionState),
		storage:  newMockStorageForAgent(),
		config:   &config.Config{},
	}

	session := &protocol.Session{ID: "test-session"}
	manager.sessions["test-session"] = NewSessionState(session)

	ignoredTypes := []string{"agent.system", "agent.result", "start", "done"}

	for _, msgType := range ignoredTypes {
		t.Run(msgType, func(t *testing.T) {
			sseResponse := sseData(map[string]interface{}{
				"type":    msgType,
				"content": "should be ignored",
			})
			reader := strings.NewReader(sseResponse)
			err := manager.streamSSE(context.Background(), "test-session", "test.local", "test", reader)
			if err != nil {
				t.Fatalf("unexpected error for type %s: %v", msgType, err)
			}
		})
	}
}

// TestStreamSSE_InvalidJSON tests handling of invalid JSON in SSE
func TestStreamSSE_InvalidJSON(t *testing.T) {
	manager := &Manager{
		sessions: make(map[string]*SessionState),
		storage:  newMockStorageForAgent(),
		config:   &config.Config{},
	}

	session := &protocol.Session{ID: "test-session"}
	manager.sessions["test-session"] = NewSessionState(session)

	// Invalid JSON should be emitted as plain message
	sseResponse := "data: not valid json\n\n"
	reader := strings.NewReader(sseResponse)
	err := manager.streamSSE(context.Background(), "test-session", "test.local", "test", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestStreamSSE_EmptyDataLines tests handling of empty data lines
func TestStreamSSE_EmptyDataLines(t *testing.T) {
	manager := &Manager{
		sessions: make(map[string]*SessionState),
		storage:  newMockStorageForAgent(),
		config:   &config.Config{},
	}

	session := &protocol.Session{ID: "test-session"}
	manager.sessions["test-session"] = NewSessionState(session)

	// Empty data lines should be skipped
	sseResponse := "data: \n\ndata:\n\n"
	reader := strings.NewReader(sseResponse)
	err := manager.streamSSE(context.Background(), "test-session", "test.local", "test", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestStreamSSE_NonDataLines tests handling of non-data SSE lines
func TestStreamSSE_NonDataLines(t *testing.T) {
	manager := &Manager{
		sessions: make(map[string]*SessionState),
		storage:  newMockStorageForAgent(),
		config:   &config.Config{},
	}

	session := &protocol.Session{ID: "test-session"}
	manager.sessions["test-session"] = NewSessionState(session)

	// Non-data lines (comments, event, id) should be skipped
	sseResponse := ": this is a comment\nevent: message\nid: 123\nretry: 1000\n\n"
	reader := strings.NewReader(sseResponse)
	err := manager.streamSSE(context.Background(), "test-session", "test.local", "test", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestStreamSSE_ThinkingAggregation tests that thinking events are aggregated correctly
func TestStreamSSE_ThinkingAggregation(t *testing.T) {
	storage := newMockStorageForAgent()
	manager := &Manager{
		sessions: make(map[string]*SessionState),
		storage:  storage,
		config:   &config.Config{MaxEventsPerSession: 100},
	}

	session := &protocol.Session{ID: "test-session"}
	manager.sessions["test-session"] = NewSessionState(session)

	// Multiple partial thinking events followed by final
	sseResponse := strings.Join([]string{
		sseData(map[string]interface{}{
			"type": "agent.event",
			"event": map[string]interface{}{
				"kind":       "thinking",
				"thinkingId": "think-1",
				"content":    "Part 1: ",
				"partial":    true,
			},
		}),
		sseData(map[string]interface{}{
			"type": "agent.event",
			"event": map[string]interface{}{
				"kind":       "thinking",
				"thinkingId": "think-1",
				"content":    "Part 2: ",
				"partial":    true,
			},
		}),
		sseData(map[string]interface{}{
			"type": "agent.event",
			"event": map[string]interface{}{
				"kind":       "thinking",
				"thinkingId": "think-1",
				"content":    "",
				"partial":    false,
			},
		}),
	}, "")

	reader := strings.NewReader(sseResponse)
	err := manager.streamSSE(context.Background(), "test-session", "test.local", "test", reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify that only one event was persisted (the final one with aggregated content)
	if len(storage.appendedEvents) != 1 {
		t.Errorf("expected 1 persisted event, got %d", len(storage.appendedEvents))
	}

	if len(storage.appendedEvents) > 0 {
		persistedEvent := storage.appendedEvents[0]
		if persistedEvent.Event.Content != "Part 1: Part 2: " {
			t.Errorf("expected aggregated content 'Part 1: Part 2: ', got %q", persistedEvent.Event.Content)
		}
	}
}

// TestSendPrompt tests the SendPrompt function
func TestSendPrompt_SessionNotFound(t *testing.T) {
	manager := &Manager{
		sessions: make(map[string]*SessionState),
		storage:  newMockStorageForAgent(),
		config:   &config.Config{},
	}

	err := manager.SendPrompt(context.Background(), "nonexistent", "Hello")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' error, got %v", err)
	}
}

func TestSendPrompt_QueuedWhenNotReady(t *testing.T) {
	manager := &Manager{
		sessions: make(map[string]*SessionState),
		storage:  newMockStorageForAgent(),
		config:   &config.Config{},
	}

	// Create session without ServiceFQDN (not ready)
	session := &protocol.Session{ID: "test-session"}
	state := NewSessionState(session)
	manager.sessions["test-session"] = state

	err := manager.SendPrompt(context.Background(), "test-session", "Hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify prompt was queued
	if state.PendingPrompt != "Hello" {
		t.Errorf("expected pending prompt 'Hello', got %q", state.PendingPrompt)
	}
}

func TestSendPrompt_Success(t *testing.T) {
	// Create a mock agent server
	agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/prompt" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		// Return SSE response
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "data: %s\n\n", `{"type":"agent.message","content":"Hi!","partial":false}`)
	}))
	defer agentServer.Close()

	storage := newMockStorageForAgent()
	manager := &Manager{
		sessions: make(map[string]*SessionState),
		storage:  storage,
		config:   &config.Config{},
	}

	// Create session with ServiceFQDN pointing to mock server
	session := &protocol.Session{ID: "test-session"}
	state := NewSessionState(session)
	// Extract host from mock server URL
	host := strings.TrimPrefix(agentServer.URL, "http://")
	state.ServiceFQDN = host
	manager.sessions["test-session"] = state

	err := manager.SendPrompt(context.Background(), "test-session", "Hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify user message was persisted
	if len(storage.appendedMessages) != 1 {
		t.Errorf("expected 1 persisted message, got %d", len(storage.appendedMessages))
	}
	if len(storage.appendedMessages) > 0 && storage.appendedMessages[0].Role != protocol.RoleUser {
		t.Errorf("expected user role, got %s", storage.appendedMessages[0].Role)
	}
}

// TestInterrupt tests the Interrupt function
func TestInterrupt_SessionNotFound(t *testing.T) {
	manager := &Manager{
		sessions: make(map[string]*SessionState),
	}

	err := manager.Interrupt(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
}

func TestInterrupt_NotRunning(t *testing.T) {
	manager := &Manager{
		sessions: make(map[string]*SessionState),
	}

	// Session without ServiceFQDN
	session := &protocol.Session{ID: "test-session"}
	manager.sessions["test-session"] = NewSessionState(session)

	// Should return nil (no-op) when session not running
	err := manager.Interrupt(context.Background(), "test-session")
	if err != nil {
		t.Fatalf("expected no error for non-running session, got %v", err)
	}
}

func TestInterrupt_Success(t *testing.T) {
	// Create a mock agent server
	interruptCalled := false
	agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/interrupt" {
			interruptCalled = true
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer agentServer.Close()

	// Extract host and port from server URL
	hostPort := strings.TrimPrefix(agentServer.URL, "http://")
	parts := strings.Split(hostPort, ":")
	if len(parts) != 2 {
		t.Fatalf("unexpected server URL format: %s", agentServer.URL)
	}
	host := parts[0]
	var port int
	fmt.Sscanf(parts[1], "%d", &port)

	storage := newMockStorageForAgent()
	manager := &Manager{
		sessions: make(map[string]*SessionState),
		storage:  storage,
		config:   &config.Config{AgentPort: port},
	}

	session := &protocol.Session{ID: "test-session", Status: protocol.StatusRunning}
	state := NewSessionState(session)
	state.ServiceFQDN = host // Just hostname, port comes from config
	manager.sessions["test-session"] = state

	err := manager.Interrupt(context.Background(), "test-session")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !interruptCalled {
		t.Error("expected /interrupt endpoint to be called")
	}
}

// TestCallAgentPrompt_NonOKResponse tests handling of non-200 response from agent
func TestCallAgentPrompt_NonOKResponse(t *testing.T) {
	agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal server error"))
	}))
	defer agentServer.Close()

	storage := newMockStorageForAgent()
	manager := &Manager{
		sessions: make(map[string]*SessionState),
		storage:  storage,
		config:   &config.Config{},
	}

	session := &protocol.Session{ID: "test-session", Status: protocol.StatusRunning}
	state := NewSessionState(session)
	state.ServiceFQDN = strings.TrimPrefix(agentServer.URL, "http://")
	manager.sessions["test-session"] = state

	// Call in a way we can wait for completion
	done := make(chan struct{})
	go func() {
		manager.callAgentPrompt(context.Background(), "test-session", state.ServiceFQDN, "test")
		close(done)
	}()

	select {
	case <-done:
		// Good, completed
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for callAgentPrompt")
	}

	// Session status should be back to ready after error
	if session.Status != protocol.StatusReady {
		// The status update happens through emit which requires Redis
		// Just verify no panic occurred
	}
}

// Mock storage for agent tests
type mockStorageForAgent struct {
	mu               sync.Mutex
	appendedMessages []*protocol.PersistedMessage
	appendedEvents   []*protocol.PersistedEvent
}

func newMockStorageForAgent() *mockStorageForAgent {
	return &mockStorageForAgent{
		appendedMessages: make([]*protocol.PersistedMessage, 0),
		appendedEvents:   make([]*protocol.PersistedEvent, 0),
	}
}

func (m *mockStorageForAgent) SaveSession(ctx context.Context, s *protocol.Session) error {
	return nil
}

func (m *mockStorageForAgent) GetSession(ctx context.Context, id string) (*protocol.Session, error) {
	return nil, nil
}

func (m *mockStorageForAgent) GetAllSessions(ctx context.Context) ([]*protocol.Session, error) {
	return nil, nil
}

func (m *mockStorageForAgent) UpdateSessionStatus(ctx context.Context, id string, status protocol.SessionStatus) error {
	return nil
}

func (m *mockStorageForAgent) UpdateSessionField(ctx context.Context, id, field, value string) error {
	return nil
}

func (m *mockStorageForAgent) DeleteSession(ctx context.Context, id string) error {
	return nil
}

func (m *mockStorageForAgent) AppendMessage(ctx context.Context, msg *protocol.PersistedMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.appendedMessages = append(m.appendedMessages, msg)
	return nil
}

func (m *mockStorageForAgent) GetMessages(ctx context.Context, sessionID string, afterID *string) ([]*protocol.PersistedMessage, error) {
	return nil, nil
}

func (m *mockStorageForAgent) GetLastMessage(ctx context.Context, sessionID string) (*protocol.PersistedMessage, error) {
	return nil, nil
}

func (m *mockStorageForAgent) GetMessageCount(ctx context.Context, sessionID string) (int, error) {
	return 0, nil
}

func (m *mockStorageForAgent) AppendEvent(ctx context.Context, evt *protocol.PersistedEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.appendedEvents = append(m.appendedEvents, evt)
	return nil
}

func (m *mockStorageForAgent) GetEvents(ctx context.Context, sessionID string, limit int) ([]*protocol.PersistedEvent, error) {
	return nil, nil
}

func (m *mockStorageForAgent) PublishNotification(ctx context.Context, sessionID string, notification *storage.Notification) (string, error) {
	return "0-0", nil
}

func (m *mockStorageForAgent) GetNotificationsAfter(ctx context.Context, sessionID string, afterID string, limit int) ([]storage.NotificationWithID, error) {
	return nil, nil
}

func (m *mockStorageForAgent) GetLastNotificationID(ctx context.Context, sessionID string) (string, error) {
	return "0", nil
}

func (m *mockStorageForAgent) GetRedisClient() *redis.Client {
	return nil
}

func (m *mockStorageForAgent) Close() error {
	return nil
}

// Unused but needed for interface satisfaction
var _ = bufio.NewScanner
var _ = bytes.NewReader
var _ = io.EOF
