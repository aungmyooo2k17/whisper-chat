package moderation

// ModerationRequest is published to moderation.check by the WS server
// when a message needs async content review.
type ModerationRequest struct {
	SessionID string `json:"session_id"`
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	Ts        int64  `json:"ts"`
}

// ModerationResult is published back to the WS server with the review outcome.
type ModerationResult struct {
	SessionID string `json:"session_id"`
	ChatID    string `json:"chat_id"`
	Blocked   bool   `json:"blocked"`
	Reason    string `json:"reason"`
	Term      string `json:"term"`
}
