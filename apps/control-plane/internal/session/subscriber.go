package session

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/angristan/netclode/apps/control-plane/internal/protocol"
	"github.com/angristan/netclode/apps/control-plane/internal/storage"
	"github.com/redis/go-redis/v9"
)

const (
	// xreadBlockTimeout is how long XREAD BLOCK waits before returning.
	// This allows the subscriber to check for context cancellation periodically.
	xreadBlockTimeout = 5 * time.Second
)

// StreamSubscriber reads notifications from a Redis Stream for a session.
// It provides cursor-based reading that survives reconnection.
type StreamSubscriber struct {
	sessionID        string
	lastNotificationID string // Redis Stream cursor ("$" = new only, "0" = from beginning)
	messages         chan protocol.ServerMessage
	done             chan struct{}
	client           *redis.Client
}

// NewStreamSubscriber creates a new subscriber for the given session.
// lastNotificationID specifies where to start reading:
//   - "$" = only new notifications
//   - "0" = from the beginning of the stream
//   - "<stream-id>" = from after the given ID (exclusive)
func NewStreamSubscriber(sessionID, lastNotificationID string, client *redis.Client) *StreamSubscriber {
	if lastNotificationID == "" {
		lastNotificationID = "$"
	}
	return &StreamSubscriber{
		sessionID:        sessionID,
		lastNotificationID: lastNotificationID,
		messages:         make(chan protocol.ServerMessage, 64),
		done:             make(chan struct{}),
		client:           client,
	}
}

// Messages returns the channel to receive notifications as ServerMessages.
func (s *StreamSubscriber) Messages() <-chan protocol.ServerMessage {
	return s.messages
}

// Run starts the XREAD BLOCK loop. It blocks until the context is cancelled
// or Close() is called. Should be run in a goroutine.
func (s *StreamSubscriber) Run(ctx context.Context) {
	defer close(s.messages)

	streamKey := storage.NotificationsStreamKey(s.sessionID)
	cursor := s.lastNotificationID

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.done:
			return
		default:
		}

		// XREAD BLOCK with timeout
		streams, err := s.client.XRead(ctx, &redis.XReadArgs{
			Streams: []string{streamKey, cursor},
			Block:   xreadBlockTimeout,
			Count:   100, // Read up to 100 at a time
		}).Result()

		if err != nil {
			if err == redis.Nil {
				// Timeout, no new messages - continue loop
				continue
			}
			if ctx.Err() != nil {
				// Context cancelled
				return
			}
			slog.Error("XREAD error", "session", s.sessionID, "error", err)
			// Brief backoff before retry
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// Process messages from the stream
		for _, stream := range streams {
			for _, msg := range stream.Messages {
				// Update cursor to this message ID for next read
				cursor = msg.ID

				// Parse the notification
				dataStr, ok := msg.Values["data"].(string)
				if !ok {
					slog.Warn("invalid notification data", "session", s.sessionID, "id", msg.ID)
					continue
				}

				var notification storage.Notification
				if err := json.Unmarshal([]byte(dataStr), &notification); err != nil {
					slog.Warn("failed to unmarshal notification", "session", s.sessionID, "error", err)
					continue
				}

				// Convert notification to ServerMessage
				serverMsg, err := notificationToServerMessage(s.sessionID, &notification, msg.ID)
				if err != nil {
					slog.Warn("failed to convert notification", "session", s.sessionID, "type", notification.Type, "error", err)
					continue
				}

				// Send to channel (non-blocking with select for done check)
				select {
				case s.messages <- serverMsg:
				case <-ctx.Done():
					return
				case <-s.done:
					return
				}
			}
		}
	}
}

// Close stops the subscriber.
func (s *StreamSubscriber) Close() {
	select {
	case <-s.done:
		// Already closed
	default:
		close(s.done)
	}
}

// notificationToServerMessage converts a storage.Notification to a protocol.ServerMessage.
func notificationToServerMessage(sessionID string, n *storage.Notification, streamID string) (protocol.ServerMessage, error) {
	switch n.Type {
	case "event":
		var event protocol.AgentEvent
		if err := json.Unmarshal(n.Payload, &event); err != nil {
			return protocol.ServerMessage{}, err
		}
		msg := protocol.NewAgentEvent(sessionID, &event)
		msg.ID = streamID // Include stream ID for cursor tracking
		return msg, nil

	case "message":
		// The payload contains message data including partial flag
		var payload struct {
			ID        string `json:"id"`
			SessionID string `json:"sessionId"`
			Role      string `json:"role"`
			Content   string `json:"content"`
			Partial   bool   `json:"partial"`
		}
		if err := json.Unmarshal(n.Payload, &payload); err != nil {
			return protocol.ServerMessage{}, err
		}
		msg := protocol.NewAgentMessage(sessionID, payload.Content, payload.Partial, payload.ID)
		msg.ID = streamID
		return msg, nil

	case "session_update":
		var session protocol.Session
		if err := json.Unmarshal(n.Payload, &session); err != nil {
			return protocol.ServerMessage{}, err
		}
		msg := protocol.NewSessionUpdated(&session)
		msg.ID = streamID
		return msg, nil

	case "user_message":
		// User message payload contains the text
		var payload struct {
			Text      string `json:"text"`
			SessionID string `json:"sessionId"`
		}
		if err := json.Unmarshal(n.Payload, &payload); err != nil {
			return protocol.ServerMessage{}, err
		}
		msg := protocol.NewUserMessage(sessionID, payload.Text)
		msg.ID = streamID
		return msg, nil

	case "agent_done":
		msg := protocol.NewAgentDone(sessionID)
		msg.ID = streamID
		return msg, nil

	case "agent_error":
		var payload struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(n.Payload, &payload); err != nil {
			return protocol.ServerMessage{}, err
		}
		msg := protocol.NewAgentError(sessionID, payload.Error)
		msg.ID = streamID
		return msg, nil

	case "session_error":
		var payload struct {
			Error     string `json:"error"`
			SessionID string `json:"sessionId"`
		}
		if err := json.Unmarshal(n.Payload, &payload); err != nil {
			return protocol.ServerMessage{}, err
		}
		// Use the session ID from payload if available, otherwise fall back to the subscriber's sessionID
		sid := payload.SessionID
		if sid == "" {
			sid = sessionID
		}
		msg := protocol.NewSessionError(sid, payload.Error)
		msg.ID = streamID
		return msg, nil

	default:
		// Unknown type - return a generic message
		return protocol.ServerMessage{
			Type:      n.Type,
			SessionID: sessionID,
			ID:        streamID,
		}, nil
	}
}

