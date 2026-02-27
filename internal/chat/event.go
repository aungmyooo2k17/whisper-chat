package chat

// ChatEvent is the payload published to NATS chat.<chat_id> subjects
// for real-time communication between paired users.
type ChatEvent struct {
	Type     string `json:"type"`               // "message", "typing", "partner_left"
	From     string `json:"from"`               // sender's session ID
	Text     string `json:"text,omitempty"`      // for message events
	IsTyping bool   `json:"is_typing,omitempty"` // for typing events
	Ts       int64  `json:"ts,omitempty"`        // unix timestamp for messages
}
