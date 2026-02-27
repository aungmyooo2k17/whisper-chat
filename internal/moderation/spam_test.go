package moderation

import "testing"

// TestSpam_URLs verifies that common URL formats are blocked.
func TestSpam_URLs(t *testing.T) {
	f := NewFilterWithTerms(nil) // no keyword blocklist — isolate spam checks

	tests := []struct {
		name    string
		input   string
		blocked bool
		term    string
	}{
		{"http url", "check out http://evil.com", true, "url"},
		{"https url", "visit https://spam.xyz/click", true, "url"},
		{"www url", "go to www.phishing.net", true, "url"},
		{"bare domain with path", "visit evil.com/free", true, "url"},
		{"bare domain .org path", "see example.org/page", true, "url"},
		{"bare domain .io path", "check app.io/signup", true, "url"},
		{"bare domain .ru path", "go to site.ru/malware", true, "url"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := f.Check(tt.input)
			if result.Blocked != tt.blocked {
				t.Errorf("Check(%q).Blocked = %v, want %v", tt.input, result.Blocked, tt.blocked)
			}
			if tt.blocked && result.Term != tt.term {
				t.Errorf("Check(%q).Term = %q, want %q", tt.input, result.Term, tt.term)
			}
			if tt.blocked && result.Reason != "spam_pattern" {
				t.Errorf("Check(%q).Reason = %q, want %q", tt.input, result.Reason, "spam_pattern")
			}
		})
	}
}

// TestSpam_PhoneNumbers verifies that common phone number formats are blocked.
func TestSpam_PhoneNumbers(t *testing.T) {
	f := NewFilterWithTerms(nil)

	tests := []struct {
		name    string
		input   string
		blocked bool
		term    string
	}{
		{"intl dashed", "+1-555-123-4567", true, "phone"},
		{"parenthesized area code", "(555) 123-4567", true, "phone"},
		{"dotted format", "555.123.4567", true, "phone"},
		{"spaced format", "555 123 4567", true, "phone"},
		{"in sentence", "call me at 555-123-4567 okay?", true, "phone"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := f.Check(tt.input)
			if result.Blocked != tt.blocked {
				t.Errorf("Check(%q).Blocked = %v, want %v", tt.input, result.Blocked, tt.blocked)
			}
			if tt.blocked && result.Term != tt.term {
				t.Errorf("Check(%q).Term = %q, want %q", tt.input, result.Term, tt.term)
			}
		})
	}
}

// TestSpam_CharFlood verifies that repeated character flooding is blocked.
func TestSpam_CharFlood(t *testing.T) {
	f := NewFilterWithTerms(nil)

	tests := []struct {
		name    string
		input   string
		blocked bool
		term    string
	}{
		{"repeated o in word", "hellooooooo", true, "char_flood"},
		{"repeated A", "AAAAAA", true, "char_flood"},
		{"repeated exclamation", "wow!!!!!", true, "char_flood"},
		{"repeated equals", "=====", true, "char_flood"},
		{"four chars ok", "heeeel no", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := f.Check(tt.input)
			if result.Blocked != tt.blocked {
				t.Errorf("Check(%q).Blocked = %v, want %v", tt.input, result.Blocked, tt.blocked)
			}
			if tt.blocked && result.Term != tt.term {
				t.Errorf("Check(%q).Term = %q, want %q", tt.input, result.Term, tt.term)
			}
		})
	}
}

// TestSpam_WordFlood verifies that repeated word flooding is blocked.
func TestSpam_WordFlood(t *testing.T) {
	f := NewFilterWithTerms(nil)

	tests := []struct {
		name    string
		input   string
		blocked bool
		term    string
	}{
		{"buy x3", "buy buy buy", true, "word_flood"},
		{"spam x4", "spam spam spam spam", true, "word_flood"},
		{"in sentence", "hey buy buy buy now", true, "word_flood"},
		{"case insensitive", "BUY buy Buy", true, "word_flood"},
		{"two repeats ok", "go go", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := f.Check(tt.input)
			if result.Blocked != tt.blocked {
				t.Errorf("Check(%q).Blocked = %v, want %v", tt.input, result.Blocked, tt.blocked)
			}
			if tt.blocked && result.Term != tt.term {
				t.Errorf("Check(%q).Term = %q, want %q", tt.input, result.Term, tt.term)
			}
		})
	}
}

// TestSpam_CleanMessages ensures normal messages are NOT flagged as spam.
func TestSpam_CleanMessages(t *testing.T) {
	f := NewFilterWithTerms(nil)

	clean := []struct {
		name  string
		input string
	}{
		{"short number", "I have 3 cats"},
		{"medium number", "My score is 100"},
		{"casual chat", "lol that's cool"},
		{"version string", "upgrade to v2.0"},
		{"decimal number", "pi is about 3.14"},
		{"normal sentence", "how are you doing today?"},
		{"multiple short nums", "I got 42 out of 50"},
		{"year reference", "see you in 2025"},
		{"temperature", "it's 72 degrees outside"},
		{"empty string", ""},
		{"single word", "hello"},
		{"two words", "hi there"},
		{"normal excitement", "wow!!! that's great!!"},
		{"repeated letters short", "sooo cool"},
		{"double word ok", "yeah yeah whatever"},
		{"dot in sentence", "ok. sure. fine."},
		{"email-like but not url", "contact support please"},
		{"money amount", "it costs $5.99"},
	}

	for _, tt := range clean {
		t.Run(tt.name, func(t *testing.T) {
			result := f.Check(tt.input)
			if result.Blocked {
				t.Errorf("Check(%q) was blocked (reason=%q, term=%q), expected clean",
					tt.input, result.Reason, result.Term)
			}
		})
	}
}

// TestSpam_IntegrationWithKeywordFilter ensures spam checks work alongside
// the keyword blocklist — a blocked keyword should still be caught first.
func TestSpam_IntegrationWithKeywordFilter(t *testing.T) {
	f := NewFilterWithTerms([]string{"badword"})

	// Keyword match takes priority over spam pattern.
	result := f.Check("badword")
	if !result.Blocked {
		t.Fatal("expected blocked for keyword")
	}
	if result.Reason != "blocked_keyword" {
		t.Errorf("Reason = %q, want %q", result.Reason, "blocked_keyword")
	}

	// Spam pattern still works when keyword is clean.
	result = f.Check("visit http://evil.com")
	if !result.Blocked {
		t.Fatal("expected blocked for URL")
	}
	if result.Reason != "spam_pattern" {
		t.Errorf("Reason = %q, want %q", result.Reason, "spam_pattern")
	}
	if result.Term != "url" {
		t.Errorf("Term = %q, want %q", result.Term, "url")
	}
}

// TestSpam_EdgeCases covers boundary conditions.
func TestSpam_EdgeCases(t *testing.T) {
	f := NewFilterWithTerms(nil)

	tests := []struct {
		name    string
		input   string
		blocked bool
	}{
		{"empty", "", false},
		{"single char", "a", false},
		{"spaces only", "   ", false},
		{"exactly 4 repeated chars", "aaaa", false},
		{"exactly 5 repeated chars", "aaaaa", true},
		{"newlines", "hello\nworld", false},
		{"tabs", "hello\tworld", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := f.Check(tt.input)
			if result.Blocked != tt.blocked {
				t.Errorf("Check(%q).Blocked = %v, want %v (reason=%q, term=%q)",
					tt.input, result.Blocked, tt.blocked, result.Reason, result.Term)
			}
		})
	}
}
