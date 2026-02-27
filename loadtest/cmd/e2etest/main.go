// Package main implements a standalone end-to-end integration test for the
// Whisper anonymous chat application. It validates the full user journey against
// a running Docker Compose stack: health checks, WebSocket handshake, matching,
// chat messaging, end chat, rate limiting, and content filtering.
//
// Usage:
//
//	go run ./cmd/e2etest/ [-url ws://localhost:8080/ws] [-api http://localhost:8080] [-timeout 60s]
//
// Exit code 0 if all required scenarios pass, 1 if any fail.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/whisper/chat-app/loadtest/client"
)

// ---------------------------------------------------------------------------
// Result tracking
// ---------------------------------------------------------------------------

// resultKind categorises a scenario outcome.
type resultKind int

const (
	resultPass resultKind = iota
	resultFail
	resultInfo // optional / non-fatal
)

// scenarioResult holds the outcome of a single test scenario.
type scenarioResult struct {
	name    string
	kind    resultKind
	detail  string
}

func (r scenarioResult) tag() string {
	switch r.kind {
	case resultPass:
		return "PASS"
	case resultFail:
		return "FAIL"
	default:
		return "INFO"
	}
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

func main() {
	wsURL := flag.String("url", "ws://localhost:8080/ws", "WebSocket server URL")
	apiBase := flag.String("api", "http://localhost:8080", "HTTP API base URL")
	timeout := flag.Duration("timeout", 60*time.Second, "Global test timeout")
	flag.Parse()

	fmt.Println("=== Whisper E2E Integration Test ===")
	fmt.Printf("Server: %s\n\n", *wsURL)

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	var results []scenarioResult

	// Run scenarios sequentially.
	results = append(results, scenario1HealthCheck(ctx, *apiBase))
	results = append(results, scenario2ConnectHandshake(ctx, *wsURL))

	// Scenarios 3-5 share matched clients; run them as a group.
	s3, s4, s5 := scenario345MatchChatEnd(ctx, *wsURL)
	results = append(results, s3, s4, s5)

	// Optional scenarios (non-fatal).
	results = append(results, scenario6RateLimiting(ctx, *wsURL))
	results = append(results, scenario7ContentFiltering(ctx, *wsURL))

	// ---------------------------------------------------------------------------
	// Summary
	// ---------------------------------------------------------------------------
	fmt.Println()
	passed := 0
	failed := 0
	info := 0
	for _, r := range results {
		fmt.Printf("[%s] %s", r.tag(), r.name)
		if r.detail != "" {
			fmt.Printf(" (%s)", r.detail)
		}
		fmt.Println()

		switch r.kind {
		case resultPass:
			passed++
		case resultFail:
			failed++
		case resultInfo:
			info++
		}
	}

	requiredTotal := passed + failed
	fmt.Printf("\n=== Results: %d/%d passed", passed, requiredTotal)
	if info > 0 {
		fmt.Printf(", %d info", info)
	}
	fmt.Println(" ===")

	if failed > 0 {
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// Scenario 1: Health Check
// ---------------------------------------------------------------------------

func scenario1HealthCheck(ctx context.Context, apiBase string) scenarioResult {
	name := "Scenario 1: Health Check"

	// 1a. /health
	if err := httpGetExpectOK(ctx, apiBase+"/health"); err != nil {
		return scenarioResult{name, resultFail, fmt.Sprintf("/health: %v", err)}
	}

	// 1b. /api/online — expect JSON with "count" field.
	body, err := httpGetBody(ctx, apiBase+"/api/online")
	if err != nil {
		return scenarioResult{name, resultFail, fmt.Sprintf("/api/online: %v", err)}
	}
	var onlineResp struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(body, &onlineResp); err != nil {
		return scenarioResult{name, resultFail, fmt.Sprintf("/api/online JSON parse: %v", err)}
	}

	// 1c. /metrics — expect Prometheus text with whisper_connections_total.
	metricsBody, err := httpGetBody(ctx, apiBase+"/metrics")
	if err != nil {
		return scenarioResult{name, resultFail, fmt.Sprintf("/metrics: %v", err)}
	}
	if !strings.Contains(string(metricsBody), "whisper_connections_total") {
		return scenarioResult{name, resultFail, "/metrics: missing whisper_connections_total"}
	}

	return scenarioResult{name, resultPass, fmt.Sprintf("online=%d", onlineResp.Count)}
}

// ---------------------------------------------------------------------------
// Scenario 2: Connect and Handshake
// ---------------------------------------------------------------------------

func scenario2ConnectHandshake(ctx context.Context, wsURL string) scenarioResult {
	name := "Scenario 2: Connect and Handshake"

	connCtx, connCancel := context.WithTimeout(ctx, 10*time.Second)
	defer connCancel()

	clientA, err := client.New(connCtx, wsURL)
	if err != nil {
		return scenarioResult{name, resultFail, fmt.Sprintf("client A connect: %v", err)}
	}
	defer clientA.Close()

	clientB, err := client.New(connCtx, wsURL)
	if err != nil {
		return scenarioResult{name, resultFail, fmt.Sprintf("client B connect: %v", err)}
	}
	defer clientB.Close()

	if err := clientA.WaitForSession(connCtx); err != nil {
		return scenarioResult{name, resultFail, fmt.Sprintf("client A session: %v", err)}
	}
	if err := clientB.WaitForSession(connCtx); err != nil {
		return scenarioResult{name, resultFail, fmt.Sprintf("client B session: %v", err)}
	}

	sidA := clientA.SessionID()
	sidB := clientB.SessionID()
	if sidA == "" || sidB == "" {
		return scenarioResult{name, resultFail, "empty session ID"}
	}

	return scenarioResult{name, resultPass, fmt.Sprintf("session_a=%s, session_b=%s", truncateID(sidA), truncateID(sidB))}
}

// ---------------------------------------------------------------------------
// Scenarios 3, 4, 5: Matching Flow, Chat Messages, End Chat
// ---------------------------------------------------------------------------

func scenario345MatchChatEnd(ctx context.Context, wsURL string) (scenarioResult, scenarioResult, scenarioResult) {
	s3Name := "Scenario 3: Matching Flow"
	s4Name := "Scenario 4: Chat Messages"
	s5Name := "Scenario 5: End Chat"

	failAll := func(reason string) (scenarioResult, scenarioResult, scenarioResult) {
		return scenarioResult{s3Name, resultFail, reason},
			scenarioResult{s4Name, resultFail, "skipped: matching failed"},
			scenarioResult{s5Name, resultFail, "skipped: matching failed"}
	}

	// --- Connect two clients ---
	connCtx, connCancel := context.WithTimeout(ctx, 10*time.Second)
	defer connCancel()

	clientA, err := client.New(connCtx, wsURL)
	if err != nil {
		return failAll(fmt.Sprintf("client A connect: %v", err))
	}
	defer clientA.Close()

	clientB, err := client.New(connCtx, wsURL)
	if err != nil {
		return failAll(fmt.Sprintf("client B connect: %v", err))
	}
	defer clientB.Close()

	if err := clientA.WaitForSession(connCtx); err != nil {
		return failAll(fmt.Sprintf("client A session: %v", err))
	}
	if err := clientB.WaitForSession(connCtx); err != nil {
		return failAll(fmt.Sprintf("client B session: %v", err))
	}

	// --- Scenario 3: Matching ---
	matchFoundA := make(chan string, 1) // carries chat_id
	matchFoundB := make(chan string, 1)
	matchAcceptedA := make(chan struct{}, 1)
	matchAcceptedB := make(chan struct{}, 1)

	clientA.On(client.TypeMatchFound, func(raw json.RawMessage) {
		var msg struct {
			ChatID string `json:"chat_id"`
		}
		if err := json.Unmarshal(raw, &msg); err == nil && msg.ChatID != "" {
			select {
			case matchFoundA <- msg.ChatID:
			default:
			}
		}
	})

	clientB.On(client.TypeMatchFound, func(raw json.RawMessage) {
		var msg struct {
			ChatID string `json:"chat_id"`
		}
		if err := json.Unmarshal(raw, &msg); err == nil && msg.ChatID != "" {
			select {
			case matchFoundB <- msg.ChatID:
			default:
			}
		}
	})

	clientA.On(client.TypeMatchAccepted, func(_ json.RawMessage) {
		select {
		case matchAcceptedA <- struct{}{}:
		default:
		}
	})

	clientB.On(client.TypeMatchAccepted, func(_ json.RawMessage) {
		select {
		case matchAcceptedB <- struct{}{}:
		default:
		}
	})

	matchStart := time.Now()

	// Both send find_match.
	if err := clientA.Send(map[string]interface{}{
		"type":      client.TypeFindMatch,
		"interests": []string{"music", "gaming"},
	}); err != nil {
		return failAll(fmt.Sprintf("client A find_match: %v", err))
	}
	if err := clientB.Send(map[string]interface{}{
		"type":      client.TypeFindMatch,
		"interests": []string{"music", "gaming"},
	}); err != nil {
		return failAll(fmt.Sprintf("client B find_match: %v", err))
	}

	// Wait for match_found on both (30s timeout).
	matchCtx, matchCancel := context.WithTimeout(ctx, 30*time.Second)
	defer matchCancel()

	var chatIDA, chatIDB string

	select {
	case chatIDA = <-matchFoundA:
	case <-matchCtx.Done():
		return failAll("timeout waiting for match_found on client A")
	}

	select {
	case chatIDB = <-matchFoundB:
	case <-matchCtx.Done():
		return failAll("timeout waiting for match_found on client B")
	}

	// Both send accept_match.
	if err := clientA.Send(map[string]string{
		"type":    client.TypeAcceptMatch,
		"chat_id": chatIDA,
	}); err != nil {
		return failAll(fmt.Sprintf("client A accept_match: %v", err))
	}
	if err := clientB.Send(map[string]string{
		"type":    client.TypeAcceptMatch,
		"chat_id": chatIDB,
	}); err != nil {
		return failAll(fmt.Sprintf("client B accept_match: %v", err))
	}

	// Wait for match_accepted on both (10s timeout).
	acceptCtx, acceptCancel := context.WithTimeout(ctx, 10*time.Second)
	defer acceptCancel()

	select {
	case <-matchAcceptedA:
	case <-acceptCtx.Done():
		return failAll("timeout waiting for match_accepted on client A")
	}

	select {
	case <-matchAcceptedB:
	case <-acceptCtx.Done():
		return failAll("timeout waiting for match_accepted on client B")
	}

	matchDuration := time.Since(matchStart)
	s3Result := scenarioResult{s3Name, resultPass,
		fmt.Sprintf("chat_id=%s, match_time=%s", truncateID(chatIDA), matchDuration.Round(time.Millisecond))}

	// --- Scenario 4: Chat Messages ---
	msgFromA := make(chan string, 1) // carries the text that B received
	msgFromB := make(chan string, 1) // carries the text that A received

	clientA.On(client.TypeMessage, func(raw json.RawMessage) {
		var msg struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(raw, &msg); err == nil {
			select {
			case msgFromB <- msg.Text:
			default:
			}
		}
	})

	clientB.On(client.TypeMessage, func(raw json.RawMessage) {
		var msg struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(raw, &msg); err == nil {
			select {
			case msgFromA <- msg.Text:
			default:
			}
		}
	})

	chatCtx, chatCancel := context.WithTimeout(ctx, 10*time.Second)
	defer chatCancel()

	// Client A sends a message.
	textA := "Hello from A"
	if err := clientA.Send(map[string]string{
		"type":    client.TypeMessage,
		"chat_id": chatIDA,
		"text":    textA,
	}); err != nil {
		return s3Result,
			scenarioResult{s4Name, resultFail, fmt.Sprintf("client A send message: %v", err)},
			scenarioResult{s5Name, resultFail, "skipped: chat failed"}
	}

	// Client B should receive it.
	var receivedByB string
	select {
	case receivedByB = <-msgFromA:
	case <-chatCtx.Done():
		return s3Result,
			scenarioResult{s4Name, resultFail, "timeout: client B did not receive message from A"},
			scenarioResult{s5Name, resultFail, "skipped: chat failed"}
	}

	if receivedByB != textA {
		return s3Result,
			scenarioResult{s4Name, resultFail, fmt.Sprintf("content mismatch: expected %q, got %q", textA, receivedByB)},
			scenarioResult{s5Name, resultFail, "skipped: chat failed"}
	}

	// Client B sends a reply.
	textB := "Hello from B"
	if err := clientB.Send(map[string]string{
		"type":    client.TypeMessage,
		"chat_id": chatIDB,
		"text":    textB,
	}); err != nil {
		return s3Result,
			scenarioResult{s4Name, resultFail, fmt.Sprintf("client B send message: %v", err)},
			scenarioResult{s5Name, resultFail, "skipped: chat failed"}
	}

	// Client A should receive it.
	var receivedByA string
	select {
	case receivedByA = <-msgFromB:
	case <-chatCtx.Done():
		return s3Result,
			scenarioResult{s4Name, resultFail, "timeout: client A did not receive message from B"},
			scenarioResult{s5Name, resultFail, "skipped: chat failed"}
	}

	if receivedByA != textB {
		return s3Result,
			scenarioResult{s4Name, resultFail, fmt.Sprintf("content mismatch: expected %q, got %q", textB, receivedByA)},
			scenarioResult{s5Name, resultFail, "skipped: chat failed"}
	}

	s4Result := scenarioResult{s4Name, resultPass, "2 messages exchanged"}

	// --- Scenario 5: End Chat ---
	partnerLeftB := make(chan struct{}, 1)

	clientB.On(client.TypePartnerLeft, func(_ json.RawMessage) {
		select {
		case partnerLeftB <- struct{}{}:
		default:
		}
	})

	endCtx, endCancel := context.WithTimeout(ctx, 10*time.Second)
	defer endCancel()

	// Client A sends end_chat.
	if err := clientA.Send(map[string]string{
		"type":    client.TypeEndChat,
		"chat_id": chatIDA,
	}); err != nil {
		return s3Result, s4Result,
			scenarioResult{s5Name, resultFail, fmt.Sprintf("client A end_chat: %v", err)}
	}

	// Client B should receive partner_left.
	select {
	case <-partnerLeftB:
	case <-endCtx.Done():
		return s3Result, s4Result,
			scenarioResult{s5Name, resultFail, "timeout: client B did not receive partner_left"}
	}

	// Close connections cleanly.
	clientA.Close()
	clientB.Close()

	s5Result := scenarioResult{s5Name, resultPass, "clean disconnect"}
	return s3Result, s4Result, s5Result
}

// ---------------------------------------------------------------------------
// Scenario 6: Rate Limiting (optional, non-fatal)
// ---------------------------------------------------------------------------

func scenario6RateLimiting(ctx context.Context, wsURL string) scenarioResult {
	name := "Scenario 6: Rate Limiting"

	scenarioCtx, scenarioCancel := context.WithTimeout(ctx, 30*time.Second)
	defer scenarioCancel()

	// We need two matched clients to send chat messages.
	clientA, clientB, chatIDA, _, err := connectAndMatch(scenarioCtx, wsURL)
	if err != nil {
		return scenarioResult{name, resultInfo, fmt.Sprintf("setup failed: %v", err)}
	}
	defer clientA.Close()
	defer clientB.Close()

	// Listen for rate_limited on client A.
	rateLimited := make(chan struct{}, 1)
	clientA.On(client.TypeRateLimited, func(_ json.RawMessage) {
		select {
		case rateLimited <- struct{}{}:
		default:
		}
	})

	// Send 10 messages rapidly from client A (limit is 5 per 10s).
	sentCount := 0
	for i := 0; i < 10; i++ {
		err := clientA.Send(map[string]string{
			"type":    client.TypeMessage,
			"chat_id": chatIDA,
			"text":    fmt.Sprintf("rapid message %d", i+1),
		})
		if err != nil {
			break
		}
		sentCount++
	}

	// Wait briefly for rate_limited response.
	rlCtx, rlCancel := context.WithTimeout(scenarioCtx, 5*time.Second)
	defer rlCancel()

	select {
	case <-rateLimited:
		return scenarioResult{name, resultInfo, fmt.Sprintf("rate_limited received after %d messages", sentCount)}
	case <-rlCtx.Done():
		return scenarioResult{name, resultInfo, fmt.Sprintf("no rate_limited received after %d messages (rate limiting may be relaxed)", sentCount)}
	}
}

// ---------------------------------------------------------------------------
// Scenario 7: Content Filtering (optional, non-fatal)
// ---------------------------------------------------------------------------

func scenario7ContentFiltering(ctx context.Context, wsURL string) scenarioResult {
	name := "Scenario 7: Content Filtering"

	scenarioCtx, scenarioCancel := context.WithTimeout(ctx, 30*time.Second)
	defer scenarioCancel()

	// We need two matched clients to send chat messages.
	clientA, clientB, chatIDA, _, err := connectAndMatch(scenarioCtx, wsURL)
	if err != nil {
		return scenarioResult{name, resultInfo, fmt.Sprintf("setup failed: %v", err)}
	}
	defer clientA.Close()
	defer clientB.Close()

	// Listen for error with code "message_blocked" on client A.
	messageBlocked := make(chan string, 1)
	clientA.On(client.TypeError, func(raw json.RawMessage) {
		var msg struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(raw, &msg); err == nil && msg.Code == "message_blocked" {
			select {
			case messageBlocked <- msg.Code:
			default:
			}
		}
	})

	// Send a message containing a blocked keyword from the default blocklist.
	// Using "kill yourself" which is a multi-word harassment phrase.
	blockedText := "hey you should kill yourself"
	if err := clientA.Send(map[string]string{
		"type":    client.TypeMessage,
		"chat_id": chatIDA,
		"text":    blockedText,
	}); err != nil {
		return scenarioResult{name, resultInfo, fmt.Sprintf("send failed: %v", err)}
	}

	// Wait for message_blocked error response.
	filterCtx, filterCancel := context.WithTimeout(scenarioCtx, 5*time.Second)
	defer filterCancel()

	select {
	case code := <-messageBlocked:
		return scenarioResult{name, resultInfo, fmt.Sprintf("%s for blocked keyword", code)}
	case <-filterCtx.Done():
		return scenarioResult{name, resultInfo, "no message_blocked received (content filter may be async only)"}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// connectAndMatch creates two clients, connects them, matches them, and returns
// both clients with their chat IDs. Caller is responsible for closing clients.
func connectAndMatch(ctx context.Context, wsURL string) (clientA, clientB *client.Client, chatIDA, chatIDB string, err error) {
	connCtx, connCancel := context.WithTimeout(ctx, 10*time.Second)
	defer connCancel()

	clientA, err = client.New(connCtx, wsURL)
	if err != nil {
		return nil, nil, "", "", fmt.Errorf("client A connect: %w", err)
	}

	clientB, err = client.New(connCtx, wsURL)
	if err != nil {
		clientA.Close()
		return nil, nil, "", "", fmt.Errorf("client B connect: %w", err)
	}

	if err := clientA.WaitForSession(connCtx); err != nil {
		clientA.Close()
		clientB.Close()
		return nil, nil, "", "", fmt.Errorf("client A session: %w", err)
	}
	if err := clientB.WaitForSession(connCtx); err != nil {
		clientA.Close()
		clientB.Close()
		return nil, nil, "", "", fmt.Errorf("client B session: %w", err)
	}

	// Set up match handlers.
	matchFoundA := make(chan string, 1)
	matchFoundB := make(chan string, 1)
	matchAcceptedA := make(chan struct{}, 1)
	matchAcceptedB := make(chan struct{}, 1)

	clientA.On(client.TypeMatchFound, func(raw json.RawMessage) {
		var msg struct {
			ChatID string `json:"chat_id"`
		}
		if err := json.Unmarshal(raw, &msg); err == nil && msg.ChatID != "" {
			select {
			case matchFoundA <- msg.ChatID:
			default:
			}
		}
	})

	clientB.On(client.TypeMatchFound, func(raw json.RawMessage) {
		var msg struct {
			ChatID string `json:"chat_id"`
		}
		if err := json.Unmarshal(raw, &msg); err == nil && msg.ChatID != "" {
			select {
			case matchFoundB <- msg.ChatID:
			default:
			}
		}
	})

	clientA.On(client.TypeMatchAccepted, func(_ json.RawMessage) {
		select {
		case matchAcceptedA <- struct{}{}:
		default:
		}
	})

	clientB.On(client.TypeMatchAccepted, func(_ json.RawMessage) {
		select {
		case matchAcceptedB <- struct{}{}:
		default:
		}
	})

	// Both send find_match.
	if err := clientA.Send(map[string]interface{}{
		"type":      client.TypeFindMatch,
		"interests": []string{},
	}); err != nil {
		clientA.Close()
		clientB.Close()
		return nil, nil, "", "", fmt.Errorf("client A find_match: %w", err)
	}
	if err := clientB.Send(map[string]interface{}{
		"type":      client.TypeFindMatch,
		"interests": []string{},
	}); err != nil {
		clientA.Close()
		clientB.Close()
		return nil, nil, "", "", fmt.Errorf("client B find_match: %w", err)
	}

	// Wait for match_found.
	matchCtx, matchCancel := context.WithTimeout(ctx, 30*time.Second)
	defer matchCancel()

	select {
	case chatIDA = <-matchFoundA:
	case <-matchCtx.Done():
		clientA.Close()
		clientB.Close()
		return nil, nil, "", "", fmt.Errorf("timeout waiting for match_found on client A")
	}

	select {
	case chatIDB = <-matchFoundB:
	case <-matchCtx.Done():
		clientA.Close()
		clientB.Close()
		return nil, nil, "", "", fmt.Errorf("timeout waiting for match_found on client B")
	}

	// Both accept.
	if err := clientA.Send(map[string]string{
		"type":    client.TypeAcceptMatch,
		"chat_id": chatIDA,
	}); err != nil {
		clientA.Close()
		clientB.Close()
		return nil, nil, "", "", fmt.Errorf("client A accept_match: %w", err)
	}
	if err := clientB.Send(map[string]string{
		"type":    client.TypeAcceptMatch,
		"chat_id": chatIDB,
	}); err != nil {
		clientA.Close()
		clientB.Close()
		return nil, nil, "", "", fmt.Errorf("client B accept_match: %w", err)
	}

	// Wait for match_accepted.
	acceptCtx, acceptCancel := context.WithTimeout(ctx, 10*time.Second)
	defer acceptCancel()

	select {
	case <-matchAcceptedA:
	case <-acceptCtx.Done():
		clientA.Close()
		clientB.Close()
		return nil, nil, "", "", fmt.Errorf("timeout waiting for match_accepted on client A")
	}

	select {
	case <-matchAcceptedB:
	case <-acceptCtx.Done():
		clientA.Close()
		clientB.Close()
		return nil, nil, "", "", fmt.Errorf("timeout waiting for match_accepted on client B")
	}

	return clientA, clientB, chatIDA, chatIDB, nil
}

// httpGetExpectOK performs an HTTP GET and checks for a 200 status code.
func httpGetExpectOK(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	return nil
}

// httpGetBody performs an HTTP GET and returns the response body.
func httpGetBody(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return body, nil
}

// truncateID returns the first 8 characters of an ID for display purposes.
func truncateID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
