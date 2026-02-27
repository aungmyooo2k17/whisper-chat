package matching

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/whisper/chat-app/internal/messaging"
)

const cleanupInterval = 5 * time.Second

// StartCleanup runs background loops that remove stale entries from the
// matching queue and expire pending chat sessions that exceeded their
// accept deadline.
func StartCleanup(ctx context.Context, queue *Queue, rdb *redis.Client, nats *messaging.NATSClient) {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[matcher] cleanup loop stopped")
			return
		case <-ticker.C:
			cleanStaleEntries(ctx, queue, rdb)
			cleanExpiredPendingChats(ctx, rdb, nats)
		}
	}
}

// cleanStaleEntries removes users from the match queue whose sessions
// no longer exist in Redis (disconnected or expired).
func cleanStaleEntries(ctx context.Context, queue *Queue, rdb *redis.Client) {
	sessionIDs, err := queue.GetAllQueued(ctx)
	if err != nil {
		log.Printf("[matcher] cleanup: failed to get queue: %v", err)
		return
	}

	removed := 0
	for _, sid := range sessionIDs {
		exists, err := rdb.Exists(ctx, "session:"+sid).Result()
		if err != nil {
			continue
		}
		if exists == 0 {
			if err := queue.Dequeue(ctx, sid); err != nil {
				log.Printf("[matcher] cleanup: failed to dequeue %s: %v", sid, err)
			} else {
				removed++
			}
		}
	}

	if removed > 0 {
		log.Printf("[matcher] cleanup: removed %d stale entries", removed)
	}
}

// cleanExpiredPendingChats removes chat sessions that exceeded the 15s
// accept deadline without both users accepting. Notifies both users.
func cleanExpiredPendingChats(ctx context.Context, rdb *redis.Client, nats *messaging.NATSClient) {
	now := float64(time.Now().Unix())

	chatIDs, err := rdb.ZRangeByScore(ctx, "match:pending_chats", &redis.ZRangeBy{
		Min: "0",
		Max: fmt.Sprintf("%.0f", now),
	}).Result()
	if err != nil {
		return
	}

	for _, chatID := range chatIDs {
		status, _ := rdb.HGet(ctx, "chat:"+chatID, "status").Result()
		if status == "pending_accept" {
			userA, _ := rdb.HGet(ctx, "chat:"+chatID, "user_a").Result()
			userB, _ := rdb.HGet(ctx, "chat:"+chatID, "user_b").Result()

			// Notify both users the accept deadline expired.
			notif, _ := json.Marshal(MatchNotification{Type: "timed_out", ChatID: chatID})
			if userA != "" {
				nats.PublishMatchNotify(userA, notif)
			}
			if userB != "" {
				nats.PublishMatchNotify(userB, notif)
			}

			// Delete the chat session.
			rdb.Del(ctx, "chat:"+chatID)
			log.Printf("[matcher] accept deadline expired for chat=%s", chatID)
		}

		// Remove from pending tracking set.
		rdb.ZRem(ctx, "match:pending_chats", chatID)
	}
}
