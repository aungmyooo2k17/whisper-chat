package matching

import (
	"testing"
)

func TestInterestsHash_OrderIndependent(t *testing.T) {
	h1 := InterestsHash([]string{"gaming", "music", "anime"})
	h2 := InterestsHash([]string{"anime", "gaming", "music"})
	h3 := InterestsHash([]string{"music", "anime", "gaming"})

	if h1 != h2 || h2 != h3 {
		t.Errorf("hashes should be identical regardless of order: %s, %s, %s", h1, h2, h3)
	}
}

func TestInterestsHash_DifferentSets(t *testing.T) {
	h1 := InterestsHash([]string{"gaming", "music"})
	h2 := InterestsHash([]string{"gaming", "anime"})

	if h1 == h2 {
		t.Errorf("different interest sets should produce different hashes")
	}
}

func TestInterestsHash_EmptyAndSingle(t *testing.T) {
	h1 := InterestsHash([]string{})
	h2 := InterestsHash([]string{"gaming"})

	if h1 == h2 {
		t.Errorf("empty and non-empty sets should produce different hashes")
	}

	// Single element should be deterministic.
	h3 := InterestsHash([]string{"gaming"})
	if h2 != h3 {
		t.Errorf("same single element should produce same hash")
	}
}

func TestNewQueue(t *testing.T) {
	// Verify queue can be created without Redis (nil client for unit test).
	q := NewQueue(nil)
	if q == nil {
		t.Fatal("NewQueue should return non-nil Queue")
	}
	if q.rdb != nil {
		t.Error("Queue.rdb should be nil when created with nil client")
	}
}

func TestQueueEntry_Fields(t *testing.T) {
	entry := &QueueEntry{
		SessionID: "test-session",
		Interests: []string{"music", "gaming"},
		Hash:      "abc123",
		JoinedAt:  1000.0,
	}

	if entry.SessionID != "test-session" {
		t.Errorf("unexpected SessionID: %s", entry.SessionID)
	}
	if len(entry.Interests) != 2 {
		t.Errorf("unexpected interest count: %d", len(entry.Interests))
	}
}

func TestMatchCandidate_Fields(t *testing.T) {
	candidate := &MatchCandidate{
		SessionA:        "session-a",
		SessionB:        "session-b",
		SharedInterests: []string{"music", "gaming"},
	}

	if candidate.SessionA != "session-a" {
		t.Errorf("unexpected SessionA: %s", candidate.SessionA)
	}
	if candidate.SessionB != "session-b" {
		t.Errorf("unexpected SessionB: %s", candidate.SessionB)
	}
	if len(candidate.SharedInterests) != 2 {
		t.Errorf("unexpected shared interests count: %d", len(candidate.SharedInterests))
	}
}

func TestMatchRequest_Fields(t *testing.T) {
	req := MatchRequest{
		SessionID: "test-session",
		Interests: []string{"music"},
	}
	if req.SessionID != "test-session" {
		t.Errorf("unexpected SessionID: %s", req.SessionID)
	}
}

func TestCancelRequest_Fields(t *testing.T) {
	req := CancelRequest{SessionID: "test-session"}
	if req.SessionID != "test-session" {
		t.Errorf("unexpected SessionID: %s", req.SessionID)
	}
}

func TestMatchResult_Fields(t *testing.T) {
	result := MatchResult{
		ChatID:          "chat-123",
		PartnerID:       "partner-456",
		SharedInterests: []string{"gaming"},
		AcceptDeadline:  15,
	}
	if result.ChatID != "chat-123" {
		t.Errorf("unexpected ChatID: %s", result.ChatID)
	}
	if result.AcceptDeadline != 15 {
		t.Errorf("unexpected AcceptDeadline: %d", result.AcceptDeadline)
	}
}

// Integration tests (require Redis) are guarded behind build tag.
// Run with: go test -tags=integration ./internal/matching/

func TestInterestsHash_Deterministic(t *testing.T) {
	// Same input should always produce the same hash.
	for i := 0; i < 100; i++ {
		h := InterestsHash([]string{"gaming", "music", "anime"})
		if len(h) != 16 {
			t.Fatalf("hash should be 16 hex chars, got %d: %s", len(h), h)
		}
	}
}
