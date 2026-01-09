package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/angristan/netclode/services/control-plane/internal/protocol"
	"github.com/angristan/netclode/services/control-plane/internal/session"
	"github.com/coder/websocket"
)

const (
	// ShutdownTimeout is the maximum time to wait for connections to drain
	ShutdownTimeout = 30 * time.Second
)

// Server is the HTTP/WebSocket server with graceful shutdown support.
type Server struct {
	manager *session.Manager
	server  *http.Server

	// Connection tracking for graceful shutdown
	connections sync.Map // map[*Connection]struct{}
	connCount   atomic.Int64
	shutdownCh  chan struct{}
	wg          sync.WaitGroup
}

// NewServer creates a new server.
func NewServer(manager *session.Manager) *Server {
	s := &Server{
		manager:    manager,
		shutdownCh: make(chan struct{}),
	}

	// Set up callback for auto-pause broadcasts
	manager.SetOnSessionUpdated(func(session *protocol.Session) {
		s.BroadcastToAll(protocol.NewSessionUpdated(session), nil)
	})

	return s
}

// BroadcastToAll sends a message to all connected clients except the sender.
func (s *Server) BroadcastToAll(msg protocol.ServerMessage, exclude *Connection) {
	s.connections.Range(func(key, value interface{}) bool {
		if conn, ok := key.(*Connection); ok && conn != exclude {
			// Non-blocking send to avoid blocking broadcast
			select {
			case conn.globalMessages <- msg:
			default:
				slog.Debug("Skipping global message for slow client")
			}
		}
		return true
	})
}

// ListenAndServe starts the HTTP server.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /ws", s.handleWebSocket)
	mux.HandleFunc("GET /internal/session-config", s.handleSessionConfig)

	s.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	slog.Info("Starting HTTP server", "addr", addr)

	errCh := make(chan error, 1)
	go func() {
		if err := s.server.ListenAndServe(); err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		return s.gracefulShutdown()
	case err := <-errCh:
		return err
	}
}

// gracefulShutdown performs graceful shutdown with connection draining.
func (s *Server) gracefulShutdown() error {
	slog.Info("Starting graceful shutdown", "activeConnections", s.connCount.Load())

	// Signal all connections to start closing
	close(s.shutdownCh)

	// Create a context with timeout for the entire shutdown process
	ctx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()

	// Wait for all WebSocket connections to close (with timeout)
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		slog.Info("All WebSocket connections closed gracefully")
	case <-ctx.Done():
		slog.Warn("Timeout waiting for WebSocket connections, forcing close",
			"remainingConnections", s.connCount.Load())
		// Force close remaining connections
		s.connections.Range(func(key, value interface{}) bool {
			if conn, ok := key.(*Connection); ok {
				conn.Close()
			}
			return true
		})
	}

	// Shutdown the HTTP server
	return s.server.Shutdown(ctx)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// handleSessionConfig returns session configuration for agents.
// GET /internal/session-config?session=<sessionID> OR ?pod=<podName>
func (s *Server) handleSessionConfig(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")

	// If no session ID, try to derive from pod name
	if sessionID == "" {
		podName := r.URL.Query().Get("pod")
		if podName != "" {
			sessionID = extractSessionIDFromPodName(podName)
		}
	}

	if sessionID == "" {
		http.Error(w, "session or pod parameter required", http.StatusBadRequest)
		return
	}

	config, err := s.manager.GetSessionConfig(r.Context(), sessionID)
	if err != nil {
		slog.Warn("Failed to get session config", "sessionID", sessionID, "error", err)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(config)
}

// extractSessionIDFromPodName extracts session ID from pod name format.
// Pod names follow patterns like: sess-<sessionID>-<suffix> or <sandboxName>-<suffix>
func extractSessionIDFromPodName(podName string) string {
	// Handle direct session format: sess-<sessionID> or sess-<sessionID>-<suffix>
	if strings.HasPrefix(podName, "sess-") {
		parts := strings.SplitN(podName, "-", 3)
		if len(parts) >= 2 {
			return parts[1]
		}
	}
	return ""
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Check if we're shutting down
	select {
	case <-s.shutdownCh:
		http.Error(w, "Server is shutting down", http.StatusServiceUnavailable)
		return
	default:
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // Allow any origin for now
	})
	if err != nil {
		slog.Error("Failed to accept WebSocket", "error", err)
		return
	}

	c := NewConnection(conn, s.manager, s)

	// Track connection
	s.connections.Store(c, struct{}{})
	s.connCount.Add(1)
	s.wg.Add(1)

	slog.Info("WebSocket connection opened",
		"remoteAddr", r.RemoteAddr,
		"activeConnections", s.connCount.Load())

	// Handle the connection
	c.Run(r.Context())

	// Untrack connection
	s.connections.Delete(c)
	s.connCount.Add(-1)
	s.wg.Done()

	slog.Info("WebSocket connection closed",
		"remoteAddr", r.RemoteAddr,
		"activeConnections", s.connCount.Load())
}

// Shutdown initiates graceful shutdown.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.gracefulShutdown()
}

// ActiveConnections returns the number of active WebSocket connections.
func (s *Server) ActiveConnections() int64 {
	return s.connCount.Load()
}
