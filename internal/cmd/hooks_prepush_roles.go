package cmd

import (
	"fmt"
	"strings"

	"github.com/steveyegge/gastown/internal/mergeproof"
)

// Coordinator guards of the pre-push hook (gt-pd1c.78): who may discard a
// polecat branch tip (delete or rewrite) and who may update a merge target.
// See hooks_prepush.go for the full rule set.

// tipResolver reads tips from the push remote through the guard's cached
// lister (listRemote).
func (g *prePushGuard) tipResolver(defaults []string) mergeproof.RemoteTips {
	return mergeproof.RemoteTips{Defaults: defaults, List: g.listRemote}
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
		rig:    rigNameOf(r),
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
// commit (mergeproof.CoveredByTargets), as gt mq integration create does.
func (g *prePushGuard) checkTargetRole(u prePushUpdate, branch string, r *prePushRig, defaults []string) error {
	c := gtCallerFrom(g.getenv)
	if c.exempt() || c.role == RoleRefinery {
		return nil
	}
	if c.role == RolePolecat && r != nil && r.mergeQueueOff {
		return nil
	}
	if u.RemoteSHA == prePushZeroSHA && strings.HasPrefix(branch, integrationBranchPrefix) &&
		mergeproof.CoveredByTargets(g.git, u.LocalSHA, g.tipResolver(defaults)) {
		return nil
	}
	return fmt.Errorf("BLOCKED: push to %s refused: %s may not update the default branch or integration/*. "+
		"Only the refinery lands work there (submit it with gt done or gt mq submit); crew and humans push directly",
		branch, c.describe())
}

func rigNameOf(r *prePushRig) string {
	if r == nil {
		return ""
	}
	return r.name
}

// archiveExits is the guidance appended to every archive refusal.
const archiveExits = "Archives are created by `gt polecat abandon`; pruning them is for a human or crew"

// checkArchiveUpdate guards the archive namespace (refs/gt/archive/*, gt-pd1c.78
// tahap 2). Archives are what makes discarding unlanded polecat work safe
// (polecatBranchDeletion), so an agent other than crew may only create one,
// with gt polecat abandon's shape (mergeproof.ArchiveRef: the stamp names the
// archived commit) and only for a tip the merge queue lets be abandoned (its
// MR closed as rejected or superseded, beads.TipDisposition.Abandonable).
// Deleting or rewriting an archive (pruning) is for humans and crew.
func (g *prePushGuard) checkArchiveUpdate(u prePushUpdate) error {
	if gtCallerFrom(g.getenv).exempt() {
		return nil
	}
	switch {
	case u.LocalSHA == prePushZeroSHA:
		return fmt.Errorf("BLOCKED: agents may not delete archive ref %s. %s", u.RemoteRef, archiveExits)
	case u.RemoteSHA != prePushZeroSHA:
		return fmt.Errorf("BLOCKED: agents may not rewrite archive ref %s. %s", u.RemoteRef, archiveExits)
	}
	branch, short, ok := mergeproof.ParseArchiveRef(u.RemoteRef)
	if !ok || !strings.HasPrefix(u.LocalSHA, short) {
		return fmt.Errorf("BLOCKED: %s is not an archive ref for commit %.12s "+
			"(refs/gt/archive/<polecat-branch>/<unix>-<sha>). %s", u.RemoteRef, u.LocalSHA, archiveExits)
	}
	if g.loadQueue == nil {
		return fmt.Errorf("BLOCKED: archiving %s: the merge queue is not available (fail closed). %s", branch, archiveExits)
	}
	disp, err := g.loadQueue().TipDisposition(branch, u.LocalSHA)
	if err != nil {
		return fmt.Errorf("BLOCKED: archiving %s: cannot read the merge queue: %s (fail closed). %s",
			branch, firstLine(err.Error()), archiveExits)
	}
	if !disp.Abandonable() {
		r, _ := g.loadRigOnce()
		return fmt.Errorf("BLOCKED: archiving %s: only work whose MR was closed as rejected or superseded may be "+
			"abandoned: %s", branch, tipRefusal(rigNameOf(r), branch, disp))
	}
	return nil
}
