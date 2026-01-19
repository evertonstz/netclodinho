package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	maxReconnectAttempts  = 5
	initialReconnectDelay = 1 * time.Second
)

// TerminalManager manages WebSocket connections to agent terminals.
type TerminalManager struct {
	mu    sync.RWMutex
	conns map[string]*terminalConn // sessionID -> connection

	// Callback to emit terminal output to clients
	emitOutput func(ctx context.Context, sessionID, data string)

	// Port where agent terminals listen
	agentPort int
}

// terminalConn represents a WebSocket connection to an agent's terminal.
type terminalConn struct {
	sessionID string
	fqdn      string
	ws        *websocket.Conn
	cancel    context.CancelFunc
	writeMu   sync.Mutex
}

// terminalMessage represents messages sent to/from the agent terminal.
type terminalMessage struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
}

// NewTerminalManager creates a new terminal manager.
func NewTerminalManager(emitOutput func(ctx context.Context, sessionID, data string), agentPort int) *TerminalManager {
	return &TerminalManager{
		conns:      make(map[string]*terminalConn),
		emitOutput: emitOutput,
		agentPort:  agentPort,
	}
}

// EnsureConnected ensures a terminal WebSocket connection exists for the session.
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

// connect establishes a WebSocket connection to the agent's terminal.
func (tm *TerminalManager) connect(ctx context.Context, sessionID, fqdn string) error {
	tm.mu.Lock()
	// Double-check after acquiring write lock
	if _, exists := tm.conns[sessionID]; exists {
		tm.mu.Unlock()
		return nil
	}
	tm.mu.Unlock()

	url := fmt.Sprintf("ws://%s:%d/terminal/ws", fqdn, tm.agentPort)
	slog.Info("Connecting to agent terminal", "sessionID", sessionID, "url", url)

	ws, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return fmt.Errorf("dial agent terminal: %w", err)
	}

	connCtx, cancel := context.WithCancel(context.Background())
	conn := &terminalConn{
		sessionID: sessionID,
		fqdn:      fqdn,
		ws:        ws,
		cancel:    cancel,
	}

	tm.mu.Lock()
	tm.conns[sessionID] = conn
	tm.mu.Unlock()

	// Start reading output
	go tm.readLoop(connCtx, conn)

	slog.Info("Connected to agent terminal", "sessionID", sessionID)
	return nil
}

// readLoop reads output from the agent terminal and forwards to clients.
func (tm *TerminalManager) readLoop(ctx context.Context, conn *terminalConn) {
	defer func() {
		conn.ws.Close(websocket.StatusNormalClosure, "")
		tm.scheduleReconnect(conn.sessionID, conn.fqdn)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_, data, err := conn.ws.Read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				// Context cancelled, don't reconnect
				return
			}
			slog.Warn("Terminal WebSocket read error", "sessionID", conn.sessionID, "error", err)
			return
		}

		var msg terminalMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			slog.Warn("Terminal message parse error", "sessionID", conn.sessionID, "error", err)
			continue
		}

		if msg.Type == "output" && tm.emitOutput != nil {
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

	msg := terminalMessage{Type: "input", Data: data}
	return tm.sendMessage(conn, msg)
}

// Resize sends a resize command to the agent terminal.
func (tm *TerminalManager) Resize(sessionID string, cols, rows int) error {
	tm.mu.RLock()
	conn := tm.conns[sessionID]
	tm.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("no terminal connection for session %s", sessionID)
	}

	msg := terminalMessage{Type: "resize", Cols: cols, Rows: rows}
	return tm.sendMessage(conn, msg)
}

// sendMessage sends a JSON message to the agent terminal.
func (tm *TerminalManager) sendMessage(conn *terminalConn, msg terminalMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	conn.writeMu.Lock()
	defer conn.writeMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return conn.ws.Write(ctx, websocket.MessageText, data)
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
		conn.ws.Close(websocket.StatusNormalClosure, "session paused")
		slog.Info("Terminal disconnected", "sessionID", sessionID)
	}
}

// Close closes all terminal connections.
func (tm *TerminalManager) Close() {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	for sessionID, conn := range tm.conns {
		conn.cancel()
		conn.ws.Close(websocket.StatusGoingAway, "shutdown")
		slog.Info("Terminal closed", "sessionID", sessionID)
	}
	tm.conns = make(map[string]*terminalConn)
}
