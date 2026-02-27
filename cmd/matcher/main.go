package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/whisper/chat-app/internal/matching"
	"github.com/whisper/chat-app/internal/messaging"
)

func main() {
	log.Println("Starting Whisper matching service...")

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
	natsConfig.Name = "whisper-matcher"

	natsClient, err := messaging.NewNATSClient(natsConfig)
	if err != nil {
		log.Fatalf("failed to connect to NATS: %v", err)
	}

	// Start matching service.
	svc := matching.NewService(rdb, natsClient)
	if err := svc.Start(); err != nil {
		log.Fatalf("failed to start matching service: %v", err)
	}

	log.Printf("Whisper matching service running")
	log.Printf("  redis_addr: %s", redisAddr)
	log.Printf("  nats_url:   %s", natsConfig.URL)

	// Graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	log.Printf("received signal %v, shutting down...", sig)

	svc.Stop()
	natsClient.Close()
	rdb.Close()
}
