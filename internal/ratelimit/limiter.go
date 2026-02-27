// Package ratelimit provides Redis-backed rate limiting using the INCR + EXPIRE
// sliding window algorithm. It is designed for high-throughput WebSocket servers
// where each action (message, match request, connection) needs per-session or
// per-identity throttling.
package ratelimit

import (
	"context"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// Rule defines a rate limiting policy: the Redis key prefix, maximum number of
// requests allowed in the window, and the window duration.
type Rule struct {
	Key    string        // Redis key prefix (e.g., "rl:msg:", "rl:match:", "rl:conn:")
	Limit  int           // max count in the window
	Window time.Duration // time window
}

// Standard rate limiting rules per the architecture spec.
var (
	// RuleMessage allows 5 messages per 10 seconds per session.
	RuleMessage = Rule{Key: "rl:msg:", Limit: 5, Window: 10 * time.Second}

	// RuleMatch allows 10 match requests per minute per fingerprint/session.
	RuleMatch = Rule{Key: "rl:match:", Limit: 10, Window: 1 * time.Minute}

	// RuleConnect allows 5 WebSocket connections per minute per IP.
	RuleConnect = Rule{Key: "rl:conn:", Limit: 5, Window: 1 * time.Minute}
)

// Limiter performs rate limiting checks against Redis.
type Limiter struct {
	client *redis.Client
}

// NewLimiter creates a Limiter backed by the given Redis client.
func NewLimiter(client *redis.Client) *Limiter {
	return &Limiter{client: client}
}

// Allow checks whether the given identifier is within the rate limit defined by
// rule. It increments the counter in Redis and sets the expiry on first access.
//
// Returns true if the request is allowed, false if rate limited. On Redis
// errors the method fails open (returns true) so that a Redis outage does not
// block legitimate traffic.
func (l *Limiter) Allow(ctx context.Context, identifier string, rule Rule) (bool, error) {
	key := rule.Key + identifier

	count, err := l.client.Incr(ctx, key).Result()
	if err != nil {
		log.Printf("[ratelimit] redis INCR error key=%s: %v (failing open)", key, err)
		return true, err
	}

	// On the first increment, set the expiry to define the window boundary.
	if count == 1 {
		if err := l.client.Expire(ctx, key, rule.Window).Err(); err != nil {
			log.Printf("[ratelimit] redis EXPIRE error key=%s: %v (failing open)", key, err)
			// The key exists but has no TTL — it will persist. Best effort: try
			// to delete it so it doesn't block the identifier forever.
			l.client.Del(ctx, key)
			return true, err
		}
	}

	if int(count) > rule.Limit {
		return false, nil
	}

	return true, nil
}

// Remaining returns the number of requests the identifier has left in the
// current window for the given rule. Returns the full limit if the key does not
// exist yet. On Redis errors it returns the full limit (fail open).
func (l *Limiter) Remaining(ctx context.Context, identifier string, rule Rule) (int, error) {
	key := rule.Key + identifier

	count, err := l.client.Get(ctx, key).Int()
	if err == redis.Nil {
		return rule.Limit, nil
	}
	if err != nil {
		log.Printf("[ratelimit] redis GET error key=%s: %v (failing open)", key, err)
		return rule.Limit, err
	}

	remaining := rule.Limit - count
	if remaining < 0 {
		remaining = 0
	}
	return remaining, nil
}
