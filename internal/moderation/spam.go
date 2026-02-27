package moderation

import (
	"regexp"
	"strings"
	"unicode"
)

// Compiled regex patterns for spam detection.
// These are compiled once at package init and reused for every call,
// making them safe and efficient for concurrent use.
var (
	// urlPattern matches http/https URLs, www. URLs, and common TLD patterns.
	// The bare-domain variant requires a trailing "/" to avoid false positives
	// on version strings like "v2.0" or decimal numbers like "3.14".
	urlPattern = regexp.MustCompile(`(?i)(https?://\S+|www\.\S+|\S+\.(com|net|org|io|co|xyz|info|biz|ru|cn|tk|ml|ga|cf)/\S*)`)

	// phonePattern matches various phone number formats such as:
	//   +1-555-123-4567, (555) 123-4567, 555.123.4567
	// Anchored to whitespace/string boundaries to avoid matching random digit
	// sequences embedded in normal words or short numbers like "100".
	phonePattern = regexp.MustCompile(`(?:^|\s)(\+?\d{1,3}[-.\s]?)?\(?\d{2,4}\)?[-.\s]?\d{3,4}[-.\s]?\d{3,4}(?:\s|$)`)
)

// spamCheck pairs a detection function with metadata used for reporting.
type spamCheck struct {
	name   string
	reason string
	match  func(string) bool
}

// spamChecks is the ordered list of spam checks applied by checkSpamPatterns.
// Order matters: the first match wins.
var spamChecks = []spamCheck{
	{name: "url", reason: "URLs are not allowed", match: func(text string) bool {
		return urlPattern.MatchString(text)
	}},
	{name: "phone", reason: "Phone numbers are not allowed", match: func(text string) bool {
		return phonePattern.MatchString(text)
	}},
	{name: "char_flood", reason: "Character flooding detected", match: hasCharFlood},
	{name: "word_flood", reason: "Repeated word flooding detected", match: hasWordFlood},
}

// hasCharFlood returns true if text contains 5 or more consecutive identical
// characters. Go's regexp package (RE2) does not support backreferences, so
// this is implemented as a simple linear scan which is both correct and fast.
func hasCharFlood(text string) bool {
	const threshold = 5

	count := 1
	prev := rune(-1)
	for _, r := range text {
		if r == prev {
			count++
			if count >= threshold {
				return true
			}
		} else {
			count = 1
			prev = r
		}
	}
	return false
}

// hasWordFlood returns true if the same word appears 3 or more times
// consecutively (case-insensitive). Words are delimited by whitespace.
// Go's regexp package (RE2) does not support backreferences, so this is
// implemented with a simple token scan.
func hasWordFlood(text string) bool {
	const threshold = 3

	words := strings.FieldsFunc(text, func(r rune) bool {
		return unicode.IsSpace(r)
	})
	if len(words) < threshold {
		return false
	}

	count := 1
	prev := ""
	for _, w := range words {
		lower := strings.ToLower(w)
		if lower == prev {
			count++
			if count >= threshold {
				return true
			}
		} else {
			count = 1
			prev = lower
		}
	}
	return false
}

// checkSpamPatterns runs every spam check against text and returns a blocking
// FilterResult on the first match. If no pattern matches, it returns a
// zero-value (non-blocking) FilterResult.
func (f *Filter) checkSpamPatterns(text string) FilterResult {
	for _, sc := range spamChecks {
		if sc.match(text) {
			return FilterResult{
				Blocked: true,
				Reason:  "spam_pattern",
				Term:    sc.name,
			}
		}
	}
	return FilterResult{}
}
