package session

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/angristan/netclode/apps/control-plane/internal/protocol"
	"github.com/angristan/netclode/apps/control-plane/internal/storage"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func setupMiniRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	return mr, client
}

func TestStreamSubscriber_ReceivesNotifications(t *testing.T) {
	mr, client := setupMiniRedis(t)
	defer mr.Close()
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sessionID := "test-session-1"
	streamKey := storage.NotificationsStreamKey(sessionID)

	// Create subscriber starting from beginning
	sub := NewStreamSubscriber(sessionID, "0", client)

	// Start subscriber in goroutine
	go sub.Run(ctx)
	defer sub.Close()

	// Publish a notification
	notification := storage.Notification{
		Type:      "event",
		Timestamp: time.Now().Format(time.RFC3339),
	}
	event := protocol.AgentEvent{
		Kind: protocol.EventKindToolStart,
		Tool: "bash",
	}
	eventJSON, _ := json.Marshal(event)
	notification.Payload = eventJSON

	notificationJSON, _ := json.Marshal(notification)
	_, err := client.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		Values: map[string]interface{}{
			"data": string(notificationJSON),
		},
	}).Result()
	if err != nil {
		t.Fatalf("failed to add to stream: %v", err)
	}

	// Fast-forward miniredis time to trigger XREAD
	mr.FastForward(100 * time.Millisecond)

	// Read from subscriber
	select {
	case msg := <-sub.Messages():
		if msg.Type != protocol.MsgTypeAgentEvent {
			t.Errorf("expected type %s, got %s", protocol.MsgTypeAgentEvent, msg.Type)
		}
		if msg.SessionID != sessionID {
			t.Errorf("expected sessionID %s, got %s", sessionID, msg.SessionID)
		}
		if msg.Event == nil {
			t.Error("expected event to be set")
		} else if msg.Event.Tool != "bash" {
			t.Errorf("expected tool bash, got %s", msg.Event.Tool)
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for notification")
	}
}

func TestStreamSubscriber_StartFromCursor(t *testing.T) {
	mr, client := setupMiniRedis(t)
	defer mr.Close()
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sessionID := "test-session-2"
	streamKey := storage.NotificationsStreamKey(sessionID)

	// Add first notification
	notification1 := storage.Notification{
		Type:      "event",
		Timestamp: time.Now().Format(time.RFC3339),
	}
	event1 := protocol.AgentEvent{Kind: protocol.EventKindToolStart, Tool: "first"}
	event1JSON, _ := json.Marshal(event1)
	notification1.Payload = event1JSON
	n1JSON, _ := json.Marshal(notification1)

	firstID, err := client.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		Values: map[string]interface{}{"data": string(n1JSON)},
	}).Result()
	if err != nil {
		t.Fatalf("failed to add first notification: %v", err)
	}

	// Add second notification
	notification2 := storage.Notification{
		Type:      "event",
		Timestamp: time.Now().Format(time.RFC3339),
	}
	event2 := protocol.AgentEvent{Kind: protocol.EventKindToolStart, Tool: "second"}
	event2JSON, _ := json.Marshal(event2)
	notification2.Payload = event2JSON
	n2JSON, _ := json.Marshal(notification2)

	_, err = client.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		Values: map[string]interface{}{"data": string(n2JSON)},
	}).Result()
	if err != nil {
		t.Fatalf("failed to add second notification: %v", err)
	}

	// Create subscriber starting AFTER the first notification
	sub := NewStreamSubscriber(sessionID, firstID, client)
	go sub.Run(ctx)
	defer sub.Close()

	mr.FastForward(100 * time.Millisecond)

	// Should only receive the second notification
	select {
	case msg := <-sub.Messages():
		if msg.Event == nil || msg.Event.Tool != "second" {
			t.Errorf("expected second notification, got tool=%v", msg.Event)
		}
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for notification")
	}

	// Should not receive any more messages immediately
	select {
	case msg := <-sub.Messages():
		t.Errorf("unexpected message: %+v", msg)
	case <-time.After(100 * time.Millisecond):
		// Expected - no more messages
	}
}

func TestStreamSubscriber_Close(t *testing.T) {
	mr, client := setupMiniRedis(t)
	defer mr.Close()
	defer client.Close()

	ctx := context.Background()
	sessionID := "test-session-3"

	sub := NewStreamSubscriber(sessionID, "$", client)
	go sub.Run(ctx)

	// Close should stop the subscriber
	sub.Close()

	// Channel should eventually be closed
	select {
	case _, ok := <-sub.Messages():
		if ok {
			t.Error("expected channel to be closed or empty")
		}
	case <-time.After(1 * time.Second):
		// OK - channel may be empty
	}
}

func TestStreamSubscriber_ContextCancellation(t *testing.T) {
	mr, client := setupMiniRedis(t)
	defer mr.Close()
	defer client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	sessionID := "test-session-4"

	sub := NewStreamSubscriber(sessionID, "$", client)
	done := make(chan struct{})
	go func() {
		sub.Run(ctx)
		close(done)
	}()

	// Cancel context should stop the subscriber
	cancel()

	select {
	case <-done:
		// Expected - subscriber stopped
	case <-time.After(2 * time.Second):
		t.Error("subscriber did not stop after context cancellation")
	}
}

func TestNotificationToServerMessage(t *testing.T) {
	sessionID := "test-session"
	streamID := "1234567890-0"

	tests := []struct {
		name           string
		notification   storage.Notification
		expectedType   string
		checkFunc      func(t *testing.T, msg protocol.ServerMessage)
	}{
		{
			name: "event notification",
			notification: storage.Notification{
				Type:    "event",
				Payload: json.RawMessage(`{"kind":"tool_start","tool":"bash"}`),
			},
			expectedType: protocol.MsgTypeAgentEvent,
			checkFunc: func(t *testing.T, msg protocol.ServerMessage) {
				if msg.Event == nil {
					t.Error("expected event to be set")
				}
				if msg.Event.Tool != "bash" {
					t.Errorf("expected tool bash, got %s", msg.Event.Tool)
				}
			},
		},
		{
			name: "session_update notification",
			notification: storage.Notification{
				Type:    "session_update",
				Payload: json.RawMessage(`{"id":"sess-1","name":"Test","status":"running"}`),
			},
			expectedType: protocol.MsgTypeSessionUpdated,
			checkFunc: func(t *testing.T, msg protocol.ServerMessage) {
				if msg.Session == nil {
					t.Error("expected session to be set")
				}
				if msg.Session.Name != "Test" {
					t.Errorf("expected name Test, got %s", msg.Session.Name)
				}
			},
		},
		{
			name: "user_message notification",
			notification: storage.Notification{
				Type:    "user_message",
				Payload: json.RawMessage(`{"text":"hello world","sessionId":"sess-1"}`),
			},
			expectedType: protocol.MsgTypeUserMessage,
			checkFunc: func(t *testing.T, msg protocol.ServerMessage) {
				if msg.Content != "hello world" {
					t.Errorf("expected content 'hello world', got %s", msg.Content)
				}
			},
		},
		{
			name: "agent_done notification",
			notification: storage.Notification{
				Type:    "agent_done",
				Payload: json.RawMessage(`{}`),
			},
			expectedType: protocol.MsgTypeAgentDone,
			checkFunc: func(t *testing.T, msg protocol.ServerMessage) {
				// No extra checks needed
			},
		},
		{
			name: "agent_error notification",
			notification: storage.Notification{
				Type:    "agent_error",
				Payload: json.RawMessage(`{"error":"something went wrong"}`),
			},
			expectedType: protocol.MsgTypeAgentError,
			checkFunc: func(t *testing.T, msg protocol.ServerMessage) {
				if msg.Error != "something went wrong" {
					t.Errorf("expected error 'something went wrong', got %s", msg.Error)
				}
			},
		},
		{
			name: "session_error notification",
			notification: storage.Notification{
				Type:    "session_error",
				Payload: json.RawMessage(`{"error":"sandbox failed","sessionId":"sess-123"}`),
			},
			expectedType: protocol.MsgTypeSessionError,
			checkFunc: func(t *testing.T, msg protocol.ServerMessage) {
				if msg.Error != "sandbox failed" {
					t.Errorf("expected error 'sandbox failed', got %s", msg.Error)
				}
			},
		},
		{
			name: "message notification with partial",
			notification: storage.Notification{
				Type:    "message",
				Payload: json.RawMessage(`{"id":"msg-1","sessionId":"sess-1","role":"assistant","content":"hello","partial":true}`),
			},
			expectedType: protocol.MsgTypeAgentMessage,
			checkFunc: func(t *testing.T, msg protocol.ServerMessage) {
				if msg.Content != "hello" {
					t.Errorf("expected content 'hello', got %s", msg.Content)
				}
				if msg.Partial == nil || !*msg.Partial {
					t.Error("expected partial to be true")
				}
			},
		},
		{
			name: "message notification without partial",
			notification: storage.Notification{
				Type:    "message",
				Payload: json.RawMessage(`{"id":"msg-2","sessionId":"sess-1","role":"assistant","content":"done","partial":false}`),
			},
			expectedType: protocol.MsgTypeAgentMessage,
			checkFunc: func(t *testing.T, msg protocol.ServerMessage) {
				if msg.Content != "done" {
					t.Errorf("expected content 'done', got %s", msg.Content)
				}
				if msg.Partial == nil || *msg.Partial {
					t.Error("expected partial to be false")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := notificationToServerMessage(sessionID, &tt.notification, streamID)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if msg.Type != tt.expectedType {
				t.Errorf("expected type %s, got %s", tt.expectedType, msg.Type)
			}
			if msg.ID != streamID {
				t.Errorf("expected stream ID %s, got %s", streamID, msg.ID)
			}
			if tt.checkFunc != nil {
				tt.checkFunc(t, msg)
			}
		})
	}
}

func TestStreamSubscriber_DefaultCursor(t *testing.T) {
	mr, client := setupMiniRedis(t)
	defer mr.Close()
	defer client.Close()

	sub := NewStreamSubscriber("test", "", client)
	if sub.lastNotificationID != "$" {
		t.Errorf("expected default cursor '$', got %s", sub.lastNotificationID)
	}
}
