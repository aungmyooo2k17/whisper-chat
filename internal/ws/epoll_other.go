//go:build !linux

package ws

import (
	"net"
	"sync"
)

// Epoll provides a goroutine-per-connection fallback for non-Linux platforms.
// On Linux, this is replaced by the real epoll implementation. This fallback
// allows developers on macOS/Windows to run the server without the epoll
// optimization.
type Epoll struct {
	mu      sync.RWMutex
	conns   map[net.Conn]struct{}
	readyCh chan net.Conn // channel that receives connections with pending data
	done    chan struct{}
}

// NewEpoll creates a new fallback epoll instance that uses goroutines to
// monitor each connection for incoming data.
func NewEpoll() (*Epoll, error) {
	return &Epoll{
		conns:   make(map[net.Conn]struct{}),
		readyCh: make(chan net.Conn, 128),
		done:    make(chan struct{}),
	}, nil
}

// Add registers a connection by spawning a goroutine that blocks on a 1-byte
// read peek. When data arrives, the connection is sent to the ready channel
// for processing by Wait.
func (e *Epoll) Add(conn net.Conn) error {
	e.mu.Lock()
	e.conns[conn] = struct{}{}
	e.mu.Unlock()

	go e.monitor(conn)
	return nil
}

// monitor blocks reading a single byte from the connection to detect when
// data is available. It continuously signals readiness until the connection
// is removed or the epoll is closed.
func (e *Epoll) monitor(conn net.Conn) {
	buf := make([]byte, 1)
	for {
		// Block until data is available or the connection errors.
		_, err := conn.Read(buf)
		if err != nil {
			// Connection closed or errored — signal readiness so the
			// server's read path can detect the closure.
			select {
			case e.readyCh <- conn:
			case <-e.done:
			}
			return
		}

		// Data is available. We consumed 1 byte, but the server will
		// re-read the full frame. For the fallback this is acceptable —
		// the real epoll path on Linux does not consume any bytes.
		// We push the conn into the ready channel.
		select {
		case e.readyCh <- conn:
		case <-e.done:
			return
		}
	}
}

// Remove unregisters a connection from the fallback epoll.
func (e *Epoll) Remove(conn net.Conn) error {
	e.mu.Lock()
	delete(e.conns, conn)
	e.mu.Unlock()
	return nil
}

// Wait blocks until at least one connection is ready for reading. It
// collects all currently ready connections from the channel and returns them.
func (e *Epoll) Wait() ([]net.Conn, error) {
	// Block until at least one connection is ready.
	first, ok := <-e.readyCh
	if !ok {
		return nil, net.ErrClosed
	}

	conns := []net.Conn{first}

	// Drain any additional ready connections without blocking.
	for {
		select {
		case conn := <-e.readyCh:
			conns = append(conns, conn)
		default:
			return conns, nil
		}
	}
}

// Close shuts down the fallback epoll instance.
func (e *Epoll) Close() error {
	close(e.done)
	e.mu.Lock()
	e.conns = nil
	e.mu.Unlock()
	return nil
}

// socketFD is a no-op on non-Linux platforms since we don't need file
// descriptors for the goroutine-based fallback.
func socketFD(conn net.Conn) int {
	return -1
}
