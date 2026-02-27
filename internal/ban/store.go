// Package ban provides fingerprint-based ban management backed by Redis.
// Ban records are stored as simple key-value pairs with TTL-based expiry:
//
//	Key:   ban:<fingerprint>
//	Value: <reason>
//	TTL:   ban duration
package ban

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// BanPrefix is the Redis key prefix for ban records.
	BanPrefix = "ban:"

	// ReportsPrefix is the Redis key prefix for report counters
	// (used by the escalating ban system in ABUSE-6).
	ReportsPrefix = "reports:"

	// Escalating ban durations (ABUSE-6).
	Ban15Min = 15 * time.Minute // 1st offense
	Ban1Hour = 1 * time.Hour   // 2nd offense
	Ban24Hour = 24 * time.Hour  // 3rd+ offense

	// ReportsTTL is how long the offense counter lives in Redis.
	// After 24h without new offenses the counter resets to zero.
	ReportsTTL = 24 * time.Hour

	// AutoBanThreshold is the number of reports within ReportsTTL that
	// triggers an automatic ban.
	AutoBanThreshold = 3
)

// Store manages ban records in Redis.
type Store struct {
	client *redis.Client
}

// NewStore creates a new ban store using the provided Redis client.
func NewStore(client *redis.Client) *Store {
	return &Store{client: client}
}

// IsBanned checks if a fingerprint is currently banned.
// Returns (isBanned, remainingSeconds, reason, error).
// If the fingerprint is not banned, isBanned is false and the other
// return values are zero/empty. Redis errors are returned so callers
// can decide how to handle them (the recommended policy is fail-open).
func (s *Store) IsBanned(ctx context.Context, fingerprint string) (bool, int, string, error) {
	key := BanPrefix + fingerprint

	reason, err := s.client.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return false, 0, "", nil
	}
	if err != nil {
		return false, 0, "", err
	}

	// Key exists — get the remaining TTL.
	ttl, err := s.client.TTL(ctx, key).Result()
	if err != nil {
		// We know the ban exists but can't read the TTL. Report banned
		// with 0 remaining rather than swallowing the ban.
		return true, 0, reason, nil
	}

	remaining := 0
	if ttl > 0 {
		remaining = int(ttl.Seconds())
	}

	return true, remaining, reason, nil
}

// Ban sets a ban on a fingerprint with the given duration and reason.
// The ban automatically expires after the specified duration.
func (s *Store) Ban(ctx context.Context, fingerprint string, duration time.Duration, reason string) error {
	key := BanPrefix + fingerprint
	return s.client.Set(ctx, key, reason, duration).Err()
}

// Unban removes a ban from a fingerprint immediately.
func (s *Store) Unban(ctx context.Context, fingerprint string) error {
	key := BanPrefix + fingerprint
	return s.client.Del(ctx, key).Err()
}

// ---------------------------------------------------------------------------
// Escalating ban system (ABUSE-6)
// ---------------------------------------------------------------------------

// escalationDuration returns the ban duration for a given offense count.
func escalationDuration(offenseCount int) time.Duration {
	switch {
	case offenseCount <= 1:
		return Ban15Min
	case offenseCount == 2:
		return Ban1Hour
	default:
		return Ban24Hour
	}
}

// GetOffenseCount returns the current offense/report counter for a fingerprint.
// Returns 0 if the key does not exist (no offenses recorded or counter expired).
func (s *Store) GetOffenseCount(ctx context.Context, fingerprint string) (int, error) {
	key := ReportsPrefix + fingerprint
	val, err := s.client.Get(ctx, key).Int()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return val, nil
}

// Escalate increments the offense counter for a fingerprint and applies a ban
// whose duration escalates with the number of offenses:
//
//	1st offense  -> 15 minutes
//	2nd offense  -> 1 hour
//	3rd+ offense -> 24 hours
//
// The offense counter has a 24h TTL that resets on first increment, so
// counters naturally expire if there is no new activity.
//
// Returns the ban duration that was applied.
func (s *Store) Escalate(ctx context.Context, fingerprint string, reason string) (time.Duration, error) {
	key := ReportsPrefix + fingerprint

	// Atomically increment the counter.
	count, err := s.client.Incr(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("ban: escalate incr: %w", err)
	}

	// Set TTL only on first increment so the window doesn't slide.
	if count == 1 {
		if err := s.client.Expire(ctx, key, ReportsTTL).Err(); err != nil {
			return 0, fmt.Errorf("ban: escalate expire: %w", err)
		}
	}

	duration := escalationDuration(int(count))
	if err := s.Ban(ctx, fingerprint, duration, reason); err != nil {
		return 0, fmt.Errorf("ban: escalate ban: %w", err)
	}

	return duration, nil
}

// ReportAndCheck increments the report counter for a fingerprint and checks
// whether the auto-ban threshold (3 reports in 24h) has been reached.
//
// If the threshold is met or exceeded, Escalate is called to apply a ban with
// escalating duration. Returns (banned, duration, error).
func (s *Store) ReportAndCheck(ctx context.Context, fingerprint string, reason string) (bool, time.Duration, error) {
	key := ReportsPrefix + fingerprint

	// Atomically increment the report counter.
	count, err := s.client.Incr(ctx, key).Result()
	if err != nil {
		return false, 0, fmt.Errorf("ban: report incr: %w", err)
	}

	// Set TTL only on first increment so the 24h window doesn't slide.
	if count == 1 {
		if err := s.client.Expire(ctx, key, ReportsTTL).Err(); err != nil {
			return false, 0, fmt.Errorf("ban: report expire: %w", err)
		}
	}

	// Auto-ban when threshold is reached.
	if count >= AutoBanThreshold {
		duration := escalationDuration(int(count))
		if err := s.Ban(ctx, fingerprint, duration, "multiple_reports"); err != nil {
			return false, 0, fmt.Errorf("ban: report ban: %w", err)
		}
		return true, duration, nil
	}

	return false, 0, nil
}
