package session

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// SessionPrefix is the Redis key prefix for all session hashes.
	SessionPrefix = "session:"

	// SessionTTL is the time-to-live for session keys in Redis.
	SessionTTL = 1 * time.Hour

	// Status constants for the session state machine.
	StatusIdle     = "idle"
	StatusMatching = "matching"
	StatusChatting = "chatting"
)

// Session represents a user's session state stored in Redis.
type Session struct {
	ID          string `redis:"id"`
	Status      string `redis:"status"`      // idle | matching | chatting
	ChatID      string `redis:"chat_id"`     // empty if not in chat
	Server      string `redis:"server"`      // which WS server instance
	Interests   string `redis:"interests"`   // comma-separated
	Fingerprint string `redis:"fingerprint"` // browser fingerprint hash
	CreatedAt   int64  `redis:"created_at"`  // unix timestamp
	LastActive  int64  `redis:"last_active"` // unix timestamp
}

// Store manages session state in Redis.
type Store struct {
	client     *redis.Client
	serverName string // identifier for this WS server instance
}

// NewStore creates a new session store connected to Redis.
func NewStore(redisAddr string, serverName string) (*Store, error) {
	client := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})

	// Verify connection.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("session: redis connection failed: %w", err)
	}

	return &Store{client: client, serverName: serverName}, nil
}

// Create stores a new session in Redis with idle status and 1h TTL.
func (s *Store) Create(ctx context.Context, sessionID string) error {
	key := SessionPrefix + sessionID
	now := time.Now().Unix()

	session := map[string]interface{}{
		"id":          sessionID,
		"status":      StatusIdle,
		"chat_id":     "",
		"server":      s.serverName,
		"interests":   "",
		"fingerprint": "",
		"created_at":  now,
		"last_active": now,
	}

	pipe := s.client.Pipeline()
	pipe.HSet(ctx, key, session)
	pipe.Expire(ctx, key, SessionTTL)
	_, err := pipe.Exec(ctx)
	return err
}

// Get retrieves a session from Redis. Returns nil if not found.
func (s *Store) Get(ctx context.Context, sessionID string) (*Session, error) {
	key := SessionPrefix + sessionID
	var session Session
	err := s.client.HGetAll(ctx, key).Scan(&session)
	if err != nil {
		return nil, err
	}
	if session.ID == "" {
		return nil, nil // not found
	}
	return &session, nil
}

// UpdateStatus updates the session status and refreshes the TTL.
func (s *Store) UpdateStatus(ctx context.Context, sessionID string, status string) error {
	key := SessionPrefix + sessionID
	pipe := s.client.Pipeline()
	pipe.HSet(ctx, key, "status", status, "last_active", time.Now().Unix())
	pipe.Expire(ctx, key, SessionTTL)
	_, err := pipe.Exec(ctx)
	return err
}

// SetInterests stores the user's selected interests.
func (s *Store) SetInterests(ctx context.Context, sessionID string, interests string) error {
	key := SessionPrefix + sessionID
	return s.client.HSet(ctx, key, "interests", interests, "last_active", time.Now().Unix()).Err()
}

// SetChatID sets the active chat ID for the session and marks status as chatting.
func (s *Store) SetChatID(ctx context.Context, sessionID string, chatID string) error {
	key := SessionPrefix + sessionID
	return s.client.HSet(ctx, key, "chat_id", chatID, "status", StatusChatting, "last_active", time.Now().Unix()).Err()
}

// ClearChatID removes the active chat ID and resets status to idle.
func (s *Store) ClearChatID(ctx context.Context, sessionID string) error {
	key := SessionPrefix + sessionID
	return s.client.HSet(ctx, key, "chat_id", "", "status", StatusIdle, "last_active", time.Now().Unix()).Err()
}

// SetFingerprint stores the browser fingerprint hash.
func (s *Store) SetFingerprint(ctx context.Context, sessionID string, fingerprint string) error {
	key := SessionPrefix + sessionID
	return s.client.HSet(ctx, key, "fingerprint", fingerprint).Err()
}

// RefreshTTL extends the session's TTL.
func (s *Store) RefreshTTL(ctx context.Context, sessionID string) error {
	key := SessionPrefix + sessionID
	return s.client.Expire(ctx, key, SessionTTL).Err()
}

// Delete removes a session from Redis.
func (s *Store) Delete(ctx context.Context, sessionID string) error {
	key := SessionPrefix + sessionID
	return s.client.Del(ctx, key).Err()
}

// Close closes the Redis connection.
func (s *Store) Close() error {
	return s.client.Close()
}

// Client returns the underlying Redis client for use by other packages.
func (s *Store) Client() *redis.Client {
	return s.client
}
