package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/mergeproof"
	"github.com/steveyegge/gastown/internal/workspace"
)

// Force-discard gate (gt-pd1c.78) of gt polecat nuke --force and gt polecat
// remove --force. --force skips the safety checks and discards the polecat's
// worktree (nuke also its local branch). Any caller may use it when, for every
// target, the polecat branch deletion rule (polecatBranchDeletion) holds and
// nothing else would be lost (no dirty worktree, no active hook bead:
// forceDiscardWorkCheck). Otherwise it is a human-only action: an agent
// (anything but a human or crew, humanOrCrewActor) is refused, and a human or
// crew confirms at a TTY by typing the targets again (humanPrompt.confirm).
// Nuke and remove without --force are not affected.

// Actions named in refusals and prompts.
const (
	nukeForceAction   = "gt polecat nuke --force"
	removeForceAction = "gt polecat remove --force"
)

// forceDiscardGate is the gate for one action. check reports what discarding
// one target would lose (nil = nothing); prompt is the human's terminal.
type forceDiscardGate struct {
	action string
	check  func(polecatTarget) error
	prompt humanPrompt
}

// newForceDiscardGate is the production gate: the deletion rule and the work
// check against the real rig, the human at stdio.
func newForceDiscardGate(action string) forceDiscardGate {
	return forceDiscardGate{action: action, check: forceDiscardCheck, prompt: stdioHumanPrompt()}
}

// forceDiscardCheck stops at a refused branch (the worktree and bead checks
// are then moot).
func forceDiscardCheck(p polecatTarget) error {
	if err := polecatTargetBranchDeletable(p); err != nil {
		return err
	}
	return forceDiscardWorkCheck(p)
}

// runIf runs the gate for an actual --force run (not a dry run).
func (g forceDiscardGate) runIf(targets []polecatTarget, force, dryRun bool) error {
	if !force || dryRun {
		return nil
	}
	return g.run(targets)
}

// run returns nil when the --force action on targets may proceed.
func (g forceDiscardGate) run(targets []polecatTarget) error {
	var unproven []string
	for _, p := range targets {
		if err := g.check(p); err != nil {
			why := strings.ReplaceAll(err.Error(), "\n", "; ")
			unproven = append(unproven, fmt.Sprintf("  - %s/%s: %s", p.rigName, p.polecatName, why))
		}
	}
	if len(unproven) == 0 {
		return nil
	}
	details := strings.Join(unproven, "\n")
	if _, err := humanOrCrewActor(g.action); err != nil {
		return fmt.Errorf("%w\nThe work of these polecats is not proven safe to discard:\n%s\n"+
			"Run it without --force (the safety checks apply), or escalate to the mayor or a human", err, details)
	}
	addrs := make([]string, 0, len(targets))
	for _, p := range targets {
		addrs = append(addrs, p.rigName+"/"+p.polecatName)
	}
	expected := strings.Join(addrs, " ")
	prompt := fmt.Sprintf("%s discards work that is not proven safe:\n%s\nType %q to confirm: ", g.action, details, expected)
	if err := g.prompt.confirm(g.action, prompt, expected); err != nil {
		return fmt.Errorf("%w\nNot proven safe to discard:\n%s", err, details)
	}
	return nil
}

// forceDiscardWorkCheck is the non-branch part of the gate (round 1, M1),
// built on the existing nuke safety checks (checkPolecatSafety) and git state
// (getGitState): a dirty worktree (uncommitted or untracked files that are not
// ignored, or stashes), an active hook bead, open work still assigned, or an
// agent bead that cannot be read refuses.
func forceDiscardWorkCheck(p polecatTarget) error {
	if p.mgr == nil || p.r == nil {
		return fmt.Errorf("cannot read the polecat (fail closed)")
	}
	res := checkPolecatSafety(p)
	state, stateErr := res.GitState, error(nil)
	if state == nil {
		state, stateErr = nukeWorktreeGitState(p)
	}
	if problems := forceNukeWorkProblems(res, state, stateErr); len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

// nukeWorktreeGitState reads the polecat worktree's git state; a worktree
// that no longer exists has nothing to lose (nil, nil).
func nukeWorktreeGitState(p polecatTarget) (*GitState, error) {
	info, err := p.mgr.Get(p.polecatName)
	if err != nil || info == nil {
		return nil, fmt.Errorf("cannot read the polecat")
	}
	if info.ClonePath == "" {
		return nil, nil
	}
	if _, err := os.Stat(info.ClonePath); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return getGitState(info.ClonePath)
}

// forceNukeWorkProblems lists what a --force run would lose besides the
// branch: from the safety-check result and the worktree git state.
func forceNukeWorkProblems(res *SafetyCheckResult, state *GitState, stateErr error) []string {
	var out []string
	if res != nil {
		if res.AgentBeadUnreadable {
			out = append(out, "agent bead unreadable: hook and cleanup state cannot be verified")
		}
		if res.HookBead != "" && !res.HookStale {
			out = append(out, fmt.Sprintf("has work on hook (%s)", res.HookBead))
		}
		if res.AssignedIssue != "" {
			out = append(out, fmt.Sprintf("open work %s is still assigned to it", res.AssignedIssue))
		}
	}
	switch {
	case stateErr != nil:
		out = append(out, "cannot check the worktree git state (fail closed)")
	case state != nil && (len(state.UncommittedFiles) > 0 || state.StashCount > 0):
		out = append(out, fmt.Sprintf("worktree is dirty (%d uncommitted/untracked file(s), %d stash(es))",
			len(state.UncommittedFiles), state.StashCount))
	}
	return out
}

// polecatTargetBranchDeletable applies the polecat branch deletion rule to the
// polecat's branch: its remote tip and, when different, its local tip (nuke
// deletes the local branch). An unreadable polecat or branch fails closed.
func polecatTargetBranchDeletable(p polecatTarget) error {
	if p.mgr == nil || p.r == nil {
		return fmt.Errorf("cannot read the polecat (fail closed)")
	}
	info, err := p.mgr.Get(p.polecatName)
	if err != nil || info == nil || strings.TrimSpace(info.Branch) == "" {
		return fmt.Errorf("cannot read the polecat's branch (fail closed)")
	}
	return polecatRepoBranchDeletable(getRepoGitForRig(p.r.Path), p.r.Name, strings.TrimSpace(info.Branch),
		p.r.DefaultBranch(), townMergeQueueFor(p.r.Path))
}

// polecatRepoBranchDeletable applies the deletion rule to branch in the rig
// repository repoGit (remote "origin"): the remote tip and, when different,
// the local tip must both pass. A branch that is on neither (origin read,
// no local ref) holds no work to lose; when origin cannot be read and there
// is no local tip, nothing is known and the deletion is refused.
func polecatRepoBranchDeletable(repoGit *git.Git, rigName, branch, defaultBranch string, queue func() *beads.MergeQueue) error {
	_ = repoGit.Fetch("origin") // best effort: remote target tips become local objects
	tips := rigRemoteTips(repoGit, defaultBranch)
	noGitProof := ""
	remoteTip, err := tips.BranchTip(branch)
	if err != nil {
		// Without the remote neither its tip nor the target tips are known:
		// the merge queue decides on the local tip alone.
		noGitProof = "cannot list origin: " + gitErrLine(err)
		tips = mergeproof.RemoteTips{}
	}
	localTip, _ := repoGit.Rev("refs/heads/" + branch + "^{commit}")
	candidates := uniqueNonEmpty(remoteTip, strings.TrimSpace(localTip))
	if len(candidates) == 0 {
		if noGitProof == "" {
			return nil // the branch exists nowhere: nothing to discard
		}
		return polecatBranchDeletion{rig: rigName, branch: branch, queue: queue, noGitProof: noGitProof}.refuse(
			"its tip is unknown (no local branch, origin unreadable)")
	}
	for _, tip := range candidates {
		d := polecatBranchDeletion{rig: rigName, branch: branch, tip: tip, tips: tips, git: repoGit, queue: queue, noGitProof: noGitProof}
		if err := d.check(); err != nil {
			return err
		}
	}
	return nil
}

// rigRemoteTips reads origin (its push URL) of a rig repository.
func rigRemoteTips(repoGit *git.Git, defaultBranch string) mergeproof.RemoteTips {
	return mergeproof.RemoteTips{
		Defaults: []string{defaultBranch},
		List: func(prefix string) ([]git.RemoteRef, error) {
			return repoGit.ListPushRemoteRefsWithHashes("origin", prefix)
		},
	}
}

// townMergeQueueFor reads the town merge queue of the rig at rigPath once,
// lazily.
func townMergeQueueFor(rigPath string) func() *beads.MergeQueue {
	var q *beads.MergeQueue
	return func() *beads.MergeQueue {
		if q == nil {
			town, err := workspace.Find(rigPath)
			if err != nil || town == "" {
				town = filepath.Dir(rigPath)
			}
			q = mergeproof.TownMergeQueue(town)
		}
		return q
	}
}

func uniqueNonEmpty(values ...string) []string {
	var out []string
	for _, v := range values {
		if v != "" && !containsString(out, v) {
			out = append(out, v)
		}
	}
	return out
}
