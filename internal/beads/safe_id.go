package beads

import "regexp"

// safeIDPattern is the shape of an identifier (bead ID, MR ID, rig name) that
// may be put into SQL, a filesystem path segment or a copy-paste command: it
// starts with an ASCII letter or digit (so never "." or "..") and contains
// only letters, digits, '.', '_' and '-' (no '/', whitespace or quotes).
var safeIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// IsSafeID reports whether s matches safeIDPattern.
func IsSafeID(s string) bool { return safeIDPattern.MatchString(s) }
