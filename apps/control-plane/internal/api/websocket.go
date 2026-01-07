package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/angristan/netclode/apps/control-plane/internal/protocol"
	"github.com/angristan/netclode/apps/control-plane/internal/session"
	"github.com/coder/websocket"
)

// subscriptionInfo holds a StreamSubscriber and its cancellation function.
type subscriptionInfo struct {
	sub    *session.StreamSubscriber
	cancel context.CancelFunc
}

// Connection represents a WebSocket connection.
type Connection struct {
	ws      *websocket.Conn
	manager *session.Manager
	server  *Server

	// Redis Streams-based subscriptions
	subscriptions map[string]*subscriptionInfo // sessionID -> subscription info
	subMu         sync.Mutex

	// Global messages channel (session create/delete events)
	globalMessages chan protocol.ServerMessage

	// For graceful shutdown
	done    chan struct{}
	writeMu sync.Mutex
}

// NewConnection creates a new WebSocket connection handler.
func NewConnection(ws *websocket.Conn, manager *session.Manager, server *Server) *Connection {
	return &Connection{
		ws:             ws,
		manager:        manager,
		server:         server,
		subscriptions:  make(map[string]*subscriptionInfo),
		globalMessages: make(chan protocol.ServerMessage, 64),
		done:           make(chan struct{}),
	}
}

// Run handles the WebSocket connection lifecycle.
func (c *Connection) Run(ctx context.Context) {
	defer c.Close()

	// Start goroutine to forward global messages
	go c.forwardGlobalMessages()

	for {
		select {
		case <-c.done:
			return
		default:
		}

		_, data, err := c.ws.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) != -1 {
				// Normal close
				return
			}
			slog.Warn("WebSocket read error", "error", err)
			return
		}

		var msg protocol.ClientMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			c.Send(protocol.NewError("Invalid JSON: " + err.Error()))
			continue
		}

		if err := c.HandleMessage(ctx, msg); err != nil {
			slog.Warn("Handler error", "type", msg.Type, "error", err)
			c.Send(protocol.NewError(err.Error()))
		}
	}
}

// forwardGlobalMessages reads from the global messages channel and sends to WebSocket.
func (c *Connection) forwardGlobalMessages() {
	for {
		select {
		case msg, ok := <-c.globalMessages:
			if !ok {
				return
			}
			if err := c.Send(msg); err != nil {
				slog.Debug("Failed to forward global message", "error", err)
				return
			}
		case <-c.done:
			return
		}
	}
}

// Send sends a message to the client.
func (c *Connection) Send(msg protocol.ServerMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	return c.ws.Write(context.Background(), websocket.MessageText, data)
}

// Close closes the connection and cancels all subscriptions.
func (c *Connection) Close() {
	// Signal done
	select {
	case <-c.done:
		// Already closed
		return
	default:
		close(c.done)
	}

	// Cancel all subscription contexts (which will stop the StreamSubscribers)
	c.subMu.Lock()
	for _, info := range c.subscriptions {
		info.cancel()
	}
	c.subscriptions = make(map[string]*subscriptionInfo)
	c.subMu.Unlock()

	c.ws.Close(websocket.StatusNormalClosure, "")
}

// subscribe adds a subscription for a session and starts forwarding messages.
// lastNotificationID specifies where to start reading from Redis Streams:
//   - "$" = only new notifications
//   - "0" = from the beginning
//   - "<stream-id>" = from after that ID
func (c *Connection) subscribe(_ context.Context, sessionID string, lastNotificationID string) error {
	c.subMu.Lock()
	defer c.subMu.Unlock()

	// If already subscribed, cancel old subscription first
	if old, ok := c.subscriptions[sessionID]; ok {
		old.cancel()
		delete(c.subscriptions, sessionID)
	}

	// Create a cancellable context for this subscription.
	// Use context.Background() instead of the HTTP request context because
	// the subscription should live as long as the WebSocket connection,
	// not just the individual HTTP request.
	subCtx, cancel := context.WithCancel(context.Background())

	sub, err := c.manager.Subscribe(subCtx, sessionID, lastNotificationID)
	if err != nil {
		cancel()
		return err
	}

	c.subscriptions[sessionID] = &subscriptionInfo{
		sub:    sub,
		cancel: cancel,
	}

	// Start goroutine to forward messages from StreamSubscriber to WebSocket
	go c.forwardMessages(sessionID, sub)

	return nil
}

// forwardMessages reads from the StreamSubscriber's Messages channel and sends to WebSocket.
func (c *Connection) forwardMessages(sessionID string, sub *session.StreamSubscriber) {
	for {
		select {
		case msg, ok := <-sub.Messages():
			if !ok {
				// Channel closed, subscriber stopped
				return
			}
			if err := c.Send(msg); err != nil {
				slog.Debug("Failed to forward message", "sessionID", sessionID, "error", err)
				return
			}
		case <-c.done:
			return
		}
	}
}

// unsubscribe removes a subscription for a session.
func (c *Connection) unsubscribe(sessionID string) {
	c.subMu.Lock()
	defer c.subMu.Unlock()

	if info, ok := c.subscriptions[sessionID]; ok {
		info.cancel() // Cancel context stops the StreamSubscriber
		delete(c.subscriptions, sessionID)
	}
}
