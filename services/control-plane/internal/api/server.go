package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/angristan/netclode/services/control-plane/gen/netclode/v1"
	"github.com/angristan/netclode/services/control-plane/gen/netclode/v1/netclodev1connect"
	"github.com/angristan/netclode/services/control-plane/internal/protocol"
	"github.com/angristan/netclode/services/control-plane/internal/session"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

const (
	// ShutdownTimeout is the maximum time to wait for connections to drain
	ShutdownTimeout = 30 * time.Second
)

// Server is the HTTP server with Connect protocol and graceful shutdown support.
type Server struct {
	manager    *session.Manager
	httpServer *http.Server

	// Connect connection tracking
	connectConnections sync.Map // map[*ConnectConnection]struct{}

	connCount  atomic.Int64
	shutdownCh chan struct{}
	wg         sync.WaitGroup
}

// NewServer creates a new server.
func NewServer(manager *session.Manager) *Server {
	s := &Server{
		manager:    manager,
		shutdownCh: make(chan struct{}),
	}

	// Set up callback for auto-pause broadcasts
	manager.SetOnSessionUpdated(func(session *protocol.Session) {
		// Convert protocol.ServerMessage to pb.ServerMessage for Connect clients
		protoMsg := protocol.NewSessionUpdated(session)
		pbMsg := convertServerMessage(protoMsg)
		s.BroadcastToAllConnect(pbMsg, nil)
	})

	return s
}

// BroadcastToAllConnect sends a message to all connected Connect clients except the sender.
func (s *Server) BroadcastToAllConnect(msg *pb.ServerMessage, exclude *ConnectConnection) {
	s.connectConnections.Range(func(key, value interface{}) bool {
		if conn, ok := key.(*ConnectConnection); ok && conn != exclude {
			// Non-blocking send to avoid blocking broadcast
			select {
			case conn.globalMessages <- msg:
			default:
				slog.Debug("Skipping global message for slow Connect client")
			}
		}
		return true
	})
}

// ListenAndServe starts the HTTP server with Connect protocol support.
func (s *Server) ListenAndServe(ctx context.Context, httpAddr string) error {
	// Create a single mux for both HTTP endpoints and Connect services
	mux := http.NewServeMux()

	// HTTP endpoints
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /internal/session-config", s.handleSessionConfig)
	mux.HandleFunc("POST /internal/session/{sessionID}/event", s.handleInternalEvent)

	// Connect services (ClientService for iOS, AgentService for agents)
	clientHandler := NewConnectClientServiceHandler(s.manager, s)
	clientPath, clientHandlerFunc := netclodev1connect.NewClientServiceHandler(clientHandler)
	mux.Handle(clientPath, clientHandlerFunc)

	agentHandler := NewConnectAgentServiceHandler(s.manager, s)
	agentPath, agentHandlerFunc := netclodev1connect.NewAgentServiceHandler(agentHandler)
	mux.Handle(agentPath, agentHandlerFunc)

	// Wrap with h2c to support both HTTP/1.1 and HTTP/2 on the same port
	h2cHandler := h2c.NewHandler(mux, &http2.Server{})

	s.httpServer = &http.Server{
		Addr:    httpAddr,
		Handler: h2cHandler,
	}

	slog.Info("Starting h2c server (HTTP/1.1 + HTTP/2)", "addr", httpAddr)

	errCh := make(chan error, 1)

	// Start the main h2c server (handles both HTTP and Connect)
	go func() {
		if err := s.httpServer.ListenAndServe(); err != http.ErrServerClosed {
			errCh <- fmt.Errorf("h2c server: %w", err)
		}
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
	select {
	case <-s.shutdownCh:
		// Already closed
	default:
		close(s.shutdownCh)
	}

	// Create a context with timeout for the entire shutdown process
	ctx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()

	// Wait for all connections to close (with timeout)
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		slog.Info("All connections closed gracefully")
	case <-ctx.Done():
		slog.Warn("Timeout waiting for connections, forcing close",
			"remainingConnections", s.connCount.Load())
		// Force close remaining Connect connections
		s.connectConnections.Range(func(key, value interface{}) bool {
			if conn, ok := key.(*ConnectConnection); ok {
				conn.close()
			}
			return true
		})
	}

	// Shutdown the HTTP server
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
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
			// First try to extract from pod name pattern (sess-<sessionID>-*)
			sessionID = extractSessionIDFromPodName(podName)

			// If that doesn't work (warm pool pods have different names),
			// query K8s to find the sandbox with this pod name
			if sessionID == "" {
				var err error
				sessionID, err = s.manager.GetSessionIDByPodName(r.Context(), podName)
				if err != nil {
					slog.Debug("No session found for pod", "podName", podName, "error", err)
					// Return empty response - agent will retry
					http.Error(w, "no session assigned yet", http.StatusNotFound)
					return
				}
			}
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

// handleInternalEvent receives events from sandbox entrypoints/agents.
// POST /internal/session/{sessionID}/event
func (s *Server) handleInternalEvent(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	if sessionID == "" {
		http.Error(w, "sessionID required", http.StatusBadRequest)
		return
	}

	var event protocol.AgentEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		http.Error(w, "invalid event JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.manager.EmitEvent(r.Context(), sessionID, &event); err != nil {
		slog.Warn("Failed to emit internal event", "sessionID", sessionID, "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// Shutdown initiates graceful shutdown.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.gracefulShutdown()
}

// ActiveConnections returns the number of active Connect connections.
func (s *Server) ActiveConnections() int64 {
	return s.connCount.Load()
}
