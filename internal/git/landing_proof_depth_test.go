package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gt-pd1c.81: landing proof against real repositories (shallow clones,
// rebase/squash merges, unreachable remotes).

func lpGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// lpCommit writes file=content in dir and commits it, returning the SHA.
func lpCommit(t *testing.T, dir, file, content string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	lpGit(t, dir, "add", file)
	lpGit(t, dir, "commit", "-q", "-m", "change "+file)
	return lpGit(t, dir, "rev-parse", "HEAD")
}

// lpOrigin creates a bare origin with main and a full working clone of it.
func lpOrigin(t *testing.T) (origin, work string) {
	t.Helper()
	tmp := t.TempDir()
	origin = filepath.Join(tmp, "origin.git")
	lpGit(t, tmp, "init", "-q", "--bare", "-b", "main", origin)
	work = filepath.Join(tmp, "work")
	lpGit(t, tmp, "clone", "-q", origin, work)
	lpGit(t, work, "checkout", "-q", "-b", "main")
	lpCommit(t, work, "base.txt", "base\n")
	lpGit(t, work, "push", "-q", "origin", "main")
	return origin, work
}

// A commit that landed below the shallow boundary is present in the clone
// (fetched with an old branch) but not reachable inside the truncated history:
// `merge-base --is-ancestor` exits 1. That must stay uncertain, never a
// definite "not on the branch" (it would reopen landed work).
func TestLandingProofShallowCommitBelowBoundaryIsUncertain(t *testing.T) {
	origin, work := lpOrigin(t)
	landed := lpCommit(t, work, "a.txt", "a\n")
	lpGit(t, work, "push", "-q", "origin", "main", "main:polecat/old")
	lpCommit(t, work, "x.txt", "x\n")
	lpCommit(t, work, "y.txt", "y\n")
	lpGit(t, work, "push", "-q", "origin", "main")

	shallow := filepath.Join(t.TempDir(), "repo.git")
	lpGit(t, work, "clone", "-q", "--bare", "--single-branch", "--depth", "1", "file://"+origin, shallow)
	lpGit(t, shallow, "fetch", "-q", "--depth", "1", "origin", "polecat/old:refs/heads/polecat/old")
	g := NewGitWithDir(shallow, "")
	if ok, err := g.IsAncestor(landed, "main"); err != nil || ok {
		t.Fatalf("precondition: shallow is-ancestor should answer false, got %v %v", ok, err)
	}

	err := g.VerifyPushedCommitReachableFromPushTarget("origin", "main", landed)
	if err == nil {
		t.Fatal("shallow clone cannot see the path, must not report reachable")
	}
	if isNotOnBranch(err) {
		t.Fatalf("landed commit below the shallow boundary read as definitely not on main: %v", err)
	}
	if !strings.Contains(err.Error(), "shallow") {
		t.Errorf("uncertainty should name the shallow clone: %v", err)
	}
	if _, perr := g.PatchEquivalentOnPushTarget("origin", "main", landed, ""); perr == nil || !strings.Contains(perr.Error(), "shallow") {
		t.Fatalf("patch check in a shallow clone that cannot decide must be uncertain, got %v", perr)
	}
}

// A commit made on top of the shallow boundary (polecat work after the rig was
// cloned) can be decided in the shallow clone: the boundary is its ancestor,
// so nothing below the boundary can be one of its descendants.
func TestLandingProofShallowCommitAboveBoundaryIsDefinite(t *testing.T) {
	origin, work := lpOrigin(t)
	lpCommit(t, work, "x.txt", "x\n")
	lpGit(t, work, "push", "-q", "origin", "main")

	shallow := filepath.Join(t.TempDir(), "sh")
	lpGit(t, work, "clone", "-q", "--single-branch", "--depth", "1", "file://"+origin, shallow)
	local := lpCommit(t, shallow, "c.txt", "polecat work\n")
	g := NewGit(shallow)

	if err := g.VerifyPushedCommitReachableFromPushTarget("origin", "main", local); !isNotOnBranch(err) {
		t.Fatalf("commit above the shallow boundary should be definitely not on main, got %v", err)
	}
	onTarget, err := g.PatchEquivalentOnPushTarget("origin", "main", local, "")
	if err != nil || onTarget {
		t.Fatalf("no equivalent patch on main: want (false, nil), got (%v, %v)", onTarget, err)
	}
}

func TestLandingProofSquashMergeIsPatchEquivalent(t *testing.T) {
	_, work := lpOrigin(t)
	lpGit(t, work, "checkout", "-q", "-b", "polecat/feat")
	lpCommit(t, work, "f.txt", "one\n")
	head := lpCommit(t, work, "g.txt", "two\n")
	lpGit(t, work, "checkout", "-q", "main")
	lpCommit(t, work, "m.txt", "unrelated\n")
	lpGit(t, work, "merge", "-q", "--squash", "polecat/feat")
	lpGit(t, work, "commit", "-q", "-m", "squash feat")
	lpGit(t, work, "push", "-q", "origin", "main")
	g := NewGit(work)

	if err := g.VerifyPushedCommitReachableFromPushTarget("origin", "main", head); !isNotOnBranch(err) {
		t.Fatalf("squashed head is not an ancestor of main, want definite not-on-branch, got %v", err)
	}
	onTarget, err := g.PatchEquivalentOnPushTarget("origin", "main", head, "")
	if err != nil || !onTarget {
		t.Fatalf("squash merge: want (true, nil), got (%v, %v)", onTarget, err)
	}
}

func TestLandingProofRebaseIsPatchEquivalent(t *testing.T) {
	_, work := lpOrigin(t)
	lpGit(t, work, "checkout", "-q", "-b", "polecat/feat")
	lpCommit(t, work, "f.txt", "one\n")
	head := lpCommit(t, work, "g.txt", "two\n")
	lpGit(t, work, "checkout", "-q", "main")
	lpCommit(t, work, "m.txt", "unrelated\n")
	lpGit(t, work, "checkout", "-q", "-b", "rebased", "polecat/feat")
	lpGit(t, work, "rebase", "-q", "main")
	lpGit(t, work, "push", "-q", "origin", "rebased:main")
	g := NewGit(work)

	onTarget, err := g.PatchEquivalentOnPushTarget("origin", "main", head, "")
	if err != nil || !onTarget {
		t.Fatalf("rebase merge: want (true, nil), got (%v, %v)", onTarget, err)
	}
}

// Only part of the work is on the target: neither landed nor provably not.
func TestLandingProofPartialPatchIsUncertain(t *testing.T) {
	_, work := lpOrigin(t)
	lpGit(t, work, "checkout", "-q", "-b", "polecat/feat")
	first := lpCommit(t, work, "f.txt", "one\n")
	head := lpCommit(t, work, "g.txt", "two\n")
	lpGit(t, work, "checkout", "-q", "main")
	lpGit(t, work, "cherry-pick", "-x", first) // -x: a distinct commit, same patch
	lpGit(t, work, "push", "-q", "origin", "main")
	g := NewGit(work)

	if onTarget, err := g.PatchEquivalentOnPushTarget("origin", "main", head, ""); err == nil || !strings.Contains(err.Error(), "only 1 of 2") {
		t.Fatalf("partially applied work must be uncertain, got (%v, %v)", onTarget, err)
	}
}

// Complete clone, the commit genuinely never reached the target.
func TestLandingProofCompleteCloneNotLanded(t *testing.T) {
	_, work := lpOrigin(t)
	lpGit(t, work, "checkout", "-q", "-b", "polecat/feat")
	head := lpCommit(t, work, "f.txt", "never merged\n")
	lpGit(t, work, "checkout", "-q", "main")
	lpCommit(t, work, "m.txt", "unrelated\n")
	lpGit(t, work, "push", "-q", "origin", "main")
	g := NewGit(work)

	if err := g.VerifyPushedCommitReachableFromPushTarget("origin", "main", head); !isNotOnBranch(err) {
		t.Fatalf("want definite not-on-branch, got %v", err)
	}
	onTarget, err := g.PatchEquivalentOnPushTarget("origin", "main", head, "")
	if err != nil || onTarget {
		t.Fatalf("want (false, nil), got (%v, %v)", onTarget, err)
	}
}

func TestLandingProofUnreachableRemoteIsUncertain(t *testing.T) {
	_, work := lpOrigin(t)
	head := lpGit(t, work, "rev-parse", "HEAD")
	lpGit(t, work, "remote", "add", "gone", filepath.Join(t.TempDir(), "missing.git"))
	g := NewGit(work)

	err := g.VerifyPushedCommitReachableFromPushTarget("gone", "main", head)
	if err == nil || isNotOnBranch(err) {
		t.Fatalf("unreachable remote must be uncertain, got %v", err)
	}
	if onTarget, perr := g.PatchEquivalentOnPushTarget("gone", "main", head, ""); perr == nil {
		t.Fatalf("unreachable remote must be uncertain, got (%v, nil)", onTarget)
	}
}

// A boundary of another branch (not reachable from the target) cannot hide a
// path to the commit and does not make the answer uncertain.
func TestLandingProofShallowUnrelatedBoundaryIsIgnored(t *testing.T) {
	origin, work := lpOrigin(t)
	lpGit(t, work, "checkout", "-q", "-b", "side")
	lpCommit(t, work, "s1.txt", "s1\n")
	lpCommit(t, work, "s2.txt", "s2\n")
	lpGit(t, work, "push", "-q", "origin", "side")
	lpGit(t, work, "checkout", "-q", "main")
	lpCommit(t, work, "x.txt", "x\n")
	lpGit(t, work, "push", "-q", "origin", "main")

	shallow := filepath.Join(t.TempDir(), "sh")
	lpGit(t, work, "clone", "-q", "--single-branch", "--branch", "main", "--depth", "1", "file://"+origin, shallow)
	lpGit(t, shallow, "fetch", "-q", "--depth", "1", "origin", "side:refs/remotes/origin/side")
	local := lpCommit(t, shallow, "c.txt", "polecat work\n")
	g := NewGit(shallow)

	if err := g.VerifyPushedCommitReachableFromPushTarget("origin", "main", local); !isNotOnBranch(err) {
		t.Fatalf("want definite not-on-branch despite the side boundary, got %v", err)
	}
	if ok, err := g.PatchEquivalentOnPushTarget("origin", "main", local, ""); err != nil || ok {
		t.Fatalf("want (false, nil), got (%v, %v)", ok, err)
	}
}

// Inputs git cannot compare are uncertain, never "no equivalent patch".
func TestLandingProofPatchCheckUncertainInputs(t *testing.T) {
	_, work := lpOrigin(t)
	lpGit(t, work, "checkout", "-q", "-b", "polecat/empty")
	lpGit(t, work, "commit", "-q", "--allow-empty", "-m", "no diff")
	empty := lpGit(t, work, "rev-parse", "HEAD")
	lpGit(t, work, "checkout", "-q", "main")
	g := NewGit(work)

	cases := map[string][2]string{
		"empty commit sha":    {"main", " "},
		"missing branch":      {"no-such-branch", empty},
		"absent commit":       {"main", "0123456789abcdef0123456789abcdef01234567"},
		"commit with no diff": {"main", empty},
	}
	for name, c := range cases {
		if ok, err := g.PatchEquivalentOnPushTarget("origin", c[0], c[1], ""); err == nil {
			t.Errorf("%s: want an uncertain error, got (%v, nil)", name, ok)
		}
	}
}
