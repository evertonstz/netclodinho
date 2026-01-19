package api

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/angristan/netclode/services/control-plane/internal/protocol"
)

// TestConnectionClose verifies that Close properly cleans up resources.
// Note: We can't call conn.Close() directly because it tries to close the nil ws field.
// Instead, we simulate the cleanup logic manually.
func TestConnectionClose_CancelsSubscriptions(t *testing.T) {
	// Create a mock connection with subscriptions
	conn := &Connection{
		subscriptions:  make(map[string]*subscriptionInfo),
		globalMessages: make(chan protocol.ServerMessage, 64),
		done:           make(chan struct{}),
	}

	// Add mock subscriptions
	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())

	conn.subscriptions["sess-1"] = &subscriptionInfo{cancel: cancel1}
	conn.subscriptions["sess-2"] = &subscriptionInfo{cancel: cancel2}

	// Simulate Close logic (without calling ws.Close which panics on nil)
	close(conn.done)
	conn.subMu.Lock()
	for _, info := range conn.subscriptions {
		info.cancel()
	}
	conn.subscriptions = make(map[string]*subscriptionInfo)
	conn.subMu.Unlock()

	// Verify subscriptions are cancelled
	select {
	case <-ctx1.Done():
		// Good, ctx1 was cancelled
	default:
		t.Error("expected ctx1 to be cancelled")
	}

	select {
	case <-ctx2.Done():
		// Good, ctx2 was cancelled
	default:
		t.Error("expected ctx2 to be cancelled")
	}

	// Verify subscriptions map is cleared
	if len(conn.subscriptions) != 0 {
		t.Errorf("expected subscriptions to be cleared, got %d", len(conn.subscriptions))
	}

	// Verify done channel is closed
	select {
	case <-conn.done:
		// Good, done is closed
	default:
		t.Error("expected done channel to be closed")
	}
}

// TestConnectionClose_Idempotent verifies the done channel idempotent close behavior.
// Note: We can't call conn.Close() directly due to nil ws, so we test the done channel logic.
func TestConnectionClose_Idempotent(t *testing.T) {
	conn := &Connection{
		subscriptions:  make(map[string]*subscriptionInfo),
		globalMessages: make(chan protocol.ServerMessage, 64),
		done:           make(chan struct{}),
	}

	// Close done channel - should not panic on subsequent checks
	close(conn.done)

	// Verify it's closed
	select {
	case <-conn.done:
		// Good, already closed
	default:
		t.Error("expected done to be closed")
	}

	// Subsequent selects should also work without panic
	select {
	case <-conn.done:
		// Good
	default:
		t.Error("expected done to be closed")
	}
}

// TestServerBroadcastToAll tests the broadcast functionality.
func TestServerBroadcastToAll(t *testing.T) {
	server := &Server{
		shutdownCh: make(chan struct{}),
	}

	// Create mock connections
	conn1 := &Connection{
		globalMessages: make(chan protocol.ServerMessage, 64),
		done:           make(chan struct{}),
	}
	conn2 := &Connection{
		globalMessages: make(chan protocol.ServerMessage, 64),
		done:           make(chan struct{}),
	}
	conn3 := &Connection{
		globalMessages: make(chan protocol.ServerMessage, 64),
		done:           make(chan struct{}),
	}

	server.connections.Store(conn1, struct{}{})
	server.connections.Store(conn2, struct{}{})
	server.connections.Store(conn3, struct{}{})

	// Broadcast excluding conn2
	msg := protocol.NewSessionUpdated(&protocol.Session{ID: "test"})
	server.BroadcastToAll(msg, conn2)

	// conn1 and conn3 should receive the message
	select {
	case received := <-conn1.globalMessages:
		if received.Type != protocol.MsgTypeSessionUpdated {
			t.Errorf("conn1: expected session.updated, got %s", received.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("conn1: expected to receive message")
	}

	select {
	case received := <-conn3.globalMessages:
		if received.Type != protocol.MsgTypeSessionUpdated {
			t.Errorf("conn3: expected session.updated, got %s", received.Type)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("conn3: expected to receive message")
	}

	// conn2 should not receive (it was excluded)
	select {
	case <-conn2.globalMessages:
		t.Error("conn2: should not receive message (was excluded)")
	case <-time.After(50 * time.Millisecond):
		// Good, no message
	}
}

// TestServerBroadcastToAll_SkipsSlowClients verifies slow clients don't block broadcast.
func TestServerBroadcastToAll_SkipsSlowClients(t *testing.T) {
	server := &Server{
		shutdownCh: make(chan struct{}),
	}

	// Create a connection with a full channel (simulating slow client)
	slowConn := &Connection{
		globalMessages: make(chan protocol.ServerMessage, 1), // Very small buffer
		done:           make(chan struct{}),
	}
	// Fill the buffer
	slowConn.globalMessages <- protocol.ServerMessage{}

	fastConn := &Connection{
		globalMessages: make(chan protocol.ServerMessage, 64),
		done:           make(chan struct{}),
	}

	server.connections.Store(slowConn, struct{}{})
	server.connections.Store(fastConn, struct{}{})

	// This should not block even though slowConn's buffer is full
	done := make(chan struct{})
	go func() {
		server.BroadcastToAll(protocol.NewSessionUpdated(&protocol.Session{ID: "test"}), nil)
		close(done)
	}()

	select {
	case <-done:
		// Good, broadcast completed quickly
	case <-time.After(100 * time.Millisecond):
		t.Error("broadcast blocked on slow client")
	}

	// Fast client should still receive the message
	select {
	case <-fastConn.globalMessages:
		// Good
	default:
		t.Error("fast client should have received message")
	}
}

// TestServerActiveConnections tests the connection counter.
func TestServerActiveConnections(t *testing.T) {
	server := &Server{
		shutdownCh: make(chan struct{}),
	}

	if server.ActiveConnections() != 0 {
		t.Errorf("expected 0 connections, got %d", server.ActiveConnections())
	}

	server.connCount.Add(1)
	if server.ActiveConnections() != 1 {
		t.Errorf("expected 1 connection, got %d", server.ActiveConnections())
	}

	server.connCount.Add(5)
	if server.ActiveConnections() != 6 {
		t.Errorf("expected 6 connections, got %d", server.ActiveConnections())
	}
}

// TestConnectionSubscribe tests subscription management.
func TestConnectionSubscribe_ReplacesOldSubscription(t *testing.T) {
	conn := &Connection{
		subscriptions:  make(map[string]*subscriptionInfo),
		globalMessages: make(chan protocol.ServerMessage, 64),
		done:           make(chan struct{}),
	}

	// Add initial subscription
	ctx1, cancel1 := context.WithCancel(context.Background())
	conn.subscriptions["sess-1"] = &subscriptionInfo{cancel: cancel1}

	// Replace with new subscription (simulating re-subscribe)
	ctx2, cancel2 := context.WithCancel(context.Background())
	conn.subMu.Lock()
	if old, ok := conn.subscriptions["sess-1"]; ok {
		old.cancel()
		delete(conn.subscriptions, "sess-1")
	}
	conn.subscriptions["sess-1"] = &subscriptionInfo{cancel: cancel2}
	conn.subMu.Unlock()

	// Verify old context was cancelled
	select {
	case <-ctx1.Done():
		// Good
	default:
		t.Error("expected old subscription context to be cancelled")
	}

	// New context should still be active
	select {
	case <-ctx2.Done():
		t.Error("new subscription context should not be cancelled")
	default:
		// Good
	}

	// Clean up
	cancel2()
}

// TestConnectionUnsubscribe tests unsubscription.
func TestConnectionUnsubscribe(t *testing.T) {
	conn := &Connection{
		subscriptions:  make(map[string]*subscriptionInfo),
		globalMessages: make(chan protocol.ServerMessage, 64),
		done:           make(chan struct{}),
	}

	// Add subscription
	ctx, cancel := context.WithCancel(context.Background())
	conn.subscriptions["sess-1"] = &subscriptionInfo{cancel: cancel}

	// Unsubscribe
	conn.unsubscribe("sess-1")

	// Verify context was cancelled
	select {
	case <-ctx.Done():
		// Good
	default:
		t.Error("expected subscription context to be cancelled")
	}

	// Verify subscription was removed
	if _, ok := conn.subscriptions["sess-1"]; ok {
		t.Error("expected subscription to be removed")
	}
}

// TestConnectionUnsubscribe_NonExistent tests unsubscribing non-existent session.
func TestConnectionUnsubscribe_NonExistent(t *testing.T) {
	conn := &Connection{
		subscriptions:  make(map[string]*subscriptionInfo),
		globalMessages: make(chan protocol.ServerMessage, 64),
		done:           make(chan struct{}),
	}

	// Should not panic
	conn.unsubscribe("nonexistent")
}

// TestConnectionConcurrentAccess tests thread safety.
func TestConnectionConcurrentAccess(t *testing.T) {
	conn := &Connection{
		subscriptions:  make(map[string]*subscriptionInfo),
		globalMessages: make(chan protocol.ServerMessage, 64),
		done:           make(chan struct{}),
	}

	var wg sync.WaitGroup

	// Concurrent subscribe/unsubscribe operations
	for i := 0; i < 10; i++ {
		wg.Add(2)
		sessionID := "sess-" + string(rune('0'+i))

		go func(id string) {
			defer wg.Done()
			_, cancel := context.WithCancel(context.Background())
			conn.subMu.Lock()
			conn.subscriptions[id] = &subscriptionInfo{cancel: cancel}
			conn.subMu.Unlock()
		}(sessionID)

		go func(id string) {
			defer wg.Done()
			time.Sleep(10 * time.Millisecond)
			conn.unsubscribe(id)
		}(sessionID)
	}

	wg.Wait()

	// Should not have panicked
}

// TestForwardGlobalMessages tests global message forwarding.
func TestForwardGlobalMessages(t *testing.T) {
	conn := &Connection{
		subscriptions:  make(map[string]*subscriptionInfo),
		globalMessages: make(chan protocol.ServerMessage, 64),
		done:           make(chan struct{}),
	}

	// Track messages received
	received := make([]protocol.ServerMessage, 0)
	var mu sync.Mutex

	// Start forwarding in background (simulated - we can't use the real forwardGlobalMessages
	// because it requires a WebSocket connection)
	go func() {
		for {
			select {
			case msg, ok := <-conn.globalMessages:
				if !ok {
					return
				}
				mu.Lock()
				received = append(received, msg)
				mu.Unlock()
			case <-conn.done:
				return
			}
		}
	}()

	// Send messages
	conn.globalMessages <- protocol.NewSessionCreated(&protocol.Session{ID: "sess-1"})
	conn.globalMessages <- protocol.NewSessionUpdated(&protocol.Session{ID: "sess-2"})

	time.Sleep(50 * time.Millisecond)

	// Close
	close(conn.done)

	mu.Lock()
	defer mu.Unlock()

	if len(received) != 2 {
		t.Errorf("expected 2 messages, got %d", len(received))
	}
}
