package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	maxStartupLogBytes = 64 * 1024 // 64 KB
)

// secretPattern matches NETCLODE_PLACEHOLDER_* tokens and common key patterns.
var secretPattern = regexp.MustCompile(`(?i)(NETCLODE_PLACEHOLDER_\S+|Bearer\s+\S{20,}|ghp_\S+|sk-\S+)`)

// agentStartupLogRequest is the payload sent by the agent on boot.
type agentStartupLogRequest struct {
	SessionID string `json:"session_id"`
	Log       string `json:"log"`
	Timestamp string `json:"timestamp"`
}

// handleAgentStartupLog receives a compact startup log from an agent and persists it.
// POST /agent-startup-log
// Header: Authorization: Bearer <AGENT_SESSION_TOKEN>
func (s *Server) handleAgentStartupLog(w http.ResponseWriter, r *http.Request) {
	// ── 1. Auth ───────────────────────────────────────────────────────────────
	authHeader := r.Header.Get("Authorization")
	token := strings.TrimPrefix(authHeader, "Bearer ")
	if token == "" || token == authHeader {
		http.Error(w, "missing or invalid Authorization header", http.StatusUnauthorized)
		return
	}

	sessionID, ok := s.manager.LookupDockerToken(token)
	if !ok {
		http.Error(w, "invalid or unknown agent token", http.StatusUnauthorized)
		return
	}

	// ── 2. Read & size-limit body ─────────────────────────────────────────────
	limitedReader := http.MaxBytesReader(w, r.Body, maxStartupLogBytes+1)
	body, err := io.ReadAll(limitedReader)
	if err != nil {
		if strings.Contains(err.Error(), "http: request body too large") {
			http.Error(w, "payload exceeds 64 KB limit", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	if len(body) > maxStartupLogBytes {
		http.Error(w, "payload exceeds 64 KB limit", http.StatusRequestEntityTooLarge)
		return
	}

	var req agentStartupLogRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	// Validate session_id matches the token owner.
	if req.SessionID != "" && req.SessionID != sessionID {
		http.Error(w, "session_id mismatch", http.StatusForbidden)
		return
	}
	if req.SessionID == "" {
		req.SessionID = sessionID
	}

	// ── 3. Sanitize log content ───────────────────────────────────────────────
	sanitized := sanitizeLog(req.Log)

	// ── 4. Persist to workspace ───────────────────────────────────────────────
	if err := s.persistStartupLog(req.SessionID, sanitized, req.Timestamp); err != nil {
		slog.Warn("agent-startup-log: failed to persist log", "sessionID", req.SessionID, "error", err)
		// Still respond 200 — don't block agent boot over a persistence error.
	} else {
		slog.Info("agent-startup-log: persisted", "sessionID", req.SessionID, "bytes", len(sanitized))
	}

	w.WriteHeader(http.StatusOK)
}

// sanitizeLog redacts secret-like patterns from log text.
func sanitizeLog(log string) string {
	return secretPattern.ReplaceAllStringFunc(log, func(m string) string {
		if len(m) > 8 {
			return m[:4] + strings.Repeat("*", len(m)-4)
		}
		return "****"
	})
}

// persistStartupLog writes the log to the session workspace and enforces retention.
func (s *Server) persistStartupLog(sessionID, log, timestamp string) error {
	workspaceRoot := s.manager.Config().BoxliteWorkspaceRoot
	sessionDir := filepath.Join(workspaceRoot, sessionID)
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		return fmt.Errorf("create session dir: %w", err)
	}

	ts := timestamp
	if ts == "" {
		ts = time.Now().UTC().Format("20060102T150405Z")
	} else {
		// Normalize to a safe filename component.
		ts = strings.NewReplacer(":", "", "-", "", "T", "T", ".", "").Replace(ts)
	}

	logPath := filepath.Join(sessionDir, "agent-startup-"+ts+".log")
	if err := os.WriteFile(logPath, []byte(log), 0o600); err != nil {
		return fmt.Errorf("write startup log: %w", err)
	}

	// Prune old startup logs beyond retention limit.
	s.pruneStartupLogs(sessionDir, sessionID)
	return nil
}

// pruneStartupLogs removes oldest agent-startup-*.log files beyond the retention limit.
func (s *Server) pruneStartupLogs(sessionDir, sessionID string) {
	retention := s.manager.Config().BoxliteStartupLogRetention
	if retention <= 0 {
		return
	}
	entries, err := filepath.Glob(filepath.Join(sessionDir, "agent-startup-*.log"))
	if err != nil || len(entries) <= retention {
		return
	}
	sort.Strings(entries) // oldest first (timestamp-based names)
	toDelete := entries[:len(entries)-retention]
	for _, f := range toDelete {
		if err := os.Remove(f); err != nil {
			slog.Warn("agent-startup-log: failed to prune old log", "file", f, "sessionID", sessionID, "error", err)
		}
	}
}

// handleAgentStartupLogGet retrieves recent startup logs for a session (admin).
// GET /internal/session/{sessionID}/startup-logs
func (s *Server) handleAgentStartupLogGet(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	if sessionID == "" {
		http.Error(w, "sessionID required", http.StatusBadRequest)
		return
	}

	workspaceRoot := s.manager.Config().BoxliteWorkspaceRoot
	sessionDir := filepath.Join(workspaceRoot, sessionID)
	entries, _ := filepath.Glob(filepath.Join(sessionDir, "agent-startup-*.log"))
	sort.Strings(entries)

	retention := s.manager.Config().BoxliteStartupLogRetention
	if retention > 0 && len(entries) > retention {
		entries = entries[len(entries)-retention:]
	}

	type logEntry struct {
		File string `json:"file"`
		Log  string `json:"log"`
	}
	result := make([]logEntry, 0, len(entries))
	for _, f := range entries {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		result = append(result, logEntry{File: filepath.Base(f), Log: string(data)})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
