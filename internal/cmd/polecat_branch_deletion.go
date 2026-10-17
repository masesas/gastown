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
// (an agent deleting or rewriting polecat/* on the remote) and gt polecat
// nuke --force.
//
// A polecat branch tip may be discarded by an agent only when that loses no
// work:
//
//  1. the tip is covered by a merge target (tipCoveredByTargets: preserved
//     on the remote default branch or a remote integration/* branch, the
//     gt polecat prune rule); or
//  2. the merge queue (beads.MergeQueue.BranchHold, the one predicate) says
//     HoldNone: no MR open from the branch and every MR from it definitely not
//     landed (or none at all). An MR closed by hand that git proves did not
//     land counts as "not landed" (user decision for gt-pd1c.78); or
//  3. HoldLanded proven by git (HoldDetail.Evidence == EvidenceGit) and the
//     deciding MR's commit_sha is exactly the tip. A landed verdict that rests
//     only on bead fields (close_reason/merge_commit) or a human verdict is
//     not trusted for discarding work.
//
// Anything else (an open MR, a landing git cannot decide, landed work with
// later commits, an unreadable queue) is refused: fail closed. Humans and crew
// are not subject to the rule.

// polecatDeletionGit is the git surface of the landed proof.
type polecatDeletionGit interface {
	Rev(ref string) (string, error)
	PreservedOn(head, base string) (git.BranchPreservationStatus, error)
}

// remoteRefLister lists refs on the remote under prefix (ls-remote, prefix*).
type remoteRefLister func(prefix string) ([]git.RemoteRef, error)

// targetTipResolver is the one reader of remote tips for the coordinator
// guards: a branch's tip and the merge-target tips (default branch(es) and
// integration/*). It reads the remote, never local refs.
type targetTipResolver struct {
	list     remoteRefLister
	defaults []string
}

// branchTip returns the tip of refs/heads/<branch> on the remote, "" when the
// branch does not exist there.
func (r targetTipResolver) branchTip(branch string) (string, error) {
	ref := "refs/heads/" + branch
	refs, err := r.list(ref)
	if err != nil {
		return "", err
	}
	for _, x := range refs {
		if x.Name == ref {
			return strings.TrimSpace(x.Hash), nil
		}
	}
	return "", nil
}

// targetTips lists the default branch(es) and every integration/* branch.
func (r targetTipResolver) targetTips() ([]git.RemoteRef, error) {
	var out []git.RemoteRef
	for _, b := range r.defaults {
		if b = strings.TrimSpace(b); b == "" {
			continue
		}
		sha, err := r.branchTip(b)
		if err != nil {
			return nil, err
		}
		if sha != "" {
			out = append(out, git.RemoteRef{Name: "refs/heads/" + b, Hash: sha})
		}
	}
	integration, err := r.list("refs/heads/" + integrationBranchPrefix)
	if err != nil {
		return nil, errors.New("cannot list integration branches on the remote")
	}
	return append(out, integration...), nil
}

// tipCoveredByTargets is the one "is this commit already on a merge target?"
// proof: commit is preserved (git.PreservedOn: ancestor, merge-tree no-op or
// every patch present) on one of the resolver's target tips. Missing objects,
// an unreadable remote or git errors mean "not proven" (false).
func tipCoveredByTargets(g polecatDeletionGit, commit string, tips targetTipResolver) bool {
	if commit == "" || g == nil || tips.list == nil {
		return false
	}
	if _, err := g.Rev(commit + "^{commit}"); err != nil {
		return false
	}
	refs, err := tips.targetTips()
	if err != nil {
		return false
	}
	for _, ref := range refs {
		if ref.Hash == "" {
			continue
		}
		if _, err := g.Rev(ref.Hash + "^{commit}"); err != nil {
			continue
		}
		if st, err := g.PreservedOn(commit, ref.Hash); err == nil && st.Preserved {
			return true
		}
	}
	return false
}

// polecatBranchDeletion is one polecat branch tip an agent wants to discard
// (delete the branch, or rewrite it away from this tip).
type polecatBranchDeletion struct {
	branch string // e.g. polecat/nux/gt-1
	tip    string // the tip being discarded; "" when absent or unknown
	tips   targetTipResolver
	git    polecatDeletionGit
	// queue reads the town's merge queue; called only when the git proof
	// fails. nil counts as unreadable.
	queue func() *beads.MergeQueue
}

// polecatDeletionExits is the guidance appended to every refusal.
const polecatDeletionExits = "Landed polecat branches are deleted by the refinery or `gt mq post-merge <rig> <mr-id>`, " +
	"preserved ones by `gt polecat prune --remote`; anything else needs a human or crew"

// check returns nil when the tip may be discarded, else why not.
func (d polecatBranchDeletion) check() error {
	if tipCoveredByTargets(d.git, d.tip, d.tips) {
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
		return d.checkLanded(detail)
	default:
		notLanded, landed := beads.UnverifiedExitCommands(detail.Rig, detail.MRID)
		return d.refuse(fmt.Sprintf("MR %s from this branch is closed but git cannot tell whether its work landed (%s); "+
			"an overseer decides with `%s` or `%s`", mrIDOf(detail), detail.Reason, notLanded, landed))
	}
}

// checkLanded allows a landed branch only on git proof and an exact tip.
func (d polecatBranchDeletion) checkLanded(detail beads.HoldDetail) error {
	if detail.Evidence != beads.EvidenceGit {
		return d.refuse(fmt.Sprintf("MR %s from this branch is marked landed (%s), but git has not proven its tip "+
			"is on a merge target; fetch and retry", mrIDOf(detail), detail.Evidence))
	}
	if detail.MR != nil && d.tip != "" {
		if fields := beads.ParseMRFields(detail.MR); fields != nil && strings.TrimSpace(fields.CommitSHA) == d.tip {
			return nil
		}
	}
	return d.refuse(fmt.Sprintf("MR %s from this branch landed, but the branch tip is not on a merge target "+
		"(commits after the MR would be lost)", mrIDOf(detail)))
}

func (d polecatBranchDeletion) refuse(why string) error {
	return fmt.Errorf("discarding %s: %s. %s", d.branch, why, polecatDeletionExits)
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
