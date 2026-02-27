package ws

import (
	"net"
	"sync"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

// Connection represents a single WebSocket client connection with its
// associated metadata and a write mutex for serializing outbound frames.
type Connection struct {
	ID         string    // session ID (UUID)
	Conn       net.Conn  // underlying TCP connection
	Fd         int       // file descriptor for epoll lookups
	CreatedAt  time.Time // when the connection was established
	LastPing   time.Time // last heartbeat received from the client
	writeMu    sync.Mutex // serializes writes to this connection
	processing int32      // atomic flag: 0 = idle, 1 = being read by handleConn
}

// WriteMessage sends a WebSocket text frame to this connection. The write
// mutex ensures that concurrent goroutines do not interleave frame bytes.
func (c *Connection) WriteMessage(data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return wsutil.WriteServerMessage(c.Conn, ws.OpText, data)
}

// Close closes the underlying network connection.
func (c *Connection) Close() error {
	return c.Conn.Close()
}

// ConnectionManager is a thread-safe registry that maps session IDs and file
// descriptors to their respective Connection objects. It supports O(1) lookups
// by both session ID and fd.
type ConnectionManager struct {
	mu      sync.RWMutex
	byID    map[string]*Connection // session_id -> Connection
	byFd    map[int]*Connection    // fd -> Connection
}

// NewConnectionManager creates an empty ConnectionManager ready for use.
func NewConnectionManager() *ConnectionManager {
	return &ConnectionManager{
		byID: make(map[string]*Connection),
		byFd: make(map[int]*Connection),
	}
}

// Add registers a new connection in both the ID and fd lookup maps.
func (cm *ConnectionManager) Add(conn *Connection) {
	cm.mu.Lock()
	cm.byID[conn.ID] = conn
	cm.byFd[conn.Fd] = conn
	cm.mu.Unlock()
}

// Remove removes a connection by session ID, closes the underlying network
// connection, and removes it from both lookup maps. Returns true if the
// connection was found and removed, false if it was already gone.
func (cm *ConnectionManager) Remove(id string) bool {
	cm.mu.Lock()
	conn, ok := cm.byID[id]
	if ok {
		delete(cm.byID, id)
		delete(cm.byFd, conn.Fd)
	}
	cm.mu.Unlock()

	if ok {
		conn.Close()
	}
	return ok
}

// RemoveByFd removes a connection by file descriptor, closes the underlying
// network connection, and removes it from both lookup maps. It returns the
// removed connection, or nil if no connection was registered for that fd.
func (cm *ConnectionManager) RemoveByFd(fd int) *Connection {
	cm.mu.Lock()
	conn, ok := cm.byFd[fd]
	if ok {
		delete(cm.byFd, fd)
		delete(cm.byID, conn.ID)
	}
	cm.mu.Unlock()

	if ok {
		conn.Close()
		return conn
	}
	return nil
}

// Get returns the connection for the given session ID, or nil if not found.
func (cm *ConnectionManager) Get(id string) *Connection {
	cm.mu.RLock()
	conn := cm.byID[id]
	cm.mu.RUnlock()
	return conn
}

// GetByFd returns the connection for the given file descriptor, or nil if
// not found.
func (cm *ConnectionManager) GetByFd(fd int) *Connection {
	cm.mu.RLock()
	conn := cm.byFd[fd]
	cm.mu.RUnlock()
	return conn
}

// GetByConn returns the connection for the given net.Conn by extracting
// its file descriptor. Returns nil if not found.
func (cm *ConnectionManager) GetByConn(c net.Conn) *Connection {
	fd := socketFD(c)
	return cm.GetByFd(fd)
}

// Count returns the current number of active connections.
func (cm *ConnectionManager) Count() int {
	cm.mu.RLock()
	n := len(cm.byID)
	cm.mu.RUnlock()
	return n
}

// Broadcast sends a message to all connected clients. Errors on individual
// connections are silently ignored — failed connections will be cleaned up
// by the epoll event loop when the next read fails.
func (cm *ConnectionManager) Broadcast(msg []byte) {
	cm.mu.RLock()
	conns := make([]*Connection, 0, len(cm.byID))
	for _, conn := range cm.byID {
		conns = append(conns, conn)
	}
	cm.mu.RUnlock()

	for _, conn := range conns {
		_ = conn.WriteMessage(msg)
	}
}

// All returns a snapshot of all current connections. The returned slice is
// safe to iterate without holding the lock.
func (cm *ConnectionManager) All() []*Connection {
	cm.mu.RLock()
	conns := make([]*Connection, 0, len(cm.byID))
	for _, conn := range cm.byID {
		conns = append(conns, conn)
	}
	cm.mu.RUnlock()
	return conns
}
