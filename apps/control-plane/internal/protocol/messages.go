package protocol

import "encoding/json"

// ClientMessage represents all possible client-to-server messages.
// The Type field determines which other fields are relevant.
type ClientMessage struct {
	Type          string  `json:"type"`
	ID            string  `json:"id,omitempty"`
	SessionID     string  `json:"sessionId,omitempty"`
	Name          string  `json:"name,omitempty"`
	Repo          string  `json:"repo,omitempty"`
	Text          string  `json:"text,omitempty"`
	Data          string  `json:"data,omitempty"`
	Cols          int     `json:"cols,omitempty"`
	Rows          int     `json:"rows,omitempty"`
	LastMessageID *string `json:"lastMessageId,omitempty"`
}

// Client message types
const (
	MsgTypeSessionCreate    = "session.create"
	MsgTypeSessionList      = "session.list"
	MsgTypeSessionResume    = "session.resume"
	MsgTypeSessionPause     = "session.pause"
	MsgTypeSessionDelete    = "session.delete"
	MsgTypePrompt           = "prompt"
	MsgTypePromptInterrupt  = "prompt.interrupt"
	MsgTypeTerminalInput    = "terminal.input"
	MsgTypeTerminalResize   = "terminal.resize"
	MsgTypeSync             = "sync"
	MsgTypeSessionOpen      = "session.open"
)

// ServerMessage represents all possible server-to-client messages.
type ServerMessage struct {
	Type       string           `json:"type"`
	Session    *Session         `json:"session,omitempty"`
	Sessions   []Session        `json:"sessions,omitempty"`
	ID         string           `json:"id,omitempty"`
	SessionID  string           `json:"sessionId,omitempty"`
	Error      string           `json:"error,omitempty"`
	Message    string           `json:"message,omitempty"`
	Data       string           `json:"data,omitempty"`
	Event      *AgentEvent      `json:"event,omitempty"`
	Content    string           `json:"content,omitempty"`
	Partial    *bool            `json:"partial,omitempty"`
	MessageID  string           `json:"messageId,omitempty"`
	ServerTime string           `json:"serverTime,omitempty"`
	Messages   []PersistedMessage `json:"messages,omitempty"`
	Events     []PersistedEvent   `json:"events,omitempty"`
	HasMore    bool             `json:"hasMore,omitempty"`

	// For sync.response with SessionWithMeta
	SessionsWithMeta []SessionWithMeta `json:"sessionsWithMeta,omitempty"`
}

// Server message types
const (
	MsgTypeSessionCreated      = "session.created"
	MsgTypeSessionUpdated      = "session.updated"
	MsgTypeSessionDeleted      = "session.deleted"
	MsgTypeSessionListResponse = "session.list"
	MsgTypeSessionError        = "session.error"
	MsgTypeTerminalOutput      = "terminal.output"
	MsgTypeAgentEvent          = "agent.event"
	MsgTypeAgentMessage        = "agent.message"
	MsgTypeAgentDone           = "agent.done"
	MsgTypeAgentError          = "agent.error"
	MsgTypeUserMessage         = "user.message"
	MsgTypeError               = "error"
	MsgTypeSyncResponse        = "sync.response"
	MsgTypeSessionState        = "session.state"
)

// NewSessionCreated creates a session.created message
func NewSessionCreated(s *Session) ServerMessage {
	return ServerMessage{Type: MsgTypeSessionCreated, Session: s}
}

// NewSessionUpdated creates a session.updated message
func NewSessionUpdated(s *Session) ServerMessage {
	return ServerMessage{Type: MsgTypeSessionUpdated, Session: s}
}

// NewSessionDeleted creates a session.deleted message
func NewSessionDeleted(id string) ServerMessage {
	return ServerMessage{Type: MsgTypeSessionDeleted, ID: id}
}

// NewSessionListMsg creates a session.list message
func NewSessionListMsg(sessions []Session) ServerMessage {
	if sessions == nil {
		sessions = []Session{}
	}
	return ServerMessage{Type: MsgTypeSessionListResponse, Sessions: sessions}
}

// NewSessionError creates a session.error message
func NewSessionError(id, err string) ServerMessage {
	return ServerMessage{Type: MsgTypeSessionError, ID: id, Error: err}
}

// NewAgentEvent creates an agent.event message
func NewAgentEvent(sessionID string, event *AgentEvent) ServerMessage {
	return ServerMessage{Type: MsgTypeAgentEvent, SessionID: sessionID, Event: event}
}

// NewAgentMessage creates an agent.message message
func NewAgentMessage(sessionID, content string, partial bool, messageID string) ServerMessage {
	return ServerMessage{
		Type:      MsgTypeAgentMessage,
		SessionID: sessionID,
		Content:   content,
		Partial:   &partial,
		MessageID: messageID,
	}
}

// NewAgentDone creates an agent.done message
func NewAgentDone(sessionID string) ServerMessage {
	return ServerMessage{Type: MsgTypeAgentDone, SessionID: sessionID}
}

// NewAgentError creates an agent.error message
func NewAgentError(sessionID, err string) ServerMessage {
	return ServerMessage{Type: MsgTypeAgentError, SessionID: sessionID, Error: err}
}

// NewUserMessage creates a user.message message for broadcasting user prompts
func NewUserMessage(sessionID, content string) ServerMessage {
	return ServerMessage{Type: MsgTypeUserMessage, SessionID: sessionID, Content: content}
}

// NewError creates a generic error message
func NewError(msg string) ServerMessage {
	return ServerMessage{Type: MsgTypeError, Message: msg}
}

// NewSyncResponse creates a sync.response message
func NewSyncResponse(sessions []SessionWithMeta, serverTime string) ServerMessage {
	return ServerMessage{
		Type:             MsgTypeSyncResponse,
		SessionsWithMeta: sessions,
		ServerTime:       serverTime,
	}
}

// NewSessionState creates a session.state message
func NewSessionState(session *Session, messages []PersistedMessage, events []PersistedEvent, hasMore bool) ServerMessage {
	return ServerMessage{
		Type:     MsgTypeSessionState,
		Session:  session,
		Messages: messages,
		Events:   events,
		HasMore:  hasMore,
	}
}

// MarshalJSON customizes JSON output for specific message types
func (m ServerMessage) MarshalJSON() ([]byte, error) {
	type Alias ServerMessage

	// For sync.response, use "sessions" key with SessionWithMeta array
	if m.Type == MsgTypeSyncResponse && len(m.SessionsWithMeta) > 0 {
		return json.Marshal(struct {
			Type       string            `json:"type"`
			Sessions   []SessionWithMeta `json:"sessions"`
			ServerTime string            `json:"serverTime"`
		}{
			Type:       m.Type,
			Sessions:   m.SessionsWithMeta,
			ServerTime: m.ServerTime,
		})
	}

	// For session.list, always include sessions array (even if empty)
	if m.Type == MsgTypeSessionListResponse {
		sessions := m.Sessions
		if sessions == nil {
			sessions = []Session{}
		}
		return json.Marshal(struct {
			Type     string    `json:"type"`
			Sessions []Session `json:"sessions"`
		}{
			Type:     m.Type,
			Sessions: sessions,
		})
	}

	return json.Marshal(Alias(m))
}
