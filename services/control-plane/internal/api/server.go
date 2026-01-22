package api

import (
	"context"
	"crypto/tls"
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
	"github.com/angristan/netclode/services/control-plane/internal/config"
	"github.com/angristan/netclode/services/control-plane/internal/protocol"
	"github.com/angristan/netclode/services/control-plane/internal/session"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"tailscale.com/tsnet"
)

const (
	// ShutdownTimeout is the maximum time to wait for connections to drain
	ShutdownTimeout = 30 * time.Second
)

// Server is the HTTP server with Connect protocol and graceful shutdown support.
type Server struct {
	manager       *session.Manager
	httpServer    *http.Server
	connectServer *http.Server
	tsnetServer   *tsnet.Server // Tailscale tsnet server (optional)

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
func (s *Server) ListenAndServe(ctx context.Context, httpAddr string, cfg *config.Config) error {
	// Create the main mux for HTTP endpoints
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /internal/session-config", s.handleSessionConfig)
	mux.HandleFunc("POST /internal/session/{sessionID}/event", s.handleInternalEvent)

	s.httpServer = &http.Server{
		Addr:    httpAddr,
		Handler: mux,
	}

	slog.Info("Starting HTTP server", "addr", httpAddr)

	errCh := make(chan error, 2)

	// Start HTTP server (for health checks and internal endpoints)
	go func() {
		if err := s.httpServer.ListenAndServe(); err != http.ErrServerClosed {
			errCh <- fmt.Errorf("http server: %w", err)
		}
	}()

	// Create Connect handler
	connectMux := http.NewServeMux()
	clientHandler := NewConnectClientServiceHandler(s.manager, s)
	path, handler := netclodev1connect.NewClientServiceHandler(clientHandler)
	connectMux.Handle(path, handler)

	// Start Connect server - either with tsnet (HTTPS) or h2c (HTTP/2 cleartext)
	if cfg.UseTailscale() {
		if err := s.startTsnetConnectServer(ctx, cfg, connectMux, errCh); err != nil {
			return err
		}
	} else {
		s.startH2cConnectServer(cfg.ConnectPort, connectMux, errCh)
	}

	select {
	case <-ctx.Done():
		return s.gracefulShutdown()
	case err := <-errCh:
		return err
	}
}

// startTsnetConnectServer starts the Connect server using Tailscale tsnet with automatic TLS.
func (s *Server) startTsnetConnectServer(ctx context.Context, cfg *config.Config, handler http.Handler, errCh chan error) error {
	s.tsnetServer = &tsnet.Server{
		Hostname: cfg.TailscaleHostname,
		Dir:      cfg.TailscaleStateDir,
		AuthKey:  cfg.TailscaleAuthKey,
	}

	// Start tsnet (joins the tailnet)
	status, err := s.tsnetServer.Up(ctx)
	if err != nil {
		return fmt.Errorf("tsnet up: %w", err)
	}

	slog.Info("Tailscale tsnet started",
		"hostname", cfg.TailscaleHostname,
		"tailscaleIP", status.TailscaleIPs[0].String(),
	)

	// Get plain TCP listener on tailscale interface
	ln, err := s.tsnetServer.Listen("tcp", ":443")
	if err != nil {
		return fmt.Errorf("tsnet listen: %w", err)
	}

	// Get the local client (needed for TLS cert management)
	lc, err := s.tsnetServer.LocalClient()
	if err != nil {
		return fmt.Errorf("tsnet local client: %w", err)
	}

	// Create TLS config with HTTP/2 support using tsnet's certificate
	tlsConfig := &tls.Config{
		GetCertificate: lc.GetCertificate,
		NextProtos:     []string{"h2", "http/1.1"}, // Enable HTTP/2 via ALPN
	}

	// Wrap listener with TLS
	tlsLn := tls.NewListener(ln, tlsConfig)

	s.connectServer = &http.Server{
		Handler: handler,
	}

	// Configure HTTP/2 for the server (required for bidirectional streaming)
	if err := http2.ConfigureServer(s.connectServer, &http2.Server{}); err != nil {
		return fmt.Errorf("configure http2: %w", err)
	}

	// Get the full hostname for logging
	fullHostname := fmt.Sprintf("https://%s.%s", cfg.TailscaleHostname, status.CurrentTailnet.MagicDNSSuffix)
	slog.Info("Starting Connect server with Tailscale TLS", "url", fullHostname)

	go func() {
		if err := s.connectServer.Serve(tlsLn); err != http.ErrServerClosed {
			errCh <- fmt.Errorf("connect server (tsnet): %w", err)
		}
	}()

	return nil
}

// startH2cConnectServer starts the Connect server using h2c (HTTP/2 cleartext).
func (s *Server) startH2cConnectServer(connectPort int, handler http.Handler, errCh chan error) {
	connectAddr := fmt.Sprintf(":%d", connectPort)

	// Use h2c for HTTP/2 without TLS (required for bidirectional streaming)
	h2cHandler := h2c.NewHandler(handler, &http2.Server{})

	s.connectServer = &http.Server{
		Addr:    connectAddr,
		Handler: h2cHandler,
	}

	slog.Info("Starting Connect server with h2c", "addr", connectAddr)

	go func() {
		if err := s.connectServer.ListenAndServe(); err != http.ErrServerClosed {
			errCh <- fmt.Errorf("connect server (h2c): %w", err)
		}
	}()
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

	// Shutdown Connect server
	if s.connectServer != nil {
		slog.Info("Stopping Connect server")
		if err := s.connectServer.Shutdown(ctx); err != nil {
			slog.Warn("Error shutting down Connect server", "error", err)
		}
	}

	// Shutdown tsnet server
	if s.tsnetServer != nil {
		slog.Info("Stopping Tailscale tsnet server")
		if err := s.tsnetServer.Close(); err != nil {
			slog.Warn("Error closing tsnet server", "error", err)
		}
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
