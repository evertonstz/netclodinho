package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	pb "github.com/angristan/netclode/services/control-plane/gen/netclode/v1"
	"github.com/angristan/netclode/services/control-plane/internal/config"
	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	keySessionsAll = "sessions:all"
)

func snapshotsSetKey(sessionID string) string {
	return "session:" + sessionID + ":snapshots"
}

func snapshotKey(sessionID, snapshotID string) string {
	return "session:" + sessionID + ":snapshot:" + snapshotID
}

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
func (r *RedisStorage) SaveSession(ctx context.Context, s *pb.Session) error {
	pipe := r.client.TxPipeline()

	pipe.SAdd(ctx, keySessionsAll, s.Id)
	pipe.HSet(ctx, sessionKey(s.Id),
		"name", s.Name,
		"status", s.Status.String(),
		"createdAt", s.CreatedAt.AsTime().Format(time.RFC3339),
		"lastActiveAt", s.LastActiveAt.AsTime().Format(time.RFC3339),
	)
	if s.Repo != nil {
		pipe.HSet(ctx, sessionKey(s.Id), "repo", *s.Repo)
	}
	if s.SdkType != nil {
		pipe.HSet(ctx, sessionKey(s.Id), "sdkType", s.SdkType.String())
	}
	if s.Model != nil {
		pipe.HSet(ctx, sessionKey(s.Id), "model", *s.Model)
	}

	_, err := pipe.Exec(ctx)
	return err
}

// GetSession retrieves a session from Redis.
func (r *RedisStorage) GetSession(ctx context.Context, id string) (*pb.Session, error) {
	data, err := r.client.HGetAll(ctx, sessionKey(id)).Result()
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}

	session := &pb.Session{
		Id:     id,
		Name:   data["name"],
		Status: parseSessionStatus(data["status"]),
	}
	if t, err := time.Parse(time.RFC3339, data["createdAt"]); err == nil {
		session.CreatedAt = timestamppb.New(t)
	}
	if t, err := time.Parse(time.RFC3339, data["lastActiveAt"]); err == nil {
		session.LastActiveAt = timestamppb.New(t)
	}
	if repo, ok := data["repo"]; ok && repo != "" {
		session.Repo = &repo
	}
	if sdkTypeStr, ok := data["sdkType"]; ok && sdkTypeStr != "" {
		sdkType := parseSdkType(sdkTypeStr)
		session.SdkType = &sdkType
	}
	if model, ok := data["model"]; ok && model != "" {
		session.Model = &model
	}

	return session, nil
}

// parseSessionStatus converts a string status to pb.SessionStatus enum.
func parseSessionStatus(s string) pb.SessionStatus {
	switch s {
	case "SESSION_STATUS_CREATING", "creating":
		return pb.SessionStatus_SESSION_STATUS_CREATING
	case "SESSION_STATUS_RESUMING", "resuming":
		return pb.SessionStatus_SESSION_STATUS_RESUMING
	case "SESSION_STATUS_READY", "ready":
		return pb.SessionStatus_SESSION_STATUS_READY
	case "SESSION_STATUS_RUNNING", "running":
		return pb.SessionStatus_SESSION_STATUS_RUNNING
	case "SESSION_STATUS_PAUSED", "paused":
		return pb.SessionStatus_SESSION_STATUS_PAUSED
	case "SESSION_STATUS_ERROR", "error":
		return pb.SessionStatus_SESSION_STATUS_ERROR
	case "SESSION_STATUS_INTERRUPTED", "interrupted":
		return pb.SessionStatus_SESSION_STATUS_INTERRUPTED
	default:
		return pb.SessionStatus_SESSION_STATUS_UNSPECIFIED
	}
}

// parseSdkType converts a string to pb.SdkType enum.
func parseSdkType(s string) pb.SdkType {
	switch s {
	case "SDK_TYPE_CLAUDE":
		return pb.SdkType_SDK_TYPE_CLAUDE
	case "SDK_TYPE_OPENCODE":
		return pb.SdkType_SDK_TYPE_OPENCODE
	default:
		return pb.SdkType_SDK_TYPE_UNSPECIFIED
	}
}

// GetAllSessions retrieves all sessions from Redis.
func (r *RedisStorage) GetAllSessions(ctx context.Context) ([]*pb.Session, error) {
	ids, err := r.client.SMembers(ctx, keySessionsAll).Result()
	if err != nil {
		return nil, err
	}

	if len(ids) == 0 {
		return []*pb.Session{}, nil
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

	sessions := make([]*pb.Session, 0, len(ids))
	for i, id := range ids {
		data, err := cmds[i].Result()
		if err != nil || len(data) == 0 {
			continue
		}

		session := &pb.Session{
			Id:     id,
			Name:   data["name"],
			Status: parseSessionStatus(data["status"]),
		}
		if t, err := time.Parse(time.RFC3339, data["createdAt"]); err == nil {
			session.CreatedAt = timestamppb.New(t)
		}
		if t, err := time.Parse(time.RFC3339, data["lastActiveAt"]); err == nil {
			session.LastActiveAt = timestamppb.New(t)
		}
		if repo, ok := data["repo"]; ok && repo != "" {
			session.Repo = &repo
		}
		sessions = append(sessions, session)
	}

	return sessions, nil
}

// UpdateSessionStatus updates the status of a session.
func (r *RedisStorage) UpdateSessionStatus(ctx context.Context, id string, status pb.SessionStatus) error {
	return r.client.HSet(ctx, sessionKey(id), "status", status.String()).Err()
}

// UpdateSessionField updates a single field of a session.
func (r *RedisStorage) UpdateSessionField(ctx context.Context, id, field, value string) error {
	return r.client.HSet(ctx, sessionKey(id), field, value).Err()
}

// DeleteSession removes a session and all related data from Redis.
func (r *RedisStorage) DeleteSession(ctx context.Context, id string) error {
	// First delete all snapshots
	if err := r.DeleteAllSnapshots(ctx, id); err != nil {
		slog.Warn("failed to delete snapshots during session delete", "sessionID", id, "error", err)
	}

	pipe := r.client.TxPipeline()
	pipe.SRem(ctx, keySessionsAll, id)
	pipe.Del(ctx, sessionKey(id))
	pipe.Del(ctx, messagesKey(id))
	pipe.Del(ctx, eventsStreamKey(id))        // Delete events stream
	pipe.Del(ctx, NotificationsStreamKey(id)) // Delete notifications stream
	_, err := pipe.Exec(ctx)
	return err
}

// protoJsonOpts configures protojson to emit readable enum names instead of numbers.
var protoJsonOpts = protojson.MarshalOptions{
	UseEnumNumbers: false,
}

// AppendMessage appends a message to a session's message list.
func (r *RedisStorage) AppendMessage(ctx context.Context, sessionID string, msg *pb.Message) error {
	data, err := protoJsonOpts.Marshal(msg)
	if err != nil {
		return err
	}

	pipe := r.client.TxPipeline()
	pipe.RPush(ctx, messagesKey(sessionID), string(data))

	// Trim to keep only the last N messages
	maxMessages := int64(r.config.MaxMessagesPerSession)
	pipe.LTrim(ctx, messagesKey(sessionID), -maxMessages, -1)

	_, err = pipe.Exec(ctx)
	return err
}

// GetMessages retrieves messages for a session, optionally after a specific message ID.
func (r *RedisStorage) GetMessages(ctx context.Context, sessionID string, afterID *string) ([]*pb.Message, error) {
	data, err := r.client.LRange(ctx, messagesKey(sessionID), 0, -1).Result()
	if err != nil {
		return nil, err
	}

	messages := make([]*pb.Message, 0, len(data))
	foundAfter := afterID == nil

	for _, d := range data {
		msg := &pb.Message{}
		if err := protojson.Unmarshal([]byte(d), msg); err != nil {
			continue
		}

		if !foundAfter {
			if msg.Id == *afterID {
				foundAfter = true
			}
			continue
		}

		messages = append(messages, msg)
	}

	return messages, nil
}

// GetLastMessage retrieves the last message for a session.
func (r *RedisStorage) GetLastMessage(ctx context.Context, sessionID string) (*pb.Message, error) {
	data, err := r.client.LRange(ctx, messagesKey(sessionID), -1, -1).Result()
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}

	msg := &pb.Message{}
	if err := protojson.Unmarshal([]byte(data[0]), msg); err != nil {
		return nil, err
	}

	return msg, nil
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
func (r *RedisStorage) AppendEvent(ctx context.Context, sessionID string, evt *pb.Event) error {
	data, err := protoJsonOpts.Marshal(evt)
	if err != nil {
		return err
	}

	streamKey := eventsStreamKey(sessionID)

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
func (r *RedisStorage) GetEvents(ctx context.Context, sessionID string, limit int) ([]*pb.Event, error) {
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
	events := make([]*pb.Event, 0, len(messages))
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		dataStr, ok := msg.Values["data"].(string)
		if !ok {
			continue
		}

		evt := &pb.Event{}
		if err := protojson.Unmarshal([]byte(dataStr), evt); err != nil {
			continue
		}
		events = append(events, evt)
	}

	return events, nil
}

// GetEventsAfter retrieves events after a specific stream ID (for replay).
func (r *RedisStorage) GetEventsAfter(ctx context.Context, sessionID, afterID string, limit int) ([]*pb.Event, error) {
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

	events := make([]*pb.Event, 0, len(messages))
	for _, msg := range messages {
		dataStr, ok := msg.Values["data"].(string)
		if !ok {
			continue
		}

		evt := &pb.Event{}
		if err := protojson.Unmarshal([]byte(dataStr), evt); err != nil {
			continue
		}
		events = append(events, evt)
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

// GetLastEventStreamID returns the ID of the most recent event in the stream.
// Returns empty string if no events exist.
func (r *RedisStorage) GetLastEventStreamID(ctx context.Context, sessionID string) (string, error) {
	streamKey := eventsStreamKey(sessionID)

	// Get the last entry in the stream
	messages, err := r.client.XRevRangeN(ctx, streamKey, "+", "-", 1).Result()
	if err != nil {
		if err == redis.Nil {
			return "", nil
		}
		return "", err
	}

	if len(messages) == 0 {
		return "", nil
	}

	return messages[0].ID, nil
}

// TruncateEventsAfter deletes all events after the given stream ID.
// If afterID is empty, all events are deleted.
func (r *RedisStorage) TruncateEventsAfter(ctx context.Context, sessionID string, afterID string) error {
	streamKey := eventsStreamKey(sessionID)

	if afterID == "" {
		// Delete entire stream
		return r.client.Del(ctx, streamKey).Err()
	}

	// Get all event IDs after the given ID
	messages, err := r.client.XRange(ctx, streamKey, "("+afterID, "+").Result()
	if err != nil {
		return err
	}

	if len(messages) == 0 {
		return nil
	}

	// Delete each event after the snapshot point
	ids := make([]string, len(messages))
	for i, msg := range messages {
		ids[i] = msg.ID
	}

	return r.client.XDel(ctx, streamKey, ids...).Err()
}

// ============================================================================
// Snapshot Storage
// ============================================================================

// SaveSnapshot saves a snapshot to Redis.
func (r *RedisStorage) SaveSnapshot(ctx context.Context, snapshot *pb.Snapshot) error {
	pipe := r.client.TxPipeline()

	// Add to sorted set with creation time as score
	pipe.ZAdd(ctx, snapshotsSetKey(snapshot.SessionId), redis.Z{
		Score:  float64(snapshot.CreatedAt.AsTime().Unix()),
		Member: snapshot.Id,
	})

	// Store snapshot data
	pipe.HSet(ctx, snapshotKey(snapshot.SessionId, snapshot.Id),
		"id", snapshot.Id,
		"sessionId", snapshot.SessionId,
		"name", snapshot.Name,
		"createdAt", snapshot.CreatedAt.AsTime().Format(time.RFC3339),
		"sizeBytes", snapshot.SizeBytes,
		"turnNumber", snapshot.TurnNumber,
		"messageCount", snapshot.MessageCount,
		"eventStreamId", snapshot.EventStreamId,
	)

	_, err := pipe.Exec(ctx)
	return err
}

// GetSnapshot retrieves a snapshot by ID.
func (r *RedisStorage) GetSnapshot(ctx context.Context, sessionID, snapshotID string) (*pb.Snapshot, error) {
	data, err := r.client.HGetAll(ctx, snapshotKey(sessionID, snapshotID)).Result()
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("snapshot not found: %s", snapshotID)
	}

	snapshot := &pb.Snapshot{
		Id:        data["id"],
		SessionId: data["sessionId"],
		Name:      data["name"],
	}

	if t, err := time.Parse(time.RFC3339, data["createdAt"]); err == nil {
		snapshot.CreatedAt = timestamppb.New(t)
	}

	if sizeStr, ok := data["sizeBytes"]; ok {
		var size int64
		fmt.Sscanf(sizeStr, "%d", &size)
		snapshot.SizeBytes = size
	}

	if turnStr, ok := data["turnNumber"]; ok {
		var turn int32
		fmt.Sscanf(turnStr, "%d", &turn)
		snapshot.TurnNumber = turn
	}

	if msgCountStr, ok := data["messageCount"]; ok {
		var count int32
		fmt.Sscanf(msgCountStr, "%d", &count)
		snapshot.MessageCount = count
	}

	if eventStreamId, ok := data["eventStreamId"]; ok {
		snapshot.EventStreamId = eventStreamId
	}

	return snapshot, nil
}

// ListSnapshots retrieves all snapshots for a session, ordered by creation time (newest first).
func (r *RedisStorage) ListSnapshots(ctx context.Context, sessionID string) ([]*pb.Snapshot, error) {
	// Get all snapshot IDs from sorted set (newest first by score)
	ids, err := r.client.ZRevRange(ctx, snapshotsSetKey(sessionID), 0, -1).Result()
	if err != nil {
		return nil, err
	}

	if len(ids) == 0 {
		return []*pb.Snapshot{}, nil
	}

	// Pipeline to get all snapshot data
	pipe := r.client.Pipeline()
	cmds := make([]*redis.MapStringStringCmd, len(ids))
	for i, id := range ids {
		cmds[i] = pipe.HGetAll(ctx, snapshotKey(sessionID, id))
	}
	_, err = pipe.Exec(ctx)
	if err != nil {
		return nil, err
	}

	snapshots := make([]*pb.Snapshot, 0, len(ids))
	for _, cmd := range cmds {
		data, err := cmd.Result()
		if err != nil || len(data) == 0 {
			continue
		}

		snapshot := &pb.Snapshot{
			Id:        data["id"],
			SessionId: data["sessionId"],
			Name:      data["name"],
		}

		if t, err := time.Parse(time.RFC3339, data["createdAt"]); err == nil {
			snapshot.CreatedAt = timestamppb.New(t)
		}

		if sizeStr, ok := data["sizeBytes"]; ok {
			var size int64
			fmt.Sscanf(sizeStr, "%d", &size)
			snapshot.SizeBytes = size
		}

		if turnStr, ok := data["turnNumber"]; ok {
			var turn int32
			fmt.Sscanf(turnStr, "%d", &turn)
			snapshot.TurnNumber = turn
		}

		if msgCountStr, ok := data["messageCount"]; ok {
			var count int32
			fmt.Sscanf(msgCountStr, "%d", &count)
			snapshot.MessageCount = count
		}

		if eventStreamId, ok := data["eventStreamId"]; ok {
			snapshot.EventStreamId = eventStreamId
		}

		snapshots = append(snapshots, snapshot)
	}

	return snapshots, nil
}

// DeleteSnapshot removes a snapshot from Redis.
func (r *RedisStorage) DeleteSnapshot(ctx context.Context, sessionID, snapshotID string) error {
	pipe := r.client.TxPipeline()
	pipe.ZRem(ctx, snapshotsSetKey(sessionID), snapshotID)
	pipe.Del(ctx, snapshotKey(sessionID, snapshotID))
	_, err := pipe.Exec(ctx)
	return err
}

// DeleteAllSnapshots removes all snapshots for a session.
func (r *RedisStorage) DeleteAllSnapshots(ctx context.Context, sessionID string) error {
	// Get all snapshot IDs first
	ids, err := r.client.ZRange(ctx, snapshotsSetKey(sessionID), 0, -1).Result()
	if err != nil {
		return err
	}

	if len(ids) == 0 {
		return nil
	}

	// Delete all snapshot data and the set
	pipe := r.client.TxPipeline()
	for _, id := range ids {
		pipe.Del(ctx, snapshotKey(sessionID, id))
	}
	pipe.Del(ctx, snapshotsSetKey(sessionID))

	_, err = pipe.Exec(ctx)
	return err
}

// TruncateMessages truncates messages to keep only the first N messages.
func (r *RedisStorage) TruncateMessages(ctx context.Context, sessionID string, keepCount int) error {
	if keepCount <= 0 {
		// Delete all messages
		return r.client.Del(ctx, messagesKey(sessionID)).Err()
	}
	// LTRIM with range [0, keepCount-1] keeps first keepCount elements
	return r.client.LTrim(ctx, messagesKey(sessionID), 0, int64(keepCount-1)).Err()
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
