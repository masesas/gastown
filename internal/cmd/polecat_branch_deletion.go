package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/git"
)

// Polecat branch deletion rule (gt-pd1c.78), shared by the pre-push guard
// (an agent deleting polecat/* on the remote) and gt polecat nuke --force.
//
// A polecat branch may be deleted by an agent only when that loses no work:
//
//  1. its tip is preserved (git.PreservedOn: ancestor, merge-tree no-op or
//     every patch present, the gt polecat prune rule) on the remote default
//     branch or a remote integration/* branch; or
//  2. the merge queue (beads.MergeQueue.BranchHold, the one predicate) says
//     HoldNone: no MR open from the branch and every MR from it definitely not
//     landed (or none at all); or
//  3. HoldLanded and the deciding MR's commit_sha is exactly the tip (the MR
//     landed and the branch carries nothing after it).
//
// Anything else (an open MR, a landing git cannot decide, landed work with
// later commits, an unreadable queue) is refused: fail closed. Humans and crew
// are not subject to the rule.

// polecatDeletionGit is the git surface of the landed proof.
type polecatDeletionGit interface {
	Rev(ref string) (string, error)
	PreservedOn(head, base string) (git.BranchPreservationStatus, error)
}

// polecatBranchDeletion is one polecat branch an agent wants to delete.
type polecatBranchDeletion struct {
	branch string // e.g. polecat/nux/gt-1
	tip    string // the remote tip being deleted; "" when absent or unknown
	// targets lists the merge-target tips on the remote (default branch and
	// integration/*). An error only skips the git proof.
	targets func() ([]git.RemoteRef, error)
	git     polecatDeletionGit
	// queue reads the town's merge queue; called only when the git proof
	// fails. nil counts as unreadable.
	queue func() *beads.MergeQueue
}

// polecatDeletionExits is the guidance appended to every refusal.
const polecatDeletionExits = "Landed polecat branches are deleted by the refinery or `gt mq post-merge <rig> <mr-id>`, " +
	"preserved ones by `gt polecat prune --remote`; anything else needs a human or crew"

// check returns nil when the branch may be deleted, else why not.
func (d polecatBranchDeletion) check() error {
	if d.preservedOnTarget() {
		return nil
	}
	if d.queue == nil {
		return d.refuse("the merge queue is not available (fail closed)")
	}
	detail, err := d.queue().BranchHold(d.branch)
	if err != nil {
		return d.refuse("cannot read the merge queue: " + firstLine(err.Error()) + " (fail closed)")
	}
	switch detail.Hold {
	case beads.HoldNone:
		return nil
	case beads.HoldPending:
		return d.refuse(fmt.Sprintf("MR %s from this branch is open; the refinery owns the branch until it merges", mrIDOf(detail)))
	case beads.HoldLanded:
		if detail.MR != nil && d.tip != "" {
			if fields := beads.ParseMRFields(detail.MR); fields != nil && strings.TrimSpace(fields.CommitSHA) == d.tip {
				return nil
			}
		}
		return d.refuse(fmt.Sprintf("MR %s from this branch landed, but the branch tip is not on a merge target "+
			"(commits after the MR would be lost)", mrIDOf(detail)))
	default:
		notLanded, landed := beads.UnverifiedExitCommands(detail.Rig, detail.MRID)
		return d.refuse(fmt.Sprintf("MR %s from this branch is closed but git cannot tell whether its work landed (%s); "+
			"an overseer decides with `%s` or `%s`", mrIDOf(detail), detail.Reason, notLanded, landed))
	}
}

// preservedOnTarget is the git proof: the tip is preserved on a remote merge
// target. Missing objects or git errors only mean "not proven".
func (d polecatBranchDeletion) preservedOnTarget() bool {
	if d.tip == "" || d.git == nil || d.targets == nil {
		return false
	}
	if _, err := d.git.Rev(d.tip + "^{commit}"); err != nil {
		return false
	}
	refs, err := d.targets()
	if err != nil {
		return false
	}
	for _, ref := range refs {
		if ref.Hash == "" {
			continue
		}
		if _, err := d.git.Rev(ref.Hash + "^{commit}"); err != nil {
			continue
		}
		if st, err := d.git.PreservedOn(d.tip, ref.Hash); err == nil && st.Preserved {
			return true
		}
	}
	return false
}

func (d polecatBranchDeletion) refuse(why string) error {
	return fmt.Errorf("deleting %s: %s. %s", d.branch, why, polecatDeletionExits)
}

// mrIDOf names the deciding MR in a refusal; an ID that is not
// beads.IsSafeID is not echoed.
func mrIDOf(d beads.HoldDetail) string {
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

// clearGitLocalEnv unsets gitLocalEnvVars in this process. The pre-push
// guard runs in the hook's working directory (worktree root or bare git
// dir), so its own git calls find the same repository without them.
func clearGitLocalEnv() {
	for _, k := range gitLocalEnvVars {
		_ = os.Unsetenv(k)
	}
}

// errNoTown is the merge-queue error outside a Gas Town workspace.
var errNoTown = errors.New("no Gas Town workspace found for this repository")
