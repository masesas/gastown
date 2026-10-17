package cmd

import (
	"errors"
	"strings"
)

// Default branch of the pre-push guard (gt-pd1c.78 N1, round 3 M1/M2): which
// branches are protected as the default branch, which one proves work
// covered, and the push target they are read from.

// isMergeTarget reports whether branch is a merge target of the push: an
// integration/* branch, or the default branch. With a configured
// default_branch only that one; otherwise main and master always, and for an
// agent also the branch the push remote's HEAD names (the only case that
// reads the network, bounded: readRemoteHead). A human's push of another
// branch never triggers the lookup.
func (g *prePushGuard) isMergeTarget(branch string, r *prePushRig, agent bool) bool {
	if strings.HasPrefix(branch, integrationBranchPrefix) {
		return true
	}
	if r != nil && r.defaultBranch != "" {
		return branch == r.defaultBranch
	}
	if branch == "main" || branch == "master" {
		return true
	}
	return agent && branch == g.trustedDefault(r)
}

// trustedDefault is the one default branch the guard trusts as a merge
// target for coverage proofs (gt-pd1c.78 N1): the rig's configured
// default_branch, else the branch the push remote's own HEAD names
// (ls-remote --symref on the push remote or URL), never the local
// refs/remotes/origin/HEAD that any agent in the repository can repoint.
// "" when neither is known: no default branch counts as covering work.
func (g *prePushGuard) trustedDefault(r *prePushRig) string {
	if r != nil && r.defaultBranch != "" {
		return r.defaultBranch
	}
	if !g.remoteHeadDone {
		g.remoteHeadDone = true
		if target, err := g.pushTarget(); err == nil {
			read := g.git.RemoteHeadBranch
			if g.readRemoteHead != nil {
				read = g.readRemoteHead
			}
			if b, err := read(target); err == nil {
				g.remoteHead = strings.TrimSpace(b)
			}
		}
	}
	return g.remoteHead
}

// coverageDefaults are the default branches whose tips prove work preserved
// (mergeproof.RemoteTips.Defaults): only the trusted default, none when it
// is unknown (fail closed).
func (g *prePushGuard) coverageDefaults(r *prePushRig) []string {
	if b := g.trustedDefault(r); b != "" {
		return []string{b}
	}
	return nil
}

// defaultBranches returns the branch names protected as the default branch
// (target-role, fast-forward, land, scope and deletion rules); the first is
// the one used as merge base. The rig's default_branch when configured;
// otherwise the push remote's HEAD branch plus main and master, so an
// unknown or misleading default never weakens the push rules.
func (g *prePushGuard) defaultBranches(r *prePushRig) []string {
	if r != nil && r.defaultBranch != "" {
		return []string{r.defaultBranch}
	}
	return uniqueNonEmpty(g.trustedDefault(r), "main", "master")
}

// errInvalidPushRemote: an empty or option-looking remote/URL (L1).
var errInvalidPushRemote = errors.New("invalid push remote")

// pushTarget is the repository git pushes to (URL when known), validated so
// it cannot be taken for an option.
func (g *prePushGuard) pushTarget() (string, error) {
	target := g.url
	if target == "" {
		target = g.remote
	}
	if target == "" || strings.HasPrefix(target, "-") {
		return "", errInvalidPushRemote
	}
	return target, nil
}
