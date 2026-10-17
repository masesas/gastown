package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/mergeproof"
	"github.com/steveyegge/gastown/internal/workspace"
)

// gt polecat nuke --force gate (gt-pd1c.78). --force skips the nuke safety
// checks and discards the polecat's branch. Any caller may use it when the
// polecat branch deletion rule (polecatBranchDeletion) holds for every target;
// otherwise it is a human-only action: an agent (anything but a human or
// crew, humanOrCrewActor) is refused, and a human or crew confirms at a TTY by
// typing the targets again (confirmHumanInteractively). Nuke without --force
// is not affected.

// nukeForceTargetCheck applies the deletion rule to one target; tests replace
// it.
var nukeForceTargetCheck = nukeTargetBranchDeletable

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
			unproven = append(unproven, fmt.Sprintf("  - %s/%s: %v", p.rigName, p.polecatName, err))
		}
	}
	if len(unproven) == 0 {
		return nil
	}
	details := strings.Join(unproven, "\n")
	if _, err := humanOrCrewActor("gt polecat nuke --force"); err != nil {
		return fmt.Errorf("%w\nThe work of these polecats is not proven safe to discard:\n%s\n"+
			"Use gt polecat nuke without --force (it runs the safety checks), or escalate to a human", err, details)
	}
	addrs := make([]string, 0, len(targets))
	for _, p := range targets {
		addrs = append(addrs, p.rigName+"/"+p.polecatName)
	}
	expected := strings.Join(addrs, " ")
	prompt := fmt.Sprintf("gt polecat nuke --force discards work that is not proven safe:\n%s\nType %q to confirm: ", details, expected)
	if err := confirmHumanInteractively(prompt, expected); err != nil {
		return fmt.Errorf("gt polecat nuke --force: %w\nNot proven safe to discard:\n%s", err, details)
	}
	return nil
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
	remoteTip := pushRemoteTip(repoGit, branch)
	localTip, _ := repoGit.Rev("refs/heads/" + branch + "^{commit}")
	defaults := []string{defaultBranch}
	tips := uniqueNonEmpty(remoteTip, strings.TrimSpace(localTip))
	if len(tips) == 0 {
		return polecatBranchDeletion{branch: branch, queue: queue}.check()
	}
	for _, tip := range tips {
		d := polecatBranchDeletion{
			branch:  branch,
			tip:     tip,
			targets: func() ([]git.RemoteRef, error) { return pushRemoteTargetTips(repoGit, defaults) },
			git:     repoGit,
			queue:   queue,
		}
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

// pushRemoteTip returns the tip of branch on origin's push URL, "" when absent
// or unreadable (the git proof is then skipped).
func pushRemoteTip(g *git.Git, branch string) string {
	refs, err := g.ListPushRemoteRefsWithHashes("origin", "refs/heads/"+branch)
	if err != nil {
		return ""
	}
	for _, ref := range refs {
		if ref.Name == "refs/heads/"+branch {
			return strings.TrimSpace(ref.Hash)
		}
	}
	return ""
}

// pushRemoteTargetTips lists the default branch and integration/* tips on
// origin's push URL.
func pushRemoteTargetTips(g *git.Git, defaults []string) ([]git.RemoteRef, error) {
	var out []git.RemoteRef
	for _, b := range defaults {
		if b = strings.TrimSpace(b); b == "" {
			continue
		}
		refs, err := g.ListPushRemoteRefsWithHashes("origin", "refs/heads/"+b)
		if err != nil {
			return nil, err
		}
		for _, ref := range refs {
			if ref.Name == "refs/heads/"+b {
				out = append(out, ref)
			}
		}
	}
	integration, err := g.ListPushRemoteRefsWithHashes("origin", "refs/heads/"+integrationBranchPrefix)
	if err != nil {
		return nil, err
	}
	return append(out, integration...), nil
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
