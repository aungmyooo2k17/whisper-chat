// Package messaging provides a NATS client wrapper for pub/sub messaging
// across Whisper services. It handles connection lifecycle, subject-based
// subscriptions, and convenience methods for chat and matchmaking channels.
package messaging

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

// NATS subject patterns used across Whisper services.
const (
	SubjectMatchRequest = "match.request"
	SubjectMatchCancel  = "match.cancel"
	SubjectMatchFound   = "match.found"      // + .<session_id>
	SubjectMatchNotify  = "match.notify"     // + .<session_id> (lifecycle events)
	SubjectChat         = "chat"             // + .<chat_id>
	SubjectModeration       = "moderation.check"
	SubjectModerationResult = "moderation.result"  // + .<session_id>
)

// NATSClient wraps the NATS connection with helper methods for pub/sub.
type NATSClient struct {
	conn *nats.Conn
	mu   sync.Mutex
	subs map[string]*nats.Subscription
}

// NATSConfig holds NATS connection settings.
type NATSConfig struct {
	URL           string        // nats://localhost:4222
	Name          string        // client name for identification
	ReconnectWait time.Duration // time between reconnect attempts
	MaxReconnects int           // max reconnect attempts (-1 for infinite)
}

// DefaultNATSConfig returns sensible defaults.
func DefaultNATSConfig() NATSConfig {
	return NATSConfig{
		URL:           "nats://localhost:4222",
		Name:          "whisper",
		ReconnectWait: 2 * time.Second,
		MaxReconnects: -1, // infinite reconnects
	}
}

// NewNATSClient connects to NATS with the given config and returns a ready client.
// It returns an error if the initial connection fails.
func NewNATSClient(config NATSConfig) (*NATSClient, error) {
	opts := []nats.Option{
		nats.Name(config.Name),
		nats.ReconnectWait(config.ReconnectWait),
		nats.MaxReconnects(config.MaxReconnects),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			if err != nil {
				log.Printf("[nats] disconnected: %v", err)
			} else {
				log.Printf("[nats] disconnected")
			}
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			log.Printf("[nats] reconnected to %s", nc.ConnectedUrl())
		}),
		nats.ClosedHandler(func(_ *nats.Conn) {
			log.Printf("[nats] connection closed")
		}),
	}

	nc, err := nats.Connect(config.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("nats connect: %w", err)
	}

	log.Printf("[nats] connected to %s", nc.ConnectedUrl())

	return &NATSClient{
		conn: nc,
		subs: make(map[string]*nats.Subscription),
	}, nil
}

// Publish sends data to the given NATS subject.
func (c *NATSClient) Publish(subject string, data []byte) error {
	return c.conn.Publish(subject, data)
}

// Subscribe registers a handler for the given subject and stores the
// subscription internally for later cleanup.
func (c *NATSClient) Subscribe(subject string, handler func(msg *nats.Msg)) error {
	sub, err := c.conn.Subscribe(subject, handler)
	if err != nil {
		return fmt.Errorf("nats subscribe %s: %w", subject, err)
	}

	c.mu.Lock()
	c.subs[subject] = sub
	c.mu.Unlock()

	return nil
}

// SubscribeToChat subscribes to the chat.<chatID> subject for a specific session.
// The subscription is keyed by sessionID to allow multiple users on the same
// server to subscribe to the same chat without overwriting each other.
func (c *NATSClient) SubscribeToChat(chatID string, sessionID string, handler func(data []byte)) error {
	subject := SubjectChat + "." + chatID
	key := "chatsub:" + sessionID
	sub, err := c.conn.Subscribe(subject, func(msg *nats.Msg) {
		handler(msg.Data)
	})
	if err != nil {
		return fmt.Errorf("nats subscribe %s: %w", subject, err)
	}

	c.mu.Lock()
	c.subs[key] = sub
	c.mu.Unlock()
	return nil
}

// UnsubscribeFromChat unsubscribes a session's chat subscription.
func (c *NATSClient) UnsubscribeFromChat(sessionID string) error {
	key := "chatsub:" + sessionID
	return c.unsubscribe(key)
}

// PublishChatMessage publishes data to the chat.<chatID> subject.
func (c *NATSClient) PublishChatMessage(chatID string, data []byte) error {
	subject := SubjectChat + "." + chatID
	return c.Publish(subject, data)
}

// PublishMatchRequest publishes data to the match.request subject.
func (c *NATSClient) PublishMatchRequest(data []byte) error {
	return c.Publish(SubjectMatchRequest, data)
}

// SubscribeMatchFound subscribes to the match.found.<sessionID> subject and
// passes the raw message data to the handler.
func (c *NATSClient) SubscribeMatchFound(sessionID string, handler func(data []byte)) error {
	subject := SubjectMatchFound + "." + sessionID
	return c.Subscribe(subject, func(msg *nats.Msg) {
		handler(msg.Data)
	})
}

// UnsubscribeMatchFound unsubscribes from the match.found.<sessionID> subject.
func (c *NATSClient) UnsubscribeMatchFound(sessionID string) error {
	subject := SubjectMatchFound + "." + sessionID
	return c.unsubscribe(subject)
}

// SubscribeMatchRequest subscribes to match request messages from WS servers.
func (c *NATSClient) SubscribeMatchRequest(handler func(data []byte)) error {
	return c.Subscribe(SubjectMatchRequest, func(msg *nats.Msg) {
		handler(msg.Data)
	})
}

// SubscribeMatchCancel subscribes to match cancellation messages from WS servers.
func (c *NATSClient) SubscribeMatchCancel(handler func(data []byte)) error {
	return c.Subscribe(SubjectMatchCancel, func(msg *nats.Msg) {
		handler(msg.Data)
	})
}

// PublishMatchCancel publishes a match cancellation request.
func (c *NATSClient) PublishMatchCancel(data []byte) error {
	return c.Publish(SubjectMatchCancel, data)
}

// SubscribeMatchNotify subscribes to match lifecycle notifications for a session.
func (c *NATSClient) SubscribeMatchNotify(sessionID string, handler func(data []byte)) error {
	subject := SubjectMatchNotify + "." + sessionID
	return c.Subscribe(subject, func(msg *nats.Msg) {
		handler(msg.Data)
	})
}

// UnsubscribeMatchNotify unsubscribes from match lifecycle notifications.
func (c *NATSClient) UnsubscribeMatchNotify(sessionID string) error {
	return c.unsubscribe(SubjectMatchNotify + "." + sessionID)
}

// PublishMatchNotify publishes a match lifecycle notification to a session.
func (c *NATSClient) PublishMatchNotify(sessionID string, data []byte) error {
	return c.Publish(SubjectMatchNotify+"."+sessionID, data)
}

// PublishModerationRequest publishes a moderation check request.
func (c *NATSClient) PublishModerationRequest(data []byte) error {
	return c.Publish(SubjectModeration, data)
}

// SubscribeModerationCheck subscribes to moderation check requests.
func (c *NATSClient) SubscribeModerationCheck(handler func(data []byte)) error {
	return c.Subscribe(SubjectModeration, func(msg *nats.Msg) {
		handler(msg.Data)
	})
}

// PublishModerationResult publishes a moderation result for a specific session.
func (c *NATSClient) PublishModerationResult(sessionID string, data []byte) error {
	return c.Publish(SubjectModerationResult+"."+sessionID, data)
}

// SubscribeModerationResult subscribes to moderation results for a specific session.
func (c *NATSClient) SubscribeModerationResult(sessionID string, handler func(data []byte)) error {
	subject := SubjectModerationResult + "." + sessionID
	return c.Subscribe(subject, func(msg *nats.Msg) {
		handler(msg.Data)
	})
}

// UnsubscribeModerationResult unsubscribes from moderation results for a session.
func (c *NATSClient) UnsubscribeModerationResult(sessionID string) error {
	return c.unsubscribe(SubjectModerationResult + "." + sessionID)
}

// Close drains all active subscriptions and closes the NATS connection.
func (c *NATSClient) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for subject, sub := range c.subs {
		if err := sub.Drain(); err != nil {
			log.Printf("[nats] drain %s: %v", subject, err)
		}
	}
	c.subs = make(map[string]*nats.Subscription)

	if err := c.conn.Drain(); err != nil {
		log.Printf("[nats] connection drain: %v", err)
	}

	log.Printf("[nats] client closed")
}

// unsubscribe removes and unsubscribes from a specific subject.
func (c *NATSClient) unsubscribe(subject string) error {
	c.mu.Lock()
	sub, ok := c.subs[subject]
	if !ok {
		c.mu.Unlock()
		return fmt.Errorf("nats: no subscription for subject %s", subject)
	}
	delete(c.subs, subject)
	c.mu.Unlock()

	if err := sub.Unsubscribe(); err != nil {
		return fmt.Errorf("nats unsubscribe %s: %w", subject, err)
	}
	return nil
}
