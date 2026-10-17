package beads

import (
	"errors"
	"sort"
	"strings"
)

// BranchHold classifies the work on a source branch (e.g. polecat/<name>/...)
// with the one merge-queue predicate (mergeQueueHoldDetail, as Detail does for
// a bead) over every MR in the town submitted from that branch, whatever
// source bead it belongs to. gt-pd1c.78: an agent may delete
// a polecat branch whose tip is not on a target only when this is HoldNone
// (no open MR, every MR definitely not landed).
//
// An unread (or nil) queue and an empty branch are errors, so callers fail
// closed. MRs without a source_issue are not in the index (TownMergeQueueIndex)
// and therefore not seen here.
func (q *MergeQueue) BranchHold(branch string) (HoldDetail, error) {
	if err := q.Err(); err != nil {
		return HoldDetail{Hold: HoldNone}, err
	}
	branch = plainBranch(branch)
	if branch == "" {
		return HoldDetail{Hold: HoldNone}, errors.New("branch name is empty")
	}
	return mergeQueueHoldDetail(q.mergeRequestsFromBranch(branch), q.resolve), nil
}

// plainBranch is a branch name without surrounding space or a refs/heads/
// prefix, so "refs/heads/polecat/x" and "polecat/x" compare equal.
func plainBranch(b string) string {
	return strings.TrimPrefix(strings.TrimSpace(b), "refs/heads/")
}

// mergeRequestsFromBranch returns the MRs whose branch field is branch,
// ordered by ID so the deciding MR is deterministic.
func (q *MergeQueue) mergeRequestsFromBranch(branch string) []*Issue {
	var out []*Issue
	for _, mrs := range q.index {
		for _, mr := range mrs {
			if mr == nil {
				continue
			}
			if fields := ParseMRFields(mr); fields != nil && plainBranch(fields.Branch) == branch {
				out = append(out, mr)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
