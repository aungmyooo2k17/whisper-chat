// Package ws handles WebSocket connection management, including upgrading
// HTTP connections, maintaining active client sessions, and dispatching
// incoming messages to the appropriate handlers.
package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/google/uuid"

	"github.com/whisper/chat-app/internal/protocol"
	"github.com/whisper/chat-app/internal/session"
)

// ServerConfig holds tunable parameters for the WebSocket server.
type ServerConfig struct {
	ListenAddr     string        // address to listen on, e.g. ":8080"
	WorkerPoolSize int           // max concurrent read-worker goroutines
	MaxConnections int           // hard cap on total connections
	ReadTimeout    time.Duration // timeout for WebSocket read operations
	WriteTimeout   time.Duration // timeout for WebSocket write operations
}

// DefaultServerConfig returns a ServerConfig with sensible production defaults.
func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		ListenAddr:     ":8080",
		WorkerPoolSize: 256,
		MaxConnections: 100000,
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
	}
}

// Server is the high-performance WebSocket server built on gobwas/ws and Linux
// epoll. It upgrades HTTP connections to WebSocket, registers them with an
// epoll instance for I/O readiness notifications, and dispatches ready
// connections to a bounded worker pool for frame reading.
type Server struct {
	config       ServerConfig
	epoll        *Epoll
	conns        *ConnectionManager
	sessionStore *session.Store                        // Redis-backed session state
	workerPool   chan struct{}                         // semaphore limiting concurrent read workers
	onMessage    func(conn *Connection, data []byte)  // message handler callback
	onDisconnect func(connID string)                  // called when a connection is removed
	httpServer   *http.Server
	bufPool      sync.Pool // pool of reusable read buffers
	done         chan struct{}
	startedAt    time.Time // server start time for uptime calculation
}

// NewServer creates a Server with the given configuration, session store, and
// message callback. The onMessage function is called from a worker goroutine
// whenever a complete WebSocket text frame is received from a client.
func NewServer(config ServerConfig, sessionStore *session.Store, onMessage func(conn *Connection, data []byte)) *Server {
	s := &Server{
		config:       config,
		conns:        NewConnectionManager(),
		sessionStore: sessionStore,
		workerPool:   make(chan struct{}, config.WorkerPoolSize),
		onMessage:    onMessage,
		done:         make(chan struct{}),
		bufPool: sync.Pool{
			New: func() interface{} {
				buf := make([]byte, 4096)
				return &buf
			},
		},
	}

	return s
}

// Start initializes the epoll instance, configures the HTTP server, and begins
// accepting WebSocket connections. It starts the epoll event loop in a
// background goroutine and blocks on http.Server.ListenAndServe.
func (s *Server) Start() error {
	var err error
	s.epoll, err = NewEpoll()
	if err != nil {
		return fmt.Errorf("ws: failed to create epoll: %w", err)
	}

	s.startedAt = time.Now()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleUpgrade)
	mux.HandleFunc("/health", s.handleHealth)

	s.httpServer = &http.Server{
		Addr:    s.config.ListenAddr,
		Handler: mux,
	}

	// Start the epoll event loop in the background.
	go s.startEventLoop()

	// Start the heartbeat monitor to detect and close dead connections.
	StartHeartbeat(s, DefaultHeartbeatConfig())

	log.Printf("ws: server listening on %s (workers=%d, max_conns=%d)",
		s.config.ListenAddr, s.config.WorkerPoolSize, s.config.MaxConnections)

	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("ws: http server error: %w", err)
	}
	return nil
}

// handleUpgrade upgrades an HTTP request to a WebSocket connection using
// gobwas/ws zero-copy upgrader. On success it creates a Connection, registers
// it with the connection manager and epoll instance.
func (s *Server) handleUpgrade(w http.ResponseWriter, r *http.Request) {
	// Enforce maximum connection limit.
	if s.conns.Count() >= s.config.MaxConnections {
		http.Error(w, "too many connections", http.StatusServiceUnavailable)
		return
	}

	// Upgrade the HTTP connection to WebSocket.
	conn, _, _, err := ws.UpgradeHTTP(r, w)
	if err != nil {
		log.Printf("ws: upgrade failed: %v", err)
		return
	}

	fd := socketFD(conn)
	sessionID := uuid.New().String()

	c := &Connection{
		ID:        sessionID,
		Conn:      conn,
		Fd:        fd,
		CreatedAt: time.Now(),
		LastPing:  time.Now(),
	}

	// Register the connection in the manager and epoll.
	s.conns.Add(c)
	if err := s.epoll.Add(conn); err != nil {
		log.Printf("ws: epoll add failed for session %s: %v", sessionID, err)
		s.conns.Remove(sessionID)
		return
	}

	// Create session in Redis.
	if s.sessionStore != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.sessionStore.Create(ctx, sessionID); err != nil {
			log.Printf("ws: failed to create redis session for %s: %v", sessionID, err)
		}
	}

	// Send session_created to the client.
	sessionMsg, err := protocol.NewServerMessage(protocol.TypeSessionCreated, protocol.SessionCreatedMsg{
		SessionID: sessionID,
	})
	if err != nil {
		log.Printf("ws: failed to build session_created for session %s: %v", sessionID, err)
	} else if err := c.WriteMessage(sessionMsg); err != nil {
		log.Printf("ws: failed to send session_created for session %s: %v", sessionID, err)
	}

	log.Printf("ws: new connection session=%s fd=%d (total=%d)", sessionID, fd, s.conns.Count())
}

// handleHealth responds with the server's health status as JSON, including the
// current connection count and uptime. It is used by HAProxy for health checks.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	resp := struct {
		Status      string `json:"status"`
		Connections int    `json:"connections"`
		Uptime      string `json:"uptime"`
	}{
		Status:      "ok",
		Connections: s.conns.Count(),
		Uptime:      time.Since(s.startedAt).Round(time.Second).String(),
	}

	_ = json.NewEncoder(w).Encode(resp)
}

// startEventLoop runs the epoll wait loop. For each batch of ready
// connections, it dispatches each to a worker goroutine (bounded by the
// worker pool semaphore) that reads and processes the WebSocket frame.
func (s *Server) startEventLoop() {
	for {
		select {
		case <-s.done:
			return
		default:
		}

		conns, err := s.epoll.Wait()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				// EINTR is expected during signal handling.
				if isEINTR(err) {
					continue
				}
				log.Printf("ws: epoll wait error: %v", err)
				continue
			}
		}

		for _, conn := range conns {
			conn := conn // capture for goroutine

			// Acquire a worker slot (blocks if pool is full).
			s.workerPool <- struct{}{}

			go func() {
				defer func() { <-s.workerPool }()
				s.handleConn(conn)
			}()
		}
	}
}

// handleConn reads a single WebSocket frame from a ready connection using
// wsutil.NextReader so that control frames (ping, pong) are handled without
// blocking on a data frame that may never arrive. If the read fails
// (connection closed, protocol error, etc.) the connection is removed from
// epoll and the connection manager.
func (s *Server) handleConn(netConn net.Conn) {
	c := s.conns.GetByConn(netConn)
	if c == nil {
		return
	}

	// Guard against duplicate dispatch from level-triggered epoll.
	if !atomic.CompareAndSwapInt32(&c.processing, 0, 1) {
		return
	}
	defer atomic.StoreInt32(&c.processing, 0)

	if s.config.ReadTimeout > 0 {
		_ = netConn.SetReadDeadline(time.Now().Add(s.config.ReadTimeout))
	}

	header, reader, err := wsutil.NextReader(netConn, ws.StateServerSide)
	if err != nil {
		// A read timeout means no data was available (stale epoll dispatch).
		// Don't kill the connection — the heartbeat handles dead connections.
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return
		}
		s.RemoveConnection(c)
		return
	}

	// Clear read deadline after successful frame read.
	_ = netConn.SetReadDeadline(time.Time{})

	// Any frame proves the connection is alive.
	c.LastPing = time.Now()

	// Handle control frames without removing the connection.
	if header.OpCode.IsControl() {
		if header.OpCode == ws.OpClose {
			s.RemoveConnection(c)
		}
		// Pong/ping: connection is alive, nothing else to do.
		return
	}

	// Read data frame payload.
	data := make([]byte, header.Length)
	if header.Length > 0 {
		_, err = io.ReadFull(reader, data)
		if err != nil {
			s.RemoveConnection(c)
			return
		}
	}

	if len(data) == 0 {
		return
	}

	if s.onMessage != nil {
		s.onMessage(c, data)
	}
}

// SetOnDisconnect registers a callback invoked when a connection is removed
// (due to read error, heartbeat timeout, or graceful close). It is called
// before the Redis session is deleted, so the handler can inspect session state.
func (s *Server) SetOnDisconnect(fn func(connID string)) {
	s.onDisconnect = fn
}

// RemoveConnection removes a connection from both epoll and the connection
// manager, and closes the underlying network connection. It is exported so
// that the heartbeat monitor can evict dead connections.
func (s *Server) RemoveConnection(c *Connection) {
	_ = s.epoll.Remove(c.Conn)

	// Guard: only proceed if the connection was actually in the manager.
	// This prevents double cleanup when multiple goroutines race to remove
	// the same connection (e.g., read error + heartbeat timeout).
	if !s.conns.Remove(c.ID) {
		return
	}

	// Notify application layer before deleting session.
	if s.onDisconnect != nil {
		s.onDisconnect(c.ID)
	}

	// Delete session from Redis.
	if s.sessionStore != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := s.sessionStore.Delete(ctx, c.ID); err != nil {
			log.Printf("ws: failed to delete redis session for %s: %v", c.ID, err)
		}
	}

	log.Printf("ws: connection closed session=%s (total=%d)", c.ID, s.conns.Count())
}

// SendMessage writes a WebSocket text frame to the connection identified by
// connID. It is goroutine-safe thanks to the per-connection write mutex.
func (s *Server) SendMessage(connID string, data []byte) error {
	c := s.conns.Get(connID)
	if c == nil {
		return fmt.Errorf("ws: connection %s not found", connID)
	}

	if s.config.WriteTimeout > 0 {
		_ = c.Conn.SetWriteDeadline(time.Now().Add(s.config.WriteTimeout))
	}

	err := c.WriteMessage(data)

	// Clear write deadline so it doesn't affect future writes (e.g., heartbeat pings).
	_ = c.Conn.SetWriteDeadline(time.Time{})

	return err
}

// Connections returns the ConnectionManager for external access to connection
// state (e.g., by the heartbeat or session layer).
func (s *Server) Connections() *ConnectionManager {
	return s.conns
}

// SessionStore returns the Redis session store for external access (e.g., by
// message handlers that need to read or update session state).
func (s *Server) SessionStore() *session.Store {
	return s.sessionStore
}

// Shutdown performs a graceful shutdown of the server. It stops the HTTP
// listener, signals the event loop to exit, closes all active connections,
// and cleans up the epoll instance.
func (s *Server) Shutdown() error {
	log.Println("ws: shutting down server...")

	// Signal the event loop to stop.
	close(s.done)

	// Stop accepting new HTTP connections with a deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.httpServer.Shutdown(ctx); err != nil {
		log.Printf("ws: http shutdown error: %v", err)
	}

	// Delete all sessions from Redis and close all active WebSocket connections.
	for _, c := range s.conns.All() {
		if s.sessionStore != nil {
			delCtx, delCancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = s.sessionStore.Delete(delCtx, c.ID)
			delCancel()
		}
		_ = s.epoll.Remove(c.Conn)
		c.Close()
	}

	// Close the epoll instance.
	if s.epoll != nil {
		_ = s.epoll.Close()
	}

	log.Printf("ws: server stopped, all connections closed")
	return nil
}

// isEINTR checks if the error is a syscall interrupted error (EINTR),
// which is expected during signal handling and should be retried.
func isEINTR(err error) bool {
	if err == nil {
		return false
	}
	return err.Error() == "interrupted system call" ||
		err.Error() == "errno 4"
}
