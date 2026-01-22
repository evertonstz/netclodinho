package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/angristan/netclode/services/control-plane/internal/config"
	"github.com/angristan/netclode/services/control-plane/internal/protocol"
	"github.com/redis/go-redis/v9"
)

const (
	keySessionsAll = "sessions:all"
)

func sessionKey(id string) string {
	return "session:" + id
}

func messagesKey(id string) string {
	return "session:" + id + ":messages"
}

// eventsStreamKey returns the Redis Stream key for events
func eventsStreamKey(id string) string {
	return "session:" + id + ":events:stream"
}

// NotificationsStreamKey returns the Redis Stream key for real-time notifications.
// Exported for use by StreamSubscriber in the session package.
func NotificationsStreamKey(id string) string {
	return "session:" + id + ":notifications"
}

// RedisStorage implements Storage using Redis.
type RedisStorage struct {
	client *redis.Client
	config *config.Config
}

// NewRedisStorage creates a new Redis storage instance.
func NewRedisStorage(ctx context.Context, cfg *config.Config) (*RedisStorage, error) {
	opts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis URL: %w", err)
	}

	client := redis.NewClient(opts)

	// Retry connection with backoff
	var lastErr error
	for i := 0; i < 10; i++ {
		if err := client.Ping(ctx).Err(); err == nil {
			slog.Info("connected to Redis", "url", ParseRedisURL(cfg.RedisURL))
			return &RedisStorage{client: client, config: cfg}, nil
		} else {
			lastErr = err
			backoff := time.Duration(100*(i+1)) * time.Millisecond
			if backoff > 3*time.Second {
				backoff = 3 * time.Second
			}
			slog.Warn("Redis connection failed, retrying", "attempt", i+1, "error", err, "backoff", backoff)
			time.Sleep(backoff)
		}
	}

	return nil, fmt.Errorf("failed to connect to Redis after 10 attempts: %w", lastErr)
}

// Close closes the Redis connection.
func (r *RedisStorage) Close() error {
	return r.client.Close()
}

// SaveSession saves a session to Redis.
func (r *RedisStorage) SaveSession(ctx context.Context, s *protocol.Session) error {
	pipe := r.client.TxPipeline()

	pipe.SAdd(ctx, keySessionsAll, s.ID)
	pipe.HSet(ctx, sessionKey(s.ID),
		"name", s.Name,
		"status", string(s.Status),
		"createdAt", s.CreatedAt,
		"lastActiveAt", s.LastActiveAt,
	)
	if s.Repo != nil {
		pipe.HSet(ctx, sessionKey(s.ID), "repo", *s.Repo)
	}

	_, err := pipe.Exec(ctx)
	return err
}

// GetSession retrieves a session from Redis.
func (r *RedisStorage) GetSession(ctx context.Context, id string) (*protocol.Session, error) {
	data, err := r.client.HGetAll(ctx, sessionKey(id)).Result()
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}

	session := &protocol.Session{
		ID:           id,
		Name:         data["name"],
		Status:       protocol.SessionStatus(data["status"]),
		CreatedAt:    data["createdAt"],
		LastActiveAt: data["lastActiveAt"],
	}
	if repo, ok := data["repo"]; ok && repo != "" {
		session.Repo = &repo
	}

	return session, nil
}

// GetAllSessions retrieves all sessions from Redis.
func (r *RedisStorage) GetAllSessions(ctx context.Context) ([]*protocol.Session, error) {
	ids, err := r.client.SMembers(ctx, keySessionsAll).Result()
	if err != nil {
		return nil, err
	}

	if len(ids) == 0 {
		return []*protocol.Session{}, nil
	}

	// Pipeline to get all session data
	pipe := r.client.Pipeline()
	cmds := make([]*redis.MapStringStringCmd, len(ids))
	for i, id := range ids {
		cmds[i] = pipe.HGetAll(ctx, sessionKey(id))
	}
	_, err = pipe.Exec(ctx)
	if err != nil {
		return nil, err
	}

	sessions := make([]*protocol.Session, 0, len(ids))
	for i, id := range ids {
		data, err := cmds[i].Result()
		if err != nil || len(data) == 0 {
			continue
		}

		session := &protocol.Session{
			ID:           id,
			Name:         data["name"],
			Status:       protocol.SessionStatus(data["status"]),
			CreatedAt:    data["createdAt"],
			LastActiveAt: data["lastActiveAt"],
		}
		if repo, ok := data["repo"]; ok && repo != "" {
			session.Repo = &repo
		}
		sessions = append(sessions, session)
	}

	return sessions, nil
}

// UpdateSessionStatus updates the status of a session.
func (r *RedisStorage) UpdateSessionStatus(ctx context.Context, id string, status protocol.SessionStatus) error {
	return r.client.HSet(ctx, sessionKey(id), "status", string(status)).Err()
}

// UpdateSessionField updates a single field of a session.
func (r *RedisStorage) UpdateSessionField(ctx context.Context, id, field, value string) error {
	return r.client.HSet(ctx, sessionKey(id), field, value).Err()
}

// DeleteSession removes a session and all related data from Redis.
func (r *RedisStorage) DeleteSession(ctx context.Context, id string) error {
	pipe := r.client.TxPipeline()
	pipe.SRem(ctx, keySessionsAll, id)
	pipe.Del(ctx, sessionKey(id))
	pipe.Del(ctx, messagesKey(id))
	pipe.Del(ctx, eventsStreamKey(id))        // Delete events stream
	pipe.Del(ctx, NotificationsStreamKey(id)) // Delete notifications stream
	_, err := pipe.Exec(ctx)
	return err
}

// AppendMessage appends a message to a session's message list.
func (r *RedisStorage) AppendMessage(ctx context.Context, msg *protocol.PersistedMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	pipe := r.client.TxPipeline()
	pipe.RPush(ctx, messagesKey(msg.SessionID), string(data))

	// Trim to keep only the last N messages
	maxMessages := int64(r.config.MaxMessagesPerSession)
	pipe.LTrim(ctx, messagesKey(msg.SessionID), -maxMessages, -1)

	_, err = pipe.Exec(ctx)
	return err
}

// GetMessages retrieves messages for a session, optionally after a specific message ID.
func (r *RedisStorage) GetMessages(ctx context.Context, sessionID string, afterID *string) ([]*protocol.PersistedMessage, error) {
	data, err := r.client.LRange(ctx, messagesKey(sessionID), 0, -1).Result()
	if err != nil {
		return nil, err
	}

	messages := make([]*protocol.PersistedMessage, 0, len(data))
	foundAfter := afterID == nil

	for _, d := range data {
		var msg protocol.PersistedMessage
		if err := json.Unmarshal([]byte(d), &msg); err != nil {
			continue
		}

		if !foundAfter {
			if msg.ID == *afterID {
				foundAfter = true
			}
			continue
		}

		messages = append(messages, &msg)
	}

	return messages, nil
}

// GetLastMessage retrieves the last message for a session.
func (r *RedisStorage) GetLastMessage(ctx context.Context, sessionID string) (*protocol.PersistedMessage, error) {
	data, err := r.client.LRange(ctx, messagesKey(sessionID), -1, -1).Result()
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}

	var msg protocol.PersistedMessage
	if err := json.Unmarshal([]byte(data[0]), &msg); err != nil {
		return nil, err
	}

	return &msg, nil
}

// GetMessageCount returns the number of messages for a session.
func (r *RedisStorage) GetMessageCount(ctx context.Context, sessionID string) (int, error) {
	count, err := r.client.LLen(ctx, messagesKey(sessionID)).Result()
	if err != nil {
		return 0, err
	}
	return int(count), nil
}

// AppendEvent appends an event to a session's event stream using Redis Streams.
func (r *RedisStorage) AppendEvent(ctx context.Context, evt *protocol.PersistedEvent) error {
	data, err := json.Marshal(evt)
	if err != nil {
		return err
	}

	streamKey := eventsStreamKey(evt.SessionID)

	// Add to stream with auto-generated ID (no limit - events cleaned up on session delete)
	_, err = r.client.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		Values: map[string]interface{}{
			"data": string(data),
		},
	}).Result()

	return err
}

// GetEvents retrieves the last N events for a session using Redis Streams.
func (r *RedisStorage) GetEvents(ctx context.Context, sessionID string, limit int) ([]*protocol.PersistedEvent, error) {
	streamKey := eventsStreamKey(sessionID)

	// Use XREVRANGE to get latest events (newest first), then reverse
	messages, err := r.client.XRevRange(ctx, streamKey, "+", "-").Result()
	if err != nil {
		return nil, err
	}

	// Take only 'limit' entries (0 = no limit)
	if limit > 0 && len(messages) > limit {
		messages = messages[:limit]
	}

	// Reverse to get chronological order (oldest first)
	events := make([]*protocol.PersistedEvent, 0, len(messages))
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		dataStr, ok := msg.Values["data"].(string)
		if !ok {
			continue
		}

		var evt protocol.PersistedEvent
		if err := json.Unmarshal([]byte(dataStr), &evt); err != nil {
			continue
		}
		events = append(events, &evt)
	}

	return events, nil
}

// GetEventsAfter retrieves events after a specific stream ID (for replay).
func (r *RedisStorage) GetEventsAfter(ctx context.Context, sessionID, afterID string, limit int) ([]*protocol.PersistedEvent, error) {
	streamKey := eventsStreamKey(sessionID)

	// Use XRANGE starting after the given ID
	startID := afterID
	if startID == "" {
		startID = "0"
	} else {
		// Exclude the given ID by using exclusive range
		startID = "(" + startID
	}

	messages, err := r.client.XRange(ctx, streamKey, startID, "+").Result()
	if err != nil {
		return nil, err
	}

	// Take only 'limit' entries
	if len(messages) > limit {
		messages = messages[:limit]
	}

	events := make([]*protocol.PersistedEvent, 0, len(messages))
	for _, msg := range messages {
		dataStr, ok := msg.Values["data"].(string)
		if !ok {
			continue
		}

		var evt protocol.PersistedEvent
		if err := json.Unmarshal([]byte(dataStr), &evt); err != nil {
			continue
		}
		events = append(events, &evt)
	}

	return events, nil
}

// GetStreamInfo returns information about a session's event stream.
func (r *RedisStorage) GetStreamInfo(ctx context.Context, sessionID string) (length int64, firstID, lastID string, err error) {
	streamKey := eventsStreamKey(sessionID)

	info, err := r.client.XInfoStream(ctx, streamKey).Result()
	if err != nil {
		if err == redis.Nil {
			return 0, "", "", nil
		}
		return 0, "", "", err
	}

	return info.Length, info.FirstEntry.ID, info.LastEntry.ID, nil
}

// PublishNotification publishes a notification to the session's notification stream.
// Returns the stream ID of the published notification.
func (r *RedisStorage) PublishNotification(ctx context.Context, sessionID string, notification *Notification) (string, error) {
	data, err := json.Marshal(notification)
	if err != nil {
		return "", err
	}

	streamKey := NotificationsStreamKey(sessionID)

	// Add to stream with auto-generated ID
	id, err := r.client.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		Values: map[string]interface{}{
			"data": string(data),
		},
		// Use MAXLEN ~ to approximately limit stream size
		// Use a larger limit than events since this includes all updates
		MaxLen: int64(r.config.MaxMessagesPerSession + r.config.MaxEventsPerSession),
		Approx: true,
	}).Result()

	return id, err
}

// GetNotificationsAfter retrieves notifications after a specific stream ID.
// If afterID is empty or "0", returns from the beginning. If "$", returns nothing (new only).
func (r *RedisStorage) GetNotificationsAfter(ctx context.Context, sessionID string, afterID string, limit int) ([]NotificationWithID, error) {
	streamKey := NotificationsStreamKey(sessionID)

	startID := afterID
	if startID == "" {
		startID = "0"
	} else if startID != "$" && startID != "0" {
		// Exclude the given ID by using exclusive range
		startID = "(" + startID
	}

	// If "$" just return empty - caller wants only new notifications
	if afterID == "$" {
		return []NotificationWithID{}, nil
	}

	messages, err := r.client.XRange(ctx, streamKey, startID, "+").Result()
	if err != nil {
		return nil, err
	}

	// Take only 'limit' entries
	if len(messages) > limit {
		messages = messages[:limit]
	}

	notifications := make([]NotificationWithID, 0, len(messages))
	for _, msg := range messages {
		dataStr, ok := msg.Values["data"].(string)
		if !ok {
			continue
		}

		var notification Notification
		if err := json.Unmarshal([]byte(dataStr), &notification); err != nil {
			continue
		}

		notifications = append(notifications, NotificationWithID{
			ID:           msg.ID,
			Notification: &notification,
		})
	}

	return notifications, nil
}

// GetLastNotificationID returns the ID of the last notification in a session's stream.
// Returns "$" if the stream is empty (meaning "only new notifications").
func (r *RedisStorage) GetLastNotificationID(ctx context.Context, sessionID string) (string, error) {
	streamKey := NotificationsStreamKey(sessionID)

	// Use XREVRANGE to get just the last entry
	messages, err := r.client.XRevRange(ctx, streamKey, "+", "-").Result()
	if err != nil {
		if err == redis.Nil {
			return "$", nil
		}
		return "", err
	}

	if len(messages) == 0 {
		return "$", nil
	}

	return messages[0].ID, nil
}

// GetRedisClient returns the underlying Redis client for direct access (used by StreamSubscriber).
func (r *RedisStorage) GetRedisClient() *redis.Client {
	return r.client
}

// ParseRedisURL extracts host from a Redis URL for logging purposes.
func ParseRedisURL(url string) string {
	// redis://[:password@]host:port
	url = strings.TrimPrefix(url, "redis://")
	if idx := strings.LastIndex(url, "@"); idx != -1 {
		url = url[idx+1:]
	}
	return url
}
