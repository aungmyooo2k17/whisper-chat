// Package moderation provides content filtering and moderation capabilities.
// It screens chat messages for prohibited content and enforces community
// guidelines before messages are delivered to recipients.
package moderation

import (
	"strings"
	"unicode"
)

// FilterResult describes the outcome of a content check.
type FilterResult struct {
	Blocked bool   // whether the message was blocked
	Reason  string // which rule triggered (e.g., "blocked_keyword", "spam_pattern")
	Term    string // the specific term/pattern that matched
}

// Filter performs in-memory content filtering against a blocklist of terms.
// It is safe for concurrent use by multiple goroutines — all state is
// read-only after construction.
type Filter struct {
	// words contains single-word blocked terms for O(1) lookup.
	words map[string]struct{}

	// phrases contains multi-word blocked terms checked via substring match
	// against the token-joined message.
	phrases []string
}

// NewFilter creates a Filter loaded with the default blocklist. All terms are
// normalized to lowercase. Single-word terms are stored in a hash set for fast
// lookup; multi-word terms are stored in a slice for substring matching.
func NewFilter() *Filter {
	return NewFilterWithTerms(defaultBlocklist)
}

// NewFilterWithTerms creates a Filter from the provided term list. This is
// useful for testing or for loading a custom blocklist.
func NewFilterWithTerms(terms []string) *Filter {
	f := &Filter{
		words: make(map[string]struct{}, len(terms)),
	}

	for _, term := range terms {
		normalized := strings.ToLower(strings.TrimSpace(term))
		if normalized == "" {
			continue
		}
		if strings.ContainsRune(normalized, ' ') {
			f.phrases = append(f.phrases, normalized)
		} else {
			f.words[normalized] = struct{}{}
		}
	}

	return f
}

// Check examines the provided text for prohibited content. It returns a
// FilterResult indicating whether the message should be blocked. The check
// is case-insensitive and applies basic leetspeak normalization.
//
// The check runs two passes:
//  1. Plain pass: tokenize on word boundaries (letters/digits), check each
//     token against the blocklist. This correctly handles "badword!" by
//     stripping trailing punctuation.
//  2. Leetspeak pass: tokenize including leet chars (@, $, !) as part of
//     tokens, then normalize (e.g., @ -> a, $ -> s). This catches evasion
//     like "b@dw0rd".
//
// For multi-word phrases, the space-joined token sequence is checked via
// substring matching in both passes.
func (f *Filter) Check(text string) FilterResult {
	lower := strings.ToLower(text)

	// --- Pass 1: plain word matching ---
	plainTokens := tokenizePlain(lower)
	if result := f.checkTokens(plainTokens); result.Blocked {
		return result
	}

	// --- Pass 2: leetspeak-aware matching ---
	leetTokens := tokenizeLeet(lower)
	normalized := make([]string, len(leetTokens))
	for i, t := range leetTokens {
		normalized[i] = normalizeLeet(t)
	}
	if result := f.checkTokens(normalized); result.Blocked {
		return result
	}

	// --- Pass 3: regex-based spam pattern detection ---
	if result := f.checkSpamPatterns(text); result.Blocked {
		return result
	}

	return FilterResult{Blocked: false}
}

// checkTokens checks a token slice against the word set and phrase list.
func (f *Filter) checkTokens(tokens []string) FilterResult {
	// Check individual words.
	for _, w := range tokens {
		if _, blocked := f.words[w]; blocked {
			return FilterResult{
				Blocked: true,
				Reason:  "blocked_keyword",
				Term:    w,
			}
		}
	}

	// Check multi-word phrases.
	joined := strings.Join(tokens, " ")
	for _, phrase := range f.phrases {
		if strings.Contains(joined, phrase) {
			return FilterResult{
				Blocked: true,
				Reason:  "blocked_keyword",
				Term:    phrase,
			}
		}
	}

	return FilterResult{Blocked: false}
}

// CheckInterests filters a slice of interest tags and returns a clean list
// with any blocked terms removed. This prevents offensive custom tags from
// being used in matching.
func (f *Filter) CheckInterests(interests []string) []string {
	clean := make([]string, 0, len(interests))
	for _, interest := range interests {
		result := f.Check(interest)
		if !result.Blocked {
			clean = append(clean, interest)
		}
	}
	return clean
}

// normalizeLeet applies basic leetspeak substitutions to a lowercase token
// to catch common evasion attempts (e.g., 0->o, @->a, $->s).
func normalizeLeet(text string) string {
	var b strings.Builder
	b.Grow(len(text))

	for _, r := range text {
		switch r {
		case '0':
			b.WriteRune('o')
		case '1':
			b.WriteRune('i')
		case '3':
			b.WriteRune('e')
		case '@':
			b.WriteRune('a')
		case '$':
			b.WriteRune('s')
		case '!':
			b.WriteRune('i')
		default:
			b.WriteRune(r)
		}
	}

	return b.String()
}

// tokenizePlain splits text into words using standard word boundaries.
// Only letters and digits are considered part of a word; everything else
// (spaces, punctuation, symbols) is a separator.
func tokenizePlain(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// leetChars contains characters used as leetspeak substitutions that should
// be treated as part of a word token in the leetspeak-aware pass.
var leetChars = map[rune]bool{
	'@': true, // a
	'$': true, // s
	'!': true, // i
}

// tokenizeLeet splits text into words, treating common leetspeak characters
// (@, $, !) as part of the token rather than separators.
func tokenizeLeet(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !leetChars[r]
	})
}
