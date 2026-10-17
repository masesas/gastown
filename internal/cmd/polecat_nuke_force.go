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

// polecatRepoBranchDeletable applies the deletion rule to one branch of the
// rig repository repoGit (newRigBranchRule; one-shot callers).
func polecatRepoBranchDeletable(repoGit *git.Git, rigName, branch, defaultBranch string, queue func() *beads.MergeQueue) error {
	return newRigBranchRule(repoGit, rigName, defaultBranch, queue).deletable(branch)
}

// rigBranchRule is the polecat branch deletion rule bound to one rig
// repository (remote "origin"), read once for many branches (gt polecat gc):
// one fetch, one ls-remote of polecat/*, one cached reader of targets and
// archives, one merge queue (gt-pd1c.78 G-M2).
type rigBranchRule struct {
	rig, defaultBranch string
	repo               *git.Git
	tips               mergeproof.RemoteTips
	proofGit           mergeproof.PreservationGit // nil: no coverage proof (fetch failed)
	branchTips         map[string]string          // remote polecat/* tips; nil: origin unreadable
	noGitProof         string
	queue              func() *beads.MergeQueue
}

// newRigBranchRule fetches origin and lists its polecat branches. A failed
// fetch disables the coverage proof (stale local target objects prove
// nothing, fail closed, G-M1); archives are matched by object name and still
// count. An unreadable origin leaves only the merge queue and the local tip.
func newRigBranchRule(repoGit *git.Git, rigName, defaultBranch string, queue func() *beads.MergeQueue) *rigBranchRule {
	r := &rigBranchRule{rig: rigName, defaultBranch: defaultBranch, repo: repoGit, queue: queue, proofGit: repoGit,
		tips: rigRemoteTips(repoGit, defaultBranch)}
	if err := repoGit.Fetch("origin"); err != nil {
		r.noGitProof = "cannot fetch origin: " + gitErrLine(err)
		r.proofGit = nil
	}
	refs, err := r.tips.List("refs/heads/polecat/")
	if err != nil {
		// Without the remote neither its tips nor the target tips are known:
		// the merge queue decides on the local tip alone.
		r.noGitProof = "cannot list origin: " + gitErrLine(err)
		r.tips, r.proofGit = mergeproof.RemoteTips{}, nil
		return r
	}
	r.branchTips = map[string]string{}
	for _, ref := range refs {
		r.branchTips[strings.TrimPrefix(ref.Name, "refs/heads/")] = strings.TrimSpace(ref.Hash)
	}
	return r
}

// deletable applies the rule to branch: its remote tip and, when different,
// its local tip must both pass. A branch that is on neither (origin read, no
// local ref) holds no work to lose; when origin cannot be read and there is
// no local tip, nothing is known and the deletion is refused.
func (r *rigBranchRule) deletable(branch string) error {
	remoteTip := r.branchTips[branch]
	if r.branchTips != nil && !strings.HasPrefix(branch, "polecat/") {
		// Not in the polecat/* listing: read this branch's tip itself.
		tip, err := r.tips.BranchTip(branch)
		if err != nil {
			return polecatBranchDeletion{rig: r.rig, branch: branch, noGitProof: "cannot list origin: " + gitErrLine(err)}.
				refuse("its remote tip is unknown")
		}
		remoteTip = tip
	}
	localTip, _ := r.repo.Rev("refs/heads/" + branch + "^{commit}")
	candidates := uniqueNonEmpty(remoteTip, strings.TrimSpace(localTip))
	base := polecatBranchDeletion{rig: r.rig, branch: branch, tips: r.tips, git: r.proofGit, queue: r.queue, noGitProof: r.noGitProof}
	if len(candidates) == 0 {
		if r.branchTips != nil {
			return nil // the branch exists nowhere: nothing to discard
		}
		return base.refuse("its tip is unknown (no local branch, origin unreadable)")
	}
	for _, tip := range candidates {
		d := base
		d.tip = tip
		if err := d.check(); err != nil {
			return err
		}
	}
	return nil
}

// rigRemoteTips reads origin (its push URL) of a rig repository, each prefix
// once.
func rigRemoteTips(repoGit *git.Git, defaultBranch string) mergeproof.RemoteTips {
	return mergeproof.RemoteTips{
		Defaults: []string{defaultBranch},
		List: mergeproof.CachedLister(func(prefix string) ([]git.RemoteRef, error) {
			return repoGit.ListPushRemoteRefsWithHashes("origin", prefix)
		}),
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
