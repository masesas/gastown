package beads

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// branchHold classifies the work on a source branch (e.g. polecat/<name>/...)
// with the one merge-queue predicate (mergeQueueHoldDetail, as Detail does for
// a bead) over every MR in the town submitted from that branch, whatever
// source bead it belongs to (gt-pd1c.78). TipDisposition builds on it to
// classify one tip of the branch.
//
// An unread (or nil) queue and an empty branch are errors, so callers fail
// closed. MRs without a source_issue are indexed under sourcelessKey, so they
// are seen here too.
func (q *MergeQueue) branchHold(branch string) (HoldDetail, error) {
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

// MR close verdicts (MRFields.CloseReason), the one source of these values:
// written by the refinery (refinery.CloseReason*, closeTerminalMR) and
// SupersedeMR, read by the landing predicate and TipDisposition.
const (
	CloseReasonMerged     = "merged"
	CloseReasonRejected   = "rejected"
	CloseReasonConflict   = "conflict"
	CloseReasonSuperseded = "superseded"
)

// TipClass is what the merge queue says about one polecat branch tip that is
// not already on a merge target (gt-pd1c.78 tahap 2): who decides what may
// happen to it. Only TipRejected and TipSuperseded may be abandoned (archived
// to refs/gt/archive/* and then deleted, gt polecat abandon).
type TipClass string

const (
	// TipOpen: an MR whose commit is this tip is open (the refinery owns it).
	TipOpen TipClass = "open"
	// TipLanded: an MR whose commit is exactly the tip landed.
	TipLanded TipClass = "landed"
	// TipUnverified: git cannot tell whether an MR from the branch landed;
	// an overseer decides (UnverifiedExitCommands).
	TipUnverified TipClass = "unverified"
	// TipNoMR: no MR recorded this tip (never submitted, or commits after the
	// last MR): the work must be resubmitted or reworked.
	TipNoMR TipClass = "no_mr"
	// TipRejected: the MR of this tip was closed with the verdict rejected.
	TipRejected TipClass = CloseReasonRejected
	// TipSuperseded: the MR of this tip was replaced by another MR.
	TipSuperseded TipClass = CloseReasonSuperseded
	// TipConflict: the MR of this tip was closed for conflicts: rework.
	TipConflict TipClass = CloseReasonConflict
	// TipNoVerdict: the MR of this tip was closed without an official verdict
	// (bare bd close, a human not-landed verdict): rework.
	TipNoVerdict TipClass = "closed_without_verdict"
)

// TipDisposition is the merge queue's classification of a branch tip.
type TipDisposition struct {
	Class TipClass
	// Hold is the branch's hold over every MR from it (branchHold).
	Hold HoldDetail
	// MRID/MR: the deciding MR (the open, landed or unverified MR, or the MR
	// of this tip whose verdict decided); empty for TipNoMR.
	MRID string
	MR   *Issue
	// Evidence is what decided a TipLanded verdict (callers that discard
	// work trust only EvidenceGit); empty for other classes.
	Evidence LandingEvidence
}

// Abandonable reports whether the tip may be abandoned: its MR was closed
// with the official verdict rejected or superseded, and no MR from the
// branch is open (the refinery would lose the branch it is merging).
func (d TipDisposition) Abandonable() bool {
	return (d.Class == TipRejected || d.Class == TipSuperseded) && !d.BranchOpen()
}

// tipVerdictRank orders the verdicts of a tip's MRs; the strictest (highest)
// decides, so one conflict or unofficial close among them refuses abandon.
var tipVerdictRank = map[TipClass]int{TipSuperseded: 1, TipRejected: 2, TipNoVerdict: 3, TipConflict: 4}

// TipDisposition classifies one tip of branch (gt-pd1c.78, per tip since
// review round 2, N2): the MRs whose commit_sha is exactly that tip decide,
// with the one predicate (mergeQueueHoldDetail): an open MR of the tip is
// TipOpen, a landed one TipLanded, an undecidable one TipUnverified;
// otherwise the strictest official close verdict of those MRs. A tip no MR
// recorded is TipNoMR, except that an undecidable landing elsewhere on the
// branch keeps it TipUnverified (fail closed). An open MR for another tip of
// the branch does not change the tip's class; it is in Hold (the branch's
// hold, branchHold), which callers that discard the whole branch check
// (BranchOpen). An unread queue or an empty branch is an error.
func (q *MergeQueue) TipDisposition(branch, tip string) (TipDisposition, error) {
	hold, err := q.branchHold(branch)
	if err != nil {
		return TipDisposition{}, err
	}
	out := TipDisposition{Hold: hold}
	tip = strings.TrimSpace(tip)
	var tipMRs []*Issue
	for _, mr := range q.mergeRequestsFromBranch(plainBranch(branch)) {
		if mrCommitIs(mr, tip) {
			tipMRs = append(tipMRs, mr)
		}
	}
	if len(tipMRs) == 0 {
		out.Class = TipNoMR
		if hold.Hold == HoldUnverified {
			out.Class, out.MRID, out.MR = TipUnverified, hold.MRID, hold.MR
		}
		return out, nil
	}
	switch d := mergeQueueHoldDetail(tipMRs, q.resolve); d.Hold {
	case HoldPending:
		out.Class, out.MRID, out.MR = TipOpen, d.MRID, d.MR
		return out, nil
	case HoldLanded:
		out.Class, out.MRID, out.MR, out.Evidence = TipLanded, d.MRID, d.MR, d.Evidence
		return out, nil
	case HoldUnverified:
		out.Class, out.MRID, out.MR = TipUnverified, d.MRID, d.MR
		return out, nil
	}
	for _, mr := range tipMRs {
		class := closeVerdictClass(mr)
		if out.MR == nil || tipVerdictRank[class] > tipVerdictRank[out.Class] {
			out.Class, out.MRID, out.MR = class, mr.ID, mr
		}
	}
	return out, nil
}

// BranchOpen reports whether an MR from the branch is open (for any tip):
// the refinery owns the branch, so it is neither deleted nor abandoned.
func (d TipDisposition) BranchOpen() bool { return d.Hold.Hold == HoldPending }

// mrCommitIs reports whether mr recorded commit (full object names, case
// insensitive). An empty commit matches nothing.
func mrCommitIs(mr *Issue, commit string) bool {
	if mr == nil || commit == "" {
		return false
	}
	fields := ParseMRFields(mr)
	return fields != nil && strings.EqualFold(strings.TrimSpace(fields.CommitSHA), commit)
}

// closeVerdictClass reads the official close verdict of a terminal MR that did
// not land.
func closeVerdictClass(mr *Issue) TipClass {
	fields := ParseMRFields(mr)
	if fields == nil {
		return TipNoVerdict
	}
	switch strings.ToLower(strings.TrimSpace(fields.CloseReason)) {
	case CloseReasonRejected:
		return TipRejected
	case CloseReasonSuperseded:
		return TipSuperseded
	case CloseReasonConflict:
		return TipConflict
	default:
		return TipNoVerdict
	}
}

// AbandonCommand is the copy-paste command that abandons branch of rig (gt
// polecat abandon: archive the tip on the remote, verify, delete the branch).
// Shared by every place that offers abandon (the deletion rule, the witness
// branch report). Values that are not safe are never inserted: rig becomes
// "<rig>" and branch "<polecat-branch>".
func AbandonCommand(rig, branch string) string {
	rig = strings.TrimSpace(rig)
	if !IsSafeID(rig) {
		rig = "<rig>"
	}
	branch = plainBranch(branch)
	if !strings.HasPrefix(branch, "polecat/") || !IsSafeRefName(branch) {
		branch = "<polecat-branch>"
	}
	return fmt.Sprintf("gt polecat abandon %s/%s --reason \"<why>\"", rig, branch)
}
