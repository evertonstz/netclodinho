package storage

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/angristan/netclode/apps/control-plane/internal/config"
	"github.com/redis/go-redis/v9"
)

func setupTestRedis(t *testing.T) (*miniredis.Miniredis, *RedisStorage) {
	t.Helper()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}

	cfg := &config.Config{
		RedisURL:              "redis://" + mr.Addr(),
		MaxMessagesPerSession: 100,
		MaxEventsPerSession:   100,
	}

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	storage := &RedisStorage{
		client: client,
		config: cfg,
	}

	return mr, storage
}

func TestPublishNotification(t *testing.T) {
	mr, storage := setupTestRedis(t)
	defer mr.Close()
	defer storage.Close()

	ctx := context.Background()
	sessionID := "test-session-1"

	notification := &Notification{
		Type:      "event",
		Payload:   json.RawMessage(`{"kind":"tool_start","tool":"bash"}`),
		Timestamp: time.Now().Format(time.RFC3339),
	}

	// Publish notification
	id, err := storage.PublishNotification(ctx, sessionID, notification)
	if err != nil {
		t.Fatalf("failed to publish notification: %v", err)
	}

	if id == "" {
		t.Error("expected non-empty stream ID")
	}

	// Verify it was written to the stream
	streamKey := NotificationsStreamKey(sessionID)
	entries, err := storage.client.XRange(ctx, streamKey, "-", "+").Result()
	if err != nil {
		t.Fatalf("failed to read stream: %v", err)
	}

	if len(entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(entries))
	}

	if entries[0].ID != id {
		t.Errorf("expected ID %s, got %s", id, entries[0].ID)
	}
}

func TestGetNotificationsAfter(t *testing.T) {
	mr, storage := setupTestRedis(t)
	defer mr.Close()
	defer storage.Close()

	ctx := context.Background()
	sessionID := "test-session-2"

	// Publish 3 notifications
	var ids []string
	for i := 0; i < 3; i++ {
		notification := &Notification{
			Type:      "message",
			Payload:   json.RawMessage(`{"content":"test"}`),
			Timestamp: time.Now().Format(time.RFC3339),
		}
		id, err := storage.PublishNotification(ctx, sessionID, notification)
		if err != nil {
			t.Fatalf("failed to publish notification %d: %v", i, err)
		}
		ids = append(ids, id)
	}

	// Get all notifications from beginning
	notifications, err := storage.GetNotificationsAfter(ctx, sessionID, "0", 100)
	if err != nil {
		t.Fatalf("failed to get notifications: %v", err)
	}

	if len(notifications) != 3 {
		t.Errorf("expected 3 notifications, got %d", len(notifications))
	}

	// Get notifications after first one
	notifications, err = storage.GetNotificationsAfter(ctx, sessionID, ids[0], 100)
	if err != nil {
		t.Fatalf("failed to get notifications after: %v", err)
	}

	if len(notifications) != 2 {
		t.Errorf("expected 2 notifications after first, got %d", len(notifications))
	}

	// Verify IDs match
	if notifications[0].ID != ids[1] {
		t.Errorf("expected first result ID %s, got %s", ids[1], notifications[0].ID)
	}

	// Get notifications with "$" (should return empty)
	notifications, err = storage.GetNotificationsAfter(ctx, sessionID, "$", 100)
	if err != nil {
		t.Fatalf("failed to get notifications with $: %v", err)
	}

	if len(notifications) != 0 {
		t.Errorf("expected 0 notifications with $, got %d", len(notifications))
	}
}

func TestGetLastNotificationID(t *testing.T) {
	mr, storage := setupTestRedis(t)
	defer mr.Close()
	defer storage.Close()

	ctx := context.Background()
	sessionID := "test-session-3"

	// Empty stream should return "$"
	id, err := storage.GetLastNotificationID(ctx, sessionID)
	if err != nil {
		t.Fatalf("failed to get last notification ID: %v", err)
	}

	if id != "$" {
		t.Errorf("expected '$' for empty stream, got %s", id)
	}

	// Publish some notifications
	var lastID string
	for i := 0; i < 3; i++ {
		notification := &Notification{
			Type:      "event",
			Payload:   json.RawMessage(`{}`),
			Timestamp: time.Now().Format(time.RFC3339),
		}
		lastID, _ = storage.PublishNotification(ctx, sessionID, notification)
	}

	// Should return the last ID
	id, err = storage.GetLastNotificationID(ctx, sessionID)
	if err != nil {
		t.Fatalf("failed to get last notification ID: %v", err)
	}

	if id != lastID {
		t.Errorf("expected last ID %s, got %s", lastID, id)
	}
}

func TestGetNotificationsAfter_Limit(t *testing.T) {
	mr, storage := setupTestRedis(t)
	defer mr.Close()
	defer storage.Close()

	ctx := context.Background()
	sessionID := "test-session-4"

	// Publish 10 notifications
	for i := 0; i < 10; i++ {
		notification := &Notification{
			Type:      "message",
			Payload:   json.RawMessage(`{}`),
			Timestamp: time.Now().Format(time.RFC3339),
		}
		_, err := storage.PublishNotification(ctx, sessionID, notification)
		if err != nil {
			t.Fatalf("failed to publish notification %d: %v", i, err)
		}
	}

	// Get with limit of 5
	notifications, err := storage.GetNotificationsAfter(ctx, sessionID, "0", 5)
	if err != nil {
		t.Fatalf("failed to get notifications: %v", err)
	}

	if len(notifications) != 5 {
		t.Errorf("expected 5 notifications with limit, got %d", len(notifications))
	}
}

func TestNotificationsStreamKey(t *testing.T) {
	expected := "session:abc123:notifications"
	actual := NotificationsStreamKey("abc123")

	if actual != expected {
		t.Errorf("expected %s, got %s", expected, actual)
	}
}
