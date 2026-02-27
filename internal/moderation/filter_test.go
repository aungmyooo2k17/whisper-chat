package moderation

import (
	"strings"
	"testing"
	"time"
)

func TestNewFilter(t *testing.T) {
	f := NewFilter()
	if f == nil {
		t.Fatal("NewFilter returned nil")
	}
	if len(f.words) == 0 && len(f.phrases) == 0 {
		t.Fatal("NewFilter created an empty filter")
	}
}

func TestCheck_BlockedSingleWord(t *testing.T) {
	f := NewFilterWithTerms([]string{"badword", "offensive"})

	tests := []struct {
		name    string
		input   string
		blocked bool
		term    string
	}{
		{"exact match", "badword", true, "badword"},
		{"in sentence", "this is badword here", true, "badword"},
		{"case insensitive", "BADWORD", true, "badword"},
		{"mixed case", "BaDwOrD", true, "badword"},
		{"with punctuation", "hello, badword!", true, "badword"},
		{"clean message", "hello world", false, ""},
		{"partial match no block", "badwording is fine", false, ""},
		{"substring no block", "mybadword", false, ""},
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
			if tt.blocked && result.Reason != "blocked_keyword" {
				t.Errorf("Check(%q).Reason = %q, want %q", tt.input, result.Reason, "blocked_keyword")
			}
		})
	}
}

func TestCheck_BlockedPhrase(t *testing.T) {
	f := NewFilterWithTerms([]string{"kill yourself", "go die"})

	tests := []struct {
		name    string
		input   string
		blocked bool
		term    string
	}{
		{"exact phrase", "kill yourself", true, "kill yourself"},
		{"phrase in sentence", "you should kill yourself now", true, "kill yourself"},
		{"case insensitive phrase", "KILL YOURSELF", true, "kill yourself"},
		{"partial word no match", "kill yourselves", false, ""},
		{"words separated", "kill and yourself", false, ""},
		{"go die phrase", "go die already", true, "go die"},
		{"clean message", "i love this chat", false, ""},
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

func TestCheck_Leetspeak(t *testing.T) {
	f := NewFilterWithTerms([]string{"badword", "offensive"})

	tests := []struct {
		name    string
		input   string
		blocked bool
	}{
		{"zero for o", "b@dw0rd", true},
		{"at for a", "b@dword", true},
		{"dollar for s", "off3n$ive", true},
		{"one for i", "offens1ve", true},
		{"exclaim for i", "offens!ve", true},
		{"mixed leet", "0ff3n$!v3", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := f.Check(tt.input)
			if result.Blocked != tt.blocked {
				t.Errorf("Check(%q).Blocked = %v, want %v", tt.input, result.Blocked, tt.blocked)
			}
		})
	}
}

func TestCheck_CleanMessages(t *testing.T) {
	f := NewFilter()

	messages := []string{
		"hello, how are you?",
		"nice weather today",
		"what are your hobbies?",
		"I love programming",
		"do you like music?",
		"let's talk about movies",
		"what class are you in?",
		"I need to assess the situation",
		"the grape harvest was great",
		"",
	}

	for _, msg := range messages {
		result := f.Check(msg)
		if result.Blocked {
			t.Errorf("Check(%q) was blocked (term=%q), expected clean", msg, result.Term)
		}
	}
}

func TestCheck_DefaultBlocklist(t *testing.T) {
	f := NewFilter()

	// Verify a few terms from each category are actually blocked.
	blocked := []string{
		"nigger",
		"faggot",
		"kill yourself",
		"child porn",
		"send nudes",
		"heil hitler",
		"bomb threat",
		"free bitcoin",
	}

	for _, term := range blocked {
		result := f.Check(term)
		if !result.Blocked {
			t.Errorf("Check(%q) was not blocked, expected blocked", term)
		}
	}
}

func TestCheckInterests(t *testing.T) {
	f := NewFilterWithTerms([]string{"badword", "kill yourself"})

	interests := []string{"music", "badword", "movies", "programming"}
	clean := f.CheckInterests(interests)

	if len(clean) != 3 {
		t.Fatalf("CheckInterests returned %d items, want 3", len(clean))
	}

	expected := []string{"music", "movies", "programming"}
	for i, want := range expected {
		if clean[i] != want {
			t.Errorf("clean[%d] = %q, want %q", i, clean[i], want)
		}
	}
}

func TestCheckInterests_AllClean(t *testing.T) {
	f := NewFilterWithTerms([]string{"badword"})

	interests := []string{"music", "movies"}
	clean := f.CheckInterests(interests)

	if len(clean) != 2 {
		t.Fatalf("CheckInterests returned %d items, want 2", len(clean))
	}
}

func TestCheckInterests_Empty(t *testing.T) {
	f := NewFilterWithTerms([]string{"badword"})

	clean := f.CheckInterests(nil)
	if len(clean) != 0 {
		t.Fatalf("CheckInterests(nil) returned %d items, want 0", len(clean))
	}

	clean = f.CheckInterests([]string{})
	if len(clean) != 0 {
		t.Fatalf("CheckInterests([]) returned %d items, want 0", len(clean))
	}
}

func TestNewFilterWithTerms_EmptyAndWhitespace(t *testing.T) {
	f := NewFilterWithTerms([]string{"", "  ", "valid"})

	if _, ok := f.words["valid"]; !ok {
		t.Error("expected 'valid' in words set")
	}
	if len(f.words) != 1 {
		t.Errorf("expected 1 word, got %d", len(f.words))
	}
}

func TestNormalizeLeet(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{"h3ll0", "hello"},
		{"@ss", "ass"},
		{"$h!t", "shit"},
		{"upper", "upper"},
		{"n0", "no"},
		{"ch@ng3", "change"},
	}

	for _, tt := range tests {
		got := normalizeLeet(tt.input)
		if got != tt.want {
			t.Errorf("normalizeLeet(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestTokenizePlain(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"hello world", []string{"hello", "world"}},
		{"hello, world!", []string{"hello", "world"}},
		{"  spaced  out  ", []string{"spaced", "out"}},
		{"one", []string{"one"}},
		{"", nil},
		{"hello---world", []string{"hello", "world"}},
	}

	for _, tt := range tests {
		got := tokenizePlain(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("tokenizePlain(%q) = %v (len %d), want %v (len %d)", tt.input, got, len(got), tt.want, len(tt.want))
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("tokenizePlain(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

func TestTokenizeLeet(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"hello world", []string{"hello", "world"}},
		{"b@dw0rd", []string{"b@dw0rd"}},
		{"hello $h!t bye", []string{"hello", "$h!t", "bye"}},
	}

	for _, tt := range tests {
		got := tokenizeLeet(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("tokenizeLeet(%q) = %v (len %d), want %v (len %d)", tt.input, got, len(got), tt.want, len(tt.want))
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("tokenizeLeet(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

// BenchmarkCheck measures filter performance to ensure < 0.1ms per message.
func BenchmarkCheck(b *testing.B) {
	f := NewFilter()
	msg := "hey how are you doing today? I love chatting about music and movies. What are your favorite hobbies?"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.Check(msg)
	}
}

// BenchmarkCheck_Blocked measures performance when a blocked term is found.
func BenchmarkCheck_Blocked(b *testing.B) {
	f := NewFilter()
	msg := "this message contains a nigger slur and should be blocked"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.Check(msg)
	}
}

// BenchmarkCheck_LongMessage measures performance on longer messages.
func BenchmarkCheck_LongMessage(b *testing.B) {
	f := NewFilter()
	msg := strings.Repeat("this is a perfectly normal message with no bad content. ", 40)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.Check(msg)
	}
}

// TestPerformance verifies the filter meets the < 0.1ms latency requirement.
func TestPerformance(t *testing.T) {
	f := NewFilter()
	msg := "hey how are you doing today? I love chatting about music and movies. What are your favorite hobbies?"

	const iterations = 1000
	start := time.Now()
	for i := 0; i < iterations; i++ {
		f.Check(msg)
	}
	elapsed := time.Since(start)
	avgNs := elapsed.Nanoseconds() / int64(iterations)
	avgUs := float64(avgNs) / 1000.0

	t.Logf("average Check latency: %.2f µs (%.4f ms)", avgUs, avgUs/1000.0)

	// 0.1ms = 100µs = 100,000ns (relaxed to 1ms under race detector).
	maxNs := int64(100_000)
	if raceDetectorEnabled {
		maxNs = 1_000_000 // race detector adds ~10-50x overhead
	}
	if avgNs > maxNs {
		t.Errorf("Check latency %.2f µs exceeds %d µs limit", avgUs, maxNs/1000)
	}
}
