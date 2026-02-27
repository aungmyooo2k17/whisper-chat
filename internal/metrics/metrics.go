// Package metrics provides Prometheus instrumentation for the Whisper chat
// application. It exposes gauges for connection and chat counts, counters for
// message throughput, and histograms for latency tracking.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// ConnectionsTotal tracks the current number of active WebSocket connections.
	ConnectionsTotal = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "whisper_connections_total",
		Help: "Current number of active WebSocket connections",
	})

	// MessagesTotal counts the total number of messages processed, labeled by
	// type: "sent", "received", or "blocked".
	MessagesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "whisper_messages_total",
		Help: "Total number of messages processed",
	}, []string{"type"}) // type = "sent", "received", "blocked"

	// MessageLatency records message processing latency in seconds.
	MessageLatency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "whisper_message_latency_seconds",
		Help:    "Message processing latency in seconds",
		Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1},
	})

	// MatchDuration records the time from match request to match found.
	MatchDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "whisper_match_duration_seconds",
		Help:    "Time from match request to match found",
		Buckets: []float64{1, 2, 5, 10, 15, 20, 25, 30},
	})

	// ActiveChats tracks the current number of active chat sessions.
	ActiveChats = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "whisper_active_chats",
		Help: "Current number of active chat sessions",
	})

	// MatchQueueSize tracks the current number of users in the matching queue.
	MatchQueueSize = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "whisper_match_queue_size",
		Help: "Current number of users in matching queue",
	})
)

func init() {
	prometheus.MustRegister(
		ConnectionsTotal,
		MessagesTotal,
		MessageLatency,
		MatchDuration,
		ActiveChats,
		MatchQueueSize,
	)
}

// Handler returns the Prometheus metrics HTTP handler.
func Handler() http.Handler {
	return promhttp.Handler()
}
