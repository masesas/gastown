package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/steveyegge/gastown/internal/git"
)

// Coordinator guards of the pre-push hook (gt-pd1c.78): who may delete a
// polecat branch and who may update a merge target. See hooks_prepush.go for
// the full rule set.

// checkPolecatDelete applies the polecat branch deletion rule
// (polecatBranchDeletion) to an agent other than crew.
func (g *prePushGuard) checkPolecatDelete(u prePushUpdate, branch string) error {
	if gtCallerFrom(g.getenv).exempt() {
		return nil
	}
	r, err := g.loadRigOnce()
	if err != nil {
		return fmt.Errorf("BLOCKED: agent deletion of %s: cannot load rig policy (%v); refusing (fail closed). %s",
			branch, err, polecatDeletionExits)
	}
	defaults := g.defaultBranches(r, true)
	tip := u.RemoteSHA
	if tip == prePushZeroSHA {
		tip = ""
	}
	d := polecatBranchDeletion{
		branch:  branch,
		tip:     tip,
		targets: func() ([]git.RemoteRef, error) { return g.remoteTargetTips(defaults) },
		git:     g.git,
		queue:   g.loadQueue,
	}
	if err := d.check(); err != nil {
		return fmt.Errorf("BLOCKED: agent %w", err)
	}
	return nil
}

// remoteTargetTips lists the merge targets on the push remote: the default
// branch(es) and every integration/* branch (ls-remote, never local refs).
func (g *prePushGuard) remoteTargetTips(defaults []string) ([]git.RemoteRef, error) {
	var out []git.RemoteRef
	for _, b := range defaults {
		sha, err := g.remoteBranchSHA(b)
		if err != nil {
			return nil, err
		}
		if sha != "" {
			out = append(out, git.RemoteRef{Name: "refs/heads/" + b, Hash: sha})
		}
	}
	target, err := g.pushTarget()
	if err != nil {
		return nil, err
	}
	integration, err := g.git.ListRemoteRefsWithHashes(target, "refs/heads/"+integrationBranchPrefix)
	if err != nil {
		return nil, errors.New("cannot list integration branches on the push remote")
	}
	return append(out, integration...), nil
}

// checkTargetRole refuses an update of a merge target by an agent role that
// does not land work. The refinery lands MRs; crew and humans keep their
// direct pushes (their other rules are unchanged). A polecat may push to a
// target only on a rig whose merge queue is disabled (gt done, direct
// strategy). Any agent may create an integration/* branch that carries no new
// commit (its tip is already on a remote target), as gt mq integration create
// does.
func (g *prePushGuard) checkTargetRole(u prePushUpdate, branch string, r *prePushRig, defaults []string) error {
	c := gtCallerFrom(g.getenv)
	if c.exempt() || c.role == RoleRefinery {
		return nil
	}
	if c.role == RolePolecat && r != nil && r.mergeQueueOff {
		return nil
	}
	if u.RemoteSHA == prePushZeroSHA && strings.HasPrefix(branch, integrationBranchPrefix) && g.alreadyOnTarget(u.LocalSHA, defaults) {
		return nil
	}
	return fmt.Errorf("BLOCKED: push to %s refused: %s may not update the default branch or integration/*. "+
		"Only the refinery lands work there (submit it with gt done or gt mq submit); crew and humans push directly",
		branch, c.describe())
}

// alreadyOnTarget reports whether commit is contained in a remote merge
// target's tip. Errors and missing objects count as "no" (fail closed).
func (g *prePushGuard) alreadyOnTarget(commit string, defaults []string) bool {
	if _, err := g.git.Rev(commit + "^{commit}"); err != nil {
		return false
	}
	tips, err := g.remoteTargetTips(defaults)
	if err != nil {
		return false
	}
	for _, tip := range tips {
		if _, err := g.git.Rev(tip.Hash + "^{commit}"); err != nil {
			continue
		}
		if in, err := g.git.IsAncestor(commit, tip.Hash); err == nil && in {
			return true
		}
	}
	return false
}
