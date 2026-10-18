package util

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// OneLine makes free text safe to show or record in one line: URL credentials
// masked (RedactURLCredentials), every whitespace run (line breaks included)
// folded into one space, every other control character (ESC, NUL, DEL, C1)
// removed, and, when maxLen > 0, cut to at most maxLen bytes on a rune
// boundary with "…" appended. It is the shared sanitizer for text recorded or
// relayed by gt (merge-queue hold reasons, review_only clear reasons,
// FIX_NEEDED errors).
func OneLine(text string, maxLen int) string {
	text = strings.Join(strings.Fields(RedactURLCredentials(text)), " ")
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, text)
	return TruncateUTF8(text, maxLen)
}

// TruncateUTF8 cuts text to at most maxLen bytes on a rune boundary and
// appends "…" when it cut anything. maxLen <= 0 returns text unchanged.
func TruncateUTF8(text string, maxLen int) string {
	if maxLen <= 0 || len(text) <= maxLen {
		return text
	}
	cut := maxLen
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "…"
}

// HasControlChars reports whether text contains a control character (line
// breaks and tabs included).
func HasControlChars(text string) bool {
	return strings.IndexFunc(text, unicode.IsControl) >= 0
}
