//go:build linux

package ws

import (
	"net"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

// Epoll wraps Linux epoll syscalls for efficient WebSocket I/O multiplexing.
// Instead of spawning a goroutine per connection, we register file descriptors
// with the kernel and get notified only when data is ready to read.
type Epoll struct {
	fd          int               // epoll file descriptor
	connections map[int]net.Conn  // fd -> net.Conn mapping
	mu          sync.RWMutex      // protects connections map
	events      []unix.EpollEvent // reusable event buffer for Wait
}

// NewEpoll creates a new epoll instance using epoll_create1.
func NewEpoll() (*Epoll, error) {
	fd, err := unix.EpollCreate1(0)
	if err != nil {
		return nil, err
	}
	return &Epoll{
		fd:          fd,
		connections: make(map[int]net.Conn),
		events:      make([]unix.EpollEvent, 128),
	}, nil
}

// Add registers a network connection with epoll for read readiness
// notifications. It extracts the underlying file descriptor from the
// connection and adds it to the epoll interest list with EPOLLIN and
// EPOLLHUP events.
func (e *Epoll) Add(conn net.Conn) error {
	fd := socketFD(conn)
	if err := unix.EpollCtl(e.fd, syscall.EPOLL_CTL_ADD, fd, &unix.EpollEvent{
		Events: unix.EPOLLIN | unix.EPOLLHUP,
		Fd:     int32(fd),
	}); err != nil {
		return err
	}

	e.mu.Lock()
	e.connections[fd] = conn
	e.mu.Unlock()
	return nil
}

// Remove unregisters a network connection from epoll. It removes the file
// descriptor from the epoll interest list and deletes it from the internal
// connection map.
func (e *Epoll) Remove(conn net.Conn) error {
	fd := socketFD(conn)
	if err := unix.EpollCtl(e.fd, syscall.EPOLL_CTL_DEL, fd, nil); err != nil {
		return err
	}

	e.mu.Lock()
	delete(e.connections, fd)
	e.mu.Unlock()
	return nil
}

// Wait blocks until one or more registered connections are ready for reading.
// It returns a slice of net.Conn for all file descriptors that have pending
// data. Connections that have been removed between epoll_wait returning and
// the lookup are silently skipped.
func (e *Epoll) Wait() ([]net.Conn, error) {
	n, err := unix.EpollWait(e.fd, e.events, -1)
	if err != nil {
		return nil, err
	}

	e.mu.RLock()
	conns := make([]net.Conn, 0, n)
	for i := 0; i < n; i++ {
		conn, ok := e.connections[int(e.events[i].Fd)]
		if ok {
			conns = append(conns, conn)
		}
	}
	e.mu.RUnlock()
	return conns, nil
}

// Close closes the epoll file descriptor.
func (e *Epoll) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.connections = nil
	return unix.Close(e.fd)
}

// socketFD extracts the file descriptor from a net.Conn using the
// SyscallConn interface. This avoids duplicating the file descriptor
// (which File() does), keeping the original fd valid for epoll registration.
func socketFD(conn net.Conn) int {
	sc, ok := conn.(syscall.Conn)
	if !ok {
		return -1
	}

	raw, err := sc.SyscallConn()
	if err != nil {
		return -1
	}

	var fd int
	_ = raw.Control(func(sfd uintptr) {
		fd = int(sfd)
	})
	return fd
}
