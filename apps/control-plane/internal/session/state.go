package session

import (
	"github.com/angristan/netclode/apps/control-plane/internal/protocol"
)

// SessionState holds the in-memory state for a session.
// Real-time updates are handled via Redis Streams, not in-memory channels.
type SessionState struct {
	Session     *protocol.Session
	ServiceFQDN string // DNS name of the agent service when running
}

// NewSessionState creates a new session state.
func NewSessionState(session *protocol.Session) *SessionState {
	return &SessionState{
		Session: session,
	}
}

// Close is a no-op now that we use Redis Streams for real-time.
// Kept for backwards compatibility with existing code.
func (s *SessionState) Close() {
	// No-op - StreamSubscribers are closed by their context
}
