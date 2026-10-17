package beads

import "strings"

// blockingDependencyTypes are the dependency types that order execution:
// the dependent may not start until the target is resolved. parent-child is
// hierarchy only and never blocks.
var blockingDependencyTypes = map[string]bool{
	"blocks":             true,
	"conditional-blocks": true,
	"waits-for":          true,
	"merge-blocks":       true,
}

// IsBlockingDependencyType reports whether a dependency of this type blocks
// its dependent. Case and surrounding whitespace are ignored. This is the one
// definition shared by the convoy feeder, convoy staging/launch and readiness.
func IsBlockingDependencyType(depType string) bool {
	return blockingDependencyTypes[strings.ToLower(strings.TrimSpace(depType))]
}

// mergedCloseReasonPrefix marks a close reason written when the blocker's
// code landed (refinery merge).
const mergedCloseReasonPrefix = "Merged in "

// IsBlockerResolved reports whether a blocking dependency of depType on a
// target with this status and close reason no longer blocks. tombstone always
// resolves. closed resolves, except for merge-blocks, which needs the close
// reason to confirm the code was merged ("Merged in ..."; #1893). Any other
// status, including "" (unknown), does not resolve.
func IsBlockerResolved(depType, status, closeReason string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "tombstone":
		return true
	case "closed":
		if strings.EqualFold(strings.TrimSpace(depType), "merge-blocks") {
			return strings.HasPrefix(closeReason, mergedCloseReasonPrefix)
		}
		return true
	default:
		return false
	}
}
