// Package client provides a reusable WebSocket load test client for the
// Whisper chat application. It connects using gobwas/ws (the same library the
// server uses), automatically handles the session_created -> set_fingerprint
// handshake, and tracks per-connection performance metrics.
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

// ---------------------------------------------------------------------------
// Protocol message types (local equivalents of internal/protocol constants)
// ---------------------------------------------------------------------------

// Client -> Server message types.
const (
	TypeSetFingerprint = "set_fingerprint"
	TypeFindMatch      = "find_match"
	TypeCancelMatch    = "cancel_match"
	TypeAcceptMatch    = "accept_match"
	TypeDeclineMatch   = "decline_match"
	TypeMessage        = "message"
	TypeTyping         = "typing"
	TypeEndChat        = "end_chat"
	TypePing           = "ping"
)

// Server -> Client message types.
const (
	TypeSessionCreated  = "session_created"
	TypeMatchingStarted = "matching_started"
	TypeMatchFound      = "match_found"
	TypeMatchAccepted   = "match_accepted"
	TypeMatchDeclined   = "match_declined"
	TypeMatchTimeout    = "match_timeout"
	TypePartnerLeft     = "partner_left"
	TypeRateLimited     = "rate_limited"
	TypeError           = "error"
	TypePong            = "pong"
)

// ---------------------------------------------------------------------------
// Metrics
// ---------------------------------------------------------------------------

// Metrics tracks per-connection performance data.
type Metrics struct {
	ConnectLatency   time.Duration
	FirstMsgLatency  time.Duration
	MessagesReceived int
	MessagesSent     int
	Errors           int
}

// ---------------------------------------------------------------------------
// Client
// ---------------------------------------------------------------------------

// Client represents a single simulated user connection to the Whisper server.
// It manages the WebSocket lifecycle, dispatches incoming messages to
// registered handlers, and automatically completes the session handshake.
type Client struct {
	conn      net.Conn
	sessionID string
	mu        sync.Mutex
	metrics   Metrics
	handlers  map[string]func(json.RawMessage)
	done      chan struct{}
	closeOnce sync.Once
	firstMsg  time.Time
}

// New creates a new load test client connected to the given WebSocket URL.
// The connection is established immediately and a background goroutine begins
// reading messages. The session_created handshake is handled automatically:
// when the server sends session_created, the client responds with
// set_fingerprint using a deterministic fingerprint derived from the session ID.
func New(ctx context.Context, url string) (*Client, error) {
	start := time.Now()
	conn, _, _, err := ws.Dial(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}

	c := &Client{
		conn:     conn,
		handlers: make(map[string]func(json.RawMessage)),
		done:     make(chan struct{}),
	}
	c.metrics.ConnectLatency = time.Since(start)

	// Start reading messages in background.
	go c.readLoop()

	return c, nil
}

// Send sends a JSON message to the server. It is goroutine-safe.
func (c *Client) Send(msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.metrics.MessagesSent++
	return wsutil.WriteClientMessage(c.conn, ws.OpText, data)
}

// On registers a handler for a specific server message type. The handler
// receives the full raw JSON of the message for flexible decoding.
// Handlers are invoked from the read loop goroutine so they should not block
// for extended periods. Only one handler per message type is supported;
// registering a second handler for the same type replaces the first.
func (c *Client) On(msgType string, handler func(json.RawMessage)) {
	c.handlers[msgType] = handler
}

// WaitForSession blocks until the server has assigned a session ID or the
// context is cancelled. This is useful for coordinating load test phases
// that depend on the handshake being complete.
func (c *Client) WaitForSession(ctx context.Context) error {
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.done:
			return fmt.Errorf("connection closed before session was created")
		case <-ticker.C:
			if c.sessionID != "" {
				return nil
			}
		}
	}
}

// Close closes the connection and stops the read loop. It is safe to call
// multiple times.
func (c *Client) Close() error {
	var err error
	c.closeOnce.Do(func() {
		close(c.done)
		err = c.conn.Close()
	})
	return err
}

// SessionID returns the session ID assigned by the server, or an empty string
// if the handshake has not completed yet.
func (c *Client) SessionID() string {
	return c.sessionID
}

// GetMetrics returns a copy of the client's metrics.
func (c *Client) GetMetrics() Metrics {
	return c.metrics
}

// readLoop continuously reads WebSocket frames from the server and dispatches
// them to registered handlers. It runs until the connection is closed or an
// unrecoverable error occurs.
func (c *Client) readLoop() {
	for {
		select {
		case <-c.done:
			return
		default:
		}

		data, err := wsutil.ReadServerText(c.conn)
		if err != nil {
			select {
			case <-c.done:
				// Connection was intentionally closed; do not count as error.
				return
			default:
			}
			c.metrics.Errors++
			return
		}

		// Track time of first message for FirstMsgLatency.
		if c.firstMsg.IsZero() {
			c.firstMsg = time.Now()
			c.metrics.FirstMsgLatency = c.metrics.ConnectLatency + time.Since(c.firstMsg)
		}

		c.metrics.MessagesReceived++

		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			continue
		}

		// Handle session_created internally: extract the session ID and
		// automatically send the set_fingerprint response.
		if envelope.Type == TypeSessionCreated {
			var msg struct {
				Type      string `json:"type"`
				SessionID string `json:"session_id"`
			}
			if err := json.Unmarshal(data, &msg); err == nil && msg.SessionID != "" {
				c.sessionID = msg.SessionID
				// Generate a deterministic fingerprint from the session ID.
				fingerprint := fmt.Sprintf("loadtest-%s", c.sessionID[:8])
				_ = c.Send(map[string]string{
					"type":        TypeSetFingerprint,
					"fingerprint": fingerprint,
				})
			}
		}

		// Dispatch to registered handler if one exists.
		if handler, ok := c.handlers[envelope.Type]; ok {
			handler(json.RawMessage(data))
		}
	}
}
