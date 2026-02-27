package ws

import (
	"log"
	"time"

	"github.com/gobwas/ws"
)

// HeartbeatConfig holds heartbeat tuning parameters.
type HeartbeatConfig struct {
	Interval time.Duration // how often to ping (default: 30s)
	Timeout  time.Duration // max time to wait for activity after ping (default: 10s)
}

// DefaultHeartbeatConfig returns sensible defaults for heartbeat monitoring.
func DefaultHeartbeatConfig() HeartbeatConfig {
	return HeartbeatConfig{
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
	}
}

// StartHeartbeat begins a background goroutine that periodically sends
// WebSocket ping frames to all connections and closes those that have gone
// stale (no successful reads within Interval + Timeout). It returns
// immediately; the goroutine exits when the server's done channel is closed.
func StartHeartbeat(server *Server, config HeartbeatConfig) {
	go func() {
		ticker := time.NewTicker(config.Interval)
		defer ticker.Stop()

		for {
			select {
			case <-server.done:
				return
			case <-ticker.C:
				checkConnections(server, config)
			}
		}
	}()
}

// checkConnections iterates over all active connections. Connections that have
// not had a successful read within Interval + Timeout are considered dead and
// are removed. All other connections receive a WebSocket-level ping frame
// (opcode 0x9) which the browser answers automatically with a pong.
func checkConnections(server *Server, config HeartbeatConfig) {
	deadline := config.Interval + config.Timeout
	now := time.Now()

	for _, c := range server.Connections().All() {
		if now.Sub(c.LastPing) > deadline {
			log.Printf("ws: heartbeat timeout session=%s last_activity=%s ago",
				c.ID, now.Sub(c.LastPing).Round(time.Second))
			server.RemoveConnection(c)
			continue
		}

		// Send a WebSocket protocol-level ping frame. The write mutex on the
		// connection serializes this with any concurrent application writes.
		if err := c.WritePing(); err != nil {
			log.Printf("ws: heartbeat ping failed session=%s: %v", c.ID, err)
			server.RemoveConnection(c)
		}
	}
}

// WritePing sends a WebSocket protocol-level ping frame (opcode 0x9) on the
// connection. The write mutex ensures this does not interleave with other
// outbound frames.
func (c *Connection) WritePing() error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return ws.WriteFrame(c.Conn, ws.NewPingFrame(nil))
}
