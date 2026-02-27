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

// runMatch implements the matching flow load test. It creates pairs of
// simulated users who connect, enter the matching queue, find each other,
// and accept the match. This test measures matching throughput and latency
// under concurrent load.
func runMatch(args []string) {
	fs := flag.NewFlagSet("match", flag.ExitOnError)
	url := fs.String("url", "ws://localhost:8080/ws", "WebSocket server URL")
	pairs := fs.Int("pairs", 500, "Number of user pairs to match")
	rampUp := fs.Duration("ramp", 10*time.Second, "Ramp-up duration for connection creation")
	matchTimeout := fs.Duration("match-timeout", 30*time.Second, "Timeout waiting for match_found")
	interests := fs.String("interests", "", "Comma-separated interest tags (empty = random matching)")
	concurrency := fs.Int("concurrency", 50, "Maximum simultaneous connection attempts during ramp-up")
	metricsURL := fs.String("metrics-url", "http://localhost:8080/metrics", "Prometheus metrics endpoint URL")
	scrapeInterval := fs.Duration("scrape-interval", 2*time.Second, "Interval between metrics scrapes")
	fs.Parse(args)

	totalClients := *pairs * 2

	fmt.Printf("Match test: %d pairs (%d clients) to %s (ramp=%s, match-timeout=%s, interests=%q, concurrency=%d)\n",
		*pairs, totalClients, *url, *rampUp, *matchTimeout, *interests, *concurrency)

	// Parse interest tags.
	var interestTags []string
	if *interests != "" {
		for _, tag := range strings.Split(*interests, ",") {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				interestTags = append(interestTags, tag)
			}
		}
	}
	if interestTags == nil {
		interestTags = []string{}
	}

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

	// Track whether ramp-up was interrupted so we can skip matching phase.
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
		fmt.Println("Interrupted — skipping matching phases.")
		cleanup(clients, &mu)
		scraper.Stop()
		collector.Report()
		return
	}

	// -----------------------------------------------------------------------
	// Phase 2 — Register handlers and send find_match from all clients
	// -----------------------------------------------------------------------
	fmt.Println("\n--- Phase 2: Start matching ---")

	// Counters for tracking match progress.
	var matchedCount atomic.Int64  // Number of clients that received match_found
	var acceptedCount atomic.Int64 // Number of clients that received match_accepted

	// WaitGroup for all client goroutines that handle match flow.
	var matchWg sync.WaitGroup

	mu.Lock()
	activeClients := make([]*client.Client, len(clients))
	copy(activeClients, clients)
	mu.Unlock()

	fmt.Printf("Sending find_match from %d clients (interests=%v)...\n", len(activeClients), interestTags)

	matchStart := time.Now()

	for _, c := range activeClients {
		c := c // capture loop variable
		matchWg.Add(1)

		// Per-client channel to signal when match_found is received.
		matchDone := make(chan struct{})
		// Per-client channel to signal when match_accepted is received.
		acceptDone := make(chan struct{})

		// Register match_found handler.
		c.On(client.TypeMatchFound, func(raw json.RawMessage) {
			latency := time.Since(matchStart)
			collector.AddMsgLatency(latency)
			matchedCount.Add(1)

			// Extract chat_id and send accept_match.
			var msg struct {
				ChatID string `json:"chat_id"`
			}
			if err := json.Unmarshal(raw, &msg); err == nil && msg.ChatID != "" {
				_ = c.Send(map[string]string{
					"type":    client.TypeAcceptMatch,
					"chat_id": msg.ChatID,
				})
			}

			close(matchDone)
		})

		// Register match_accepted handler.
		c.On(client.TypeMatchAccepted, func(raw json.RawMessage) {
			acceptedCount.Add(1)
			close(acceptDone)
		})

		// Per-client goroutine to enforce match timeout.
		go func() {
			defer matchWg.Done()

			timeoutTimer := time.NewTimer(*matchTimeout)
			defer timeoutTimer.Stop()

			// Wait for match_found or timeout.
			select {
			case <-matchDone:
				// Match found, now wait for match_accepted or timeout.
				select {
				case <-acceptDone:
					// Fully matched and accepted.
				case <-timeoutTimer.C:
					collector.AddError()
				case <-ctx.Done():
				}
			case <-timeoutTimer.C:
				collector.AddError()
			case <-ctx.Done():
			}
		}()

		// Send find_match.
		err := c.Send(map[string]interface{}{
			"type":      client.TypeFindMatch,
			"interests": interestTags,
		})
		if err != nil {
			collector.AddError()
		}
	}

	// -----------------------------------------------------------------------
	// Phase 3 & 4 — Wait for matches with progress reporting
	// -----------------------------------------------------------------------
	fmt.Println("\n--- Phase 3: Waiting for matches ---")

	// Progress reporting while waiting for matches.
	matchProgressStop := make(chan struct{})
	var matchProgressWg sync.WaitGroup
	matchProgressWg.Add(1)
	go func() {
		defer matchProgressWg.Done()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		lastMatched := int64(0)
		lastTime := time.Now()
		for {
			select {
			case <-ticker.C:
				now := time.Now()
				currentMatched := matchedCount.Load()
				currentAccepted := acceptedCount.Load()
				currentErrors := collector.ErrorCount()
				dt := now.Sub(lastTime).Seconds()
				matchRate := float64(currentMatched-lastMatched) / dt
				// Each pair = 2 clients matched, so pairs matched = accepted / 2
				pairsMatched := currentAccepted / 2
				fmt.Printf("  [match] pairs: %d/%d  matched: %d  accepted: %d  errors: %d  rate: %.1f match/s\n",
					pairsMatched, *pairs, currentMatched, currentAccepted, currentErrors, matchRate)
				lastMatched = currentMatched
				lastTime = now
			case <-matchProgressStop:
				return
			}
		}
	}()

	// Wait for all client goroutines to complete (match or timeout).
	allDone := make(chan struct{})
	go func() {
		matchWg.Wait()
		close(allDone)
	}()

	select {
	case <-allDone:
		// All clients finished.
	case <-ctx.Done():
		fmt.Println("\nInterrupted during matching phase.")
	}

	close(matchProgressStop)
	matchProgressWg.Wait()

	matchElapsed := time.Since(matchStart)

	// -----------------------------------------------------------------------
	// Final report
	// -----------------------------------------------------------------------
	finalMatched := matchedCount.Load()
	finalAccepted := acceptedCount.Load()
	successfulPairs := finalAccepted / 2

	fmt.Printf("\n--- Match Results ---\n")
	fmt.Printf("Successful pairs:  %d / %d\n", successfulPairs, *pairs)
	fmt.Printf("Clients matched:   %d / %d\n", finalMatched, len(activeClients))
	fmt.Printf("Clients accepted:  %d / %d\n", finalAccepted, len(activeClients))
	fmt.Printf("Match duration:    %s\n", matchElapsed.Round(time.Millisecond))
	if matchElapsed.Seconds() > 0 {
		fmt.Printf("Match throughput:  %.1f pairs/s\n", float64(successfulPairs)/matchElapsed.Seconds())
	}

	// -----------------------------------------------------------------------
	// Cleanup
	// -----------------------------------------------------------------------
	cleanup(clients, &mu)
	scraper.Stop()
	collector.Report()
}

// cleanup closes all client connections.
func cleanup(clients []*client.Client, mu *sync.Mutex) {
	fmt.Println("\n--- Cleanup ---")
	mu.Lock()
	total := len(clients)
	fmt.Printf("Closing %d connections...\n", total)
	for _, c := range clients {
		c.Close()
	}
	mu.Unlock()
	fmt.Println("All connections closed.")
}
