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
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	// xreadBlockTimeout is how long XREAD BLOCK waits before returning.
	// This allows the subscriber to check for context cancellation periodically.
	xreadBlockTimeout = 5 * time.Second
)

// StreamSubscriber reads entries from a Redis Stream for a session.
// It provides cursor-based reading that survives reconnection.
type StreamSubscriber struct {
	sessionID    string
	lastStreamID string // Redis Stream cursor ("$" = new only, "0" = from beginning)
	messages     chan *pb.ServerMessage
	done         chan struct{}
	client       *redis.Client
}

// NewStreamSubscriber creates a new subscriber for the given session.
// lastStreamID specifies where to start reading:
//   - "$" = only new entries
//   - "0" = from the beginning of the stream
//   - "<stream-id>" = from after the given ID (exclusive)
func NewStreamSubscriber(sessionID, lastStreamID string, client *redis.Client) *StreamSubscriber {
	if lastStreamID == "" {
		lastStreamID = "$"
	}
	return &StreamSubscriber{
		sessionID:    sessionID,
		lastStreamID: lastStreamID,
		messages:     make(chan *pb.ServerMessage, 64),
		done:         make(chan struct{}),
		client:       client,
	}
}

// Messages returns the channel to receive entries as ServerMessages.
func (s *StreamSubscriber) Messages() <-chan *pb.ServerMessage {
	return s.messages
}

// Run starts the XREAD BLOCK loop. It blocks until the context is cancelled
// or Close() is called. Should be run in a goroutine.
func (s *StreamSubscriber) Run(ctx context.Context) {
	defer close(s.messages)

	streamKey := storage.StreamKey(s.sessionID)
	cursor := s.lastStreamID

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
			slog.ErrorContext(ctx, "XREAD error", "session", s.sessionID, "error", err)
			// Brief backoff before retry
			time.Sleep(100 * time.Millisecond)
			continue
		}

		// Process entries from the stream
		for _, stream := range streams {
			for _, msg := range stream.Messages {
				// Update cursor to this message ID for next read
				cursor = msg.ID

				// Parse the stream entry
				dataStr, ok := msg.Values["data"].(string)
				if !ok {
					slog.WarnContext(ctx, "invalid stream entry data", "session", s.sessionID, "id", msg.ID)
					continue
				}

				var entry storage.StreamEntry
				if err := json.Unmarshal([]byte(dataStr), &entry); err != nil {
					slog.WarnContext(ctx, "failed to unmarshal stream entry", "session", s.sessionID, "error", err)
					continue
				}

				// Convert stream entry to ServerMessage
				serverMsg := streamEntryToServerMessage(s.sessionID, msg.ID, &entry)
				if serverMsg == nil {
					slog.WarnContext(ctx, "failed to convert stream entry", "session", s.sessionID, "type", entry.Type)
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

// streamEntryToServerMessage converts a storage.StreamEntry to a pb.ServerMessage.
// All stream entries are now wrapped in StreamEntryResponse for unified handling.
func streamEntryToServerMessage(sessionID string, streamID string, e *storage.StreamEntry) *pb.ServerMessage {
	protoEntry := storageEntryToProto(sessionID, streamID, e)
	if protoEntry == nil {
		return nil
	}

	return &pb.ServerMessage{
		Message: &pb.ServerMessage_StreamEntry{
			StreamEntry: &pb.StreamEntryResponse{
				SessionId: sessionID,
				Entry:     protoEntry,
			},
		},
	}
}

// storageEntryToProto converts a storage.StreamEntry to a pb.StreamEntry.
// The new unified model uses oneof payload with: AgentEvent, TerminalOutput, Session, Error
func storageEntryToProto(sessionID string, streamID string, e *storage.StreamEntry) *pb.StreamEntry {
	entry := &pb.StreamEntry{
		Id:      streamID,
		Partial: e.Partial,
	}

	// Parse timestamp
	if t, err := time.Parse(time.RFC3339, e.Timestamp); err == nil {
		entry.Timestamp = timestamppb.New(t)
	}

	// Set payload based on type
	switch e.Type {
	case storage.StreamEntryTypeEvent:
		// AgentEvent includes all content: messages, thinking, tools, etc.
		var event pb.AgentEvent
		if err := protojson.Unmarshal(e.Payload, &event); err != nil {
			slog.Warn("failed to unmarshal agent event", "session", sessionID, "error", err)
			return nil
		}
		entry.Payload = &pb.StreamEntry_Event{Event: &event}

	case storage.StreamEntryTypeTerminalOutput:
		var output pb.TerminalOutput
		if err := protojson.Unmarshal(e.Payload, &output); err != nil {
			slog.Warn("failed to unmarshal terminal output payload", "session", sessionID, "error", err)
			return nil
		}
		entry.Payload = &pb.StreamEntry_TerminalOutput{TerminalOutput: &output}

	case storage.StreamEntryTypeSessionUpdate:
		var sess pb.Session
		if err := protojson.Unmarshal(e.Payload, &sess); err != nil {
			slog.Warn("failed to unmarshal session update", "session", sessionID, "error", err)
			return nil
		}
		entry.Payload = &pb.StreamEntry_SessionUpdate{SessionUpdate: &sess}

	case storage.StreamEntryTypeError:
		var errProto pb.Error
		if err := protojson.Unmarshal(e.Payload, &errProto); err != nil {
			slog.Warn("failed to unmarshal error payload", "session", sessionID, "error", err)
			return nil
		}
		entry.Payload = &pb.StreamEntry_Error{Error: &errProto}

	default:
		slog.Warn("unknown stream entry type", "session", sessionID, "type", e.Type)
		return nil
	}

	return entry
}
