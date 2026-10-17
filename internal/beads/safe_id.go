package beads

import (
	"regexp"
	"strings"
)

// safeIDPattern is the shape of an identifier (bead ID, MR ID, rig name) that
// may be put into SQL, a filesystem path segment, a copy-paste command or a
// bd command line: it starts with an ASCII letter or digit (so never "." or
// "..", and never a flag such as "--db=...") and contains
// only letters, digits, '.', '_' and '-' (no '/', whitespace or quotes).
var safeIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// IsSafeID reports whether s matches safeIDPattern. Wrapped
// external:<rig>:<id> references must be unwrapped with ExtractIssueID first.
func IsSafeID(s string) bool { return safeIDPattern.MatchString(s) }

// objectNamePattern is a hex git object name, abbreviated (7) up to SHA-256
// (64): the only commit_sha / base_sha values the landing proof hands to git.
var objectNamePattern = regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`)

// IsGitObjectName reports whether s matches objectNamePattern.
func IsGitObjectName(s string) bool { return objectNamePattern.MatchString(s) }

// IsSafeRefName reports whether s is a branch name the landing proof hands to
// git: '/'-separated segments that are each IsSafeID (so never an option,
// "..", whitespace or an empty segment).
func IsSafeRefName(s string) bool {
	if s == "" {
		return false
	}
	for _, seg := range strings.Split(s, "/") {
		if !IsSafeID(seg) {
			return false
		}
	}
	return true
}
