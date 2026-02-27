package main

import (
	"context"
	"flag"
	"fmt"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/whisper/chat-app/loadtest/client"
	"github.com/whisper/chat-app/loadtest/stats"
)

// runSaturate implements the connection saturation test. It opens a specified
// number of WebSocket connections to the server, ramping up over a configurable
// duration, then holds them open for a hold period while monitoring server
// health. This test is designed to find the maximum connection capacity before
// the server starts rejecting or dropping connections.
func runSaturate(args []string) {
	fs := flag.NewFlagSet("saturate", flag.ExitOnError)
	url := fs.String("url", "ws://localhost:8080/ws", "WebSocket server URL")
	connections := fs.Int("connections", 1000, "Number of connections to open")
	rampUp := fs.Duration("ramp", 10*time.Second, "Ramp-up duration")
	hold := fs.Duration("hold", 30*time.Second, "Hold duration after all connections are open")
	concurrency := fs.Int("concurrency", 50, "Maximum simultaneous connection attempts during ramp-up")
	fs.Parse(args)

	fmt.Printf("Saturate test: %d connections to %s (ramp=%s, hold=%s, concurrency=%d)\n",
		*connections, *url, *rampUp, *hold, *concurrency)

	// Set up signal handling for graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	collector := stats.NewCollector()

	// Slice to track all open connections for cleanup.
	var mu sync.Mutex
	clients := make([]*client.Client, 0, *connections)

	// Track connection drops during the hold phase.
	var dropped atomic.Int64

	// Track whether ramp-up was interrupted so we can skip the hold phase.
	interrupted := false

	// -----------------------------------------------------------------------
	// Ramp-up phase
	// -----------------------------------------------------------------------
	fmt.Println("\n--- Ramp-up phase ---")

	// Calculate the interval between connection launches.
	interval := *rampUp / time.Duration(*connections)
	if interval <= 0 {
		interval = time.Millisecond
	}

	// Semaphore to bound concurrent connection attempts.
	sem := make(chan struct{}, *concurrency)
	var wg sync.WaitGroup

	// Progress reporting: every 1 second during ramp-up.
	progressStop := make(chan struct{})
	var progressWg sync.WaitGroup
	progressWg.Add(1)
	go func() {
		defer progressWg.Done()
		ticker := time.NewTicker(1 * time.Second)
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
				fmt.Printf("  [ramp] connections: %d/%d  errors: %d  rate: %.1f conn/s\n",
					currentConns, *connections, currentErrs, rate)
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
	for launched < *connections {
		select {
		case <-ctx.Done():
			fmt.Println("\nInterrupted during ramp-up.")
			interrupted = true
			launched = *connections // Break the loop.
		case <-rampTicker.C:
			launched++
			wg.Add(1)
			sem <- struct{}{} // Acquire semaphore slot.

			go func() {
				defer wg.Done()
				defer func() { <-sem }() // Release semaphore slot.

				// Create connection with a per-connection timeout.
				connCtx, connCancel := context.WithTimeout(ctx, 10*time.Second)
				defer connCancel()

				c, err := client.New(connCtx, *url)
				if err != nil {
					collector.AddError()
					return
				}

				// Wait for the session handshake to complete.
				if err := c.WaitForSession(connCtx); err != nil {
					collector.AddError()
					c.Close()
					return
				}

				// Record connect latency from client metrics.
				m := c.GetMetrics()
				collector.AddConnect(m.ConnectLatency)

				// Add to the tracked clients slice.
				mu.Lock()
				clients = append(clients, c)
				mu.Unlock()
			}()
		}
	}

	rampTicker.Stop()

	// Wait for all in-flight connection goroutines to finish.
	wg.Wait()

	// Stop the progress reporting goroutine.
	close(progressStop)
	progressWg.Wait()

	rampElapsed := time.Since(rampStart)
	fmt.Printf("\nRamp-up complete: %d/%d connections in %s (%d errors)\n",
		collector.ConnectionCount(), *connections,
		rampElapsed.Round(time.Millisecond), collector.ErrorCount())

	// -----------------------------------------------------------------------
	// Hold phase (skipped if ramp-up was interrupted)
	// -----------------------------------------------------------------------
	if !interrupted {
		fmt.Println("\n--- Hold phase ---")

		mu.Lock()
		initialAlive := len(clients)
		mu.Unlock()
		fmt.Printf("Holding %d connections for %s...\n", initialAlive, *hold)

		holdTimer := time.NewTimer(*hold)
		statusTicker := time.NewTicker(5 * time.Second)

	holdLoop:
		for {
			select {
			case <-ctx.Done():
				fmt.Println("\nInterrupted during hold phase.")
				break holdLoop
			case <-holdTimer.C:
				fmt.Println("\nHold period complete.")
				break holdLoop
			case <-statusTicker.C:
				// Count alive connections by checking for errors in metrics.
				mu.Lock()
				alive := 0
				for _, c := range clients {
					m := c.GetMetrics()
					if m.Errors == 0 {
						alive++
					}
				}
				mu.Unlock()
				droppedNow := int64(initialAlive - alive)
				dropped.Store(droppedNow)
				fmt.Printf("  [hold] alive: %d/%d  dropped: %d\n",
					alive, initialAlive, droppedNow)
			}
		}

		holdTimer.Stop()
		statusTicker.Stop()
	}

	// -----------------------------------------------------------------------
	// Cleanup
	// -----------------------------------------------------------------------
	fmt.Println("\n--- Cleanup ---")
	mu.Lock()
	total := len(clients)
	fmt.Printf("Closing %d connections...\n", total)
	for _, c := range clients {
		c.Close()
	}
	mu.Unlock()
	fmt.Println("All connections closed.")

	// -----------------------------------------------------------------------
	// Final report
	// -----------------------------------------------------------------------
	if d := dropped.Load(); d > 0 {
		fmt.Printf("\nConnections dropped during hold: %d\n", d)
	}
	collector.Report()
}
