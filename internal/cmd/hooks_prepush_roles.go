package cmd

import (
	"fmt"
	"strings"

	"github.com/steveyegge/gastown/internal/git"
)

// Coordinator guards of the pre-push hook (gt-pd1c.78): who may discard a
// polecat branch tip (delete or rewrite) and who may update a merge target.
// See hooks_prepush.go for the full rule set.

// tipResolver is the guard's targetTipResolver on the push remote.
func (g *prePushGuard) tipResolver(defaults []string) targetTipResolver {
	return targetTipResolver{
		defaults: defaults,
		list: func(prefix string) ([]git.RemoteRef, error) {
			target, err := g.pushTarget()
			if err != nil {
				return nil, err
			}
			return g.git.ListRemoteRefsWithHashes(target, prefix)
		},
	}
}

// checkPolecatUpdate applies the polecat branch deletion rule to an update of
// polecat/* that discards the remote tip: a deletion by any agent other than
// crew, or a rewrite (not a fast-forward) by an agent other than crew, the
// refinery and the polecat that owns the branch (round 1, H1: a force-push
// onto a landed commit followed by a delete must not launder unlanded work).
// Creations and fast-forwards lose nothing and pass without I/O beyond git.
func (g *prePushGuard) checkPolecatUpdate(u prePushUpdate, branch string) error {
	c := gtCallerFrom(g.getenv)
	if c.exempt() || u.RemoteSHA == prePushZeroSHA {
		return nil
	}
	deletion := u.LocalSHA == prePushZeroSHA
	if !deletion {
		if c.role == RoleRefinery || c.ownsPolecatBranch(branch) || g.fastForward(u) {
			return nil
		}
	}
	r, err := g.loadRigOnce()
	if err != nil {
		return fmt.Errorf("BLOCKED: agent update of %s: cannot load rig policy (%v); refusing (fail closed). %s",
			branch, err, polecatDeletionExits)
	}
	d := polecatBranchDeletion{
		branch: branch,
		tip:    u.RemoteSHA,
		tips:   g.tipResolver(g.defaultBranches(r, true)),
		git:    g.git,
		queue:  g.loadQueue,
	}
	if err := d.check(); err != nil {
		if deletion {
			return fmt.Errorf("BLOCKED: agent deletion: %w", err)
		}
		return fmt.Errorf("BLOCKED: agent rewrite (not a fast-forward) of a polecat branch it does not own: %w", err)
	}
	return nil
}

// fastForward reports whether the update keeps the remote tip (remote tip
// known locally and an ancestor of the pushed commit). Unknown = no.
func (g *prePushGuard) fastForward(u prePushUpdate) bool {
	if _, err := g.git.Rev(u.RemoteSHA + "^{commit}"); err != nil {
		return false
	}
	ff, err := g.git.IsAncestor(u.RemoteSHA, u.LocalSHA)
	return err == nil && ff
}

// checkTargetRole refuses an update of a merge target by an agent role that
// does not land work. The refinery lands MRs; crew and humans keep their
// direct pushes (their other rules are unchanged). A polecat may push to a
// target only on a rig whose merge queue is disabled (gt done, direct
// strategy). Any agent may create an integration/* branch that carries no new
// commit (tipCoveredByTargets), as gt mq integration create does.
func (g *prePushGuard) checkTargetRole(u prePushUpdate, branch string, r *prePushRig, defaults []string) error {
	c := gtCallerFrom(g.getenv)
	if c.exempt() || c.role == RoleRefinery {
		return nil
	}
	if c.role == RolePolecat && r != nil && r.mergeQueueOff {
		return nil
	}
	if u.RemoteSHA == prePushZeroSHA && strings.HasPrefix(branch, integrationBranchPrefix) &&
		tipCoveredByTargets(g.git, u.LocalSHA, g.tipResolver(defaults)) {
		return nil
	}
	return fmt.Errorf("BLOCKED: push to %s refused: %s may not update the default branch or integration/*. "+
		"Only the refinery lands work there (submit it with gt done or gt mq submit); crew and humans push directly",
		branch, c.describe())
}
