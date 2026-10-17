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

// gt polecat nuke --force gate (gt-pd1c.78). --force skips the nuke safety
// checks and discards the polecat's branch and worktree. Any caller may use
// it when, for every target, the polecat branch deletion rule
// (polecatBranchDeletion) holds and nothing else would be lost (no dirty
// worktree, no active hook bead: nukeForceWorkCheck). Otherwise it is a
// human-only action: an agent (anything but a human or crew,
// humanOrCrewActor) is refused, and a human or crew confirms at a TTY by
// typing the targets again (confirmHumanInteractively). Nuke without --force
// is not affected.

// nukeForceAction names the action in refusals and prompts.
const nukeForceAction = "gt polecat nuke --force"

// nukeForceTargetCheck reports what a --force nuke of one target would lose
// (nil = nothing); tests replace it.
var nukeForceTargetCheck = func(p polecatTarget) error {
	if err := nukeTargetBranchDeletable(p); err != nil {
		return err // already refused: skip the worktree and bead checks
	}
	return nukeForceWorkCheck(p)
}

// gateNukeForceFor runs the gate for an actual --force nuke (not a dry run).
func gateNukeForceFor(targets []polecatTarget, force, dryRun bool) error {
	if !force || dryRun {
		return nil
	}
	return gateNukeForce(targets, nukeForceTargetCheck)
}

// gateNukeForce returns nil when the --force nuke of targets may proceed.
func gateNukeForce(targets []polecatTarget, check func(polecatTarget) error) error {
	var unproven []string
	for _, p := range targets {
		if err := check(p); err != nil {
			why := strings.ReplaceAll(err.Error(), "\n", "; ")
			unproven = append(unproven, fmt.Sprintf("  - %s/%s: %s", p.rigName, p.polecatName, why))
		}
	}
	if len(unproven) == 0 {
		return nil
	}
	details := strings.Join(unproven, "\n")
	if _, err := humanOrCrewActor(nukeForceAction); err != nil {
		return fmt.Errorf("%w\nThe work of these polecats is not proven safe to discard:\n%s\n"+
			"Use gt polecat nuke without --force (it runs the safety checks), or escalate to a human", err, details)
	}
	addrs := make([]string, 0, len(targets))
	for _, p := range targets {
		addrs = append(addrs, p.rigName+"/"+p.polecatName)
	}
	expected := strings.Join(addrs, " ")
	prompt := fmt.Sprintf("%s discards work that is not proven safe:\n%s\nType %q to confirm: ", nukeForceAction, details, expected)
	if err := confirmHumanInteractively(nukeForceAction, prompt, expected); err != nil {
		return fmt.Errorf("%w\nNot proven safe to discard:\n%s", err, details)
	}
	return nil
}

// nukeForceWorkCheck is the non-branch part of the gate (round 1, M1),
// built on the existing nuke safety checks (checkPolecatSafety) and git state
// (getGitState): a dirty worktree (uncommitted or untracked files that are not
// ignored, or stashes), an active hook bead, open work still assigned, or an
// agent bead that cannot be read refuses.
func nukeForceWorkCheck(p polecatTarget) error {
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

// forceNukeWorkProblems lists what a --force nuke would lose besides the
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

// nukeTargetBranchDeletable applies the polecat branch deletion rule to the
// polecat's branch: its remote tip and, when different, its local tip (the
// nuke deletes the local branch). An unreadable polecat or branch fails
// closed.
func nukeTargetBranchDeletable(p polecatTarget) error {
	if p.mgr == nil || p.r == nil {
		return fmt.Errorf("cannot read the polecat (fail closed)")
	}
	info, err := p.mgr.Get(p.polecatName)
	if err != nil || info == nil || strings.TrimSpace(info.Branch) == "" {
		return fmt.Errorf("cannot read the polecat's branch (fail closed)")
	}
	return polecatRepoBranchDeletable(getRepoGitForRig(p.r.Path), strings.TrimSpace(info.Branch),
		p.r.DefaultBranch(), nukeMergeQueue(p.r.Path))
}

// polecatRepoBranchDeletable applies the deletion rule to branch in the rig
// repository repoGit (remote "origin"): the remote tip and, when different,
// the local tip must both pass; with neither, the merge queue decides.
func polecatRepoBranchDeletable(repoGit *git.Git, branch, defaultBranch string, queue func() *beads.MergeQueue) error {
	_ = repoGit.Fetch("origin") // best effort: remote target tips become local objects
	tips := targetTipResolver{
		defaults: []string{defaultBranch},
		list: func(prefix string) ([]git.RemoteRef, error) {
			return repoGit.ListPushRemoteRefsWithHashes("origin", prefix)
		},
	}
	noGitProof := ""
	remoteTip, err := tips.branchTip(branch)
	if err != nil {
		// Without the remote neither its tip nor the target tips are known:
		// the merge queue decides on the local tip alone.
		noGitProof = "cannot list origin: " + firstLine(err.Error())
		tips = targetTipResolver{}
	}
	localTip, _ := repoGit.Rev("refs/heads/" + branch + "^{commit}")
	candidates := uniqueNonEmpty(remoteTip, strings.TrimSpace(localTip))
	if len(candidates) == 0 {
		return polecatBranchDeletion{branch: branch, queue: queue, noGitProof: noGitProof}.check()
	}
	for _, tip := range candidates {
		d := polecatBranchDeletion{branch: branch, tip: tip, tips: tips, git: repoGit, queue: queue, noGitProof: noGitProof}
		if err := d.check(); err != nil {
			return err
		}
	}
	return nil
}

// nukeMergeQueue reads the town merge queue once per target, lazily.
func nukeMergeQueue(rigPath string) func() *beads.MergeQueue {
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
