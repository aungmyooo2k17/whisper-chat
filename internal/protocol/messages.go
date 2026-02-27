// Package protocol defines the WebSocket message types and structures used for
// communication between the client and server. All messages are serialized as
// JSON and follow a consistent envelope format with a type discriminator.
package protocol

import (
	"encoding/json"
	"fmt"
)

// ---------------------------------------------------------------------------
// Message type constants
// ---------------------------------------------------------------------------

// Client -> Server message types.
const (
	TypeSetFingerprint = "set_fingerprint"
	TypeFindMatch      = "find_match"
	TypeCancelMatch    = "cancel_match"
	TypeAcceptMatch    = "accept_match"
	TypeDeclineMatch   = "decline_match"
	TypeMessage        = "message"
	TypeTyping         = "typing"
	TypeEndChat        = "end_chat"
	TypeReport         = "report"
	TypePing           = "ping"
)

// Server -> Client message types.
const (
	TypeSessionCreated  = "session_created"
	TypeMatchingStarted = "matching_started"
	TypeMatchFound      = "match_found"
	TypeMatchAccepted   = "match_accepted"
	TypeMatchDeclined   = "match_declined"
	TypeMatchTimeout    = "match_timeout"
	TypePartnerLeft     = "partner_left"
	TypeRateLimited     = "rate_limited"
	TypeBanned          = "banned"
	TypeError           = "error"
	TypePong            = "pong"
)

// ---------------------------------------------------------------------------
// Envelope — used for initial JSON parsing to extract the type discriminator.
// ---------------------------------------------------------------------------

// Envelope holds the message type and the raw JSON payload for deferred
// parsing into a concrete struct.
type Envelope struct {
	Type string          `json:"type"`
	Raw  json.RawMessage `json:"-"`
}

// UnmarshalJSON implements the json.Unmarshaler interface. It captures the
// full raw bytes and extracts only the "type" field so that the rest of the
// payload can be decoded later into the appropriate concrete struct.
func (e *Envelope) UnmarshalJSON(data []byte) error {
	// Capture the full raw message for deferred parsing.
	e.Raw = make(json.RawMessage, len(data))
	copy(e.Raw, data)

	// Extract only the type field.
	var partial struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &partial); err != nil {
		return fmt.Errorf("protocol: failed to unmarshal envelope: %w", err)
	}
	if partial.Type == "" {
		return fmt.Errorf("protocol: missing or empty \"type\" field")
	}
	e.Type = partial.Type
	return nil
}

// ---------------------------------------------------------------------------
// Client -> Server message structs
// ---------------------------------------------------------------------------

// SetFingerprintMsg is sent by the client to associate a browser fingerprint
// hash with the current session for ban enforcement.
type SetFingerprintMsg struct {
	Type        string `json:"type"`
	Fingerprint string `json:"fingerprint"`
}

// FindMatchMsg is sent by the client to enter the matching queue with optional
// interest tags.
type FindMatchMsg struct {
	Type      string   `json:"type"`
	Interests []string `json:"interests"`
}

// CancelMatchMsg is sent by the client to leave the matching queue.
type CancelMatchMsg struct {
	Type string `json:"type"`
}

// AcceptMatchMsg is sent by the client to accept a proposed match.
type AcceptMatchMsg struct {
	Type   string `json:"type"`
	ChatID string `json:"chat_id"`
}

// DeclineMatchMsg is sent by the client to decline a proposed match.
type DeclineMatchMsg struct {
	Type   string `json:"type"`
	ChatID string `json:"chat_id"`
}

// ChatMsg is a text message sent by the client within a chat session.
type ChatMsg struct {
	Type   string `json:"type"`
	ChatID string `json:"chat_id"`
	Text   string `json:"text"`
}

// TypingMsg indicates whether the client is currently typing.
type TypingMsg struct {
	Type     string `json:"type"`
	ChatID   string `json:"chat_id"`
	IsTyping bool   `json:"is_typing"`
}

// EndChatMsg is sent by the client to end a chat session.
type EndChatMsg struct {
	Type   string `json:"type"`
	ChatID string `json:"chat_id"`
}

// ReportMsg is sent by the client to report the chat partner.
type ReportMsg struct {
	Type   string `json:"type"`
	ChatID string `json:"chat_id"`
	Reason string `json:"reason"`
}

// PingMsg is a client-initiated keepalive ping.
type PingMsg struct {
	Type string `json:"type"`
}

// ---------------------------------------------------------------------------
// Server -> Client message structs
// ---------------------------------------------------------------------------

// SessionCreatedMsg is sent by the server when a new session is established.
type SessionCreatedMsg struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
}

// MatchingStartedMsg is sent by the server to confirm the client has entered
// the matching queue.
type MatchingStartedMsg struct {
	Type    string `json:"type"`
	Timeout int    `json:"timeout"`
}

// MatchFoundMsg is sent by the server when a compatible partner has been found.
type MatchFoundMsg struct {
	Type            string   `json:"type"`
	ChatID          string   `json:"chat_id"`
	SharedInterests []string `json:"shared_interests"`
	AcceptDeadline  int      `json:"accept_deadline"`
}

// MatchAcceptedMsg is sent by the server when both parties have accepted the
// match and the chat session is ready.
type MatchAcceptedMsg struct {
	Type   string `json:"type"`
	ChatID string `json:"chat_id"`
}

// MatchDeclinedMsg is sent by the server when the partner declined the match.
type MatchDeclinedMsg struct {
	Type string `json:"type"`
}

// MatchTimeoutMsg is sent by the server when the matching queue timed out
// without finding a partner.
type MatchTimeoutMsg struct {
	Type string `json:"type"`
}

// ServerChatMsg is a text message relayed from the partner by the server.
type ServerChatMsg struct {
	Type string `json:"type"`
	From string `json:"from"`
	Text string `json:"text"`
	Ts   int64  `json:"ts"`
}

// ServerTypingMsg relays the partner's typing indicator to the client.
type ServerTypingMsg struct {
	Type     string `json:"type"`
	IsTyping bool   `json:"is_typing"`
}

// PartnerLeftMsg is sent by the server when the chat partner has disconnected
// or ended the chat.
type PartnerLeftMsg struct {
	Type string `json:"type"`
}

// RateLimitedMsg is sent by the server when the client has been rate-limited.
type RateLimitedMsg struct {
	Type       string `json:"type"`
	RetryAfter int    `json:"retry_after"`
}

// BannedMsg is sent by the server when the client has been banned.
type BannedMsg struct {
	Type     string `json:"type"`
	Duration int    `json:"duration"`
	Reason   string `json:"reason"`
}

// ErrorMsg is sent by the server to communicate an error condition.
type ErrorMsg struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// PongMsg is the server's response to a client ping.
type PongMsg struct {
	Type string `json:"type"`
}

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

// ParseClientMessage parses raw WebSocket bytes into a typed client message.
// It returns the message type string, the decoded struct, and any error
// encountered during parsing. An error is returned for unknown or
// server-only message types.
func ParseClientMessage(data []byte) (string, interface{}, error) {
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return "", nil, fmt.Errorf("protocol: failed to parse message: %w", err)
	}

	var (
		msg interface{}
		err error
	)

	switch env.Type {
	case TypeSetFingerprint:
		var m SetFingerprintMsg
		err = json.Unmarshal(env.Raw, &m)
		msg = m
	case TypeFindMatch:
		var m FindMatchMsg
		err = json.Unmarshal(env.Raw, &m)
		msg = m
	case TypeCancelMatch:
		var m CancelMatchMsg
		err = json.Unmarshal(env.Raw, &m)
		msg = m
	case TypeAcceptMatch:
		var m AcceptMatchMsg
		err = json.Unmarshal(env.Raw, &m)
		msg = m
	case TypeDeclineMatch:
		var m DeclineMatchMsg
		err = json.Unmarshal(env.Raw, &m)
		msg = m
	case TypeMessage:
		var m ChatMsg
		err = json.Unmarshal(env.Raw, &m)
		msg = m
	case TypeTyping:
		var m TypingMsg
		err = json.Unmarshal(env.Raw, &m)
		msg = m
	case TypeEndChat:
		var m EndChatMsg
		err = json.Unmarshal(env.Raw, &m)
		msg = m
	case TypeReport:
		var m ReportMsg
		err = json.Unmarshal(env.Raw, &m)
		msg = m
	case TypePing:
		var m PingMsg
		err = json.Unmarshal(env.Raw, &m)
		msg = m
	default:
		return env.Type, nil, fmt.Errorf("protocol: unknown client message type: %q", env.Type)
	}

	if err != nil {
		return env.Type, nil, fmt.Errorf("protocol: failed to decode %q payload: %w", env.Type, err)
	}
	return env.Type, msg, nil
}

// NewServerMessage creates a JSON-encoded byte slice for a server message.
// The msgType is injected into the payload under the "type" key. The payload
// should be one of the Server*Msg structs; this function marshals it to JSON,
// injects the type field, and returns the final bytes.
func NewServerMessage(msgType string, payload interface{}) ([]byte, error) {
	// Marshal the payload struct to a generic map so we can ensure the "type"
	// field is present and correct.
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("protocol: failed to marshal payload: %w", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("protocol: failed to unmarshal payload into map: %w", err)
	}

	m["type"] = msgType

	out, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("protocol: failed to marshal server message: %w", err)
	}
	return out, nil
}
