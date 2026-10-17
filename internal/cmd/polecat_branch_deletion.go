package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/mergeproof"
)

// Polecat branch deletion rule (gt-pd1c.78), shared by the pre-push guard
// (an agent deleting or rewriting polecat/* on the remote), gt polecat nuke
// --force / remove --force and gt polecat gc.
//
// A polecat branch tip may be discarded by an agent only when that loses no
// work:
//
//  1. the tip is covered by a merge target (mergeproof.CoveredByTargets:
//     preserved on the remote default branch or a remote integration/*
//     branch, the gt polecat prune rule); or
//  2. the tip is archived on the remote (refs/gt/archive/*, written by gt
//     polecat abandon, which only archives work whose MR was closed as
//     rejected or superseded); or
//  3. the MR whose commit is exactly the tip landed by git proof
//     (beads.TipLanded with EvidenceGit; a landed verdict that rests only on
//     bead fields or a human verdict is not trusted for discarding work).
//
// Anything else is refused (fail closed), with the one way out for its class
// (beads.TipDisposition): an open MR belongs to the refinery; rejected or
// superseded work is abandoned (gt polecat abandon); conflicts, unsubmitted
// work and MRs closed without an official verdict are reworked or resubmitted
// (the mayor decides, the witness reconciler reports them); an undecidable
// landing needs an overseer verdict. A queue that cannot be read refuses.
// Humans and crew are not subject to the rule.

// polecatBranchDeletion is one polecat branch tip an agent wants to discard
// (delete the branch, or rewrite it away from this tip).
type polecatBranchDeletion struct {
	rig    string // rig name for the copy-paste exits ("" or unsafe: <rig>)
	branch string // e.g. polecat/nux/gt-1
	tip    string // the tip being discarded; "" when absent or unknown
	tips   mergeproof.RemoteTips
	git    mergeproof.PreservationGit
	// queue reads the town's merge queue; called only when the git proofs
	// fail. nil counts as unreadable.
	queue func() *beads.MergeQueue
	// noGitProof explains why the git proof could not run (e.g. the remote
	// could not be listed); it is added to refusals.
	noGitProof string
	// owner: the owning polecat rewrites its own branch; a tip never
	// submitted or whose MR is still open may then be rewritten too
	// (checkPolecatUpdate).
	owner bool
}

// polecatDeletionExits is the guidance appended to every refusal.
const polecatDeletionExits = "Landed polecat branches are deleted by the refinery or `gt mq post-merge <rig> <mr-id>`, " +
	"preserved ones by `gt polecat prune --remote`; work closed as rejected or superseded is abandoned with " +
	"`gt polecat abandon` (it archives the tip first); anything else is reworked or resubmitted (the mayor decides) " +
	"or deleted by a human or crew"

// check returns nil when the tip may be discarded, else why not.
func (d polecatBranchDeletion) check() error {
	if mergeproof.CoveredByTargets(d.git, d.tip, d.tips) {
		return nil
	}
	if archived, err := d.tips.Archived(d.tip); err == nil && archived {
		return nil
	}
	if d.queue == nil {
		return d.refuse("the merge queue is not available (fail closed)")
	}
	disp, err := d.queue().TipDisposition(d.branch, d.tip)
	if err != nil {
		return d.refuse("cannot read the merge queue: " + firstLine(err.Error()) + " (fail closed)")
	}
	if disp.Class == beads.TipLanded && disp.Evidence == beads.EvidenceGit {
		return nil
	}
	if d.owner && (disp.Class == beads.TipNoMR || disp.Class == beads.TipOpen) {
		return nil
	}
	return d.refuse(tipRefusal(d.rig, d.branch, disp))
}

// tipRefusal says why a tip of its class may not be discarded and what the
// way out is (one text per beads.TipClass, shared by the deletion rule and the
// archive guard).
func tipRefusal(rig, branch string, disp beads.TipDisposition) string {
	mr := mrIDOf(disp)
	switch disp.Class {
	case beads.TipOpen:
		return fmt.Sprintf("MR %s from this branch is open; the refinery owns the branch until it merges", mr)
	case beads.TipLanded:
		return fmt.Sprintf("MR %s of this tip is marked landed (%s), but git has not proven its tip is on a merge target; "+
			"fetch and retry", mr, disp.Evidence)
	case beads.TipUnverified:
		notLanded, landed := beads.UnverifiedExitCommands(disp.Hold.Rig, disp.MRID)
		return fmt.Sprintf("MR %s from this branch is closed but git cannot tell whether its work landed (%s); "+
			"an overseer decides with `%s` or `%s`", mr, disp.Hold.Reason, notLanded, landed)
	case beads.TipRejected, beads.TipSuperseded:
		return fmt.Sprintf("MR %s of this tip was closed as %s and its work is not on a merge target; "+
			"to discard it, abandon it (archives the tip, then deletes): `%s`", mr, disp.Class, beads.AbandonCommand(rig, branch))
	case beads.TipConflict:
		return fmt.Sprintf("MR %s of this tip was closed for conflicts and its work is not on a merge target; "+
			"rework and resubmit it (the mayor decides); conflicted work is not abandoned", mr)
	case beads.TipNoVerdict:
		return fmt.Sprintf("MR %s of this tip was closed without an official verdict (rejected or superseded) and its work "+
			"is not on a merge target; rework and resubmit it (the mayor decides)", mr)
	default:
		return "no MR recorded this tip (work never submitted, or commits after the MR would be lost) and it is not on " +
			"a merge target; resubmit it (gt done / gt mq submit) or rework it (the mayor decides)"
	}
}

func (d polecatBranchDeletion) refuse(why string) error {
	if d.noGitProof != "" {
		why += " (git proof unavailable: " + d.noGitProof + ")"
	}
	return fmt.Errorf("discarding %s: %s. %s", d.branch, why, polecatDeletionExits)
}

// mrIDOf names the deciding MR in a refusal; an ID that is not
// beads.IsSafeID is not echoed.
func mrIDOf(d beads.TipDisposition) string {
	if id := strings.TrimSpace(d.MRID); beads.IsSafeID(id) {
		return id
	}
	return "(unknown)"
}

// gitLocalEnvVars are the repository-local variables git exports to hooks
// (`git rev-parse --local-env-vars`). In a worktree git sets GIT_DIR for the
// pre-push hook; reading the town's merge queue (bd, and git provers of other
// repositories) must not inherit them.
var gitLocalEnvVars = []string{
	"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_CONFIG", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT",
	"GIT_OBJECT_DIRECTORY", "GIT_DIR", "GIT_WORK_TREE", "GIT_IMPLICIT_WORK_TREE", "GIT_GRAFT_FILE",
	"GIT_INDEX_FILE", "GIT_NO_REPLACE_OBJECTS", "GIT_REPLACE_REF_BASE", "GIT_PREFIX",
	"GIT_SHALLOW_FILE", "GIT_COMMON_DIR",
}

// clearGitLocalEnv unsets gitLocalEnvVars in this process. runHooksPrePush
// calls it first: git runs the hook in the worktree root (or the bare git
// dir), so every git call of the guard finds the same repository from its
// working directory, and nothing it starts (bd, provers of rig repositories)
// is pointed at the pushing repository.
func clearGitLocalEnv() {
	for _, k := range gitLocalEnvVars {
		_ = os.Unsetenv(k)
	}
}

// errNoTown is the merge-queue error outside a Gas Town workspace.
var errNoTown = errors.New("no Gas Town workspace found for this repository")
