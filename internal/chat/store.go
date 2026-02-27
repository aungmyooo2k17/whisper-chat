package chat

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	ChatPrefix     = "chat:"
	PendingKey     = "match:pending_chats"
	ChatTTLPending = 60 * time.Second
	ChatTTLActive  = 2 * time.Hour

	StatusPendingAccept = "pending_accept"
	StatusActive        = "active"
	StatusEnded         = "ended"
)

// ChatSession represents an active or pending chat between two users.
type ChatSession struct {
	ChatID         string
	UserA          string
	UserB          string
	Status         string
	CreatedAt      int64
	AcceptDeadline int64
	AcceptedA      bool
	AcceptedB      bool
}

// GetPartner returns the partner's session ID.
func (cs *ChatSession) GetPartner(sessionID string) string {
	if sessionID == cs.UserA {
		return cs.UserB
	}
	if sessionID == cs.UserB {
		return cs.UserA
	}
	return ""
}

// IsParticipant checks if a session is part of this chat.
func (cs *ChatSession) IsParticipant(sessionID string) bool {
	return sessionID == cs.UserA || sessionID == cs.UserB
}

// Store manages chat session state in Redis.
type Store struct {
	rdb          *redis.Client
	acceptScript *redis.Script
}

// NewStore creates a new chat store backed by Redis.
func NewStore(rdb *redis.Client) *Store {
	return &Store{
		rdb:          rdb,
		acceptScript: redis.NewScript(acceptMatchLua),
	}
}

// CreatePending creates a new chat session with pending_accept status.
// Called by the matcher when a match is found.
func (s *Store) CreatePending(ctx context.Context, chatID, userA, userB string) error {
	key := ChatPrefix + chatID
	now := time.Now().Unix()
	deadline := now + 15

	pipe := s.rdb.Pipeline()
	pipe.HSet(ctx, key, map[string]interface{}{
		"user_a":          userA,
		"user_b":          userB,
		"status":          StatusPendingAccept,
		"created_at":      now,
		"accept_deadline": deadline,
		"accepted_a":      "false",
		"accepted_b":      "false",
	})
	pipe.Expire(ctx, key, ChatTTLPending)
	pipe.ZAdd(ctx, PendingKey, redis.Z{Score: float64(deadline), Member: chatID})
	_, err := pipe.Exec(ctx)
	return err
}

// Get retrieves a chat session. Returns nil if not found.
func (s *Store) Get(ctx context.Context, chatID string) (*ChatSession, error) {
	key := ChatPrefix + chatID
	result, err := s.rdb.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, nil
	}

	createdAt, _ := strconv.ParseInt(result["created_at"], 10, 64)
	acceptDeadline, _ := strconv.ParseInt(result["accept_deadline"], 10, 64)

	return &ChatSession{
		ChatID:         chatID,
		UserA:          result["user_a"],
		UserB:          result["user_b"],
		Status:         result["status"],
		CreatedAt:      createdAt,
		AcceptDeadline: acceptDeadline,
		AcceptedA:      result["accepted_a"] == "true",
		AcceptedB:      result["accepted_b"] == "true",
	}, nil
}

// AcceptMatch atomically records a user's acceptance. Returns:
//
//	1 = both accepted (chat is now active)
//	0 = waiting for partner
//	-1 = chat not found
//	-2 = wrong status (not pending_accept)
//	-3 = session not a participant
func (s *Store) AcceptMatch(ctx context.Context, chatID, sessionID string) (int, error) {
	key := ChatPrefix + chatID
	result, err := s.acceptScript.Run(ctx, s.rdb, []string{key}, sessionID).Int()
	if err != nil {
		return -1, fmt.Errorf("chat: accept match: %w", err)
	}
	return result, nil
}

// Delete removes a chat session and its pending tracking entry.
func (s *Store) Delete(ctx context.Context, chatID string) error {
	pipe := s.rdb.Pipeline()
	pipe.Del(ctx, ChatPrefix+chatID)
	pipe.ZRem(ctx, PendingKey, chatID)
	_, err := pipe.Exec(ctx)
	return err
}

// acceptMatchLua atomically marks a user as accepted and checks if both have.
// If both accepted, it sets status to active and extends TTL to 2 hours.
const acceptMatchLua = `
local key = KEYS[1]
local session_id = ARGV[1]

local status = redis.call('HGET', key, 'status')
if not status then return -1 end
if status ~= 'pending_accept' then return -2 end

local user_a = redis.call('HGET', key, 'user_a')
local user_b = redis.call('HGET', key, 'user_b')

if session_id == user_a then
    redis.call('HSET', key, 'accepted_a', 'true')
elseif session_id == user_b then
    redis.call('HSET', key, 'accepted_b', 'true')
else
    return -3
end

local accepted_a = redis.call('HGET', key, 'accepted_a')
local accepted_b = redis.call('HGET', key, 'accepted_b')

if accepted_a == 'true' and accepted_b == 'true' then
    redis.call('HSET', key, 'status', 'active')
    redis.call('EXPIRE', key, 7200)
    return 1
end

return 0
`
