package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/whisper/chat-app/internal/messaging"
	"github.com/whisper/chat-app/internal/moderation"
)

func main() {
	log.Println("Starting Whisper moderation service...")

	// Redis setup.
	redisAddr := "localhost:6379"
	if v := os.Getenv("REDIS_ADDR"); v != "" {
		redisAddr = v
	}

	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := rdb.Ping(ctx).Err(); err != nil {
		cancel()
		log.Fatalf("failed to connect to Redis: %v", err)
	}
	cancel()

	// NATS setup.
	natsConfig := messaging.DefaultNATSConfig()
	if v := os.Getenv("NATS_URL"); v != "" {
		natsConfig.URL = v
	}
	natsConfig.Name = "whisper-moderator"

	natsClient, err := messaging.NewNATSClient(natsConfig)
	if err != nil {
		log.Fatalf("failed to connect to NATS: %v", err)
	}

	// Initialize content filter.
	filter := moderation.NewFilter()

	// Subscribe to moderation check requests.
	err = natsClient.SubscribeModerationCheck(func(data []byte) {
		var req moderation.ModerationRequest
		if err := json.Unmarshal(data, &req); err != nil {
			log.Printf("[moderator] failed to unmarshal request: %v", err)
			return
		}

		result := filter.Check(req.Text)

		if result.Blocked {
			log.Printf("[moderator] FLAGGED session=%s chat=%s reason=%s term=%q",
				req.SessionID, req.ChatID, result.Reason, result.Term)

			resp := moderation.ModerationResult{
				SessionID: req.SessionID,
				ChatID:    req.ChatID,
				Blocked:   true,
				Reason:    result.Reason,
				Term:      result.Term,
			}
			respData, err := json.Marshal(resp)
			if err != nil {
				log.Printf("[moderator] failed to marshal result: %v", err)
				return
			}
			if err := natsClient.PublishModerationResult(req.SessionID, respData); err != nil {
				log.Printf("[moderator] failed to publish result: %v", err)
			}
		} else {
			log.Printf("[moderator] CLEAN session=%s chat=%s",
				req.SessionID, req.ChatID)
		}
	})
	if err != nil {
		log.Fatalf("failed to subscribe to moderation checks: %v", err)
	}

	log.Printf("Whisper moderation service running")
	log.Printf("  redis_addr: %s", redisAddr)
	log.Printf("  nats_url:   %s", natsConfig.URL)

	// Graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Printf("received signal %v, shutting down...", sig)

	natsClient.Close()
	rdb.Close()
}
