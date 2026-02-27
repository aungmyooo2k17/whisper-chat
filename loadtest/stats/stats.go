// Package stats provides a goroutine-safe metrics collector that aggregates
// performance data from multiple load test clients and prints a summary report
// with percentile distributions.
package stats

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

// Collector aggregates metrics from multiple load test clients. All methods
// are goroutine-safe and can be called concurrently from many client
// goroutines.
type Collector struct {
	mu               sync.Mutex
	connectLatencies []time.Duration
	msgLatencies     []time.Duration
	errors           int
	connections      int
	startTime        time.Time
	scraper          *Scraper
}

// SetScraper attaches a Prometheus metrics scraper to this collector. When set,
// Report() will also print server-side metrics collected by the scraper.
func (c *Collector) SetScraper(s *Scraper) {
	c.mu.Lock()
	c.scraper = s
	c.mu.Unlock()
}

// NewCollector creates a new Collector with the start time set to now.
func NewCollector() *Collector {
	return &Collector{startTime: time.Now()}
}

// AddConnect records a successful connection with the given connect latency.
func (c *Collector) AddConnect(d time.Duration) {
	c.mu.Lock()
	c.connectLatencies = append(c.connectLatencies, d)
	c.connections++
	c.mu.Unlock()
}

// AddMsgLatency records a message round-trip latency measurement.
func (c *Collector) AddMsgLatency(d time.Duration) {
	c.mu.Lock()
	c.msgLatencies = append(c.msgLatencies, d)
	c.mu.Unlock()
}

// AddError increments the error counter.
func (c *Collector) AddError() {
	c.mu.Lock()
	c.errors++
	c.mu.Unlock()
}

// ConnectionCount returns the current number of recorded connections.
func (c *Collector) ConnectionCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connections
}

// ErrorCount returns the current number of recorded errors.
func (c *Collector) ErrorCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.errors
}

// Report prints a formatted summary of the collected metrics to stdout,
// including total duration, connection count, error count, and percentile
// distributions for connect and message latencies.
func (c *Collector) Report() {
	c.mu.Lock()
	defer c.mu.Unlock()

	elapsed := time.Since(c.startTime)

	fmt.Println("\n=== Load Test Results ===")
	fmt.Printf("Duration:     %s\n", elapsed.Round(time.Second))
	fmt.Printf("Connections:  %d\n", c.connections)
	fmt.Printf("Errors:       %d\n", c.errors)

	if c.connections > 0 {
		errorRate := float64(c.errors) / float64(c.connections) * 100
		fmt.Printf("Error rate:   %.2f%%\n", errorRate)
	}

	if len(c.connectLatencies) > 0 {
		fmt.Println("\n--- Connect Latency ---")
		printPercentiles(c.connectLatencies)
	}

	if len(c.msgLatencies) > 0 {
		fmt.Println("\n--- Message Latency ---")
		printPercentiles(c.msgLatencies)
	}

	if c.scraper != nil {
		c.scraper.Report()
	}

	fmt.Println()
}

// printPercentiles sorts the given durations and prints avg, p50, p95, p99,
// and max values along with the sample count.
func printPercentiles(durations []time.Duration) {
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })

	n := len(durations)
	p50 := durations[n/2]
	p95 := durations[int(math.Ceil(float64(n)*0.95))-1]
	p99 := durations[int(math.Ceil(float64(n)*0.99))-1]

	var sum time.Duration
	for _, d := range durations {
		sum += d
	}
	avg := sum / time.Duration(n)

	fmt.Printf("  avg: %v  p50: %v  p95: %v  p99: %v  max: %v  (n=%d)\n",
		avg.Round(time.Microsecond),
		p50.Round(time.Microsecond),
		p95.Round(time.Microsecond),
		p99.Round(time.Microsecond),
		durations[n-1].Round(time.Microsecond),
		n,
	)
}
