package beads

import (
	"regexp"

	"github.com/steveyegge/gastown/internal/refname"
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

// objectNamePattern is a full hex git object name, SHA-1 (40) or SHA-256
// (64): the only commit_sha / base_sha values the landing proof hands to git.
// gt done and gt mq submit always record full names (git rev-parse), so an
// abbreviated or otherwise shaped value was not written by them.
var objectNamePattern = regexp.MustCompile(`^(?:[0-9a-fA-F]{40}|[0-9a-fA-F]{64})$`)

// IsGitObjectName reports whether s matches objectNamePattern.
func IsGitObjectName(s string) bool { return objectNamePattern.MatchString(s) }

// IsSafeRefName reports whether s is a branch name gt hands to git (the
// landing proof, post-merge cleanup, abandon, archive refs, copy-paste
// commands). It is refname.Valid, the one ref-name rule: '/'-separated
// segments that start with a letter or digit (never an option or a forcing
// "+" refspec) and contain only letters, digits, '.', '_', '+', '@', '-',
// with no "..", "@{", ".lock" segment, trailing '.' or "HEAD" (a strict
// subset of git check-ref-format --branch). Real generated polecat branches
// (polecat/<name>/<issue>+<suffix>) pass.
func IsSafeRefName(s string) bool { return refname.Valid(s) }
