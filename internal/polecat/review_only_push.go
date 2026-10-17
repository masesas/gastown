package polecat

import (
	"errors"
	"fmt"
	"strings"

	"github.com/steveyegge/gastown/internal/beads"
)

// BeadShowFunc loads one bead (beads.Beads.Show).
type BeadShowFunc func(id string) (*beads.Issue, error)

// ReviewOnlyVerdict classifies a polecat's work for the lifecycle paths that
// preserve it (push before nuke/removal, doctor --fix, checkpoint commits).
type ReviewOnlyVerdict int

const (
	// ReviewOnlyNotReview: ordinary work; preserve it as usual.
	ReviewOnlyNotReview ReviewOnlyVerdict = iota
	// ReviewOnlyConfirmed: review-only work; its commits are not work to keep
	// (findings live in bead comments), so do not push or checkpoint, and the
	// local branch may be deleted.
	ReviewOnlyConfirmed
	// ReviewOnlyUnverified: the status could not be read. Fail closed: do not
	// push or checkpoint, and keep the local branch so the commits survive in
	// the rig repo until someone decides.
	ReviewOnlyUnverified
)

// ReviewOnlyDecision is the verdict plus a human-readable reason ("" for
// ReviewOnlyNotReview).
type ReviewOnlyDecision struct {
	Verdict ReviewOnlyVerdict
	Reason  string
}

// PreserveAllowed reports whether the work may be pushed or checkpointed.
func (d ReviewOnlyDecision) PreserveAllowed() bool { return d.Verdict == ReviewOnlyNotReview }

// KeepLocalBranch reports whether the local branch must not be deleted.
func (d ReviewOnlyDecision) KeepLocalBranch() bool { return d.Verdict == ReviewOnlyUnverified }

// ReviewOnlyWorkDecision is the single review-only guard for every polecat
// lifecycle path that preserves a branch's work (gt-pd1c.69). The work is
// identified by the bead embedded in the branch name
// (polecat/<name>/<bead>+<suffix>, which survives a --force re-sling) and by
// issueIDs (the polecat's hooked bead). A branch bead that no longer exists
// does not count; any other lookup failure, or a named issue that cannot be
// read, yields ReviewOnlyUnverified.
func ReviewOnlyWorkDecision(show BeadShowFunc, branch string, issueIDs ...string) ReviewOnlyDecision {
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
			return unverifiedDecision(c.id, branch, errors.New("no beads lookup"))
		}
		issue, err := show(c.id)
		if c.ignoreMissing && errors.Is(err, beads.ErrNotFound) {
			continue
		}
		if err != nil {
			return unverifiedDecision(c.id, branch, err)
		}
		if beads.IsReviewOnly(issue) {
			return ReviewOnlyDecision{
				Verdict: ReviewOnlyConfirmed,
				Reason:  fmt.Sprintf("%s is review-only: commits on branch %s are not preserved (the leg's findings belong in bead comments)", c.id, branch),
			}
		}
	}
	return ReviewOnlyDecision{Verdict: ReviewOnlyNotReview}
}

func unverifiedDecision(id, branch string, err error) ReviewOnlyDecision {
	return ReviewOnlyDecision{
		Verdict: ReviewOnlyUnverified,
		Reason:  fmt.Sprintf("could not verify review-only status of %s (%v); branch %s is kept locally and not pushed", id, err, branch),
	}
}

// ReviewOnlyWorkDecision is ReviewOnlyWorkDecision with this manager's rig
// beads as the lookup. When name is set, the polecat's hooked bead (Get) is
// checked too; read it before work beads are unassigned.
func (m *Manager) ReviewOnlyWorkDecision(name, branch string, issueIDs ...string) ReviewOnlyDecision {
	if name != "" {
		p, err := m.Get(name)
		switch {
		case err == nil && p != nil:
			issueIDs = append(append([]string(nil), issueIDs...), p.Issue)
		case err != nil && !errors.Is(err, ErrPolecatNotFound):
			return unverifiedDecision(name+" hook", branch, err)
		}
	}
	return ReviewOnlyWorkDecision(m.beads.Show, branch, issueIDs...)
}
