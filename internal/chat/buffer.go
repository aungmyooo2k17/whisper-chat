package chat

import "sync"

// MaxBufferMessages is the number of recent messages retained per chat.
const MaxBufferMessages = 5

// BufferedMessage represents a single message stored in the ring buffer.
type BufferedMessage struct {
	From string `json:"from"` // session ID of sender
	Text string `json:"text"`
	Ts   int64  `json:"ts"`
}

// MessageBuffer stores the last N messages per chat in memory.
// It is goroutine-safe and uses a ring buffer internally.
type MessageBuffer struct {
	mu      sync.RWMutex
	buffers map[string]*ringBuffer // chatID -> ring buffer
}

// ringBuffer is a fixed-size circular buffer of BufferedMessage.
type ringBuffer struct {
	items []BufferedMessage
	pos   int
	count int
}

// NewMessageBuffer creates a new empty MessageBuffer.
func NewMessageBuffer() *MessageBuffer {
	return &MessageBuffer{
		buffers: make(map[string]*ringBuffer),
	}
}

// Add appends a message to the chat's ring buffer. If the buffer is full,
// the oldest message is overwritten.
func (mb *MessageBuffer) Add(chatID string, msg BufferedMessage) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	rb, ok := mb.buffers[chatID]
	if !ok {
		rb = &ringBuffer{
			items: make([]BufferedMessage, MaxBufferMessages),
		}
		mb.buffers[chatID] = rb
	}

	rb.items[rb.pos] = msg
	rb.pos = (rb.pos + 1) % MaxBufferMessages
	if rb.count < MaxBufferMessages {
		rb.count++
	}
}

// Get returns the last N messages for a chat in chronological order
// (oldest first). Returns an empty slice if the chat has no buffer.
func (mb *MessageBuffer) Get(chatID string) []BufferedMessage {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	rb, ok := mb.buffers[chatID]
	if !ok {
		return []BufferedMessage{}
	}

	result := make([]BufferedMessage, rb.count)
	// The oldest message is at position (pos - count) mod MaxBufferMessages.
	start := (rb.pos - rb.count + MaxBufferMessages) % MaxBufferMessages
	for i := 0; i < rb.count; i++ {
		result[i] = rb.items[(start+i)%MaxBufferMessages]
	}
	return result
}

// Remove deletes the buffer for a chat (called when chat ends).
func (mb *MessageBuffer) Remove(chatID string) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	delete(mb.buffers, chatID)
}
