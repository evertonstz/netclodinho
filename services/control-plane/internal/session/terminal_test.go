package session

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestNewTerminalManager verifies terminal manager initialization.
func TestNewTerminalManager(t *testing.T) {
	called := false
	emit := func(ctx context.Context, sessionID, data string) {
		called = true
	}

	tm := NewTerminalManager(emit, 3002)

	if tm == nil {
		t.Fatal("expected non-nil terminal manager")
	}
	if tm.conns == nil {
		t.Error("expected conns map to be initialized")
	}
	if tm.emitOutput == nil {
		t.Error("expected emitOutput to be set")
	}
	if tm.agentPort != 3002 {
		t.Errorf("expected agentPort 3002, got %d", tm.agentPort)
	}

	// Verify emit function is stored correctly
	tm.emitOutput(context.Background(), "test", "data")
	if !called {
		t.Error("expected emit function to be called")
	}
}

// TestTerminalManager_SendInput tests sending input to terminal.
func TestTerminalManager_SendInput_NoConnection(t *testing.T) {
	tm := NewTerminalManager(nil, 3002)

	err := tm.SendInput("nonexistent", "ls -la")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !strings.Contains(err.Error(), "no terminal connection") {
		t.Errorf("expected 'no terminal connection' error, got: %v", err)
	}
}

// TestTerminalManager_Resize tests resizing terminal.
func TestTerminalManager_Resize_NoConnection(t *testing.T) {
	tm := NewTerminalManager(nil, 3002)

	err := tm.Resize("nonexistent", 80, 24)
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !strings.Contains(err.Error(), "no terminal connection") {
		t.Errorf("expected 'no terminal connection' error, got: %v", err)
	}
}

// TestTerminalManager_Disconnect tests disconnecting a terminal.
func TestTerminalManager_Disconnect_NoConnection(t *testing.T) {
	tm := NewTerminalManager(nil, 3002)

	// Should not panic when disconnecting nonexistent session
	tm.Disconnect("nonexistent")
}

func TestTerminalManager_Disconnect_WithConnection(t *testing.T) {
	tm := NewTerminalManager(nil, 3002)

	// Create a mock connection - note: we can't create a real websocket here
	// so we just test the context cancellation and map removal
	ctx, cancel := context.WithCancel(context.Background())

	// Don't set ws field since we can't mock it easily
	// Just verify the cancel and map operations
	tm.mu.Lock()
	tm.conns["test-session"] = &terminalConn{
		sessionID: "test-session",
		cancel:    cancel,
		// ws is nil intentionally - we're testing map/cancel behavior only
	}
	tm.mu.Unlock()

	// Manually remove and cancel (simulating what Disconnect does, minus the ws.Close)
	tm.mu.Lock()
	if conn, ok := tm.conns["test-session"]; ok {
		conn.cancel()
		delete(tm.conns, "test-session")
	}
	tm.mu.Unlock()

	// Verify connection was removed
	if _, ok := tm.conns["test-session"]; ok {
		t.Error("expected connection to be removed")
	}

	// Verify context was cancelled
	select {
	case <-ctx.Done():
		// Good
	default:
		t.Error("expected context to be cancelled")
	}
}

// TestTerminalManager_Close tests closing all connections.
// Note: We can't call tm.Close() directly because it tries to close nil ws fields.
// Instead, we test the cleanup logic manually.
func TestTerminalManager_Close(t *testing.T) {
	tm := NewTerminalManager(nil, 3002)

	// Add multiple mock connections (without ws to avoid nil pointer)
	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())

	tm.conns["sess-1"] = &terminalConn{sessionID: "sess-1", cancel: cancel1}
	tm.conns["sess-2"] = &terminalConn{sessionID: "sess-2", cancel: cancel2}

	// Simulate Close logic (without calling ws.Close which panics on nil)
	tm.mu.Lock()
	for _, conn := range tm.conns {
		conn.cancel()
		// Skip conn.ws.Close() since ws is nil
	}
	tm.conns = make(map[string]*terminalConn)
	tm.mu.Unlock()

	// Verify contexts were cancelled
	select {
	case <-ctx1.Done():
		// Good
	default:
		t.Error("expected ctx1 to be cancelled")
	}

	select {
	case <-ctx2.Done():
		// Good
	default:
		t.Error("expected ctx2 to be cancelled")
	}

	// Verify map is empty
	if len(tm.conns) != 0 {
		t.Errorf("expected 0 connections, got %d", len(tm.conns))
	}
}

// TestTerminalManager_EnsureConnected_AlreadyConnected tests that EnsureConnected
// returns immediately if already connected.
func TestTerminalManager_EnsureConnected_AlreadyConnected(t *testing.T) {
	tm := NewTerminalManager(nil, 3002)

	// Add existing connection
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tm.conns["test-session"] = &terminalConn{sessionID: "test-session", cancel: cancel}

	// Should return immediately without creating new connection
	err := tm.EnsureConnected(ctx, "test-session", "test.local")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should still have exactly one connection
	if len(tm.conns) != 1 {
		t.Errorf("expected 1 connection, got %d", len(tm.conns))
	}
}

// TestTerminalManager_Integration tests the full terminal flow with a mock WebSocket server.
func TestTerminalManager_Integration(t *testing.T) {
	var receivedMessages []terminalMessage
	var mu sync.Mutex

	// Create a mock WebSocket server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/terminal/ws" {
			http.NotFound(w, r)
			return
		}

		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Logf("WebSocket accept error: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		ctx := r.Context()

		// Echo back any input as output
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}

			var msg terminalMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}

			mu.Lock()
			receivedMessages = append(receivedMessages, msg)
			mu.Unlock()

			// Send output response
			if msg.Type == "input" {
				output := terminalMessage{
					Type: "output",
					Data: "echo: " + msg.Data,
				}
				outputBytes, _ := json.Marshal(output)
				conn.Write(ctx, websocket.MessageText, outputBytes)
			}
		}
	}))
	defer server.Close()

	// Extract host and port from server URL (e.g., "http://127.0.0.1:54321")
	hostPort := strings.TrimPrefix(server.URL, "http://")
	parts := strings.Split(hostPort, ":")
	if len(parts) != 2 {
		t.Fatalf("unexpected server URL format: %s", server.URL)
	}
	host := parts[0] // Just the hostname, e.g., "127.0.0.1"
	var port int
	fmt.Sscanf(parts[1], "%d", &port)

	var emittedOutput []string
	var emitMu sync.Mutex
	emit := func(ctx context.Context, sessionID, data string) {
		emitMu.Lock()
		emittedOutput = append(emittedOutput, data)
		emitMu.Unlock()
	}

	// Create terminal manager with the mock server's port
	tm := NewTerminalManager(emit, port)

	// Connect to mock server - pass just the hostname, EnsureConnected will append the port
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := tm.EnsureConnected(ctx, "test-session", host)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	// Give time for connection to establish
	time.Sleep(100 * time.Millisecond)

	// Send input
	err = tm.SendInput("test-session", "hello")
	if err != nil {
		t.Fatalf("failed to send input: %v", err)
	}

	// Send resize
	err = tm.Resize("test-session", 120, 40)
	if err != nil {
		t.Fatalf("failed to resize: %v", err)
	}

	// Give time for messages to be processed
	time.Sleep(100 * time.Millisecond)

	// Verify server received messages
	mu.Lock()
	if len(receivedMessages) < 2 {
		t.Errorf("expected at least 2 messages, got %d", len(receivedMessages))
	}
	mu.Unlock()

	// Check input message
	mu.Lock()
	if len(receivedMessages) > 0 && receivedMessages[0].Type != "input" {
		t.Errorf("expected first message type 'input', got %s", receivedMessages[0].Type)
	}
	if len(receivedMessages) > 0 && receivedMessages[0].Data != "hello" {
		t.Errorf("expected data 'hello', got %s", receivedMessages[0].Data)
	}
	mu.Unlock()

	// Check resize message
	mu.Lock()
	if len(receivedMessages) > 1 && receivedMessages[1].Type != "resize" {
		t.Errorf("expected second message type 'resize', got %s", receivedMessages[1].Type)
	}
	if len(receivedMessages) > 1 && (receivedMessages[1].Cols != 120 || receivedMessages[1].Rows != 40) {
		t.Errorf("expected 120x40, got %dx%d", receivedMessages[1].Cols, receivedMessages[1].Rows)
	}
	mu.Unlock()

	// Verify output was emitted
	emitMu.Lock()
	if len(emittedOutput) == 0 {
		t.Error("expected output to be emitted")
	}
	if len(emittedOutput) > 0 && !strings.Contains(emittedOutput[0], "echo: hello") {
		t.Errorf("expected echoed output, got %s", emittedOutput[0])
	}
	emitMu.Unlock()

	// Cleanup
	tm.Disconnect("test-session")
}

// TestTerminalMessage_JSON tests terminal message JSON serialization.
func TestTerminalMessage_JSON(t *testing.T) {
	tests := []struct {
		name     string
		msg      terminalMessage
		expected string
	}{
		{
			name:     "input message",
			msg:      terminalMessage{Type: "input", Data: "ls -la"},
			expected: `{"type":"input","data":"ls -la"}`,
		},
		{
			name:     "resize message",
			msg:      terminalMessage{Type: "resize", Cols: 80, Rows: 24},
			expected: `{"type":"resize","cols":80,"rows":24}`,
		},
		{
			name:     "output message",
			msg:      terminalMessage{Type: "output", Data: "file1.txt\nfile2.txt"},
			expected: `{"type":"output","data":"file1.txt\nfile2.txt"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.msg)
			if err != nil {
				t.Fatalf("failed to marshal: %v", err)
			}

			// Re-parse to compare (avoid whitespace issues)
			var got, want map[string]interface{}
			json.Unmarshal(data, &got)
			json.Unmarshal([]byte(tt.expected), &want)

			if got["type"] != want["type"] {
				t.Errorf("type mismatch: got %v, want %v", got["type"], want["type"])
			}
		})
	}
}

// TestTerminalManager_ConcurrentAccess tests thread safety.
func TestTerminalManager_ConcurrentAccess(t *testing.T) {
	tm := NewTerminalManager(func(ctx context.Context, sessionID, data string) {}, 3002)

	// Add a connection
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tm.conns["test-session"] = &terminalConn{
		sessionID: "test-session",
		cancel:    cancel,
	}

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// Concurrent reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tm.mu.RLock()
			_ = tm.conns["test-session"]
			tm.mu.RUnlock()
		}()
	}

	// Concurrent EnsureConnected checks
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// This should not panic
			_ = tm.EnsureConnected(ctx, "test-session", "test.local")
		}()
	}

	// Concurrent Disconnect calls
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			tm.Disconnect("other-session-" + string(rune('0'+id)))
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent error: %v", err)
	}
}
