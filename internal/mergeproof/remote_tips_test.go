package mergeproof

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/git"
)

// remoteFixture is a rig repo (clone) with a bare origin: main, one
// integration branch, a polecat branch with unlanded work and an archive ref.
type remoteFixture struct {
	origin, repo                string
	main, epic, unlanded, other string
}

func newRemoteFixture(t *testing.T) remoteFixture {
	t.Helper()
	tmp := t.TempDir()
	f := remoteFixture{origin: filepath.Join(tmp, "origin.git"), repo: filepath.Join(tmp, "rig", "mayor", "rig")}
	run(t, tmp, "init", "-q", "--bare", "-b", "main", f.origin)
	if err := os.MkdirAll(filepath.Dir(f.repo), 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, tmp, "clone", "-q", f.origin, f.repo)
	writeFile(t, f.repo, "a.txt", "a\n")
	run(t, f.repo, "add", "-A")
	run(t, f.repo, "commit", "-q", "-m", "base")
	run(t, f.repo, "push", "-q", "origin", "HEAD:main")
	f.main = run(t, f.repo, "rev-parse", "HEAD")
	run(t, f.repo, "checkout", "-q", "-b", "integration/epic")
	writeFile(t, f.repo, "e.txt", "e\n")
	run(t, f.repo, "add", "-A")
	run(t, f.repo, "commit", "-q", "-m", "epic")
	run(t, f.repo, "push", "-q", "origin", "integration/epic")
	f.epic = run(t, f.repo, "rev-parse", "HEAD")
	run(t, f.repo, "checkout", "-q", "-b", "polecat/nux/gt-1", "main")
	writeFile(t, f.repo, "p.txt", "p\n")
	run(t, f.repo, "add", "-A")
	run(t, f.repo, "commit", "-q", "-m", "polecat work")
	run(t, f.repo, "push", "-q", "origin", "polecat/nux/gt-1")
	f.unlanded = run(t, f.repo, "rev-parse", "HEAD")
	run(t, f.repo, "checkout", "-q", "-b", "polecat/max/gt-2", "main")
	writeFile(t, f.repo, "q.txt", "q\n")
	run(t, f.repo, "add", "-A")
	run(t, f.repo, "commit", "-q", "-m", "other work")
	f.other = run(t, f.repo, "rev-parse", "HEAD")
	run(t, f.repo, "push", "-q", "origin", f.other+":refs/gt/archive/polecat/max/gt-2/1760000000-"+f.other[:12])
	run(t, f.repo, "checkout", "-q", "main")
	return f
}

func (f remoteFixture) tips() RemoteTips {
	g := git.NewGit(f.repo)
	return RemoteTips{
		Defaults: []string{"main"},
		List: func(prefix string) ([]git.RemoteRef, error) {
			return g.ListRemoteRefsWithHashes("origin", prefix)
		},
	}
}

func TestRemoteTipsCoveredAndArchived(t *testing.T) {
	f := newRemoteFixture(t)
	g := git.NewGit(f.repo)
	tips := f.tips()
	if tip, err := tips.BranchTip("polecat/nux/gt-1"); err != nil || tip != f.unlanded {
		t.Fatalf("BranchTip = %q, %v", tip, err)
	}
	if tip, err := tips.BranchTip("polecat/nux"); err != nil || tip != "" {
		t.Fatalf("BranchTip of a prefix must be exact: %q, %v", tip, err)
	}
	for name, c := range map[string]struct {
		commit  string
		covered bool
	}{
		"main tip":        {f.main, true},
		"integration tip": {f.epic, true},
		"unlanded":        {f.unlanded, false},
		"empty":           {"", false},
		"unknown object":  {strings.Repeat("1", 40), false},
	} {
		if got := CoveredByTargets(g, c.commit, tips); got != c.covered {
			t.Errorf("%s: CoveredByTargets = %v, want %v", name, got, c.covered)
		}
	}
	if CoveredByTargets(nil, f.main, tips) || CoveredByTargets(g, f.main, RemoteTips{}) {
		t.Error("no git or no lister proves nothing")
	}

	if ok, err := tips.Archived(f.other); err != nil || !ok {
		t.Fatalf("archived tip: %v %v", ok, err)
	}
	if ok, err := tips.Archived(f.unlanded); err != nil || ok {
		t.Fatalf("unarchived tip: %v %v", ok, err)
	}
	if ok, _ := tips.Archived(""); ok {
		t.Fatal("an empty commit is never archived")
	}
	broken := RemoteTips{List: func(string) ([]git.RemoteRef, error) { return nil, errors.New("offline") }}
	if ok, err := broken.Archived(f.other); ok || err == nil {
		t.Fatal("an unreadable remote is an error, never archived")
	}
	if _, err := broken.TargetTips(); err == nil {
		t.Fatal("an unreadable remote must fail TargetTips")
	}
}

func TestArchiveRef(t *testing.T) {
	tip := "abcdef0123456789abcdef0123456789abcdef01"
	at := time.Unix(1760000000, 0)
	ref, err := ArchiveRef("polecat/nux/gt-1", at, tip)
	if err != nil || ref != "refs/gt/archive/polecat/nux/gt-1/1760000000-abcdef012345" {
		t.Fatalf("ArchiveRef = %q, %v", ref, err)
	}
	if b, short, ok := ParseArchiveRef(ref); !ok || b != "polecat/nux/gt-1" || short != "abcdef012345" {
		t.Fatalf("ParseArchiveRef(%q) = %q, %q, %v", ref, b, short, ok)
	}
	// Real generated polecat branches (polecat/<name>/<issue>+<suffix>, the
	// legacy '@' form, polecat/<name>-<suffix>) round-trip.
	for _, real := range []string{"polecat/mirelurk/kp-znd+mvcf8o91", "polecat/s2test/kp-s2b+t1", "polecat/nux/gt-1@mvcf8o91", "polecat/nux-mvcf8o91"} {
		ref, err := ArchiveRef(real, at, tip)
		if err != nil || ref != "refs/gt/archive/"+real+"/1760000000-abcdef012345" {
			t.Fatalf("ArchiveRef(%q) = %q, %v", real, ref, err)
		}
		if b, short, ok := ParseArchiveRef(ref); !ok || b != real || short != "abcdef012345" {
			t.Fatalf("ParseArchiveRef(%q) = %q, %q, %v", ref, b, short, ok)
		}
	}
	for _, bad := range []struct{ branch, tip string }{
		{"main", tip},
		{"polecat/nux/+x", tip},
		{"polecat/nux/x.lock", tip},
		{"polecat/../x", tip},
		{"polecat/a b", tip},
		{"polecat/-x", tip},
		{"polecat/nux", "abc"},
		{"polecat/nux", "zz" + tip[2:]},
		{"", tip},
	} {
		if ref, err := ArchiveRef(bad.branch, at, bad.tip); err == nil {
			t.Errorf("ArchiveRef(%q, %q) = %q, want error", bad.branch, bad.tip, ref)
		}
	}
	for _, bad := range []string{
		"refs/gt/archive/polecat/nux",              // no stamp
		"refs/gt/archive/1760000000-abcdef0",       // no branch
		"refs/gt/archive/main/1760000000-abcd1234", // not a polecat branch
		"refs/gt/archive/polecat/nux/latest",       // not a stamp
		"refs/heads/polecat/nux/1760000000-abcdef0",
		"refs/gt/archive/polecat/../1760000000-abcdef0",
		"refs/gt/archive/polecat/nux/+x/1760000000-abcdef0",
	} {
		if b, _, ok := ParseArchiveRef(bad); ok {
			t.Errorf("ParseArchiveRef(%q) = %q, want not an archive ref", bad, b)
		}
	}
}

// gt-pd1c.78 tahap 2 (b): an MR's target is trusted as landing proof only when
// it is the rig's default branch or integration/*. An agent that sets the
// target to its own polecat branch gets an undecidable answer, not "landed".
func TestProverTrustsOnlyMergeTargets(t *testing.T) {
	f := newRemoteFixture(t)
	rigPath := filepath.Dir(filepath.Dir(f.repo))
	p := ForRigPath(rigPath)
	if p == nil {
		t.Fatal("no prover")
	}
	err := p.VerifyPushedCommitReachableFromPushTarget("origin", "polecat/nux/gt-1", f.unlanded)
	if err == nil || beads.IsCommitNotOnTarget(err) || !strings.Contains(err.Error(), "not a merge target") {
		t.Fatalf("polecat branch as target: %v, want an uncertain refusal", err)
	}
	pp := p.(beads.PatchProver)
	if ok, err := pp.PatchEquivalentOnPushTarget("origin", "polecat/nux/gt-1", f.unlanded, ""); ok || err == nil {
		t.Fatalf("patch proof against an untrusted target: %v %v", ok, err)
	}
	mr := closedMR("polecat/nux/gt-1", f.unlanded)
	if reason := beads.UnverifiedReason(mr, p); !strings.Contains(reason, "not a merge target") {
		t.Fatalf("landing via an untrusted target must be unverified, reason %q", reason)
	}
	if err := p.VerifyPushedCommitReachableFromPushTarget("origin", "integration/epic", f.epic); err != nil {
		t.Fatalf("integration/* is a merge target: %v", err)
	}
	if err := p.VerifyPushedCommitReachableFromPushTarget("origin", "main", f.main); err != nil {
		t.Fatalf("the default branch is a merge target: %v", err)
	}

	// The rig's configured default_branch is a merge target too.
	run(t, f.repo, "push", "-q", "origin", f.unlanded+":refs/heads/develop")
	writeFile(t, rigPath, "config.json", `{"type":"rig","default_branch":"develop"}`)
	if err := ForRigPath(rigPath).VerifyPushedCommitReachableFromPushTarget("origin", "develop", f.unlanded); err != nil {
		t.Fatalf("configured default branch: %v", err)
	}
}

func TestCachedLister(t *testing.T) {
	calls := 0
	fail := true
	list := CachedLister(func(prefix string) ([]git.RemoteRef, error) {
		calls++
		if fail {
			return nil, errors.New("offline")
		}
		return []git.RemoteRef{{Name: prefix + "x", Hash: "h"}}, nil
	})
	if _, err := list("refs/heads/"); err == nil {
		t.Fatal("first read fails")
	}
	fail = false
	for i := 0; i < 3; i++ {
		if refs, err := list("refs/heads/"); err != nil || len(refs) != 1 {
			t.Fatalf("read %d: %v %v", i, refs, err)
		}
	}
	if _, err := list("refs/gt/archive/"); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3 (errors are not cached, each prefix read once)", calls)
	}
}

// gt-pd1c.78 S-M1: the local refs/remotes/origin/HEAD symref is not trusted
// (an agent in the shared repository can repoint it); the remote's own HEAD
// is. A configured default_branch is the only trusted default.
func TestProverDefaultBranchNotFromLocalOriginHead(t *testing.T) {
	f := newRemoteFixture(t)
	rigPath := filepath.Dir(filepath.Dir(f.repo))
	run(t, f.repo, "push", "-q", "origin", f.unlanded+":refs/heads/evil")
	run(t, f.repo, "fetch", "-q", "origin")
	run(t, f.repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/evil")
	p := ForRigPath(rigPath)
	if err := p.VerifyPushedCommitReachableFromPushTarget("origin", "evil", f.unlanded); err == nil ||
		!strings.Contains(err.Error(), "not a merge target") {
		t.Fatalf("a repointed local origin/HEAD must not make the polecat branch a target: %v", err)
	}
	if err := p.VerifyPushedCommitReachableFromPushTarget("origin", "main", f.main); err != nil {
		t.Fatalf("the remote's HEAD (main) is the default branch: %v", err)
	}

	writeFile(t, rigPath, "config.json", `{"type":"rig","default_branch":"integration-only"}`)
	cfg := ForRigPath(rigPath)
	if err := cfg.VerifyPushedCommitReachableFromPushTarget("origin", "main", f.main); err == nil ||
		!strings.Contains(err.Error(), "not a merge target") {
		t.Fatalf("with a configured default branch, main is not trusted: %v", err)
	}
	if UntrustedMergeTarget("integration/", "main") == nil || UntrustedMergeTarget("main", "") == nil {
		t.Fatal("an empty integration name or an unknown default branch is not a target")
	}
}

// F7 round 1 against real git: the refinery merges the polecat branch with
// --no-ff and resolves a conflict in the merge commit. The recorded merge is
// confirmed by git (the submitted commit is reachable), not held as a
// contradiction.
func TestRecordedMergeWithConflictResolutionIsLanded(t *testing.T) {
	f := newRemoteFixture(t)
	rigPath := filepath.Dir(filepath.Dir(f.repo))
	run(t, f.repo, "checkout", "-q", "-b", "polecat/c/gt-7", f.main)
	writeFile(t, f.repo, "a.txt", "polecat change\n")
	run(t, f.repo, "commit", "-q", "-am", "polecat edits a")
	submitted := run(t, f.repo, "rev-parse", "HEAD")
	run(t, f.repo, "push", "-q", "origin", "polecat/c/gt-7")
	run(t, f.repo, "checkout", "-q", "main")
	run(t, f.repo, "reset", "-q", "--hard", f.main)
	writeFile(t, f.repo, "a.txt", "main change\n")
	run(t, f.repo, "commit", "-q", "-am", "main edits a")
	cmd := exec.Command("git", "merge", "--no-ff", "-m", "merge polecat", "polecat/c/gt-7")
	cmd.Dir = f.repo
	_ = cmd.Run() // conflicts
	writeFile(t, f.repo, "a.txt", "resolved\n")
	run(t, f.repo, "add", "a.txt")
	run(t, f.repo, "commit", "-q", "--no-edit")
	merge := run(t, f.repo, "rev-parse", "HEAD")
	run(t, f.repo, "push", "-q", "origin", "HEAD:main")

	mr := &beads.Issue{ID: "gt-wisp-c", Status: "closed", Description: "branch: polecat/c/gt-7\ntarget: main\nsource_issue: gt-c\nrig: gastown\ncommit_sha: " +
		submitted + "\nbase_sha: " + f.main + "\nclose_reason: merged\nmerge_commit: " + merge + "\n"}
	q := beads.NewMergeQueue(beads.MergeQueueIndex{"gt-c": {mr}}, nil, func(string) beads.MergeProver { return ForRigPath(rigPath) })
	d, err := q.Detail("gt-c")
	if err != nil || d.Hold != beads.HoldLanded || d.Evidence != beads.EvidenceGit {
		t.Fatalf("conflict-resolved --no-ff merge: %+v %v, want landed by git", d, err)
	}
}

func TestRigDefaultBranch(t *testing.T) {
	f := newRemoteFixture(t)
	rigPath := filepath.Dir(filepath.Dir(f.repo))
	g := git.NewGit(f.repo)
	if got := RigDefaultBranch(rigPath, g); got != "main" {
		t.Fatalf("remote HEAD: %q", got)
	}
	writeFile(t, rigPath, "config.json", `{"default_branch":"develop"}`)
	if got := RigDefaultBranch(rigPath, g); got != "develop" {
		t.Fatalf("configured: %q", got)
	}
	if got := RigDefaultBranch(t.TempDir(), nil); got != "" {
		t.Fatalf("nothing known: %q", got)
	}
}
