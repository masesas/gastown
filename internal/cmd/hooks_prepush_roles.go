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
// crew, or a rewrite (not a fast-forward) by an agent other than crew and the
// refinery (round 1, H1: a force-push onto a landed commit followed by a
// delete must not launder unlanded work). The owning polecat's rewrite is the
// same rule with one more allowance (round 1 of tahap 2, S-M2): an old tip
// that was never submitted (work in progress, a rebase before gt done) or
// whose MR is still open (the rebase that REWORK_REQUEST and MERGE_FAILED ask
// for before resubmitting). A tip whose MR was decided (rejected, superseded,
// conflict, closed) is not rewritten away by its owner either: abandon it
// first. Creations and fast-forwards lose nothing and pass without I/O beyond
// git.
func (g *prePushGuard) checkPolecatUpdate(u prePushUpdate, branch string) error {
	c := gtCallerFrom(g.getenv)
	if c.exempt() || u.RemoteSHA == prePushZeroSHA {
		return nil
	}
	deletion := u.LocalSHA == prePushZeroSHA
	if !deletion && (c.role == RoleRefinery || g.fastForward(u)) {
		return nil
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
		tips:   g.tipResolver(g.coverageDefaults(r)),
		git:    g.git,
		queue:  g.loadQueue,
		owner:  !deletion && c.ownsPolecatBranch(branch),
	}
	if err := d.check(); err != nil {
		switch {
		case deletion:
			return fmt.Errorf("BLOCKED: agent deletion: %w", err)
		case d.owner:
			return fmt.Errorf("BLOCKED: rewrite (not a fast-forward) of your own polecat branch over a tip whose MR "+
				"was already decided; push without rewriting (merge instead of rebase) or have it abandoned first: %w", err)
		default:
			return fmt.Errorf("BLOCKED: agent rewrite (not a fast-forward) of a polecat branch it does not own: %w", err)
		}
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
func (g *prePushGuard) checkTargetRole(u prePushUpdate, branch string, r *prePushRig) error {
	c := gtCallerFrom(g.getenv)
	if c.exempt() || c.role == RoleRefinery {
		return nil
	}
	if c.role == RolePolecat && r != nil && r.mergeQueueOff {
		return nil
	}
	if u.RemoteSHA == prePushZeroSHA && strings.HasPrefix(branch, integrationBranchPrefix) &&
		mergeproof.CoveredByTargets(g.git, u.LocalSHA, g.tipResolver(g.coverageDefaults(r))) {
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

// refineryMergeExit is the guidance appended to every rule 2(c) refusal.
const refineryMergeExit = "The refinery lands work only with gt mq merge <rig> <mr-id> " +
	"(a merge commit of an open MR's commit_sha onto the current tip, pushed by SHA)"

// checkRefineryMerge is rule 2(c): a refinery update of a merge target of its
// rig must have the shape gt mq merge pushes (see the file doc in
// hooks_prepush.go). Other callers, and pushes outside a detected rig, are
// not checked here. Everything that cannot be verified refuses.
func (g *prePushGuard) checkRefineryMerge(u prePushUpdate, branch string, r *prePushRig) error {
	c := gtCallerFrom(g.getenv)
	if r == nil || c.exempt() || c.role != RoleRefinery {
		return nil
	}
	refuse := func(why string) error {
		return fmt.Errorf("BLOCKED: refinery push to %s refused: %s. %s", branch, why, refineryMergeExit)
	}
	if u.RemoteSHA == prePushZeroSHA {
		if strings.HasPrefix(branch, integrationBranchPrefix) &&
			mergeproof.CoveredByTargets(g.git, u.LocalSHA, g.tipResolver(g.coverageDefaults(r))) {
			return nil // gt mq integration create: no new commit
		}
		return refuse("the refinery does not create merge targets")
	}
	parents, err := g.git.CommitParents(u.LocalSHA)
	if err != nil {
		return refuse("cannot read the parents of the pushed commit (" + firstLine(err.Error()) + "; fail closed)")
	}
	if len(parents) != 2 {
		return refuse(fmt.Sprintf("the pushed commit %.12s has %d parent(s), not a merge of exactly two (a fast-forward or octopus is never pushed by gt mq merge)", u.LocalSHA, len(parents)))
	}
	if parents[0] != u.RemoteSHA {
		return refuse(fmt.Sprintf("the merge's first parent %.12s is not the current remote tip %.12s", parents[0], u.RemoteSHA))
	}
	if err := g.cleanMergeTree(u.LocalSHA, parents); err != nil {
		return refuse(err.Error())
	}
	if g.integrationLandMerge(u, r, parents[1]) {
		return nil
	}
	if g.loadQueue == nil {
		return refuse("the merge queue is not available (fail closed)")
	}
	mrID, err := g.loadQueue().OpenMergeRequestFor(r.name, branch, parents[1])
	if err != nil {
		return refuse("cannot read the merge queue: " + firstLine(err.Error()) + " (fail closed)")
	}
	if mrID == "" {
		return refuse(fmt.Sprintf("the merged commit %.12s is not the commit_sha of an open merge request for %s", parents[1], branch))
	}
	return nil
}

// cleanMergeTree checks that commit's tree is exactly the clean merge of its
// two parents: nothing was added, dropped or resolved by hand in the merge.
func (g *prePushGuard) cleanMergeTree(commit string, parents []string) error {
	want, err := g.git.MergeTreeWrite(parents[0], parents[1])
	if err != nil {
		return fmt.Errorf("the parents do not merge cleanly (%s)", firstLine(err.Error()))
	}
	// Read like the proofs: a local `git replace` of the pushed commit must
	// not show the clean tree while the push sends the original object.
	got, err := g.git.CommitTree(commit)
	if err != nil {
		return fmt.Errorf("cannot read the pushed tree (%s; fail closed)", firstLine(err.Error()))
	}
	if strings.TrimSpace(got) != strings.TrimSpace(want) {
		return fmt.Errorf("the merge's tree %.12s differs from the clean merge of its parents %.12s (changes were added in the merge)", got, want)
	}
	return nil
}

// integrationLandMerge reports whether the merge is gt mq integration land's:
// auto-land on for the rig, GT_INTEGRATION_LAND=1, and the merged commit the
// tip of another integration/* branch on the push remote (checkLand already
// allowed that land).
func (g *prePushGuard) integrationLandMerge(u prePushUpdate, r *prePushRig, merged string) bool {
	if g.getenv("GT_INTEGRATION_LAND") != "1" || !r.autoLand.enabled {
		return false
	}
	tips, err := g.listRemote("refs/heads/" + integrationBranchPrefix)
	if err != nil {
		return false
	}
	for _, tip := range tips {
		if tip.Name != u.RemoteRef && tip.Hash == merged {
			return true
		}
	}
	return false
}
