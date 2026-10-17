package util

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// OneLine makes free text safe to show or record in one line: URL credentials
// masked (RedactURLCredentials), every whitespace run (line breaks included)
// folded into one space, and, when maxLen > 0, cut to at most maxLen bytes on
// a rune boundary with "…" appended. It is the shared sanitizer for reasons
// recorded by gt (merge-queue hold reasons, review_only clear reasons).
func OneLine(text string, maxLen int) string {
	text = strings.Join(strings.Fields(RedactURLCredentials(text)), " ")
	if maxLen > 0 && len(text) > maxLen {
		cut := maxLen
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text = text[:cut] + "…"
	}
	return text
}

// HasControlChars reports whether text contains a control character (line
// breaks and tabs included).
func HasControlChars(text string) bool {
	return strings.IndexFunc(text, unicode.IsControl) >= 0
}
