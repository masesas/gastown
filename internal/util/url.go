package util

import (
	"net/url"
	"regexp"
	"strings"
)

// RedactURL strips credentials from a URL for safe logging.
// "https://x-access-token:ghp_abc@github.com/org/repo" → "https://github.com/org/repo"
// SSH-style URLs (git@github.com:org/repo.git) are returned as-is.
// URLs that net/url can't parse but contain no credentials are returned as-is for debugging.
func RedactURL(rawURL string) string {
	// SSH-style URLs and non-standard transports don't use standard URL conventions.
	if !strings.Contains(rawURL, "://") {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		// Can't parse — return as-is if no credentials, otherwise mask it.
		if !strings.Contains(rawURL, "@") {
			return rawURL
		}
		return "<invalid URL>"
	}
	u.User = nil
	return u.String()
}

// urlCredentialsPattern matches the userinfo of a URL inside free text
// ("https://user:token@host" → "://user:token@").
var urlCredentialsPattern = regexp.MustCompile(`://[^/\s@]+@`)

// RedactURLCredentials masks URL credentials anywhere in free text (e.g. a git
// error message): "://user:token@" becomes "://***@". Use RedactURL for a
// value that is a single URL.
func RedactURLCredentials(text string) string {
	return urlCredentialsPattern.ReplaceAllString(text, "://***@")
}
