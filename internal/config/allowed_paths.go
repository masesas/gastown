package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
)

// ErrInvalidAllowedPath indicates a malformed merge_queue.allowed_paths pattern.
var ErrInvalidAllowedPath = errors.New("invalid merge_queue.allowed_paths pattern")

// globstar is the pattern segment that matches zero or more path segments.
const globstar = "**"

// ValidateAllowedPaths checks merge_queue.allowed_paths patterns.
//
// A pattern is a repository-relative, slash-separated path. Each segment is a
// path.Match pattern (*, ?, [...]); a segment that is exactly "**" matches zero
// or more whole segments. Rejected: empty patterns, surrounding whitespace, a
// leading "/", empty segments ("a//b", trailing "/"), "." or ".." segments,
// "**" mixed with other characters in one segment, and malformed brackets.
func ValidateAllowedPaths(patterns []string) error {
	for i, p := range patterns {
		if err := validateAllowedPath(p); err != nil {
			return fmt.Errorf("%w: allowed_paths[%d] %q: %v", ErrInvalidAllowedPath, i, p, err)
		}
	}
	return nil
}

func validateAllowedPath(p string) error {
	if strings.TrimSpace(p) == "" {
		return errors.New("empty pattern")
	}
	if strings.TrimSpace(p) != p {
		return errors.New("surrounding whitespace")
	}
	if strings.HasPrefix(p, "/") {
		return errors.New("must be repository-relative (no leading /)")
	}
	for _, seg := range strings.Split(p, "/") {
		switch {
		case seg == "":
			return errors.New("empty path segment")
		case seg == "." || seg == "..":
			return errors.New("'.' and '..' segments are not allowed")
		case seg == globstar:
			continue
		case strings.Contains(seg, globstar):
			return errors.New("'**' must be a whole path segment")
		}
		if _, err := path.Match(seg, ""); err != nil {
			return err
		}
	}
	return nil
}

// LoadMergeQueueAllowedPaths reads only merge_queue.allowed_paths from the
// rig settings file at settingsPath, independently of the rest of the file.
//
// It deliberately does not go through LoadRigSettings: an unrelated invalid
// field (newer version, bad poll_interval, ...) must not turn an unscoped rig
// into a blocked one, nor hide a declared scope. Results:
//   - file missing, no merge_queue, key absent or empty list: nil, nil
//     (unrestricted);
//   - file unreadable, not valid JSON, allowed_paths of the wrong type, or an
//     invalid pattern: error (fail closed, the declared scope is unknown).
func LoadMergeQueueAllowedPaths(settingsPath string) ([]string, error) {
	data, err := os.ReadFile(settingsPath) //nolint:gosec // G304: path is constructed internally
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", settingsPath, err)
	}
	var raw struct {
		MergeQueue *struct {
			AllowedPaths []string `json:"allowed_paths"`
		} `json:"merge_queue"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing merge_queue.allowed_paths in %s: %w", settingsPath, err)
	}
	if raw.MergeQueue == nil || len(raw.MergeQueue.AllowedPaths) == 0 {
		return nil, nil
	}
	if err := ValidateAllowedPaths(raw.MergeQueue.AllowedPaths); err != nil {
		return nil, err
	}
	return raw.MergeQueue.AllowedPaths, nil
}

// MatchAllowedPath reports whether the repository-relative, slash-separated
// path p matches pattern (syntax: see ValidateAllowedPaths). A malformed
// pattern matches nothing, so an unvalidated pattern fails closed.
func MatchAllowedPath(pattern, p string) bool {
	if validateAllowedPath(pattern) != nil {
		return false
	}
	return matchSegments(strings.Split(pattern, "/"), strings.Split(p, "/"))
}

func matchSegments(pattern, segs []string) bool {
	for len(pattern) > 0 {
		if pattern[0] == globstar {
			rest := pattern[1:]
			for i := 0; i <= len(segs); i++ {
				if matchSegments(rest, segs[i:]) {
					return true
				}
			}
			return false
		}
		if len(segs) == 0 {
			return false
		}
		if ok, err := path.Match(pattern[0], segs[0]); err != nil || !ok {
			return false
		}
		pattern, segs = pattern[1:], segs[1:]
	}
	return len(segs) == 0
}

// HasPathScope reports whether this merge queue restricts which paths an MR
// may change. A nil config or empty AllowedPaths means unrestricted.
func (c *MergeQueueConfig) HasPathScope() bool {
	return c != nil && len(c.AllowedPaths) > 0
}

// PathsOutsideScope returns, in input order, the changed paths that match
// none of AllowedPaths. It returns nil when every path is in scope or when the
// queue has no path scope (HasPathScope is false).
func (c *MergeQueueConfig) PathsOutsideScope(changed []string) []string {
	if !c.HasPathScope() {
		return nil
	}
	return PathsOutsideAllowed(c.AllowedPaths, changed)
}

// PathsOutsideAllowed returns, in input order, the paths that match none of
// the allowed patterns. An empty allowed list means unrestricted (nil result).
func PathsOutsideAllowed(allowed, changed []string) []string {
	if len(allowed) == 0 {
		return nil
	}
	var outside []string
	for _, p := range changed {
		if !matchesAny(allowed, p) {
			outside = append(outside, p)
		}
	}
	return outside
}

func matchesAny(patterns []string, p string) bool {
	for _, pattern := range patterns {
		if MatchAllowedPath(pattern, p) {
			return true
		}
	}
	return false
}
