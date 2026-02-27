package matching

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/whisper/chat-app/internal/chat"
	"github.com/whisper/chat-app/internal/messaging"
)

const (
	matchInterval = 2 * time.Second
	tier1MaxWait  = 10 * time.Second // 0-10s: exact match only
	matchTimeout  = 30 * time.Second // 30s: give up
)

// MatchRequest is the NATS payload sent by wsserver when a user starts matching.
type MatchRequest struct {
	SessionID string   `json:"session_id"`
	Interests []string `json:"interests"`
}

// CancelRequest is the NATS payload sent by wsserver when a user cancels.
type CancelRequest struct {
	SessionID string `json:"session_id"`
}

// Service is the background matching service that pairs users based on
// shared interests using tiered algorithms.
type Service struct {
	queue     *Queue
	nats      *messaging.NATSClient
	rdb       *redis.Client
	chatStore *chat.Store
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewService creates a new matching service.
func NewService(rdb *redis.Client, nats *messaging.NATSClient) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{
		queue:     NewQueue(rdb),
		nats:      nats,
		rdb:       rdb,
		chatStore: chat.NewStore(rdb),
		ctx:       ctx,
		cancel:    cancel,
	}
}

// Start subscribes to NATS subjects and starts the matching loop.
func (s *Service) Start() error {
	if err := s.nats.SubscribeMatchRequest(s.handleMatchRequest); err != nil {
		return err
	}
	if err := s.nats.SubscribeMatchCancel(s.handleCancelRequest); err != nil {
		return err
	}

	go s.matchLoop()
	go StartCleanup(s.ctx, s.queue, s.rdb, s.nats)

	log.Println("[matcher] service started")
	return nil
}

// Stop gracefully shuts down the matching service.
func (s *Service) Stop() {
	s.cancel()
	log.Println("[matcher] service stopped")
}

func (s *Service) handleMatchRequest(data []byte) {
	var req MatchRequest
	if err := json.Unmarshal(data, &req); err != nil {
		log.Printf("[matcher] invalid match request: %v", err)
		return
	}

	if err := s.queue.Enqueue(s.ctx, req.SessionID, req.Interests); err != nil {
		log.Printf("[matcher] enqueue %s: %v", req.SessionID, err)
		return
	}

	size, _ := s.queue.QueueSize(s.ctx)
	log.Printf("[matcher] enqueued %s with interests %v (queue size: %d)",
		req.SessionID, req.Interests, size)
}

func (s *Service) handleCancelRequest(data []byte) {
	var req CancelRequest
	if err := json.Unmarshal(data, &req); err != nil {
		log.Printf("[matcher] invalid cancel request: %v", err)
		return
	}

	if err := s.queue.Dequeue(s.ctx, req.SessionID); err != nil {
		log.Printf("[matcher] dequeue %s: %v", req.SessionID, err)
		return
	}

	log.Printf("[matcher] dequeued %s (cancelled)", req.SessionID)
}

// matchLoop runs the core matching algorithm every 2 seconds.
func (s *Service) matchLoop() {
	ticker := time.NewTicker(matchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			log.Println("[matcher] match loop stopped")
			return
		case <-ticker.C:
			s.processQueue()
		}
	}
}

// processQueue iterates through all queued users and attempts to match them
// using tiered algorithms based on wait time.
func (s *Service) processQueue() {
	ctx := s.ctx
	sessionIDs, err := s.queue.GetAllQueued(ctx)
	if err != nil {
		log.Printf("[matcher] failed to get queue: %v", err)
		return
	}

	for _, sid := range sessionIDs {
		// Re-check: user may have been matched earlier in this cycle.
		queued, err := s.queue.IsQueued(ctx, sid)
		if err != nil || !queued {
			continue
		}

		entry, err := s.queue.GetEntry(ctx, sid)
		if err != nil || entry == nil {
			continue
		}

		waitMs := float64(time.Now().UnixMilli()) - entry.JoinedAt
		waitDuration := time.Duration(waitMs) * time.Millisecond

		// MATCH-6: 30s timeout — no match found, give up.
		if waitDuration >= matchTimeout {
			s.handleTimeout(ctx, sid)
			continue
		}

		var match *MatchCandidate

		// Tier 1: Exact match (always attempted).
		match, err = s.queue.TryExactMatch(ctx, sid)
		if err != nil {
			log.Printf("[matcher] exact match error for %s: %v", sid, err)
		}

		// Tier 2: Overlap match (after 10s wait).
		if match == nil && waitDuration >= tier1MaxWait {
			match, err = s.queue.TryOverlapMatch(ctx, sid)
			if err != nil {
				log.Printf("[matcher] overlap match error for %s: %v", sid, err)
			}
		}

		// Tier 3 & 4 deferred to MATCH-4.

		if match != nil {
			s.handleMatch(ctx, match)
		}
	}
}

func (s *Service) handleMatch(ctx context.Context, match *MatchCandidate) {
	chatID := uuid.New().String()

	// Remove both users from the queue.
	if err := s.queue.Dequeue(ctx, match.SessionA); err != nil {
		log.Printf("[matcher] dequeue %s: %v", match.SessionA, err)
	}
	if err := s.queue.Dequeue(ctx, match.SessionB); err != nil {
		log.Printf("[matcher] dequeue %s: %v", match.SessionB, err)
	}

	// Create pending chat session in Redis (CHAT-6).
	if err := s.chatStore.CreatePending(ctx, chatID, match.SessionA, match.SessionB); err != nil {
		log.Printf("[matcher] create pending chat: %v", err)
	}

	// Publish match result to both users via NATS.
	if err := PublishMatchFound(s.nats, chatID, match); err != nil {
		log.Printf("[matcher] publish match: %v", err)
	}
}

// handleTimeout removes a user from the queue and sends a timeout notification.
func (s *Service) handleTimeout(ctx context.Context, sessionID string) {
	if err := s.queue.Dequeue(ctx, sessionID); err != nil {
		log.Printf("[matcher] timeout dequeue %s: %v", sessionID, err)
	}

	// Send timeout via match.found with Timeout flag.
	msg := MatchResult{Timeout: true}
	data, _ := json.Marshal(msg)
	if err := s.nats.Publish(messaging.SubjectMatchFound+"."+sessionID, data); err != nil {
		log.Printf("[matcher] publish timeout for %s: %v", sessionID, err)
	}

	log.Printf("[matcher] timeout for %s (30s)", sessionID)
}
