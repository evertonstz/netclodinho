package storage

import (
	"context"
	"encoding/json"

	"github.com/angristan/netclode/apps/control-plane/internal/protocol"
	"github.com/redis/go-redis/v9"
)

// Notification represents a real-time update published to Redis Streams.
type Notification struct {
	Type      string          `json:"type"`      // "event", "message", "session_update", "user_message"
	Payload   json.RawMessage `json:"payload"`   // The actual data
	Timestamp string          `json:"timestamp"`
}

// Storage defines the interface for session persistence.
type Storage interface {
	// Sessions
	SaveSession(ctx context.Context, s *protocol.Session) error
	GetSession(ctx context.Context, id string) (*protocol.Session, error)
	GetAllSessions(ctx context.Context) ([]*protocol.Session, error)
	UpdateSessionStatus(ctx context.Context, id string, status protocol.SessionStatus) error
	UpdateSessionField(ctx context.Context, id, field, value string) error
	DeleteSession(ctx context.Context, id string) error

	// Messages
	AppendMessage(ctx context.Context, msg *protocol.PersistedMessage) error
	GetMessages(ctx context.Context, sessionID string, afterID *string) ([]*protocol.PersistedMessage, error)
	GetLastMessage(ctx context.Context, sessionID string) (*protocol.PersistedMessage, error)
	GetMessageCount(ctx context.Context, sessionID string) (int, error)

	// Events
	AppendEvent(ctx context.Context, evt *protocol.PersistedEvent) error
	GetEvents(ctx context.Context, sessionID string, limit int) ([]*protocol.PersistedEvent, error)

	// Notifications (real-time updates via Redis Streams)
	PublishNotification(ctx context.Context, sessionID string, notification *Notification) (string, error)
	GetNotificationsAfter(ctx context.Context, sessionID string, afterID string, limit int) ([]NotificationWithID, error)
	GetLastNotificationID(ctx context.Context, sessionID string) (string, error)

	// Redis client access (for StreamSubscriber)
	GetRedisClient() *redis.Client

	// Lifecycle
	Close() error
}

// NotificationWithID wraps a notification with its Redis Stream ID.
type NotificationWithID struct {
	ID           string
	Notification *Notification
}
