package matching

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/whisper/chat-app/internal/messaging"
)

// MatchResult is the payload published via NATS when a match is found or times out.
// Each matched user receives this on their match.found.<session_id> subject.
type MatchResult struct {
	Timeout         bool     `json:"timeout,omitempty"`
	ChatID          string   `json:"chat_id,omitempty"`
	PartnerID       string   `json:"partner_id,omitempty"`
	SharedInterests []string `json:"shared_interests,omitempty"`
	AcceptDeadline  int      `json:"accept_deadline,omitempty"`
}

// MatchNotification is sent via NATS match.notify.<session_id> for match lifecycle events.
type MatchNotification struct {
	Type   string `json:"type"`    // "accepted", "declined", "timed_out"
	ChatID string `json:"chat_id"`
}

// PublishMatchFound publishes match results to both users via NATS.
func PublishMatchFound(nats *messaging.NATSClient, chatID string, candidate *MatchCandidate) error {
	deadline := 15 // 15 seconds to accept/decline

	// Notify session A (partner = B).
	msgA := MatchResult{
		ChatID:          chatID,
		PartnerID:       candidate.SessionB,
		SharedInterests: candidate.SharedInterests,
		AcceptDeadline:  deadline,
	}
	dataA, err := json.Marshal(msgA)
	if err != nil {
		return fmt.Errorf("matching: marshal result for A: %w", err)
	}
	if err := nats.Publish(messaging.SubjectMatchFound+"."+candidate.SessionA, dataA); err != nil {
		return fmt.Errorf("matching: publish match.found for %s: %w", candidate.SessionA, err)
	}

	// Notify session B (partner = A).
	msgB := MatchResult{
		ChatID:          chatID,
		PartnerID:       candidate.SessionA,
		SharedInterests: candidate.SharedInterests,
		AcceptDeadline:  deadline,
	}
	dataB, err := json.Marshal(msgB)
	if err != nil {
		return fmt.Errorf("matching: marshal result for B: %w", err)
	}
	if err := nats.Publish(messaging.SubjectMatchFound+"."+candidate.SessionB, dataB); err != nil {
		return fmt.Errorf("matching: publish match.found for %s: %w", candidate.SessionB, err)
	}

	log.Printf("[matcher] match published: chat=%s a=%s b=%s shared=%v",
		chatID, candidate.SessionA, candidate.SessionB, candidate.SharedInterests)
	return nil
}
