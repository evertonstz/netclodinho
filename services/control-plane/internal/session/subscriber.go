package session

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	pb "github.com/angristan/netclode/services/control-plane/gen/netclode/v1"
	"github.com/angristan/netclode/services/control-plane/internal/storage"
	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	// xreadBlockTimeout is how long XREAD BLOCK waits before returning.
	// This allows the subscriber to check for context cancellation periodically.
	xreadBlockTimeout = 5 * time.Second
)

// StreamSubscriber reads notifications from a Redis Stream for a session.
// It provides cursor-based reading that survives reconnection.
type StreamSubscriber struct {
	sessionID          string
	lastNotificationID string // Redis Stream cursor ("$" = new only, "0" = from beginning)
	messages           chan *pb.ServerMessage
	done               chan struct{}
	client             *redis.Client
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
		sessionID:          sessionID,
		lastNotificationID: lastNotificationID,
		messages:           make(chan *pb.ServerMessage, 64),
		done:               make(chan struct{}),
		client:             client,
	}
}

// Messages returns the channel to receive notifications as ServerMessages.
func (s *StreamSubscriber) Messages() <-chan *pb.ServerMessage {
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
				serverMsg := notificationToServerMessage(s.sessionID, &notification)
				if serverMsg == nil {
					slog.Warn("failed to convert notification", "session", s.sessionID, "type", notification.Type)
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

// notificationToServerMessage converts a storage.Notification to a pb.ServerMessage.
func notificationToServerMessage(sessionID string, n *storage.Notification) *pb.ServerMessage {
	switch n.Type {
	case "event":
		var event pb.AgentEvent
		if err := protojson.Unmarshal(n.Payload, &event); err != nil {
			slog.Warn("failed to unmarshal agent event", "session", sessionID, "error", err)
			return nil
		}
		return &pb.ServerMessage{
			Message: &pb.ServerMessage_AgentEvent{
				AgentEvent: &pb.AgentEventResponse{
					SessionId: sessionID,
					Event:     &event,
				},
			},
		}

	case "message":
		var msg pb.AgentMessageResponse
		if err := protojson.Unmarshal(n.Payload, &msg); err != nil {
			slog.Warn("failed to unmarshal message payload", "session", sessionID, "error", err)
			return nil
		}
		return &pb.ServerMessage{
			Message: &pb.ServerMessage_AgentMessage{
				AgentMessage: &msg,
			},
		}

	case "session_update":
		var session pb.Session
		if err := protojson.Unmarshal(n.Payload, &session); err != nil {
			slog.Warn("failed to unmarshal session", "session", sessionID, "error", err)
			return nil
		}
		return &pb.ServerMessage{
			Message: &pb.ServerMessage_SessionUpdated{
				SessionUpdated: &pb.SessionUpdatedResponse{
					Session: &session,
				},
			},
		}

	case "user_message":
		var msg pb.UserMessageResponse
		if err := protojson.Unmarshal(n.Payload, &msg); err != nil {
			slog.Warn("failed to unmarshal user message payload", "session", sessionID, "error", err)
			return nil
		}
		return &pb.ServerMessage{
			Message: &pb.ServerMessage_UserMessage{
				UserMessage: &msg,
			},
		}

	case "agent_done":
		var msg pb.AgentDoneResponse
		if err := protojson.Unmarshal(n.Payload, &msg); err != nil {
			slog.Warn("failed to unmarshal agent done payload", "session", sessionID, "error", err)
			return nil
		}
		return &pb.ServerMessage{
			Message: &pb.ServerMessage_AgentDone{
				AgentDone: &msg,
			},
		}

	case "agent_error":
		var err pb.Error
		if unmarshalErr := protojson.Unmarshal(n.Payload, &err); unmarshalErr != nil {
			slog.Warn("failed to unmarshal agent error payload", "session", sessionID, "error", unmarshalErr)
			return nil
		}
		return &pb.ServerMessage{
			Message: &pb.ServerMessage_Error{
				Error: &pb.ErrorResponse{
					Error: &err,
				},
			},
		}

	case "session_error":
		var err pb.Error
		if unmarshalErr := protojson.Unmarshal(n.Payload, &err); unmarshalErr != nil {
			slog.Warn("failed to unmarshal session error payload", "session", sessionID, "error", unmarshalErr)
			return nil
		}
		return &pb.ServerMessage{
			Message: &pb.ServerMessage_Error{
				Error: &pb.ErrorResponse{
					Error: &err,
				},
			},
		}

	case "terminal_output":
		var msg pb.TerminalOutputResponse
		if err := protojson.Unmarshal(n.Payload, &msg); err != nil {
			slog.Warn("failed to unmarshal terminal output payload", "session", sessionID, "error", err)
			return nil
		}
		return &pb.ServerMessage{
			Message: &pb.ServerMessage_TerminalOutput{
				TerminalOutput: &msg,
			},
		}

	default:
		slog.Warn("unknown notification type", "session", sessionID, "type", n.Type)
		return nil
	}
}
