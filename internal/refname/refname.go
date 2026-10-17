// Package refname is the one rule for the branch names Gas Town hands to git
// (polecat/*, integration/*, the default branch) and reads back from beads,
// config files and the remote. It is a leaf package (no gastown imports) so
// every layer (beads, config, mergeproof, the pre-push guard, the witness)
// applies the same rule.
package refname

import (
	"regexp"
	"strings"
)

// segment is one '/'-separated component of a branch name: it starts with an
// ASCII letter or digit (never '-', '+' or '.', so a name can never be taken
// for an option, a forcing refspec "+src:dst", a hidden component or "..")
// and contains only letters, digits and '.', '_', '+', '@', '-'.
//
// '+' is the separator of generated polecat branches
// (polecat/<name>/<issue>+<suffix>, internal/polecat/branch_name.go) and '@'
// the legacy separator still accepted for in-flight branches.
var segment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+@-]*$`)

// Valid reports whether name is a branch name gt may hand to git (without
// refs/heads/). The rule is a strict subset of `git check-ref-format
// --branch` (git is never called):
//
//   - one or more '/'-separated segments, none empty (no leading, trailing
//     or doubled '/'), each matching segment (no whitespace, control
//     characters, shell or glob metacharacters, '~', '^', ':', '?', '*',
//     '[', '\\', '{' or '}');
//   - no "..", no "@{", no segment ending in ".lock", no trailing '.';
//   - not "HEAD".
//
// Bead and rig IDs are not branch names: they keep beads.IsSafeID (no '+',
// '@' or '/').
func Valid(name string) bool {
	if name == "" || name == "HEAD" || strings.Contains(name, "..") || strings.Contains(name, "@{") ||
		strings.HasSuffix(name, ".") {
		return false
	}
	for _, seg := range strings.Split(name, "/") {
		if !segment.MatchString(seg) || strings.HasSuffix(seg, ".lock") {
			return false
		}
	}
	return true
}
