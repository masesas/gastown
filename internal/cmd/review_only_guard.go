package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/steveyegge/gastown/internal/beads"
)

// reviewOnlyCandidate is a bead whose review-only status gt done or gt mq
// submit must honour. Issue may be nil; it is then loaded through routing.
type reviewOnlyCandidate struct {
	ID    string
	Issue *beads.Issue
	// IgnoreMissing treats a bead that does not exist as not review-only
	// (branch-derived targets of purged beads). Other lookup errors still fail.
	IgnoreMissing bool
}

// findHookedBeadsForReviewOnly is a seam for tests.
var findHookedBeadsForReviewOnly = findHookedBeadsForAgent

// findReviewOnlyWork returns the ID of the first review-only bead among the
// candidates and the beads currently hooked to agentID, or ""
// when none is review-only. gt done and gt mq submit both decide through it
// (gt-pd1c.69), so a review leg cannot produce commits or a merge request by
// naming another bead with --issue or a branch name. Lookup failures are
// returned as errors: callers fail closed.
func findReviewOnlyWork(cwd, agentID string, candidates ...reviewOnlyCandidate) (string, error) {
	seen := make(map[string]bool)
	check := func(id string, issue *beads.Issue, ignoreMissing bool) (bool, error) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return false, nil
		}
		seen[id] = true
		if issue == nil {
			bd, _, _ := routedIssueBeads(cwd, id)
			loaded, err := bd.Show(id)
			if ignoreMissing && errors.Is(err, beads.ErrNotFound) {
				return false, nil
			}
			if err != nil {
				return false, fmt.Errorf("cannot verify review-only status of %s: %w", id, err)
			}
			issue = loaded
		}
		return beads.IsReviewOnly(issue), nil
	}
	for _, c := range candidates {
		reviewOnly, err := check(c.ID, c.Issue, c.IgnoreMissing)
		if err != nil {
			return "", err
		}
		if reviewOnly {
			return strings.TrimSpace(c.ID), nil
		}
	}
	if agentID == "" {
		return "", nil
	}
	assigned, err := findHookedBeadsForReviewOnly(cwd, agentID)
	if err != nil {
		return "", fmt.Errorf("cannot verify review-only status of work hooked to %s: %w", agentID, err)
	}
	for _, id := range assigned {
		reviewOnly, err := check(id, nil, false)
		if err != nil {
			return "", err
		}
		if reviewOnly {
			return id, nil
		}
	}
	return "", nil
}

// reviewOnlyCommitRefusal is the gt done error for commits on review-only work.
func reviewOnlyCommitRefusal(reviewBeadID, baseRef string) error {
	return fmt.Errorf("cannot complete review-only issue %s with commits ahead of %s: review-only work must not commit, push or open a merge request\n"+
		"Recover: save your findings outside the repo, drop the commits with `git reset --hard %s` (discards them), add a REPORT:/FINDINGS: comment with head_sha: <new HEAD> to %s, then run gt done again",
		reviewBeadID, baseRef, baseRef, reviewBeadID)
}

// reviewOnlySubmitRefusal is the gt mq submit error for review-only work.
func reviewOnlySubmitRefusal(reviewBeadID string) error {
	return fmt.Errorf("cannot submit to the merge queue: review-only issue %s reports findings in a bead comment and completes with gt done without new commits", reviewBeadID)
}

// reviewOnlyCallerAgent returns the agent whose hook gt mq submit checks. In a
// polecat session (GT_POLECAT set) the identity is resolved exactly as gt done
// does (polecatEnvIdentity) and an unresolvable identity is an error: fail
// closed. Elsewhere BD_ACTOR, else GT_ROLE, names the caller; a human with
// neither has no hook.
func reviewOnlyCallerAgent() (string, error) {
	if strings.TrimSpace(os.Getenv("GT_POLECAT")) != "" {
		rig, name, err := polecatEnvIdentity()
		if err != nil {
			return "", fmt.Errorf("cannot identify the calling polecat: %w", err)
		}
		return fmt.Sprintf("%s/polecats/%s", rig, name), nil
	}
	if actor := strings.TrimSpace(os.Getenv("BD_ACTOR")); actor != "" {
		return actor, nil
	}
	return strings.TrimSpace(os.Getenv("GT_ROLE")), nil
}
