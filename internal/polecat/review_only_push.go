package polecat

import (
	"errors"
	"fmt"
	"strings"

	"github.com/steveyegge/gastown/internal/beads"
)

// BeadShowFunc loads one bead (beads.Beads.Show).
type BeadShowFunc func(id string) (*beads.Issue, error)

// ReviewOnlyPushSkipReason decides whether a polecat branch may be pushed to
// origin to preserve its commits (nuke, worktree removal, doctor --fix). It is
// the single guard for every such push (gt-pd1c.69): commits on a review-only
// leg are not work to keep — the leg's findings live in bead comments — so
// pushing them only leaks review scratch to the remote.
//
// The work is identified by the bead embedded in the branch name
// (polecat/<name>/<bead>+<suffix>, which survives a --force re-sling to another
// polecat) and by issueIDs (e.g. the polecat's hooked bead). It returns "" when
// the push may proceed, otherwise the reason to skip it. A bead named by the
// branch that no longer exists does not block; any other lookup failure, or a
// named issue that cannot be read, fails closed and skips the push.
func ReviewOnlyPushSkipReason(show BeadShowFunc, branch string, issueIDs ...string) string {
	type candidate struct {
		id            string
		ignoreMissing bool
	}
	var candidates []candidate
	if meta, ok := ParseBranchName(branch); ok && meta.Issue != "" {
		candidates = append(candidates, candidate{id: meta.Issue, ignoreMissing: true})
	}
	for _, id := range issueIDs {
		candidates = append(candidates, candidate{id: strings.TrimSpace(id)})
	}

	seen := make(map[string]bool)
	for _, c := range candidates {
		if c.id == "" || seen[c.id] {
			continue
		}
		seen[c.id] = true
		if show == nil {
			return fmt.Sprintf("could not verify review-only status of %s (no beads lookup); not pushing branch %s", c.id, branch)
		}
		issue, err := show(c.id)
		if c.ignoreMissing && errors.Is(err, beads.ErrNotFound) {
			continue
		}
		if err != nil {
			return fmt.Sprintf("could not verify review-only status of %s (%v); not pushing branch %s", c.id, err, branch)
		}
		if beads.IsReviewOnly(issue) {
			return fmt.Sprintf("%s is review-only: commits on branch %s are not preserved on origin (the leg's findings belong in bead comments)", c.id, branch)
		}
	}
	return ""
}

// ReviewOnlyPushSkipReason is ReviewOnlyPushSkipReason with this manager's
// rig beads as the lookup.
func (m *Manager) ReviewOnlyPushSkipReason(branch string, issueIDs ...string) string {
	return ReviewOnlyPushSkipReason(m.beads.Show, branch, issueIDs...)
}
