package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/whisper/chat-app/loadtest/client"
	"github.com/whisper/chat-app/loadtest/stats"
)

// pairResult tracks the outcome of a single chat pair's lifecycle.
type pairResult struct {
	matched       bool
	chatStarted   bool
	msgSent       int64
	msgRecv       int64
	endedCleanly  bool
	matchLatency  time.Duration
}

// runChat implements the full chat lifecycle load test. Each simulated user
// pair goes through the complete flow: connect -> set_fingerprint ->
// find_match -> accept_match -> exchange messages -> end_chat. This test
// measures end-to-end latency and throughput for the entire chat experience.
func runChat(args []string) {
	fs := flag.NewFlagSet("chat", flag.ExitOnError)
	url := fs.String("url", "ws://localhost:8080/ws", "WebSocket server URL")
	pairs := fs.Int("pairs", 100, "Number of user pairs for full chat lifecycle")
	rampUp := fs.Duration("ramp", 10*time.Second, "Ramp-up duration for connection creation")
	chatDuration := fs.Duration("chat-duration", 30*time.Second, "How long each pair chats")
	msgInterval := fs.Duration("msg-interval", 2*time.Second, "Interval between messages per user")
	msgSize := fs.Int("msg-size", 128, "Size of each message payload in bytes")
	concurrency := fs.Int("concurrency", 50, "Maximum simultaneous connection attempts during ramp-up")
	matchTimeout := fs.Duration("match-timeout", 30*time.Second, "Timeout waiting for match completion")
	metricsURL := fs.String("metrics-url", "http://localhost:8080/metrics", "Prometheus metrics endpoint URL")
	scrapeInterval := fs.Duration("scrape-interval", 2*time.Second, "Interval between metrics scrapes")
	fs.Parse(args)

	totalClients := *pairs * 2

	fmt.Printf("Chat test: %d pairs (%d clients) to %s (ramp=%s, chat=%s, interval=%s, msg-size=%d, concurrency=%d)\n",
		*pairs, totalClients, *url, *rampUp, *chatDuration, *msgInterval, *msgSize, *concurrency)

	// Set up signal handling for graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	collector := stats.NewCollector()

	// Set up metrics scraper.
	scraper := stats.NewScraper(*metricsURL, *scrapeInterval)
	collector.SetScraper(scraper)
	scraper.Start(ctx)

	// Slice to track all open connections for cleanup.
	var mu sync.Mutex
	clients := make([]*client.Client, 0, totalClients)

	// Track whether ramp-up was interrupted so we can skip later phases.
	interrupted := false

	// -----------------------------------------------------------------------
	// Phase 1 — Connect all users
	// -----------------------------------------------------------------------
	fmt.Println("\n--- Phase 1: Connect all users ---")

	interval := *rampUp / time.Duration(totalClients)
	if interval <= 0 {
		interval = time.Millisecond
	}

	// Semaphore to bound concurrent connection attempts.
	sem := make(chan struct{}, *concurrency)
	var wg sync.WaitGroup

	// Progress reporting: every 2 seconds during ramp-up.
	progressStop := make(chan struct{})
	var progressWg sync.WaitGroup
	progressWg.Add(1)
	go func() {
		defer progressWg.Done()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		lastCount := 0
		lastTime := time.Now()
		for {
			select {
			case <-ticker.C:
				now := time.Now()
				currentConns := collector.ConnectionCount()
				currentErrs := collector.ErrorCount()
				dt := now.Sub(lastTime).Seconds()
				rate := float64(currentConns-lastCount) / dt
				fmt.Printf("  [connect] connections: %d/%d  errors: %d  rate: %.1f conn/s\n",
					currentConns, totalClients, currentErrs, rate)
				lastCount = currentConns
				lastTime = now
			case <-progressStop:
				return
			}
		}
	}()

	rampStart := time.Now()
	rampTicker := time.NewTicker(interval)

	launched := 0
	for launched < totalClients {
		select {
		case <-ctx.Done():
			fmt.Println("\nInterrupted during connection phase.")
			interrupted = true
			launched = totalClients // Break the loop.
		case <-rampTicker.C:
			launched++
			wg.Add(1)
			sem <- struct{}{} // Acquire semaphore slot.

			go func() {
				defer wg.Done()
				defer func() { <-sem }() // Release semaphore slot.

				connCtx, connCancel := context.WithTimeout(ctx, 10*time.Second)
				defer connCancel()

				c, err := client.New(connCtx, *url)
				if err != nil {
					collector.AddError()
					return
				}

				if err := c.WaitForSession(connCtx); err != nil {
					collector.AddError()
					c.Close()
					return
				}

				m := c.GetMetrics()
				collector.AddConnect(m.ConnectLatency)

				mu.Lock()
				clients = append(clients, c)
				mu.Unlock()
			}()
		}
	}

	rampTicker.Stop()
	wg.Wait()
	close(progressStop)
	progressWg.Wait()

	rampElapsed := time.Since(rampStart)
	mu.Lock()
	connectedCount := len(clients)
	mu.Unlock()
	fmt.Printf("\nPhase 1 complete: %d/%d connections in %s (%d errors)\n",
		connectedCount, totalClients,
		rampElapsed.Round(time.Millisecond), collector.ErrorCount())

	if interrupted {
		fmt.Println("Interrupted — skipping chat phases.")
		cleanup(clients, &mu)
		scraper.Stop()
		collector.Report()
		return
	}

	// We need an even number of clients to form pairs. Drop any extra.
	mu.Lock()
	if len(clients)%2 != 0 {
		extra := clients[len(clients)-1]
		clients = clients[:len(clients)-1]
		extra.Close()
	}
	actualPairs := len(clients) / 2
	mu.Unlock()

	if actualPairs == 0 {
		fmt.Println("No pairs could be formed — not enough connections.")
		cleanup(clients, &mu)
		scraper.Stop()
		collector.Report()
		return
	}

	// -----------------------------------------------------------------------
	// Phase 2 + 3 + 4 — Match, Chat, End (per pair)
	// -----------------------------------------------------------------------
	fmt.Printf("\n--- Phase 2-4: Running %d chat pairs ---\n", actualPairs)

	// Global atomic counters for progress reporting.
	var totalMsgSent atomic.Int64
	var totalMsgRecv atomic.Int64
	var activePairCount atomic.Int64
	var completedPairs atomic.Int64
	var errorCount atomic.Int64

	// Collect results from each pair.
	results := make([]pairResult, actualPairs)

	// WaitGroup for all pair goroutines.
	var pairWg sync.WaitGroup

	// Generate message payload once (reused by all pairs).
	msgPayload := strings.Repeat("abcdefgh", (*msgSize/8)+1)
	msgPayload = msgPayload[:*msgSize]

	// Progress reporting every 5 seconds.
	chatProgressStop := make(chan struct{})
	var chatProgressWg sync.WaitGroup
	chatProgressWg.Add(1)
	go func() {
		defer chatProgressWg.Done()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				active := activePairCount.Load()
				completed := completedPairs.Load()
				sent := totalMsgSent.Load()
				recv := totalMsgRecv.Load()
				errs := errorCount.Load()
				fmt.Printf("  [chat] active: %d  completed: %d/%d  sent: %d  recv: %d  errors: %d\n",
					active, completed, actualPairs, sent, recv, errs)
			case <-chatProgressStop:
				return
			}
		}
	}()

	chatStart := time.Now()

	mu.Lock()
	pairedClients := make([]*client.Client, len(clients))
	copy(pairedClients, clients)
	mu.Unlock()

	for i := 0; i < actualPairs; i++ {
		i := i // capture loop variable
		c1 := pairedClients[i*2]
		c2 := pairedClients[i*2+1]

		pairWg.Add(1)
		go func() {
			defer pairWg.Done()

			// Stagger find_match sends by 100ms between pairs to avoid
			// overwhelming the matcher.
			stagger := time.Duration(i) * 100 * time.Millisecond
			select {
			case <-time.After(stagger):
			case <-ctx.Done():
				return
			}

			runPair(ctx, c1, c2, *chatDuration, *msgInterval, *matchTimeout,
				msgPayload, collector, &results[i],
				&totalMsgSent, &totalMsgRecv, &activePairCount, &completedPairs, &errorCount)
		}()
	}

	// Wait for all pairs to complete.
	allDone := make(chan struct{})
	go func() {
		pairWg.Wait()
		close(allDone)
	}()

	select {
	case <-allDone:
		// All pairs finished.
	case <-ctx.Done():
		fmt.Println("\nInterrupted — waiting for pairs to wind down...")
		<-allDone
	}

	close(chatProgressStop)
	chatProgressWg.Wait()

	chatElapsed := time.Since(chatStart)

	// -----------------------------------------------------------------------
	// Final report
	// -----------------------------------------------------------------------
	var successfulChats int
	var totalSent, totalRecv int64
	var totalMatchLatency time.Duration
	matchedCount := 0

	for _, r := range results {
		if r.endedCleanly {
			successfulChats++
		}
		totalSent += r.msgSent
		totalRecv += r.msgRecv
		if r.matched {
			matchedCount++
			totalMatchLatency += r.matchLatency
		}
	}

	fmt.Printf("\n--- Chat Results ---\n")
	fmt.Printf("Successful chats:  %d / %d\n", successfulChats, actualPairs)
	fmt.Printf("Pairs matched:     %d / %d\n", matchedCount, actualPairs)
	fmt.Printf("Total msg sent:    %d\n", totalSent)
	fmt.Printf("Total msg recv:    %d\n", totalRecv)
	fmt.Printf("Chat duration:     %s\n", chatElapsed.Round(time.Millisecond))
	if matchedCount > 0 {
		avgMatch := totalMatchLatency / time.Duration(matchedCount)
		fmt.Printf("Avg match latency: %s\n", avgMatch.Round(time.Millisecond))
	}
	if chatElapsed.Seconds() > 0 && totalSent > 0 {
		fmt.Printf("Msg throughput:    %.1f msg/s\n", float64(totalSent)/chatElapsed.Seconds())
	}

	// -----------------------------------------------------------------------
	// Cleanup
	// -----------------------------------------------------------------------
	cleanup(clients, &mu)
	scraper.Stop()
	collector.Report()
}

// runPair executes the full chat lifecycle for a pair of clients:
// find_match -> accept_match -> exchange messages -> end_chat.
// It returns after the chat ends or the context is cancelled.
func runPair(
	ctx context.Context,
	c1, c2 *client.Client,
	chatDuration, msgInterval, matchTimeout time.Duration,
	msgPayload string,
	collector *stats.Collector,
	result *pairResult,
	totalMsgSent, totalMsgRecv, activePairCount, completedPairs, errorCount *atomic.Int64,
) {
	defer completedPairs.Add(1)

	// --- Phase 2: Matching ---

	// Channels to coordinate the matching flow. Carries chat_id.
	c1MatchFound := make(chan string, 1)
	c2MatchFound := make(chan string, 1)
	c1Accepted := make(chan struct{}, 1)
	c2Accepted := make(chan struct{}, 1)

	// Channels for message reception during chat phase.
	c1MsgRecv := make(chan struct{}, 100)
	c2MsgRecv := make(chan struct{}, 100)

	// Channels for partner_left notification.
	c1PartnerLeft := make(chan struct{}, 1)
	c2PartnerLeft := make(chan struct{}, 1)

	// Register match_found handler for c1.
	c1.On(client.TypeMatchFound, func(raw json.RawMessage) {
		var msg struct {
			ChatID string `json:"chat_id"`
		}
		if err := json.Unmarshal(raw, &msg); err == nil && msg.ChatID != "" {
			select {
			case c1MatchFound <- msg.ChatID:
			default:
			}
		}
	})

	// Register match_found handler for c2.
	c2.On(client.TypeMatchFound, func(raw json.RawMessage) {
		var msg struct {
			ChatID string `json:"chat_id"`
		}
		if err := json.Unmarshal(raw, &msg); err == nil && msg.ChatID != "" {
			select {
			case c2MatchFound <- msg.ChatID:
			default:
			}
		}
	})

	// Register match_accepted handler for c1.
	c1.On(client.TypeMatchAccepted, func(raw json.RawMessage) {
		select {
		case c1Accepted <- struct{}{}:
		default:
		}
	})

	// Register match_accepted handler for c2.
	c2.On(client.TypeMatchAccepted, func(raw json.RawMessage) {
		select {
		case c2Accepted <- struct{}{}:
		default:
		}
	})

	// Register message handler for c1.
	c1.On(client.TypeMessage, func(raw json.RawMessage) {
		totalMsgRecv.Add(1)
		select {
		case c1MsgRecv <- struct{}{}:
		default:
		}
	})

	// Register message handler for c2.
	c2.On(client.TypeMessage, func(raw json.RawMessage) {
		totalMsgRecv.Add(1)
		select {
		case c2MsgRecv <- struct{}{}:
		default:
		}
	})

	// Register partner_left handler for c1.
	c1.On(client.TypePartnerLeft, func(raw json.RawMessage) {
		select {
		case c1PartnerLeft <- struct{}{}:
		default:
		}
	})

	// Register partner_left handler for c2.
	c2.On(client.TypePartnerLeft, func(raw json.RawMessage) {
		select {
		case c2PartnerLeft <- struct{}{}:
		default:
		}
	})

	// Both send find_match.
	matchStart := time.Now()

	if err := c1.Send(map[string]interface{}{
		"type":      client.TypeFindMatch,
		"interests": []string{},
	}); err != nil {
		errorCount.Add(1)
		collector.AddError()
		return
	}

	if err := c2.Send(map[string]interface{}{
		"type":      client.TypeFindMatch,
		"interests": []string{},
	}); err != nil {
		errorCount.Add(1)
		collector.AddError()
		return
	}

	// Wait for match_found on both clients.
	matchCtx, matchCancel := context.WithTimeout(ctx, matchTimeout)
	defer matchCancel()

	var chatID1, chatID2 string

	// Wait for c1 match_found.
	select {
	case chatID1 = <-c1MatchFound:
	case <-matchCtx.Done():
		errorCount.Add(1)
		collector.AddError()
		return
	}

	// Wait for c2 match_found.
	select {
	case chatID2 = <-c2MatchFound:
	case <-matchCtx.Done():
		errorCount.Add(1)
		collector.AddError()
		return
	}

	// Both send accept_match.
	if err := c1.Send(map[string]string{
		"type":    client.TypeAcceptMatch,
		"chat_id": chatID1,
	}); err != nil {
		errorCount.Add(1)
		collector.AddError()
		return
	}

	if err := c2.Send(map[string]string{
		"type":    client.TypeAcceptMatch,
		"chat_id": chatID2,
	}); err != nil {
		errorCount.Add(1)
		collector.AddError()
		return
	}

	// Wait for match_accepted on both clients.
	select {
	case <-c1Accepted:
	case <-matchCtx.Done():
		errorCount.Add(1)
		collector.AddError()
		return
	}

	select {
	case <-c2Accepted:
	case <-matchCtx.Done():
		errorCount.Add(1)
		collector.AddError()
		return
	}

	matchLatency := time.Since(matchStart)
	result.matched = true
	result.matchLatency = matchLatency
	collector.AddMsgLatency(matchLatency)

	// --- Phase 3: Chat ---

	activePairCount.Add(1)
	defer activePairCount.Add(-1)
	result.chatStarted = true

	chatCtx, chatCancel := context.WithTimeout(ctx, chatDuration)
	defer chatCancel()

	// Each client sends messages on its own ticker. We track approximate
	// message latency by recording the time of the last send and measuring
	// until the next receive on the same client.
	var c1LastSend atomic.Int64 // unix nanoseconds of last send
	var c2LastSend atomic.Int64 // unix nanoseconds of last send

	// Goroutine for c1 sending messages.
	var chatWg sync.WaitGroup
	chatWg.Add(2)

	go func() {
		defer chatWg.Done()
		ticker := time.NewTicker(msgInterval)
		defer ticker.Stop()

		for {
			select {
			case <-chatCtx.Done():
				return
			case <-ticker.C:
				c1LastSend.Store(time.Now().UnixNano())
				if err := c1.Send(map[string]string{
					"type":    client.TypeMessage,
					"chat_id": chatID1,
					"text":    msgPayload,
				}); err != nil {
					errorCount.Add(1)
					collector.AddError()
					return
				}
				totalMsgSent.Add(1)
				result.msgSent++
			}
		}
	}()

	// Goroutine for c2 sending messages.
	go func() {
		defer chatWg.Done()
		ticker := time.NewTicker(msgInterval)
		defer ticker.Stop()

		for {
			select {
			case <-chatCtx.Done():
				return
			case <-ticker.C:
				c2LastSend.Store(time.Now().UnixNano())
				if err := c2.Send(map[string]string{
					"type":    client.TypeMessage,
					"chat_id": chatID2,
					"text":    msgPayload,
				}); err != nil {
					errorCount.Add(1)
					collector.AddError()
					return
				}
				totalMsgSent.Add(1)
				result.msgSent++
			}
		}
	}()

	// Goroutine for c1 receiving messages and recording latency.
	chatWg.Add(2)
	go func() {
		defer chatWg.Done()
		for {
			select {
			case <-chatCtx.Done():
				return
			case <-c1MsgRecv:
				result.msgRecv++
				// Approximate latency: time since c1's last send.
				if ts := c1LastSend.Load(); ts > 0 {
					latency := time.Since(time.Unix(0, ts))
					collector.AddMsgLatency(latency)
				}
			}
		}
	}()

	// Goroutine for c2 receiving messages and recording latency.
	go func() {
		defer chatWg.Done()
		for {
			select {
			case <-chatCtx.Done():
				return
			case <-c2MsgRecv:
				result.msgRecv++
				// Approximate latency: time since c2's last send.
				if ts := c2LastSend.Load(); ts > 0 {
					latency := time.Since(time.Unix(0, ts))
					collector.AddMsgLatency(latency)
				}
			}
		}
	}()

	// Wait for the chat duration to expire.
	chatWg.Wait()

	// --- Phase 4: End Chat ---

	// c1 sends end_chat.
	if err := c1.Send(map[string]string{
		"type":    client.TypeEndChat,
		"chat_id": chatID1,
	}); err != nil {
		errorCount.Add(1)
		collector.AddError()
		return
	}

	// Wait for c2 to receive partner_left (with a short timeout).
	endCtx, endCancel := context.WithTimeout(ctx, 5*time.Second)
	defer endCancel()

	select {
	case <-c2PartnerLeft:
		result.endedCleanly = true
	case <-c1PartnerLeft:
		// c1 got partner_left instead — still counts as ended.
		result.endedCleanly = true
	case <-endCtx.Done():
		errorCount.Add(1)
		collector.AddError()
	}
}
