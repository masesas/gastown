package refinery

import (
	"errors"
	"fmt"
	"strings"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/rig"
	"github.com/steveyegge/gastown/internal/util"
)

// PostMergeOptions are the options of PostMerge (gt mq post-merge).
type PostMergeOptions struct {
	// SkipBranchDelete keeps the source branch on origin and locally.
	// Default: false (the branch is deleted as by gt mq merge).
	SkipBranchDelete bool
}

// PostMerge finishes an MR that a merge run already closed as merged but did
// not fully finalize (gt mq merge reported cleanup_failed with next =
// `gt mq post-merge <rig> <mr>`): it reruns the one finalize (finalizeMerged:
// proof, source close "Merged in <mr>", branch cleanup, MERGED mail, convoy
// check, mayor nudge) under the rig merge lock. It merges and pushes nothing.
//
// It refuses, before any side effect (error, nothing written):
//   - an id that is not a plain bead id, a bead that is not a gt:merge-request
//     of this rig, or one that cannot be read;
//   - an MR that is still open: `gt mq merge` finalizes a landed open MR;
//   - an MR not closed with close_reason merged (rejected, superseded, a bare
//     close): the human verdicts (--confirm-landed / --confirm-not-landed)
//     decide those;
//   - a landing proof not bound to this MR (gt-pd1c.89): the MR must record
//     merge_commit M, git must prove M on the trusted target, and M must have
//     exactly two parents, the second being commit_sha (provenRecordedMerge,
//     the proof gt mq merge's already-landed path uses). commit_sha merely
//     reachable from the target is not enough.
//
// The source bead is closed only when it is bound to the MR (the back-link
// comment, sourceBoundToMR); otherwise the report's SourceErr says so.
func (e *Engineer) PostMerge(mrID string, opts PostMergeOptions) (*MRInfo, FinalizeReport, error) {
	mrID = strings.TrimSpace(mrID)
	if !beads.IsSafeID(mrID) {
		return nil, FinalizeReport{}, fmt.Errorf("%q is not a bead id", util.OneLine(mrID, 80))
	}
	if err := e.LoadConfig(); err != nil {
		return nil, FinalizeReport{}, fmt.Errorf("load merge queue config: %w", err)
	}
	unlock, err := e.lockMergeWorktree()
	if err != nil {
		return nil, FinalizeReport{}, err
	}
	defer unlock()
	defer e.waitPendingMail()

	mr, merge, err := e.loadMergedMR(mrID)
	if err != nil {
		return mr, FinalizeReport{}, err
	}
	_, _ = fmt.Fprintf(e.output, "[Engineer] %s: its merge %s of commit_sha %s is on origin/%s; finalizing\n",
		mr.ID, shortSHA(merge), shortSHA(mr.CommitSHA), mr.Target)
	report := e.finalizeMerged(mr, ProcessResult{Success: true, MergeCommit: merge},
		finalizeOptions{skipBranchDelete: opts.SkipBranchDelete})
	return mr, report, nil
}

// loadMergedMR reads MR mrID and returns it with its proven merge commit when
// PostMerge may finalize it (see PostMerge for the refusals).
func (e *Engineer) loadMergedMR(mrID string) (*MRInfo, string, error) {
	issue, err := e.beads.Show(mrID)
	if err != nil {
		if errors.Is(err, beads.ErrNotFound) {
			return nil, "", fmt.Errorf("MR %s not found", mrID)
		}
		return nil, "", fmt.Errorf("read MR %s: %w", mrID, err)
	}
	if issue == nil {
		return nil, "", fmt.Errorf("MR %s not found", mrID)
	}
	if !beads.HasLabel(issue, "gt:merge-request") {
		return nil, "", fmt.Errorf("%s is not a merge request (no gt:merge-request label)", mrID)
	}
	fields := beads.ParseMRFields(issue)
	if fields == nil {
		return nil, "", fmt.Errorf("MR %s has no merge-request fields", mrID)
	}
	if fields.Rig != "" && !rig.SameName(fields.Rig, e.rig.Name) {
		return nil, "", fmt.Errorf("MR %s belongs to rig %s, not %s", mrID, util.OneLine(fields.Rig, 80), e.rig.Name)
	}
	if status := beads.IssueStatus(strings.TrimSpace(issue.Status)); !status.IsTerminal() {
		return nil, "", fmt.Errorf("MR %s is %s, not closed: run gt mq merge %s %s (it finalizes a merge that already landed without merging again)",
			mrID, status, e.rig.Name, mrID)
	}
	if verdict := strings.TrimSpace(fields.CloseReason); !strings.EqualFold(verdict, string(CloseReasonMerged)) {
		if verdict == "" {
			verdict = "none"
		}
		return nil, "", fmt.Errorf("MR %s is closed with close_reason %s, not merged: post-merge only finishes an MR a merge run closed as merged "+
			"(an unverified hold is decided with --confirm-landed / gt mq reject --confirm-not-landed)", mrID, util.OneLine(verdict, 80))
	}
	if src := strings.TrimSpace(fields.SourceIssue); src != "" && !beads.IsSafeID(src) {
		return nil, "", fmt.Errorf("MR %s: source_issue %q is not a plain bead id", mrID, util.OneLine(src, 80))
	}
	mr := issueToMRInfo(issue, fields)
	if err := e.verifyMRInfoPostMergeProof(mr); err != nil {
		return mr, "", fmt.Errorf("MR %s: post-merge proof: %w", mrID, err)
	}
	merge, why := e.provenRecordedMerge(mr, fields)
	if why != "" {
		return mr, "", fmt.Errorf("MR %s: the landing is not bound to this MR: %s; nothing is closed "+
			"(commit_sha on the target alone does not prove this MR merged)", mrID, why)
	}
	return mr, merge, nil
}
