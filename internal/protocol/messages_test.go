package protocol

import (
	"encoding/json"
	"testing"
)

// ---------------------------------------------------------------------------
// Test: Parsing a valid find_match message
// ---------------------------------------------------------------------------

func TestParseClientMessage_FindMatch(t *testing.T) {
	input := []byte(`{"type":"find_match","interests":["music","gaming","anime"]}`)

	msgType, msg, err := ParseClientMessage(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msgType != TypeFindMatch {
		t.Fatalf("expected type %q, got %q", TypeFindMatch, msgType)
	}

	fm, ok := msg.(FindMatchMsg)
	if !ok {
		t.Fatalf("expected FindMatchMsg, got %T", msg)
	}
	if len(fm.Interests) != 3 {
		t.Fatalf("expected 3 interests, got %d", len(fm.Interests))
	}
	expected := []string{"music", "gaming", "anime"}
	for i, v := range expected {
		if fm.Interests[i] != v {
			t.Errorf("interest[%d]: expected %q, got %q", i, v, fm.Interests[i])
		}
	}
}

// ---------------------------------------------------------------------------
// Test: Parsing a valid message (chat) message
// ---------------------------------------------------------------------------

func TestParseClientMessage_ChatMsg(t *testing.T) {
	input := []byte(`{"type":"message","chat_id":"abc-123","text":"Hello!"}`)

	msgType, msg, err := ParseClientMessage(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msgType != TypeMessage {
		t.Fatalf("expected type %q, got %q", TypeMessage, msgType)
	}

	cm, ok := msg.(ChatMsg)
	if !ok {
		t.Fatalf("expected ChatMsg, got %T", msg)
	}
	if cm.ChatID != "abc-123" {
		t.Errorf("expected chat_id %q, got %q", "abc-123", cm.ChatID)
	}
	if cm.Text != "Hello!" {
		t.Errorf("expected text %q, got %q", "Hello!", cm.Text)
	}
}

// ---------------------------------------------------------------------------
// Test: Creating a match_found server message
// ---------------------------------------------------------------------------

func TestNewServerMessage_MatchFound(t *testing.T) {
	payload := MatchFoundMsg{
		ChatID:          "uuid-456",
		SharedInterests: []string{"music", "gaming"},
		AcceptDeadline:  15,
	}

	data, err := NewServerMessage(TypeMatchFound, payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Decode back and verify structure.
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}

	if result["type"] != TypeMatchFound {
		t.Errorf("expected type %q, got %v", TypeMatchFound, result["type"])
	}
	if result["chat_id"] != "uuid-456" {
		t.Errorf("expected chat_id %q, got %v", "uuid-456", result["chat_id"])
	}

	interests, ok := result["shared_interests"].([]interface{})
	if !ok {
		t.Fatalf("expected shared_interests to be an array, got %T", result["shared_interests"])
	}
	if len(interests) != 2 {
		t.Fatalf("expected 2 shared interests, got %d", len(interests))
	}
	if interests[0] != "music" || interests[1] != "gaming" {
		t.Errorf("unexpected shared interests: %v", interests)
	}

	deadline, ok := result["accept_deadline"].(float64)
	if !ok {
		t.Fatalf("expected accept_deadline to be a number, got %T", result["accept_deadline"])
	}
	if int(deadline) != 15 {
		t.Errorf("expected accept_deadline 15, got %v", deadline)
	}
}

// ---------------------------------------------------------------------------
// Test: Parsing an unknown message type returns an error
// ---------------------------------------------------------------------------

func TestParseClientMessage_UnknownType(t *testing.T) {
	input := []byte(`{"type":"unknown_type","data":"something"}`)

	msgType, msg, err := ParseClientMessage(input)
	if err == nil {
		t.Fatal("expected an error for unknown message type, got nil")
	}
	if msg != nil {
		t.Errorf("expected nil message for unknown type, got %v", msg)
	}
	if msgType != "unknown_type" {
		t.Errorf("expected returned type %q, got %q", "unknown_type", msgType)
	}
}

// ---------------------------------------------------------------------------
// Test: Round-trip fidelity (marshal -> unmarshal)
// ---------------------------------------------------------------------------

func TestRoundTrip_FindMatch(t *testing.T) {
	original := FindMatchMsg{
		Type:      TypeFindMatch,
		Interests: []string{"music", "gaming"},
	}

	// Marshal to JSON.
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	// Parse back through the protocol parser.
	msgType, msg, err := ParseClientMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msgType != TypeFindMatch {
		t.Fatalf("expected type %q, got %q", TypeFindMatch, msgType)
	}

	decoded, ok := msg.(FindMatchMsg)
	if !ok {
		t.Fatalf("expected FindMatchMsg, got %T", msg)
	}
	if decoded.Type != original.Type {
		t.Errorf("type mismatch: expected %q, got %q", original.Type, decoded.Type)
	}
	if len(decoded.Interests) != len(original.Interests) {
		t.Fatalf("interests length mismatch: expected %d, got %d", len(original.Interests), len(decoded.Interests))
	}
	for i := range original.Interests {
		if decoded.Interests[i] != original.Interests[i] {
			t.Errorf("interest[%d] mismatch: expected %q, got %q", i, original.Interests[i], decoded.Interests[i])
		}
	}
}

func TestRoundTrip_ServerMessage(t *testing.T) {
	original := MatchFoundMsg{
		Type:            TypeMatchFound,
		ChatID:          "test-uuid",
		SharedInterests: []string{"anime"},
		AcceptDeadline:  10,
	}

	// Create server message bytes.
	data, err := NewServerMessage(TypeMatchFound, original)
	if err != nil {
		t.Fatalf("failed to create server message: %v", err)
	}

	// Unmarshal back into the struct.
	var decoded MatchFoundMsg
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Type != TypeMatchFound {
		t.Errorf("type mismatch: expected %q, got %q", TypeMatchFound, decoded.Type)
	}
	if decoded.ChatID != original.ChatID {
		t.Errorf("chat_id mismatch: expected %q, got %q", original.ChatID, decoded.ChatID)
	}
	if decoded.AcceptDeadline != original.AcceptDeadline {
		t.Errorf("accept_deadline mismatch: expected %d, got %d", original.AcceptDeadline, decoded.AcceptDeadline)
	}
	if len(decoded.SharedInterests) != len(original.SharedInterests) {
		t.Fatalf("shared_interests length mismatch: expected %d, got %d", len(original.SharedInterests), len(decoded.SharedInterests))
	}
	for i := range original.SharedInterests {
		if decoded.SharedInterests[i] != original.SharedInterests[i] {
			t.Errorf("shared_interests[%d] mismatch: expected %q, got %q", i, original.SharedInterests[i], decoded.SharedInterests[i])
		}
	}
}

// ---------------------------------------------------------------------------
// Test: Envelope UnmarshalJSON edge cases
// ---------------------------------------------------------------------------

func TestEnvelope_MissingType(t *testing.T) {
	input := []byte(`{"data":"no type field"}`)
	var env Envelope
	if err := json.Unmarshal(input, &env); err == nil {
		t.Fatal("expected error for missing type field, got nil")
	}
}

func TestEnvelope_InvalidJSON(t *testing.T) {
	input := []byte(`{invalid json}`)
	var env Envelope
	if err := json.Unmarshal(input, &env); err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// ---------------------------------------------------------------------------
// Test: Parsing all client message types succeeds
// ---------------------------------------------------------------------------

func TestParseClientMessage_AllTypes(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		wantType string
	}{
		{"find_match", `{"type":"find_match","interests":["music"]}`, TypeFindMatch},
		{"cancel_match", `{"type":"cancel_match"}`, TypeCancelMatch},
		{"accept_match", `{"type":"accept_match","chat_id":"id1"}`, TypeAcceptMatch},
		{"decline_match", `{"type":"decline_match","chat_id":"id1"}`, TypeDeclineMatch},
		{"message", `{"type":"message","chat_id":"id1","text":"hi"}`, TypeMessage},
		{"typing", `{"type":"typing","chat_id":"id1","is_typing":true}`, TypeTyping},
		{"end_chat", `{"type":"end_chat","chat_id":"id1"}`, TypeEndChat},
		{"report", `{"type":"report","chat_id":"id1","reason":"spam"}`, TypeReport},
		{"ping", `{"type":"ping"}`, TypePing},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msgType, msg, err := ParseClientMessage([]byte(tc.input))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if msgType != tc.wantType {
				t.Errorf("expected type %q, got %q", tc.wantType, msgType)
			}
			if msg == nil {
				t.Error("expected non-nil message")
			}
		})
	}
}
