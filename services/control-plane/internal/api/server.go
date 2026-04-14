package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	httptrace "github.com/DataDog/dd-trace-go/contrib/net/http/v2"

	pb "github.com/angristan/netclode/services/control-plane/gen/netclode/v1"
	"github.com/angristan/netclode/services/control-plane/gen/netclode/v1/netclodev1connect"
	"github.com/angristan/netclode/services/control-plane/internal/session"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/protobuf/encoding/protojson"
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
	manager.SetOnSessionUpdated(func(session *pb.Session) {
		// Session is already *pb.Session, create pb.ServerMessage directly
		pbMsg := &pb.ServerMessage{
			Message: &pb.ServerMessage_SessionUpdated{
				SessionUpdated: &pb.SessionUpdatedResponse{Session: session},
			},
		}
		s.BroadcastToAllConnect(pbMsg, nil)
	})

	return s
}

// BroadcastToAllConnect sends a message to all connected Connect clients except the sender.
func (s *Server) BroadcastToAllConnect(msg *pb.ServerMessage, exclude *ConnectConnection) {
	s.connectConnections.Range(func(key, value any) bool {
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
	mux.HandleFunc("POST /internal/session/{sessionID}/event", s.handleInternalEvent)
	mux.HandleFunc("POST /internal/validate-proxy-auth", s.handleValidateProxyAuth)
	mux.HandleFunc("POST /agent-startup-log", s.handleAgentStartupLog)
	mux.HandleFunc("GET /internal/session/{sessionID}/startup-logs", s.handleAgentStartupLogGet)

	// Connect services (ClientService for iOS, AgentService for agents)
	clientHandler := NewConnectClientServiceHandler(s.manager, s)
	clientPath, clientHandlerFunc := netclodev1connect.NewClientServiceHandler(clientHandler)
	mux.Handle(clientPath, clientHandlerFunc)

	agentHandler := NewConnectAgentServiceHandler(s.manager, s)
	agentPath, agentHandlerFunc := netclodev1connect.NewAgentServiceHandler(agentHandler)
	mux.Handle(agentPath, agentHandlerFunc)

	// Wrap with Datadog tracing to capture HTTP spans
	tracedHandler := httptrace.WrapHandler(mux, "control-plane", "http.request")

	// Wrap with h2c to support both HTTP/1.1 and HTTP/2 on the same port
	h2cHandler := h2c.NewHandler(tracedHandler, &http2.Server{})

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
		s.connectConnections.Range(func(key, value any) bool {
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

// validateProxyAuthRequest is the request body for proxy auth validation.
type validateProxyAuthRequest struct {
	Token      string `json:"token"`
	TargetHost string `json:"target_host"`
}

// validateProxyAuthResponse is the response for proxy auth validation.
type validateProxyAuthResponse struct {
	Allowed     bool   `json:"allowed"`
	SecretKey   string `json:"secret_key,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	SessionID   string `json:"session_id,omitempty"`
	Error       string `json:"error,omitempty"`
}

// handleValidateProxyAuth validates proxy authentication requests from the secret-proxy.
// POST /internal/validate-proxy-auth
// Body: {"token": "<k8s-sa-token>", "target_host": "api.anthropic.com"}
// Returns: {"allowed": true, "secret_key": "anthropic", "placeholder": "NETCLODE_PLACEHOLDER_anthropic"}
func (s *Server) handleValidateProxyAuth(w http.ResponseWriter, r *http.Request) {
	var req validateProxyAuthRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(validateProxyAuthResponse{Error: "invalid request body"})
		return
	}

	if req.Token == "" || req.TargetHost == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(validateProxyAuthResponse{Error: "token and target_host required"})
		return
	}

	result, err := s.manager.ValidateProxyAuth(r.Context(), req.Token, req.TargetHost)
	if err != nil {
		slog.Warn("Proxy auth validation failed",
			"targetHost", req.TargetHost,
			"error", err,
			"tokenLen", len(req.Token),
		)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(validateProxyAuthResponse{Allowed: false, Error: err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(validateProxyAuthResponse{
		Allowed:     result.Allowed,
		SecretKey:   result.SecretKey,
		Placeholder: result.Placeholder,
		SessionID:   result.SessionID,
	})
}

// handleInternalEvent receives events from sandbox entrypoints/agents.
// POST /internal/session/{sessionID}/event
func (s *Server) handleInternalEvent(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	if sessionID == "" {
		http.Error(w, "sessionID required", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body: "+err.Error(), http.StatusBadRequest)
		return
	}

	var event pb.AgentEvent
	if err := protojson.Unmarshal(body, &event); err != nil {
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
