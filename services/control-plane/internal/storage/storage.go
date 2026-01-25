package storage

import (
	"context"
	"encoding/json"

	pb "github.com/angristan/netclode/services/control-plane/gen/netclode/v1"
	"github.com/redis/go-redis/v9"
)

// Notification represents a real-time update published to Redis Streams.
type Notification struct {
	Type      string          `json:"type"`    // "event", "message", "session_update", "user_message"
	Payload   json.RawMessage `json:"payload"` // The actual data
	Timestamp string          `json:"timestamp"`
}

// Storage defines the interface for session persistence.
type Storage interface {
	// Sessions
	SaveSession(ctx context.Context, s *pb.Session) error
	GetSession(ctx context.Context, id string) (*pb.Session, error)
	GetAllSessions(ctx context.Context) ([]*pb.Session, error)
	UpdateSessionStatus(ctx context.Context, id string, status pb.SessionStatus) error
	UpdateSessionField(ctx context.Context, id, field, value string) error
	DeleteSession(ctx context.Context, id string) error

	// Messages (sessionID passed separately since pb.Message doesn't include it)
	AppendMessage(ctx context.Context, sessionID string, msg *pb.Message) error
	GetMessages(ctx context.Context, sessionID string, afterID *string) ([]*pb.Message, error)
	GetLastMessage(ctx context.Context, sessionID string) (*pb.Message, error)
	GetMessageCount(ctx context.Context, sessionID string) (int, error)

	// Events (sessionID passed separately since pb.Event doesn't include it)
	AppendEvent(ctx context.Context, sessionID string, evt *pb.Event) error
	GetEvents(ctx context.Context, sessionID string, limit int) ([]*pb.Event, error)

	// Notifications (real-time updates via Redis Streams)
	PublishNotification(ctx context.Context, sessionID string, notification *Notification) (string, error)
	GetNotificationsAfter(ctx context.Context, sessionID string, afterID string, limit int) ([]NotificationWithID, error)
	GetLastNotificationID(ctx context.Context, sessionID string) (string, error)

	// Snapshots
	SaveSnapshot(ctx context.Context, snapshot *pb.Snapshot) error
	GetSnapshot(ctx context.Context, sessionID, snapshotID string) (*pb.Snapshot, error)
	ListSnapshots(ctx context.Context, sessionID string) ([]*pb.Snapshot, error)
	DeleteSnapshot(ctx context.Context, sessionID, snapshotID string) error
	DeleteAllSnapshots(ctx context.Context, sessionID string) error

	// Messages truncation (for snapshot restore)
	TruncateMessages(ctx context.Context, sessionID string, keepCount int) error

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
