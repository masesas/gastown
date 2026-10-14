package cmd

import (
	"fmt"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/style"
)

// doneDirectLanding carries what landDoneDirect needs to push a polecat branch
// straight to its resolved target (convoy strategy "direct", only reachable on
// a rig without an active merge queue).
type doneDirectLanding struct {
	g          *git.Git
	bd         *beads.Beads // source-owned beads (close, verified-push notes)
	cwd        string
	townRoot   string
	rigName    string
	sender     string
	issueID    string
	source     *beads.Issue
	branch     string
	baseRef    string
	target     string // from resolveDoneTarget, same as the MR path would use
	skipVerify bool
}

// doneDirectResult reports a recoverable push failure. A nil error with
// pushFailed=false means the branch landed on the target.
type doneDirectResult struct {
	pushFailed bool
	errMsg     string
}

// landDoneDirect pushes branch:target, verifies the pushed commit on the
// remote, and closes the source issue (no MR or refinery will close it).
// The returned error is fatal for gt done; push failures are returned in the
// result so the caller can notify the witness and keep the bead in progress.
func landDoneDirect(p doneDirectLanding) (doneDirectResult, error) {
	fmt.Printf("%s Direct merge strategy: pushing to %s\n", style.Bold.Render("→"), p.target)
	if skipReason := doneDirectMergeSkipReason(p.bd, p.issueID, p.source, p.target); skipReason != "" {
		style.PrintWarning("%s", skipReason)
		notifyDoneCloseSkipped(p.townRoot, p.rigName, p.sender, p.issueID, skipReason)
		return doneDirectResult{}, fmt.Errorf("cannot complete direct-merge work: %s", skipReason)
	}

	// Push submodule changes before the direct push (gt-dzs).
	pushSubmoduleChanges(p.g, p.baseRef)
	if err := p.g.Push("origin", p.branch+":"+p.target, false); err != nil {
		errMsg := fmt.Sprintf("direct push to %s failed: %v", p.target, err)
		style.PrintWarning("%s", errMsg)
		return doneDirectResult{pushFailed: true, errMsg: errMsg}, nil
	}
	commitSHA, revErr := p.g.Rev("HEAD")
	if revErr != nil && !p.skipVerify {
		errMsg := fmt.Sprintf("direct push to %s: cannot resolve pushed commit: %v", p.target, revErr)
		style.PrintWarning("%s\nSource bead will remain in progress.", errMsg)
		return doneDirectResult{pushFailed: true, errMsg: errMsg}, nil
	}
	if p.skipVerify {
		noteVerifiedPushSkipped(p.bd, p.cwd, p.issueID, p.target, commitSHA, "--skip-verify on direct merge")
	} else if verifyErr := p.g.VerifyPushedCommitReachableFromPushTarget("origin", p.target, commitSHA); verifyErr != nil {
		noteVerifiedPushFailure(p.bd, p.cwd, p.issueID, p.target, commitSHA, verifyErr)
		errMsg := fmt.Sprintf("direct push to %s: %v", p.target, verifyErr)
		style.PrintWarning("%s\nDirect merge pushed but remote verification failed. Source bead will remain in progress.", errMsg)
		return doneDirectResult{pushFailed: true, errMsg: errMsg}, nil
	}
	fmt.Printf("%s Branch pushed directly to %s\n", style.Bold.Render("✓"), p.target)

	if p.issueID == "" {
		return doneDirectResult{}, nil
	}
	if skipReason, fatal := doneSourceCloseSkipReason(p.bd, p.issueID, p.source); skipReason != "" {
		style.PrintWarning("%s", skipReason)
		notifyDoneCloseSkipped(p.townRoot, p.rigName, p.sender, p.issueID, skipReason)
		if fatal {
			return doneDirectResult{}, fmt.Errorf("cannot complete direct-merge work: %s", skipReason)
		}
		return doneDirectResult{}, nil
	}
	closeReason := fmt.Sprintf("Direct merge to %s (convoy strategy)", p.target)
	if err := forceCloseIssueWithRetry(p.bd.ForceCloseWithReason, p.issueID, closeReason, "Issue %s closed (direct merge)"); err != nil {
		style.PrintWarning("could not close issue %s after 3 attempts: %v", p.issueID, err)
	}
	return doneDirectResult{}, nil
}
