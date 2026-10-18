package refinery

import (
	"errors"
	"fmt"
	"strings"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/util"
)

// A MergeOne run first asks git whether the MR's commit_sha is already on its
// target (gt-pd1c.93): a run that pushed but failed to finalize, or a merge by
// hand, is finalized, never merged a second time.

// mergeStep is what MergeOne does with a claimed MR after the landing check.
type mergeStep int

const (
	stepMerge    mergeStep = iota // not landed: merge it
	stepFinalize                  // landed: only finalize
	stepRefuse                    // the outcome is final (claim released)
)

// landingStep decides the step from git's answer. Finalizing without a merge
// needs proof that THIS MR was merged, not just that commit_sha is on the
// target (any commit already on main would pass that, and the finalize closes
// source_issue "Merged in <mr>" with --force: security review H1): the MR
// must record merge_commit M, git must prove M on origin/<target>, and M must
// have exactly two parents, the second being commit_sha (the shape the engine
// pushes and records before finalizing). Then out.MergeCommit is M.
// commit_sha on the target without that proof is refused, never merged
// again. A recorded close_reason merged must be backed by git: anything short
// of proof refuses. An MR without it merges unless git proves commit_sha
// landed; when git cannot decide (e.g. a shallow clone whose boundary hides
// the branch base) the merge proceeds as before, because doMerge refuses to
// push a merge whose second parent is not commit_sha.
func (e *Engineer) landingStep(out MergeOutcome, mr *MRInfo, fields *beads.MRFields) (mergeStep, MergeOutcome) {
	proofErr := e.verifyMRInfoPostMergeProof(mr)
	recordedMerged := fields != nil && strings.EqualFold(strings.TrimSpace(fields.CloseReason), string(CloseReasonMerged))
	switch landed := landingFromProof(proofErr); {
	case landed == landingProven:
		merge, why := e.provenRecordedMerge(mr, fields)
		if why != "" {
			return stepRefuse, out.with(OutcomeNotEligible, fmt.Sprintf("%v: MR %s: commit_sha %s is already on origin/%s, but %s; "+
				"it is not merged again and nothing is closed (resolve by hand: inspect the MR, then gt mq post-merge or gt mq reject it)",
				ErrMRNotEligible, mr.ID, shortSHA(mr.CommitSHA), mr.Target, why))
		}
		out.MergeCommit = merge
		return stepFinalize, out
	case recordedMerged && landed == landingUnknown:
		return stepRefuse, out.with(OutcomeError, fmt.Sprintf("MR %s records close_reason merged, but git cannot tell whether commit_sha %s is on origin/%s (%v); "+
			"refusing to merge or close it", mr.ID, shortSHA(mr.CommitSHA), mr.Target, proofErr))
	case recordedMerged:
		return stepRefuse, out.with(OutcomeNotEligible, fmt.Sprintf("%v: MR %s records close_reason merged, but commit_sha %s is not on origin/%s; "+
			"refusing to merge or close it (inspect the MR; gt mq reject it if it did not land)",
			ErrMRNotEligible, mr.ID, shortSHA(mr.CommitSHA), mr.Target))
	case landed == landingUnknown:
		_, _ = fmt.Fprintf(e.output, "[Engineer] Note: cannot tell whether %s already landed (%v); merging\n", mr.ID, proofErr)
	}
	return stepMerge, out
}

// landing is what git says about an MR's commit_sha on its target.
type landing int

const (
	landingAbsent  landing = iota // definitely not on the target (git.CommitNotOnBranchError)
	landingProven                 // reachable from origin/<target>
	landingUnknown                // git could not decide (remote unreadable, shallow clone, untrusted target)
)

// landingFromProof classifies a verifyMRInfoPostMergeProof result.
func landingFromProof(err error) landing {
	var notOn *git.CommitNotOnBranchError
	switch {
	case err == nil:
		return landingProven
	case errors.As(err, &notOn):
		return landingAbsent
	default:
		return landingUnknown
	}
}

// finalizeLanded finalizes an MR whose own merge (out.MergeCommit, proven
// by landingStep) is already on its target: no merge, no gates, no push,
// only HandleMRInfoSuccess.
func (e *Engineer) finalizeLanded(out MergeOutcome, mr *MRInfo) (MergeOutcome, bool) {
	out.AlreadyLanded = true
	_, _ = fmt.Fprintf(e.output, "[Engineer] %s: its merge %s of commit_sha %s is already on origin/%s; finalizing without merging again\n",
		mr.ID, shortSHA(out.MergeCommit), shortSHA(mr.CommitSHA), mr.Target)
	return e.finalizeMergeOne(out, mr, ProcessResult{Success: true, MergeCommit: out.MergeCommit})
}

// provenRecordedMerge returns the MR's recorded merge_commit M when git proves
// M is on origin/<target> and M has exactly two parents, the second being
// commit_sha; otherwise "" and why not. The field is bead data anyone can
// write, so only git decides.
func (e *Engineer) provenRecordedMerge(mr *MRInfo, fields *beads.MRFields) (string, string) {
	recorded := ""
	if fields != nil {
		recorded = strings.TrimSpace(fields.MergeCommit)
	}
	if recorded == "" {
		return "", "the MR has no recorded merge (merge_commit) of it"
	}
	shown := util.OneLine(shortSHA(recorded), 40)
	if !beads.IsGitObjectName(recorded) ||
		e.git.VerifyPushedCommitReachableFromPushTarget("origin", mr.Target, recorded) != nil {
		return "", fmt.Sprintf("its recorded merge_commit %s is not proven on origin/%s", shown, mr.Target)
	}
	second, err := e.git.Rev(recorded + "^2")
	if err != nil || strings.TrimSpace(second) != strings.TrimSpace(mr.CommitSHA) {
		return "", fmt.Sprintf("its recorded merge_commit %s does not have commit_sha as its second parent", shown)
	}
	if _, err := e.git.Rev(recorded + "^3"); err == nil {
		return "", fmt.Sprintf("its recorded merge_commit %s has more than two parents", shown)
	}
	return recorded, ""
}
