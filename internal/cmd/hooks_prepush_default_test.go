package cmd

import (
	"strings"
	"testing"
)

// gt-pd1c.78 N1: without a configured default_branch the guard never trusts
// the local refs/remotes/origin/HEAD (any agent in the repository can repoint
// it). Coverage uses only the push remote's own HEAD; the protected targets
// stay main/master plus that HEAD.
func TestPrePushGuardIgnoresLocalOriginHead(t *testing.T) {
	f := newPrePushFixture(t)
	noDefault := &prePushRig{name: "myrig"}
	witness := []string{"GT_ROLE", "myrig/witness"}

	// Agent repoints origin/HEAD at beads-sync and parks unlanded work there.
	runGitForMQSubmitTest(t, f.repo, "push", "-q", "origin", f.good+":refs/heads/beads-sync")
	runGitForMQSubmitTest(t, f.repo, "fetch", "-q", "origin")
	runGitForMQSubmitTest(t, f.repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/beads-sync")
	del := []prePushUpdate{prePushUpd("refs/heads/polecat/nux/gt-1", testZeroSHA, f.good)}
	for _, r := range []*prePushRig{noDefault, nil} {
		g := coordinatorGuard(f, r, mqWith(), witness...)
		if r == nil {
			g.isManaged = func() bool { return false }
		}
		if err := g.check(del); err == nil || !strings.Contains(err.Error(), "no MR recorded this tip") {
			t.Fatalf("rig %v: a tip only on the branch local origin/HEAD names must not count as covered: %v", r, err)
		}
	}

	// The remote's own HEAD is trusted: main there covers its tip.
	runGitForMQSubmitTest(t, f.remote, "symbolic-ref", "HEAD", "refs/heads/main")
	delMain := []prePushUpdate{prePushUpd("refs/heads/polecat/nux/gt-1", testZeroSHA, f.mainSHA)}
	if err := coordinatorGuard(f, noDefault, mqWith(), witness...).check(delMain); err != nil {
		t.Fatalf("tip on the remote HEAD branch: %v", err)
	}
	// The remote HEAD pointing at beads-sync is the server's decision, not the
	// local repository's: trusted.
	runGitForMQSubmitTest(t, f.remote, "symbolic-ref", "HEAD", "refs/heads/beads-sync")
	if err := coordinatorGuard(f, noDefault, mqWith(), witness...).check(del); err != nil {
		t.Fatalf("tip on the remote's HEAD branch: %v", err)
	}
}

// N1 (bash variant, Go side): origin/HEAD -> zzz does not take main out of
// the land check.
func TestPrePushGuardLandCheckWithRepointedOriginHead(t *testing.T) {
	f := newPrePushFixture(t)
	runGitForMQSubmitTest(t, f.repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/zzz")
	land := []prePushUpdate{prePushUpd("refs/heads/main", f.land, f.mainSHA)}
	for _, r := range []*prePushRig{{name: "myrig"}, nil} {
		g := newGuard(f, r, "GT_ROLE", "refinery")
		if r == nil {
			g.isManaged = func() bool { return false }
		}
		if err := g.check(land); err == nil || !strings.Contains(err.Error(), "lands integration branch") {
			t.Fatalf("rig %v: land to main with origin/HEAD -> zzz: %v", r, err)
		}
	}
}

// Round 3 M1: the remote HEAD lookup is lazy. A human's feature-branch push,
// a push to main/master/integration/* and any push with a configured default
// never read it; an agent's push of another branch reads it once (bounded).
func TestPrePushGuardRemoteHeadLookupIsLazy(t *testing.T) {
	f := newPrePushFixture(t)
	count := func(r *prePushRig, env []string, updates ...prePushUpdate) int {
		calls := 0
		g := newGuard(f, r, env...)
		g.isManaged = func() bool { return false }
		g.readRemoteHead = func(string) (string, error) { calls++; return "main", nil }
		_ = g.check(updates)
		return calls
	}
	feature := prePushUpd("refs/heads/feature/x", f.good, testZeroSHA)
	if n := count(nil, nil, feature); n != 0 {
		t.Fatalf("human feature push read the remote HEAD %d times", n)
	}
	if n := count(&prePushRig{name: "myrig", defaultBranch: "main"}, []string{"GT_ROLE", "mayor"}, feature); n != 0 {
		t.Fatalf("configured default: %d lookups", n)
	}
	if n := count(nil, nil, prePushUpd("refs/heads/integration/epic-z", f.good, testZeroSHA)); n != 0 {
		t.Fatalf("human integration push needs no default lookup: %d", n)
	}
	if n := count(nil, []string{"GT_ROLE", "mayor"}, feature, feature); n != 1 {
		t.Fatalf("agent push of a non-target branch: %d lookups, want 1 (cached)", n)
	}
}
