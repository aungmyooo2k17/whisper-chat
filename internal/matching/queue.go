package matching

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// Redis key patterns for matching data structures.
	keyMatchQueue     = "match:queue"        // Sorted set, score = join timestamp (ms)
	keyExactPrefix    = "match:exact:"       // + <interests_hash> -> Set of session IDs
	keyInterestPrefix = "match:interest:"    // + <tag> -> Set of session IDs
	keySessionPrefix  = "match:session:"     // + <session_id> -> Hash

	// TTL for matching data structures (auto-expire stale keys).
	matchKeyTTL = 60 * time.Second
)

// QueueEntry represents a user's state in the matching queue.
type QueueEntry struct {
	SessionID string
	Interests []string
	Hash      string  // SHA256 prefix of sorted interests
	JoinedAt  float64 // Unix timestamp in milliseconds
}

// Queue manages the Redis data structures for the matching queue.
type Queue struct {
	rdb *redis.Client
}

// NewQueue creates a new matching queue backed by Redis.
func NewQueue(rdb *redis.Client) *Queue {
	return &Queue{rdb: rdb}
}

// InterestsHash computes a deterministic hash of the interest set.
// Interests are sorted alphabetically before hashing to ensure order-independence.
func InterestsHash(interests []string) string {
	sorted := make([]string, len(interests))
	copy(sorted, interests)
	sort.Strings(sorted)
	joined := strings.Join(sorted, ",")
	h := sha256.Sum256([]byte(joined))
	return fmt.Sprintf("%x", h[:8]) // 16-char hex prefix
}

// Enqueue adds a user to the matching queue and all associated data structures.
func (q *Queue) Enqueue(ctx context.Context, sessionID string, interests []string) error {
	hash := InterestsHash(interests)
	now := float64(time.Now().UnixMilli())

	pipe := q.rdb.Pipeline()

	// Global sorted queue (score = timestamp for wait-time ordering).
	pipe.ZAdd(ctx, keyMatchQueue, redis.Z{Score: now, Member: sessionID})

	// Exact-match set (all users with identical interest hash).
	exactKey := keyExactPrefix + hash
	pipe.SAdd(ctx, exactKey, sessionID)
	pipe.Expire(ctx, exactKey, matchKeyTTL)

	// Per-interest sets (for overlap matching).
	for _, tag := range interests {
		interestKey := keyInterestPrefix + tag
		pipe.SAdd(ctx, interestKey, sessionID)
		pipe.Expire(ctx, interestKey, matchKeyTTL)
	}

	// Session match metadata.
	sessionKey := keySessionPrefix + sessionID
	pipe.HSet(ctx, sessionKey, map[string]interface{}{
		"interests": strings.Join(interests, ","),
		"hash":      hash,
		"joined_at": fmt.Sprintf("%.0f", now),
	})
	pipe.Expire(ctx, sessionKey, matchKeyTTL)

	_, err := pipe.Exec(ctx)
	return err
}

// Dequeue removes a user from the matching queue and all associated data structures.
func (q *Queue) Dequeue(ctx context.Context, sessionID string) error {
	entry, err := q.GetEntry(ctx, sessionID)
	if err != nil {
		return err
	}
	if entry == nil {
		return nil // already removed
	}

	pipe := q.rdb.Pipeline()

	pipe.ZRem(ctx, keyMatchQueue, sessionID)
	pipe.SRem(ctx, keyExactPrefix+entry.Hash, sessionID)

	for _, tag := range entry.Interests {
		pipe.SRem(ctx, keyInterestPrefix+tag, sessionID)
	}

	pipe.Del(ctx, keySessionPrefix+sessionID)

	_, err = pipe.Exec(ctx)
	return err
}

// GetEntry retrieves a user's queue entry. Returns nil if not found.
func (q *Queue) GetEntry(ctx context.Context, sessionID string) (*QueueEntry, error) {
	sessionKey := keySessionPrefix + sessionID
	result, err := q.rdb.HGetAll(ctx, sessionKey).Result()
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, nil
	}

	var interests []string
	if result["interests"] != "" {
		interests = strings.Split(result["interests"], ",")
	}

	var joinedAt float64
	if v, ok := result["joined_at"]; ok {
		fmt.Sscanf(v, "%f", &joinedAt)
	}

	return &QueueEntry{
		SessionID: sessionID,
		Interests: interests,
		Hash:      result["hash"],
		JoinedAt:  joinedAt,
	}, nil
}

// GetAllQueued returns all session IDs in the queue, ordered by join time (oldest first).
func (q *Queue) GetAllQueued(ctx context.Context) ([]string, error) {
	return q.rdb.ZRange(ctx, keyMatchQueue, 0, -1).Result()
}

// IsQueued checks if a session is currently in the matching queue.
func (q *Queue) IsQueued(ctx context.Context, sessionID string) (bool, error) {
	_, err := q.rdb.ZScore(ctx, keyMatchQueue, sessionID).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// GetExactCandidates returns all session IDs with the same interest hash.
func (q *Queue) GetExactCandidates(ctx context.Context, hash string) ([]string, error) {
	return q.rdb.SMembers(ctx, keyExactPrefix+hash).Result()
}

// GetInterestCandidates returns all session IDs interested in the given tag.
func (q *Queue) GetInterestCandidates(ctx context.Context, tag string) ([]string, error) {
	return q.rdb.SMembers(ctx, keyInterestPrefix+tag).Result()
}

// QueueSize returns the number of users currently in the matching queue.
func (q *Queue) QueueSize(ctx context.Context) (int64, error) {
	return q.rdb.ZCard(ctx, keyMatchQueue).Result()
}

// RefreshTTLs extends the TTL of all data structures for a queued session.
func (q *Queue) RefreshTTLs(ctx context.Context, sessionID string) error {
	entry, err := q.GetEntry(ctx, sessionID)
	if err != nil || entry == nil {
		return err
	}

	pipe := q.rdb.Pipeline()
	pipe.Expire(ctx, keyExactPrefix+entry.Hash, matchKeyTTL)
	for _, tag := range entry.Interests {
		pipe.Expire(ctx, keyInterestPrefix+tag, matchKeyTTL)
	}
	pipe.Expire(ctx, keySessionPrefix+sessionID, matchKeyTTL)
	_, err = pipe.Exec(ctx)
	return err
}
