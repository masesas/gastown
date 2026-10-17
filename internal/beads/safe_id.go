package beads

import "regexp"

// safeIDPattern is the shape of an identifier (bead ID, MR ID, rig name) that
// may be put into SQL, a filesystem path segment, a copy-paste command or a
// bd command line: it starts with an ASCII letter or digit (so never "." or
// "..", and never a flag such as "--db=...") and contains
// only letters, digits, '.', '_' and '-' (no '/', whitespace or quotes).
var safeIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// IsSafeID reports whether s matches safeIDPattern. Wrapped
// external:<rig>:<id> references must be unwrapped with ExtractIssueID first.
func IsSafeID(s string) bool { return safeIDPattern.MatchString(s) }
