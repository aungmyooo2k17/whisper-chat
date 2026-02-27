package chat

import (
	"fmt"
	"sync"
	"testing"
)

func TestAddAndGet(t *testing.T) {
	mb := NewMessageBuffer()

	mb.Add("chat1", BufferedMessage{From: "a", Text: "hello", Ts: 1})
	mb.Add("chat1", BufferedMessage{From: "b", Text: "hi", Ts: 2})
	mb.Add("chat1", BufferedMessage{From: "a", Text: "how are you?", Ts: 3})

	msgs := mb.Get("chat1")
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if msgs[0].Text != "hello" {
		t.Errorf("expected first message 'hello', got %q", msgs[0].Text)
	}
	if msgs[1].Text != "hi" {
		t.Errorf("expected second message 'hi', got %q", msgs[1].Text)
	}
	if msgs[2].Text != "how are you?" {
		t.Errorf("expected third message 'how are you?', got %q", msgs[2].Text)
	}
}

func TestRingBufferWraparound(t *testing.T) {
	mb := NewMessageBuffer()

	// Add 7 messages; the buffer holds only 5.
	for i := 1; i <= 7; i++ {
		mb.Add("chat1", BufferedMessage{
			From: "sender",
			Text: fmt.Sprintf("msg-%d", i),
			Ts:   int64(i),
		})
	}

	msgs := mb.Get("chat1")
	if len(msgs) != MaxBufferMessages {
		t.Fatalf("expected %d messages, got %d", MaxBufferMessages, len(msgs))
	}

	// Should contain messages 3 through 7 in order.
	for i, msg := range msgs {
		expected := fmt.Sprintf("msg-%d", i+3)
		if msg.Text != expected {
			t.Errorf("index %d: expected %q, got %q", i, expected, msg.Text)
		}
	}
}

func TestGetNonExistentChat(t *testing.T) {
	mb := NewMessageBuffer()

	msgs := mb.Get("does-not-exist")
	if msgs == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(msgs))
	}
}

func TestRemove(t *testing.T) {
	mb := NewMessageBuffer()

	mb.Add("chat1", BufferedMessage{From: "a", Text: "hello", Ts: 1})
	mb.Add("chat1", BufferedMessage{From: "b", Text: "hi", Ts: 2})

	mb.Remove("chat1")

	msgs := mb.Get("chat1")
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages after remove, got %d", len(msgs))
	}
}

func TestRemoveNonExistent(t *testing.T) {
	mb := NewMessageBuffer()

	// Should not panic.
	mb.Remove("does-not-exist")
}

func TestMultipleChats(t *testing.T) {
	mb := NewMessageBuffer()

	mb.Add("chat1", BufferedMessage{From: "a", Text: "c1-msg1", Ts: 1})
	mb.Add("chat2", BufferedMessage{From: "b", Text: "c2-msg1", Ts: 2})
	mb.Add("chat1", BufferedMessage{From: "b", Text: "c1-msg2", Ts: 3})

	msgs1 := mb.Get("chat1")
	msgs2 := mb.Get("chat2")

	if len(msgs1) != 2 {
		t.Fatalf("chat1: expected 2 messages, got %d", len(msgs1))
	}
	if len(msgs2) != 1 {
		t.Fatalf("chat2: expected 1 message, got %d", len(msgs2))
	}
	if msgs1[0].Text != "c1-msg1" || msgs1[1].Text != "c1-msg2" {
		t.Errorf("chat1 messages out of order: %+v", msgs1)
	}
	if msgs2[0].Text != "c2-msg1" {
		t.Errorf("chat2 unexpected message: %+v", msgs2[0])
	}
}

func TestExactlyMaxMessages(t *testing.T) {
	mb := NewMessageBuffer()

	for i := 1; i <= MaxBufferMessages; i++ {
		mb.Add("chat1", BufferedMessage{
			From: "sender",
			Text: fmt.Sprintf("msg-%d", i),
			Ts:   int64(i),
		})
	}

	msgs := mb.Get("chat1")
	if len(msgs) != MaxBufferMessages {
		t.Fatalf("expected %d messages, got %d", MaxBufferMessages, len(msgs))
	}

	for i, msg := range msgs {
		expected := fmt.Sprintf("msg-%d", i+1)
		if msg.Text != expected {
			t.Errorf("index %d: expected %q, got %q", i, expected, msg.Text)
		}
	}
}

func TestConcurrentAccess(t *testing.T) {
	mb := NewMessageBuffer()
	chatID := "concurrent-chat"
	goroutines := 100
	messagesPerGoroutine := 20

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for m := 0; m < messagesPerGoroutine; m++ {
				mb.Add(chatID, BufferedMessage{
					From: fmt.Sprintf("sender-%d", id),
					Text: fmt.Sprintf("g%d-m%d", id, m),
					Ts:   int64(id*messagesPerGoroutine + m),
				})
				// Interleave reads to stress the RWMutex.
				_ = mb.Get(chatID)
			}
		}(g)
	}

	wg.Wait()

	msgs := mb.Get(chatID)
	if len(msgs) != MaxBufferMessages {
		t.Fatalf("expected %d messages after concurrent writes, got %d", MaxBufferMessages, len(msgs))
	}

	// Verify chronological order (timestamps should be non-decreasing if
	// the ring buffer is correct, but with concurrent writes exact ordering
	// depends on goroutine scheduling). At minimum, we must get exactly 5
	// messages and no panics.
}
