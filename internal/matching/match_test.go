package matching

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// setupTestQueue creates a Queue connected to a test Redis instance.
// Requires Redis running on localhost:6379. Tests are skipped if unavailable.
func setupTestQueue(t *testing.T) (*Queue, context.Context) {
	t.Helper()

	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
		DB:   15, // use DB 15 for tests to avoid conflicts
	})

	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("skipping: Redis not available: %v", err)
	}

	// Flush test DB before each test.
	rdb.FlushDB(ctx)

	t.Cleanup(func() {
		rdb.FlushDB(ctx)
		rdb.Close()
	})

	return NewQueue(rdb), ctx
}

// enqueueTestUser is a helper that enqueues a user with a specific join time offset.
func enqueueTestUser(t *testing.T, q *Queue, ctx context.Context, sessionID string, interests []string) {
	t.Helper()
	if err := q.Enqueue(ctx, sessionID, interests); err != nil {
		t.Fatalf("failed to enqueue %s: %v", sessionID, err)
	}
}

// ---------- TrySingleInterestMatch tests ----------

func TestTrySingleInterestMatch_FindsCandidateWithOneSharedInterest(t *testing.T) {
	q, ctx := setupTestQueue(t)

	enqueueTestUser(t, q, ctx, "alice", []string{"gaming", "music", "cooking"})
	enqueueTestUser(t, q, ctx, "bob", []string{"sports", "music", "travel"})

	match, err := q.TrySingleInterestMatch(ctx, "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if match == nil {
		t.Fatal("expected a match, got nil")
	}

	if match.SessionA != "alice" {
		t.Errorf("expected SessionA=alice, got %s", match.SessionA)
	}
	if match.SessionB != "bob" {
		t.Errorf("expected SessionB=bob, got %s", match.SessionB)
	}
	// They share "music".
	if len(match.SharedInterests) == 0 {
		t.Fatal("expected at least one shared interest")
	}
	found := false
	for _, interest := range match.SharedInterests {
		if interest == "music" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'music' in shared interests, got %v", match.SharedInterests)
	}
}

func TestTrySingleInterestMatch_NoOverlap(t *testing.T) {
	q, ctx := setupTestQueue(t)

	enqueueTestUser(t, q, ctx, "alice", []string{"gaming", "music"})
	enqueueTestUser(t, q, ctx, "bob", []string{"sports", "cooking"})

	match, err := q.TrySingleInterestMatch(ctx, "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if match != nil {
		t.Errorf("expected no match when no interests overlap, got %+v", match)
	}
}

func TestTrySingleInterestMatch_SkipsSelf(t *testing.T) {
	q, ctx := setupTestQueue(t)

	enqueueTestUser(t, q, ctx, "alice", []string{"gaming"})

	match, err := q.TrySingleInterestMatch(ctx, "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if match != nil {
		t.Errorf("expected no match when only self is queued, got %+v", match)
	}
}

func TestTrySingleInterestMatch_EmptyQueue(t *testing.T) {
	q, ctx := setupTestQueue(t)

	// Enqueue and dequeue to have a valid session but empty interest sets.
	enqueueTestUser(t, q, ctx, "alice", []string{"gaming"})

	// No other users in queue.
	match, err := q.TrySingleInterestMatch(ctx, "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if match != nil {
		t.Errorf("expected no match with no other users, got %+v", match)
	}
}

func TestTrySingleInterestMatch_MultipleSharedInterests(t *testing.T) {
	q, ctx := setupTestQueue(t)

	enqueueTestUser(t, q, ctx, "alice", []string{"gaming", "music", "anime"})
	enqueueTestUser(t, q, ctx, "bob", []string{"gaming", "anime", "cooking"})

	match, err := q.TrySingleInterestMatch(ctx, "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if match == nil {
		t.Fatal("expected a match, got nil")
	}

	// They share "gaming" and "anime" — Tier 3 accepts any overlap >= 1,
	// but still returns all shared interests.
	if len(match.SharedInterests) < 1 {
		t.Errorf("expected at least 1 shared interest, got %d", len(match.SharedInterests))
	}
}

func TestTrySingleInterestMatch_SkipsDequeuedCandidate(t *testing.T) {
	q, ctx := setupTestQueue(t)

	enqueueTestUser(t, q, ctx, "alice", []string{"gaming"})
	enqueueTestUser(t, q, ctx, "bob", []string{"gaming"})

	// Dequeue bob so he's no longer valid.
	if err := q.Dequeue(ctx, "bob"); err != nil {
		t.Fatalf("failed to dequeue bob: %v", err)
	}

	match, err := q.TrySingleInterestMatch(ctx, "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if match != nil {
		t.Errorf("expected no match after candidate dequeued, got %+v", match)
	}
}

func TestTrySingleInterestMatch_NonExistentSession(t *testing.T) {
	q, ctx := setupTestQueue(t)

	match, err := q.TrySingleInterestMatch(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if match != nil {
		t.Errorf("expected nil for nonexistent session, got %+v", match)
	}
}

func TestTrySingleInterestMatch_SharedInterestsAreSorted(t *testing.T) {
	q, ctx := setupTestQueue(t)

	enqueueTestUser(t, q, ctx, "alice", []string{"zumba", "anime", "gaming"})
	enqueueTestUser(t, q, ctx, "bob", []string{"zumba", "gaming", "anime"})

	match, err := q.TrySingleInterestMatch(ctx, "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if match == nil {
		t.Fatal("expected a match, got nil")
	}

	// Verify shared interests are sorted alphabetically.
	for i := 1; i < len(match.SharedInterests); i++ {
		if match.SharedInterests[i] < match.SharedInterests[i-1] {
			t.Errorf("shared interests not sorted: %v", match.SharedInterests)
			break
		}
	}
}

// ---------- TryRandomMatch tests ----------

func TestTryRandomMatch_FindsAnyCandidate(t *testing.T) {
	q, ctx := setupTestQueue(t)

	enqueueTestUser(t, q, ctx, "alice", []string{"gaming"})
	enqueueTestUser(t, q, ctx, "bob", []string{"cooking"})

	match, err := q.TryRandomMatch(ctx, "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if match == nil {
		t.Fatal("expected a match, got nil")
	}

	if match.SessionA != "alice" {
		t.Errorf("expected SessionA=alice, got %s", match.SessionA)
	}
	if match.SessionB != "bob" {
		t.Errorf("expected SessionB=bob, got %s", match.SessionB)
	}
	if match.SharedInterests != nil {
		t.Errorf("expected nil SharedInterests for random match, got %v", match.SharedInterests)
	}
}

func TestTryRandomMatch_NoOtherUsers(t *testing.T) {
	q, ctx := setupTestQueue(t)

	enqueueTestUser(t, q, ctx, "alice", []string{"gaming"})

	match, err := q.TryRandomMatch(ctx, "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if match != nil {
		t.Errorf("expected no match when alone in queue, got %+v", match)
	}
}

func TestTryRandomMatch_EmptyQueue(t *testing.T) {
	q, ctx := setupTestQueue(t)

	match, err := q.TryRandomMatch(ctx, "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if match != nil {
		t.Errorf("expected no match with empty queue, got %+v", match)
	}
}

func TestTryRandomMatch_SkipsSelf(t *testing.T) {
	q, ctx := setupTestQueue(t)

	enqueueTestUser(t, q, ctx, "alice", []string{"gaming"})

	match, err := q.TryRandomMatch(ctx, "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if match != nil {
		t.Errorf("expected no match when only self is queued, got %+v", match)
	}
}

func TestTryRandomMatch_SkipsDequeuedCandidate(t *testing.T) {
	q, ctx := setupTestQueue(t)

	enqueueTestUser(t, q, ctx, "alice", []string{"gaming"})
	enqueueTestUser(t, q, ctx, "bob", []string{"cooking"})

	if err := q.Dequeue(ctx, "bob"); err != nil {
		t.Fatalf("failed to dequeue bob: %v", err)
	}

	match, err := q.TryRandomMatch(ctx, "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if match != nil {
		t.Errorf("expected no match after candidate dequeued, got %+v", match)
	}
}

func TestTryRandomMatch_OldestFirstFairness(t *testing.T) {
	q, ctx := setupTestQueue(t)

	// Enqueue in order: bob, charlie, alice requests match.
	enqueueTestUser(t, q, ctx, "bob", []string{"sports"})
	time.Sleep(10 * time.Millisecond) // ensure ordering
	enqueueTestUser(t, q, ctx, "charlie", []string{"music"})
	time.Sleep(10 * time.Millisecond)
	enqueueTestUser(t, q, ctx, "alice", []string{"gaming"})

	match, err := q.TryRandomMatch(ctx, "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if match == nil {
		t.Fatal("expected a match, got nil")
	}

	// Bob joined first, so he should be the match (oldest-first fairness).
	if match.SessionB != "bob" {
		t.Errorf("expected oldest user 'bob' as match, got %s", match.SessionB)
	}
}

func TestTryRandomMatch_PairsUsersWithNoSharedInterests(t *testing.T) {
	q, ctx := setupTestQueue(t)

	enqueueTestUser(t, q, ctx, "alice", []string{"gaming", "music"})
	enqueueTestUser(t, q, ctx, "bob", []string{"cooking", "travel"})

	match, err := q.TryRandomMatch(ctx, "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if match == nil {
		t.Fatal("expected a random match even without shared interests")
	}
	if match.SharedInterests != nil {
		t.Errorf("random match should have nil SharedInterests, got %v", match.SharedInterests)
	}
}

func TestTryRandomMatch_MultipleCandidates(t *testing.T) {
	q, ctx := setupTestQueue(t)

	// Add several users.
	for i := 0; i < 5; i++ {
		enqueueTestUser(t, q, ctx, fmt.Sprintf("user-%d", i), []string{fmt.Sprintf("interest-%d", i)})
		time.Sleep(5 * time.Millisecond)
	}

	match, err := q.TryRandomMatch(ctx, "user-4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if match == nil {
		t.Fatal("expected a match with multiple users")
	}
	if match.SessionA != "user-4" {
		t.Errorf("expected SessionA=user-4, got %s", match.SessionA)
	}
	// Should get the oldest user (user-0).
	if match.SessionB != "user-0" {
		t.Errorf("expected oldest user 'user-0' as match, got %s", match.SessionB)
	}
}

// ---------- Tier timing constant tests ----------

func TestTierTimingConstants(t *testing.T) {
	if tier1MaxWait != 10*time.Second {
		t.Errorf("tier1MaxWait should be 10s, got %v", tier1MaxWait)
	}
	if tier2MaxWait != 20*time.Second {
		t.Errorf("tier2MaxWait should be 20s, got %v", tier2MaxWait)
	}
	if tier3MaxWait != 25*time.Second {
		t.Errorf("tier3MaxWait should be 25s, got %v", tier3MaxWait)
	}
	if matchTimeout != 30*time.Second {
		t.Errorf("matchTimeout should be 30s, got %v", matchTimeout)
	}

	// Verify tier ordering is correct.
	if tier1MaxWait >= tier2MaxWait {
		t.Error("tier1MaxWait should be less than tier2MaxWait")
	}
	if tier2MaxWait >= tier3MaxWait {
		t.Error("tier2MaxWait should be less than tier3MaxWait")
	}
	if tier3MaxWait >= matchTimeout {
		t.Error("tier3MaxWait should be less than matchTimeout")
	}
}
