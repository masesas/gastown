// Package mergescope is the one merge-scope gate (gt-pd1c.68): the rig's
// merge_queue.allowed_paths in <rig>/settings/config.json limits which
// repository paths a merge request may change.
//
// Enforcement layers, all built on Load and Check (byte-exact paths, renames
// counted on both sides):
//   - submit time: gt done and gt mq submit refuse an out-of-scope branch
//     before any MR bead exists;
//   - merge time: the refinery merge engine (refinery.Engineer, used by
//     gt mq merge) checks the pinned submitted commit against the target tip
//     right before merging and never merges or pushes a violation;
//     gt mq check-scope offers the same check for manual use;
//   - push time: the Gas Town managed pre-push hook (gt hooks pre-push)
//     refuses an agent push to the default branch or integration/* whose
//     remote..local range leaves the scope.
package mergescope

import (
	"fmt"
	"strings"

	"github.com/steveyegge/gastown/internal/config"
)

// MaxListed caps how many offending paths Error lists.
const MaxListed = 20

// Range is how a diff joins its base and head, as shown in messages.
type Range string

const (
	// MergeBase is base...head: the changes on head since its merge base
	// with base (git diff base...head). The default.
	MergeBase Range = "..."
	// TwoDot is base..head: the direct difference of the two commits (a
	// pushed range in the pre-push hook).
	TwoDot Range = ".."
)

// Diff lists the paths changed between base and head. It must implement the
// Range it is passed with (git.Git.DiffNameOnly for MergeBase,
// git.Git.DiffNameOnlyRange for TwoDot).
type Diff func(base, head string) ([]string, error)

// Error reports changed paths outside merge_queue.allowed_paths.
type Error struct {
	Paths []string // offending paths, in diff order
	Base  string   // base ref of the diff
	Head  string   // head ref/commit of the diff
	Range Range    // MergeBase (default when empty) or TwoDot
}

func (e *Error) Error() string {
	var b strings.Builder
	sep := e.Range
	if sep == "" {
		sep = MergeBase
	}
	fmt.Fprintf(&b, "merge scope violation: %d path(s) changed in %s%s%s are outside merge_queue.allowed_paths:",
		len(e.Paths), e.Base, sep, e.Head)
	for i, p := range e.Paths {
		if i == MaxListed {
			fmt.Fprintf(&b, "\n  ... and %d more", len(e.Paths)-MaxListed)
			break
		}
		fmt.Fprintf(&b, "\n  %q", p)
	}
	b.WriteString("\nRemove these changes from the branch (or ask the rig operator to widen allowed_paths in settings/config.json).")
	return b.String()
}

// Load returns the rig's merge_queue.allowed_paths (nil = unrestricted).
// Only that key is read, so unrelated invalid settings never block; an
// unreadable or unparseable file or an invalid pattern is an error (callers
// fail closed). See config.LoadMergeQueueAllowedPaths.
func Load(rigPath string) ([]string, error) {
	allowed, err := config.LoadMergeQueueAllowedPaths(config.RigSettingsPath(rigPath))
	if err != nil {
		return nil, fmt.Errorf("merge scope: %w", err)
	}
	return allowed, nil
}

// Check lists the paths changed in base<rng>head with diff and returns how
// many were checked, or an *Error naming those outside allowed. An empty
// allowed list is unrestricted and never runs diff. A diff failure is
// returned as an error (fail closed).
func Check(allowed []string, base string, rng Range, head string, diff Diff) (int, error) {
	if len(allowed) == 0 {
		return 0, nil
	}
	if rng == "" {
		rng = MergeBase
	}
	changed, err := diff(base, head)
	if err != nil {
		return 0, fmt.Errorf("merge scope: diff %s%s%s: %w", base, rng, head, err)
	}
	if outside := config.PathsOutsideAllowed(allowed, changed); len(outside) > 0 {
		return len(changed), &Error{Paths: outside, Base: base, Head: head, Range: rng}
	}
	return len(changed), nil
}
