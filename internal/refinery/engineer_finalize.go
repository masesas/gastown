package refinery

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/mergeproof"
	"github.com/steveyegge/gastown/internal/util"
)

// FinalizeReport is what HandleMRInfoSuccess did after a merge landed.
type FinalizeReport struct {
	// ProofErr: git did not prove the submitted commit_sha is on a trusted
	// target (the rig default branch or integration/*). Nothing was closed,
	// no branch was touched and no MERGED mail was sent.
	ProofErr error
	// CloseErr: closing the MR bead as merged (and with it the source bead,
	// "Merged in <mr>") failed. No branch was touched, no MERGED mail sent.
	CloseErr error
	// SourceErr: the source (work) bead was not left closed "Merged in <mr>"
	// (mergedWorkBeadCloseResult.CloseErr). SourceSkip names a by-design
	// reason it is not closed (no source bead; no_merge, review_only or
	// merge_strategy=local); that is not a failure.
	SourceErr  error
	SourceSkip string
	// MergedMailErr: the MERGED mail to the witness could not be sent. The
	// merge and the closes stand; the witness also finds the closed MR on
	// patrol, so this is reported but does not make the report fail.
	MergedMailErr error
	// BranchCleanup is the source branch deletion (only after a merged close).
	BranchCleanup BranchCleanup
}

// MRClosed reports whether the MR was proven merged and closed as merged.
func (r FinalizeReport) MRClosed() bool { return r.ProofErr == nil && r.CloseErr == nil }

// OK reports whether the MR was proven, closed as merged and its branch
// cleanup did not fail. MergedMailErr does not count (see its doc).
func (r FinalizeReport) OK() bool { return r.Err() == nil }

// Err is the first failure that makes the report not OK, or nil.
func (r FinalizeReport) Err() error {
	switch {
	case r.ProofErr != nil:
		return fmt.Errorf("post-merge proof: %w", r.ProofErr)
	case r.CloseErr != nil:
		return fmt.Errorf("close merged MR: %w", r.CloseErr)
	case r.SourceErr != nil:
		return fmt.Errorf("close the source \"Merged in\": %w", r.SourceErr)
	case r.BranchCleanup.Err != nil:
		return fmt.Errorf("branch cleanup: %w", r.BranchCleanup.Err)
	}
	return nil
}

// BranchCleanup is the deletion of a merged MR's source branch. Remote first;
// local only when the remote side did not fail; both only at the submitted
// commit_sha (a branch that moved after submit is kept).
type BranchCleanup struct {
	Branch        string `json:"branch,omitempty"`
	NoBranch      bool   `json:"no_branch,omitempty"`      // the MR names no branch
	Disabled      bool   `json:"disabled,omitempty"`       // not a polecat branch and delete_merged_branches=false
	UnsafeName    bool   `json:"unsafe_name,omitempty"`    // not a plain branch name: refused (beads.IsSafeRefName)
	Protected     bool   `json:"protected,omitempty"`      // the default branch or integration/*: never deleted as a merged source
	OpenPR        bool   `json:"open_pr,omitempty"`        // remote kept: an open PR (or no PR lookup, e.g. non-GitHub remote) (gas-fk4)
	RemoteAbsent  bool   `json:"remote_absent,omitempty"`  // already gone on origin
	RemoteDeleted bool   `json:"remote_deleted,omitempty"` // deleted on origin at commit_sha
	LocalDeleted  bool   `json:"local_deleted,omitempty"`  // local branch deleted at commit_sha
	Err           error  `json:"-"`                        // the deletion that failed or was refused
}

// HandleMRInfoSuccess finalizes a merge that landed: prove it from git
// (verifyMRInfoPostMergeProof), close the MR bead as merged (closeTerminalMR
// also closes the source bead "Merged in <mr>"), close moot conflict tasks,
// delete the source branch, send MERGED to the witness, then the convoy check
// and the mayor nudge. Each step runs only when the ones it depends on
// succeeded; the report says which did.
func (e *Engineer) HandleMRInfoSuccess(mr *MRInfo, result ProcessResult) FinalizeReport {
	var report FinalizeReport
	workBeadID := resolveMergedWorkBead(e.beads.ForAgentBead(), mergedWorkBeadCloseRequest{
		MRID:        mr.ID,
		Branch:      mr.Branch,
		SourceIssue: mr.SourceIssue,
		AgentBead:   mr.AgentBead,
	})

	// Release merge slot if this was a conflict resolution
	// The slot is held while conflict resolution is in progress
	holder := e.rig.Name + "/refinery"
	if err := e.mergeSlotRelease(holder); err != nil {
		// Best-effort: slot release failures are always non-fatal.
		// Slot may not have been held (optional acquisition) or may have expired.
		_, _ = fmt.Fprintf(e.output, "[Engineer] Note: merge slot release: %v\n", err)
	} else {
		_, _ = fmt.Fprintf(e.output, "[Engineer] Released merge slot\n")
	}
	if err := e.verifyMRInfoPostMergeProof(mr); err != nil {
		_, _ = fmt.Fprintf(e.output, "[Engineer] Post-merge proof failed for %s: %v\n", mr.ID, err)
		report.ProofErr = err
		return report
	}

	// 1. Close the MR bead as merged. closeTerminalMR also closes the source
	// issue (the single merged-MR rule). Synthetic/bead-less MRs have no MR bead
	// to close, so their source issue is closed directly.
	if mr.ID != "" && !e.isSyntheticMergeMechanicsMR(mr) {
		closed, err := e.closeMRWithReason(mr, string(CloseReasonMerged), result.MergeCommit)
		if err != nil {
			_, _ = fmt.Fprintf(e.output, "[Engineer] Post-merge cleanup failed for %s: %v\n", mr.ID, err)
			report.CloseErr = err
			return report
		}
		report.SourceErr = mergedSourceErr(closed)
		report.SourceSkip = closed.SourceClose.Skip
	} else {
		src := closeMergedWorkBead(e.beads, nil, e.output, mergedWorkBeadCloseRequest{
			MRID:        mr.ID,
			Target:      mr.Target,
			SourceIssue: workBeadID,
			MergeCommit: result.MergeCommit,
		})
		report.SourceErr, report.SourceSkip = src.mergedErr(), src.Skip
	}
	if report.SourceErr != nil {
		_, _ = fmt.Fprintf(e.output, "[Engineer] Warning: source of %s not closed as merged: %v\n", mr.ID, report.SourceErr)
	}

	// 1.2. Close conflict-resolution tasks that this land has made moot (hq-jnap).
	// Conflict beads otherwise outlive the successful re-land of their content
	// and rot as open issues (re-dlcs/re-4i3b/re-gcii pattern).
	e.closeSupersededConflictArtifacts(mr)

	// 2. Delete the source branch.
	report.BranchCleanup = e.cleanupMergedBranch(mr)
	if report.BranchCleanup.Err != nil {
		_, _ = fmt.Fprintf(e.output, "[Engineer] Warning: branch cleanup for %s: %v\n", mr.ID, report.BranchCleanup.Err)
	}

	// 3. MERGED to the witness (the refinery formula's promise): it closes the
	// polecat's cleanup wisp.
	if err := e.sendMerged(mr, result.MergeCommit); err != nil {
		report.MergedMailErr = err
		_, _ = fmt.Fprintf(e.output, "[Engineer] Warning: failed to send MERGED for %s: %v\n", mr.ID, err)
	} else {
		_, _ = fmt.Fprintf(e.output, "[Engineer] Sent MERGED for %s to %s/witness\n", mr.ID, e.rig.Name)
	}

	// 4. Check and auto-close completed convoys
	// After closing a source issue, its parent convoy may now be complete.
	// Run convoy check to auto-close and notify subscribers.
	e.postMergeConvoyCheck(mr)

	// 5. Nudge mayor about successful merge so dispatcher can unblock
	// dependent work. Without this, mayor only discovers completion by polling.
	// Uses nudge (not mail) to avoid permanent Dolt commits for routine signals (GH#2434).
	nudgeMsg := fmt.Sprintf("MERGED: %s issue=%s branch=%s", mr.ID, mr.SourceIssue, mr.Branch)
	nudgeCmd := exec.Command("gt", "nudge", "mayor/", nudgeMsg)
	util.SetDetachedProcessGroup(nudgeCmd)
	nudgeCmd.Dir = e.workDir
	if err := nudgeCmd.Run(); err != nil {
		_, _ = fmt.Fprintf(e.output, "[Engineer] Warning: failed to nudge mayor about merge: %v\n", err)
	}

	_, _ = fmt.Fprintf(e.output, "[Engineer] ✓ Merged: %s (commit: %s)\n", mr.ID, result.MergeCommit)
	return report
}

// verifyMRInfoPostMergeProof proves from git that the submitted commit_sha is
// reachable from the MR target on origin, with the guards of gt mq post-merge
// (gt-pd1c.78): the target must be a merge target (mergeproof.UntrustedMergeTarget:
// the rig default branch, read from config.RigDefaultBranch else the remote's
// own HEAD via ls-remote --symref, never the local origin/HEAD; or
// integration/*), and the source must not be the target.
func (e *Engineer) verifyMRInfoPostMergeProof(mr *MRInfo) error {
	if mr == nil {
		return fmt.Errorf("merge request is missing")
	}
	if e.git == nil {
		return fmt.Errorf("git client is missing")
	}
	target := strings.TrimSpace(mr.Target)
	if target == "" {
		return fmt.Errorf("missing target branch")
	}
	defaultBranch := mergeproof.RigDefaultBranch(e.rig.Path, e.git)
	if err := mergeproof.UntrustedMergeTarget(target, defaultBranch); err != nil {
		if strings.TrimSpace(defaultBranch) == "" {
			return fmt.Errorf("%w (the rig's default branch is unknown: set default_branch in <rig>/config.json, "+
				"or make the remote advertise HEAD)", err)
		}
		return err
	}
	if source := strings.TrimSpace(mr.Branch); source != "" && source == target {
		return fmt.Errorf("source branch %s matches target branch", source)
	}
	commit := strings.TrimSpace(mr.CommitSHA)
	if commit == "" {
		return fmt.Errorf("missing submitted commit_sha")
	}
	if err := e.git.VerifyPushedCommitReachableFromPushTarget("origin", target, commit); err != nil {
		return fmt.Errorf("target %s does not contain submitted head %s: %w", target, commit, err)
	}
	return nil
}

// cleanupMergedBranch deletes a merged MR's source branch. Polecat branches
// (polecat/*) are always cleaned up, on origin and locally: they are
// ephemeral work branches. Other branches respect delete_merged_branches and
// are only deleted locally: they may belong to contributor forks with open
// upstream PRs, which GitHub auto-closes on head_ref_delete (GH#2669). A
// branch name from the bead is handed to git only when it is a plain branch
// name (beads.IsSafeRefName, gt-pd1c.78 N3).
func (e *Engineer) cleanupMergedBranch(mr *MRInfo) BranchCleanup {
	c := BranchCleanup{Branch: strings.TrimSpace(mr.Branch)}
	if c.Branch == "" {
		c.NoBranch = true
		return c
	}
	if c.Branch == e.defaultBranch() || strings.HasPrefix(c.Branch, mergeproof.IntegrationBranchPrefix) {
		c.Protected = true
		_, _ = fmt.Fprintf(e.output, "[Engineer] Not deleting %s: the default branch and integration/* are never deleted as a merged source\n", c.Branch)
		return c
	}
	isPolecat := strings.HasPrefix(c.Branch, "polecat/")
	if !isPolecat && !e.config.DeleteMergedBranches {
		c.Disabled = true
		return c
	}
	if !beads.IsSafeRefName(c.Branch) {
		c.UnsafeName = true
		c.Err = fmt.Errorf("branch name %q is not a plain branch name; refusing to delete it (delete it by hand)", util.OneLine(c.Branch, 120))
		return c
	}
	expectedHead := strings.TrimSpace(mr.CommitSHA)
	if expectedHead == "" {
		c.Err = errors.New("missing submitted commit_sha; refusing to delete the branch")
		return c
	}
	if isPolecat {
		if err := e.deleteMergedRemoteBranch(&c, mr, expectedHead); err != nil {
			c.Err = err
			return c
		}
	}
	if err := e.deleteLocalBranchIfAt(c.Branch, expectedHead); err != nil {
		_, _ = fmt.Fprintf(e.output, "[Engineer] Note: local branch %s kept: %v\n", c.Branch, err)
	} else {
		c.LocalDeleted = true
		_, _ = fmt.Fprintf(e.output, "[Engineer] Deleted local branch: %s\n", c.Branch)
	}
	return c
}

// deleteMergedRemoteBranch deletes a polecat branch on origin at expectedHead,
// unless it has an open PR (gas-fk4: a PR merged via the provider should show
// "merged", not "closed") or is already gone.
func (e *Engineer) deleteMergedRemoteBranch(c *BranchCleanup, mr *MRInfo, expectedHead string) error {
	if e.git.HasOpenPullRequest(git.PullRequestRef{URL: mr.PRURL, Number: mr.PRNumber, Branch: c.Branch, HeadSHA: expectedHead}) {
		c.OpenPR = true
		_, _ = fmt.Fprintf(e.output, "[Engineer] Skipping remote branch delete for %s: open PR exists or PR state unknown (gas-fk4)\n", c.Branch)
		return nil
	}
	tip, err := e.git.PushRemoteBranchTip("origin", c.Branch)
	if err != nil {
		return fmt.Errorf("read origin/%s: %w", c.Branch, err)
	}
	if strings.TrimSpace(tip) == "" {
		c.RemoteAbsent = true
		return nil
	}
	if err := e.git.DeleteRemoteBranchIfAt("origin", c.Branch, expectedHead); err != nil {
		return fmt.Errorf("delete origin/%s at %s: %w", c.Branch, shortSHA(expectedHead), err)
	}
	c.RemoteDeleted = true
	_, _ = fmt.Fprintf(e.output, "[Engineer] Deleted remote branch: %s\n", c.Branch)
	return nil
}

func (e *Engineer) deleteLocalBranchIfAt(branch, expectedHead string) error {
	branch = strings.TrimSpace(branch)
	expectedHead = strings.TrimSpace(expectedHead)
	if branch == "" {
		return fmt.Errorf("missing source branch")
	}
	if expectedHead == "" {
		return fmt.Errorf("missing submitted commit_sha")
	}
	localHead, err := e.git.Rev("refs/heads/" + branch + "^{commit}")
	if err != nil {
		return fmt.Errorf("resolve local branch head: %w", err)
	}
	if strings.TrimSpace(localHead) != expectedHead {
		return fmt.Errorf("local branch head changed from submitted %s to %s", shortSHA(expectedHead), shortSHA(localHead))
	}
	return e.git.DeleteBranch(branch, false)
}

// mergedSourceErr is why a merged close of an MR did not leave its source
// closed "Merged in <mr>", or nil (closed, or skipped by design).
func mergedSourceErr(closed *terminalMRCloseResult) error {
	if closed == nil || !closed.SourceCloseAttempted {
		return errors.New("the MR was not closed as merged now, so its source close did not run")
	}
	return closed.SourceClose.mergedErr()
}
