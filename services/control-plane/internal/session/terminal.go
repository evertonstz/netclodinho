package session

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"connectrpc.com/connect"
	v1 "github.com/angristan/netclode/services/control-plane/gen/netclode/v1"
	"github.com/angristan/netclode/services/control-plane/gen/netclode/v1/netclodev1connect"
)

const (
	maxReconnectAttempts  = 5
	initialReconnectDelay = 1 * time.Second
)

// TerminalManager manages Connect streams to agent terminals.
type TerminalManager struct {
	mu    sync.RWMutex
	conns map[string]*terminalConn // sessionID -> connection

	// Callback to emit terminal output to clients
	emitOutput func(ctx context.Context, sessionID, data string)

	// Port where agent terminals listen
	agentPort int
}

// terminalConn represents a Connect bidirectional stream to an agent's terminal.
type terminalConn struct {
	sessionID string
	fqdn      string
	stream    *connect.BidiStreamForClient[v1.TerminalInput, v1.TerminalOutput]
	cancel    context.CancelFunc
	writeMu   sync.Mutex
}

// NewTerminalManager creates a new terminal manager.
func NewTerminalManager(emitOutput func(ctx context.Context, sessionID, data string), agentPort int) *TerminalManager {
	return &TerminalManager{
		conns:      make(map[string]*terminalConn),
		emitOutput: emitOutput,
		agentPort:  agentPort,
	}
}

// EnsureConnected ensures a terminal Connect stream exists for the session.
// If not connected, it connects to the agent.
func (tm *TerminalManager) EnsureConnected(ctx context.Context, sessionID, fqdn string) error {
	tm.mu.RLock()
	conn := tm.conns[sessionID]
	tm.mu.RUnlock()

	if conn != nil {
		return nil // Already connected
	}

	return tm.connect(ctx, sessionID, fqdn)
}

// connect establishes a Connect bidirectional stream to the agent's terminal.
func (tm *TerminalManager) connect(ctx context.Context, sessionID, fqdn string) error {
	tm.mu.Lock()
	// Double-check after acquiring write lock
	if _, exists := tm.conns[sessionID]; exists {
		tm.mu.Unlock()
		return nil
	}
	tm.mu.Unlock()

	baseURL := fmt.Sprintf("http://%s:%d", fqdn, tm.agentPort)
	slog.Info("Connecting to agent terminal via Connect", "sessionID", sessionID, "url", baseURL)

	client := netclodev1connect.NewAgentServiceClient(http.DefaultClient, baseURL)

	// Create a context that can be cancelled for this connection
	connCtx, cancel := context.WithCancel(context.Background())

	stream := client.Terminal(connCtx)

	conn := &terminalConn{
		sessionID: sessionID,
		fqdn:      fqdn,
		stream:    stream,
		cancel:    cancel,
	}

	tm.mu.Lock()
	tm.conns[sessionID] = conn
	tm.mu.Unlock()

	// Start reading output
	go tm.readLoop(connCtx, conn)

	slog.Info("Connected to agent terminal via Connect", "sessionID", sessionID)
	return nil
}

// readLoop reads output from the agent terminal and forwards to clients.
func (tm *TerminalManager) readLoop(ctx context.Context, conn *terminalConn) {
	defer func() {
		conn.stream.CloseResponse()
		tm.scheduleReconnect(conn.sessionID, conn.fqdn)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msg, err := conn.stream.Receive()
		if err != nil {
			if ctx.Err() != nil {
				// Context cancelled, don't reconnect
				return
			}
			slog.Warn("Terminal Connect stream error", "sessionID", conn.sessionID, "error", err)
			return
		}

		if msg != nil && msg.Data != "" && tm.emitOutput != nil {
			tm.emitOutput(ctx, conn.sessionID, msg.Data)
		}
	}
}

// scheduleReconnect attempts to reconnect after a connection drop.
func (tm *TerminalManager) scheduleReconnect(sessionID, fqdn string) {
	// Remove the old connection
	tm.mu.Lock()
	delete(tm.conns, sessionID)
	tm.mu.Unlock()

	go func() {
		delay := initialReconnectDelay
		for attempt := 1; attempt <= maxReconnectAttempts; attempt++ {
			slog.Info("Terminal reconnect attempt", "sessionID", sessionID, "attempt", attempt, "delay", delay)
			time.Sleep(delay)

			// Check if session still needs terminal (not paused/deleted)
			tm.mu.RLock()
			_, stillTracked := tm.conns[sessionID]
			tm.mu.RUnlock()

			// If another connection was established, stop reconnecting
			if stillTracked {
				slog.Info("Terminal reconnect cancelled, new connection exists", "sessionID", sessionID)
				return
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := tm.connect(ctx, sessionID, fqdn)
			cancel()

			if err == nil {
				slog.Info("Terminal reconnected", "sessionID", sessionID)
				return
			}

			slog.Warn("Terminal reconnect failed", "sessionID", sessionID, "attempt", attempt, "error", err)
			delay *= 2 // Exponential backoff: 1s, 2s, 4s, 8s, 16s
		}

		slog.Warn("Terminal reconnect gave up after max attempts", "sessionID", sessionID)
	}()
}

// SendInput sends input data to the agent terminal.
func (tm *TerminalManager) SendInput(sessionID, data string) error {
	tm.mu.RLock()
	conn := tm.conns[sessionID]
	tm.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("no terminal connection for session %s", sessionID)
	}

	conn.writeMu.Lock()
	defer conn.writeMu.Unlock()

	return conn.stream.Send(&v1.TerminalInput{
		Input: &v1.TerminalInput_Data{Data: data},
	})
}

// Resize sends a resize command to the agent terminal.
func (tm *TerminalManager) Resize(sessionID string, cols, rows int) error {
	tm.mu.RLock()
	conn := tm.conns[sessionID]
	tm.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("no terminal connection for session %s", sessionID)
	}

	conn.writeMu.Lock()
	defer conn.writeMu.Unlock()

	return conn.stream.Send(&v1.TerminalInput{
		Input: &v1.TerminalInput_Resize{
			Resize: &v1.TerminalResize{
				Cols: int32(cols),
				Rows: int32(rows),
			},
		},
	})
}

// Disconnect closes the terminal connection for a session.
func (tm *TerminalManager) Disconnect(sessionID string) {
	tm.mu.Lock()
	conn := tm.conns[sessionID]
	if conn != nil {
		conn.cancel() // Cancel context stops readLoop
		delete(tm.conns, sessionID)
	}
	tm.mu.Unlock()

	if conn != nil {
		conn.stream.CloseRequest()
		slog.Info("Terminal disconnected", "sessionID", sessionID)
	}
}

// Close closes all terminal connections.
func (tm *TerminalManager) Close() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	for sessionID, conn := range tm.conns {
		conn.cancel()
		conn.stream.CloseRequest()
		slog.Info("Terminal closed", "sessionID", sessionID)
	}
	tm.conns = make(map[string]*terminalConn)
}
