// Package stats — scraper.go provides a lightweight Prometheus metrics scraper
// that periodically fetches server-side metrics during a load test and records
// snapshots for post-test reporting.
package stats

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// metricSnapshot holds the values of all tracked server metrics at a point in
// time.
type metricSnapshot struct {
	timestamp   time.Time
	connections float64
	messagesTotal float64
	activeChats float64
	queueSize   float64
	// histogram _sum and _count for computing averages
	latencySum   float64
	latencyCount float64
	matchSum     float64
	matchCount   float64
}

// Scraper periodically fetches Prometheus metrics from the server and records
// snapshots that can be included in the load test report.
type Scraper struct {
	metricsURL string
	interval   time.Duration

	mu        sync.Mutex
	snapshots []metricSnapshot

	cancel context.CancelFunc
	done   chan struct{}
	client *http.Client
}

// NewScraper creates a new Scraper that will fetch metrics from metricsURL at
// the given interval.
func NewScraper(metricsURL string, interval time.Duration) *Scraper {
	return &Scraper{
		metricsURL: metricsURL,
		interval:   interval,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
		done: make(chan struct{}),
	}
}

// Start begins scraping metrics in the background. It takes an initial
// snapshot immediately and then scrapes at the configured interval until the
// context is cancelled or Stop is called.
func (s *Scraper) Start(ctx context.Context) {
	ctx, s.cancel = context.WithCancel(ctx)

	// Take an initial snapshot right away.
	s.scrapeOnce()

	go func() {
		defer close(s.done)
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				// Take a final snapshot before exiting.
				s.scrapeOnce()
				return
			case <-ticker.C:
				s.scrapeOnce()
			}
		}
	}()
}

// Stop stops the background scraper and waits for it to finish.
func (s *Scraper) Stop() {
	if s.cancel != nil {
		s.cancel()
		<-s.done
	}
}

// scrapeOnce fetches the metrics endpoint and records a snapshot.
func (s *Scraper) scrapeOnce() {
	snap, err := s.fetch()
	if err != nil {
		// Silently skip failed scrapes — the server may not be ready yet.
		return
	}

	s.mu.Lock()
	s.snapshots = append(s.snapshots, snap)
	s.mu.Unlock()
}

// fetch performs an HTTP GET to the metrics endpoint and parses the response.
func (s *Scraper) fetch() (metricSnapshot, error) {
	resp, err := s.client.Get(s.metricsURL)
	if err != nil {
		return metricSnapshot{}, err
	}
	defer resp.Body.Close()

	snap := metricSnapshot{timestamp: time.Now()}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()

		// Skip comments and empty lines.
		if len(line) == 0 || line[0] == '#' {
			continue
		}

		name, value, ok := parseMetricLine(line)
		if !ok {
			continue
		}

		switch name {
		case "whisper_connections_total":
			snap.connections = value
		case "whisper_messages_total":
			// When there are labels (type="..."), the counter shows up as
			// multiple lines. We sum them to get the total.
			snap.messagesTotal += value
		case "whisper_active_chats":
			snap.activeChats = value
		case "whisper_match_queue_size":
			snap.queueSize = value
		case "whisper_message_latency_seconds_sum":
			snap.latencySum = value
		case "whisper_message_latency_seconds_count":
			snap.latencyCount = value
		case "whisper_match_duration_seconds_sum":
			snap.matchSum = value
		case "whisper_match_duration_seconds_count":
			snap.matchCount = value
		}
	}

	return snap, scanner.Err()
}

// parseMetricLine parses a Prometheus text exposition line into the metric name
// (without labels) and its float value. Returns false if the line cannot be
// parsed.
func parseMetricLine(line string) (name string, value float64, ok bool) {
	// Metric lines are in the form:
	//   metric_name 1.23
	//   metric_name{label="value"} 1.23
	//
	// We strip labels by cutting at '{' and split on whitespace for the value.

	// Remove label portion if present.
	raw := line
	if idx := strings.IndexByte(raw, '{'); idx != -1 {
		// name is everything before '{'
		name = raw[:idx]
		// value is after the closing '}'
		closing := strings.IndexByte(raw[idx:], '}')
		if closing == -1 {
			return "", 0, false
		}
		raw = name + raw[idx+closing+1:]
	}

	fields := strings.Fields(raw)
	if len(fields) < 2 {
		return "", 0, false
	}

	if name == "" {
		name = fields[0]
	}

	v, err := strconv.ParseFloat(fields[len(fields)-1], 64)
	if err != nil {
		return "", 0, false
	}

	return name, v, true
}

// Report prints a summary of the server-side metrics collected during the load
// test. For each metric it shows the initial value, final value, delta, and
// peak observed value.
func (s *Scraper) Report() {
	s.mu.Lock()
	snaps := make([]metricSnapshot, len(s.snapshots))
	copy(snaps, s.snapshots)
	s.mu.Unlock()

	if len(snaps) == 0 {
		fmt.Println("\n--- Server Metrics (no data collected) ---")
		return
	}

	first := snaps[0]
	last := snaps[len(snaps)-1]

	fmt.Println("\n--- Server Metrics (Prometheus) ---")
	fmt.Printf("  Scrape count:  %d snapshots over %s\n",
		len(snaps), last.timestamp.Sub(first.timestamp).Round(time.Second))

	type gauge struct {
		label   string
		initial float64
		final   float64
		peak    float64
	}

	gauges := []gauge{
		{label: "Connections", initial: first.connections, final: last.connections,
			peak: peakValue(snaps, func(s metricSnapshot) float64 { return s.connections })},
		{label: "Active Chats", initial: first.activeChats, final: last.activeChats,
			peak: peakValue(snaps, func(s metricSnapshot) float64 { return s.activeChats })},
		{label: "Queue Size", initial: first.queueSize, final: last.queueSize,
			peak: peakValue(snaps, func(s metricSnapshot) float64 { return s.queueSize })},
		{label: "Messages Total", initial: first.messagesTotal, final: last.messagesTotal,
			peak: peakValue(snaps, func(s metricSnapshot) float64 { return s.messagesTotal })},
	}

	fmt.Println()
	fmt.Printf("  %-16s %10s %10s %10s %10s\n", "Metric", "Initial", "Final", "Delta", "Peak")
	fmt.Printf("  %-16s %10s %10s %10s %10s\n", "------", "-------", "-----", "-----", "----")
	for _, g := range gauges {
		delta := g.final - g.initial
		fmt.Printf("  %-16s %10.0f %10.0f %10.0f %10.0f\n",
			g.label, g.initial, g.final, delta, g.peak)
	}

	// Histogram averages.
	fmt.Println()
	printHistogramAvg("Msg Latency", first.latencySum, first.latencyCount,
		last.latencySum, last.latencyCount)
	printHistogramAvg("Match Duration", first.matchSum, first.matchCount,
		last.matchSum, last.matchCount)
}

// printHistogramAvg prints the average computed from histogram _sum/_count
// deltas between the first and last snapshot.
func printHistogramAvg(label string, sumFirst, countFirst, sumLast, countLast float64) {
	deltaSum := sumLast - sumFirst
	deltaCount := countLast - countFirst
	if deltaCount > 0 {
		avg := deltaSum / deltaCount
		fmt.Printf("  %-16s avg: %.4fs  (%.0f observations)\n", label, avg, deltaCount)
	} else {
		fmt.Printf("  %-16s avg: N/A  (no observations)\n", label)
	}
}

// peakValue returns the maximum value of the given extractor across all
// snapshots.
func peakValue(snaps []metricSnapshot, extract func(metricSnapshot) float64) float64 {
	peak := math.Inf(-1)
	for _, s := range snaps {
		if v := extract(s); v > peak {
			peak = v
		}
	}
	return peak
}
