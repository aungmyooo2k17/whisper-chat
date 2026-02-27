package chat

import (
	"fmt"
	"unicode/utf8"
)

const (
	MaxMessageBytes = 4096 // 4KB max frame size
	MaxTextChars    = 2000 // max character count
)

// ValidateMessage checks that a chat message meets content requirements.
func ValidateMessage(text string) error {
	if len(text) == 0 {
		return fmt.Errorf("message text is empty")
	}
	if len(text) > MaxMessageBytes {
		return fmt.Errorf("message exceeds %d byte limit", MaxMessageBytes)
	}
	if utf8.RuneCountInString(text) > MaxTextChars {
		return fmt.Errorf("message exceeds %d character limit", MaxTextChars)
	}
	if !utf8.ValidString(text) {
		return fmt.Errorf("message contains invalid UTF-8")
	}
	return nil
}
