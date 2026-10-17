package mergeproof

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
)

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// Pilot shape: the refinery pushed a merge of the polecat branch to the
// integration branch; the MR's recorded commit_sha must be provable from git.
func TestResolverProvesLandedCommitFromGit(t *testing.T) {
	tmp := t.TempDir()
	origin := filepath.Join(tmp, "origin.git")
	run(t, tmp, "init", "-q", "--bare", "-b", "main", origin)
	rigRepo := filepath.Join(tmp, "town", "gastown", "mayor", "rig")
	if err := os.MkdirAll(filepath.Dir(rigRepo), 0755); err != nil {
		t.Fatal(err)
	}
	run(t, tmp, "clone", "-q", origin, rigRepo)
	run(t, rigRepo, "commit", "-q", "--allow-empty", "-m", "base")
	run(t, rigRepo, "push", "-q", "origin", "HEAD:main")
	run(t, rigRepo, "checkout", "-q", "-b", "integration/gt-epic")
	run(t, rigRepo, "push", "-q", "origin", "integration/gt-epic")

	run(t, rigRepo, "checkout", "-q", "-b", "polecat/rust/gt-work1")
	run(t, rigRepo, "commit", "-q", "--allow-empty", "-m", "work")
	landed := run(t, rigRepo, "rev-parse", "HEAD")
	run(t, rigRepo, "checkout", "-q", "integration/gt-epic")
	run(t, rigRepo, "merge", "-q", "--no-ff", "-m", "merge", "polecat/rust/gt-work1")
	run(t, rigRepo, "push", "-q", "origin", "integration/gt-epic")

	run(t, rigRepo, "checkout", "-q", "-b", "polecat/nux/gt-work2", "main")
	run(t, rigRepo, "commit", "-q", "--allow-empty", "-m", "unmerged work")
	unmerged := run(t, rigRepo, "rev-parse", "HEAD")

	p := Resolver(filepath.Join(tmp, "town"))("gastown")
	if p == nil {
		t.Fatal("no prover for rig")
	}
	if err := p.VerifyPushedCommitReachableFromPushTarget("origin", "integration/gt-epic", landed); err != nil {
		t.Errorf("landed commit not proven: %v", err)
	}
	if err := p.VerifyPushedCommitReachableFromPushTarget("origin", "integration/gt-epic", unmerged); err == nil {
		t.Error("unmerged commit wrongly proven landed")
	}

	// Integration branch landed on main and was deleted: fall back to default.
	run(t, rigRepo, "checkout", "-q", "main")
	run(t, rigRepo, "merge", "-q", "--ff-only", "integration/gt-epic")
	run(t, rigRepo, "push", "-q", "origin", "main")
	run(t, rigRepo, "push", "-q", "origin", "--delete", "integration/gt-epic")
	p2 := Resolver(filepath.Join(tmp, "town"))("gastown")
	if err := p2.VerifyPushedCommitReachableFromPushTarget("origin", "integration/gt-epic", landed); err != nil {
		t.Errorf("deleted target should fall back to default branch: %v", err)
	}

	if Resolver(tmp)("") != nil || Resolver(tmp)("../x") != nil {
		t.Error("empty or path-traversing rig names must not resolve")
	}
}

type notOnErr struct{}

func (notOnErr) Error() string           { return "commit not on branch" }
func (notOnErr) CommitNotOnTarget() bool { return true }

// gt-pd1c.59 round 3: "not landed" is definite only when both branches answer
// it definitely; any failure to find out stays uncertain.
func TestCombineFallback(t *testing.T) {
	uncertain := errors.New("unable to fetch")
	cases := []struct {
		target, def  error
		wantNil      bool
		wantNotOnTgt bool
	}{
		{notOnErr{}, nil, true, false},
		{notOnErr{}, notOnErr{}, false, true},
		{notOnErr{}, uncertain, false, false},
		{uncertain, notOnErr{}, false, false},
	}
	for i, c := range cases {
		got := combineFallback(c.target, c.def)
		if (got == nil) != c.wantNil || beads.IsCommitNotOnTarget(got) != c.wantNotOnTgt {
			t.Errorf("case %d: combineFallback = %v, want nil=%v notOn=%v", i, got, c.wantNil, c.wantNotOnTgt)
		}
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func closedMR(target, commit string) *beads.Issue {
	return &beads.Issue{ID: "gt-wisp-x", Status: "closed",
		Description: "branch: polecat/a/gt-w\ntarget: " + target + "\nsource_issue: gt-w\nrig: gastown\ncommit_sha: " + commit + "\n"}
}

// gt-pd1c.81: an MR squash-merged (the recorded commit rewritten) and closed by
// a bare `bd close` landed; an unmerged one did not.
func TestResolverProvesSquashMergedWork(t *testing.T) {
	tmp := t.TempDir()
	origin := filepath.Join(tmp, "origin.git")
	run(t, tmp, "init", "-q", "--bare", "-b", "main", origin)
	rigRepo := filepath.Join(tmp, "town", "gastown", "mayor", "rig")
	if err := os.MkdirAll(filepath.Dir(rigRepo), 0755); err != nil {
		t.Fatal(err)
	}
	run(t, tmp, "clone", "-q", origin, rigRepo)
	run(t, rigRepo, "checkout", "-q", "-b", "main")
	writeFile(t, rigRepo, "base.txt", "base\n")
	run(t, rigRepo, "add", ".")
	run(t, rigRepo, "commit", "-q", "-m", "base")
	run(t, rigRepo, "push", "-q", "origin", "main")

	run(t, rigRepo, "checkout", "-q", "-b", "polecat/a/gt-w")
	writeFile(t, rigRepo, "w1.txt", "one\n")
	run(t, rigRepo, "add", ".")
	run(t, rigRepo, "commit", "-q", "-m", "w1")
	writeFile(t, rigRepo, "w2.txt", "two\n")
	run(t, rigRepo, "add", ".")
	run(t, rigRepo, "commit", "-q", "-m", "w2")
	squashed := run(t, rigRepo, "rev-parse", "HEAD")
	run(t, rigRepo, "checkout", "-q", "-b", "polecat/b/gt-v", "main")
	writeFile(t, rigRepo, "v.txt", "never merged\n")
	run(t, rigRepo, "add", ".")
	run(t, rigRepo, "commit", "-q", "-m", "v")
	unmerged := run(t, rigRepo, "rev-parse", "HEAD")
	run(t, rigRepo, "checkout", "-q", "main")
	run(t, rigRepo, "merge", "-q", "--squash", "polecat/a/gt-w")
	run(t, rigRepo, "commit", "-q", "-m", "squash gt-w")
	run(t, rigRepo, "push", "-q", "origin", "main")

	p := Resolver(filepath.Join(tmp, "town"))("gastown")
	if !beads.MRWorkLanded(closedMR("main", squashed), p) {
		t.Errorf("squash-merged work not proven landed: %s", beads.UnverifiedReason(closedMR("main", squashed), p))
	}
	// Deleted target: the squash is on the default branch only. Whatever the
	// answer, it must never be a definite "not landed".
	if gone := closedMR("integration/gone", squashed); !beads.MRWorkLanded(gone, p) && beads.UnverifiedReason(gone, p) == "" {
		t.Error("squash on the default branch after target deletion read as definitely not landed")
	}
	if beads.MRWorkLanded(closedMR("main", unmerged), p) || beads.UnverifiedReason(closedMR("main", unmerged), p) != "" {
		t.Errorf("unmerged work must be a definite not landed, reason %q", beads.UnverifiedReason(closedMR("main", unmerged), p))
	}
}

// gt-pd1c.81: the rig's .repo.git is a --depth 1 clone. A commit that landed
// below the shallow boundary stays unverified instead of reading not landed.
func TestResolverShallowRepoLeavesOldLandingUnverified(t *testing.T) {
	tmp := t.TempDir()
	origin := filepath.Join(tmp, "origin.git")
	run(t, tmp, "init", "-q", "--bare", "-b", "main", origin)
	work := filepath.Join(tmp, "work")
	run(t, tmp, "clone", "-q", origin, work)
	run(t, work, "checkout", "-q", "-b", "main")
	writeFile(t, work, "a.txt", "a\n")
	run(t, work, "add", ".")
	run(t, work, "commit", "-q", "-m", "a")
	landed := run(t, work, "rev-parse", "HEAD")
	run(t, work, "push", "-q", "origin", "main", "main:polecat/old")
	writeFile(t, work, "b.txt", "b\n")
	run(t, work, "add", ".")
	run(t, work, "commit", "-q", "-m", "b")
	run(t, work, "push", "-q", "origin", "main")

	repoGit := filepath.Join(tmp, "town", "gastown", ".repo.git")
	run(t, tmp, "clone", "-q", "--bare", "--single-branch", "--depth", "1", "file://"+origin, repoGit)
	run(t, repoGit, "fetch", "-q", "--depth", "1", "origin", "polecat/old:refs/heads/polecat/old")

	p := Resolver(filepath.Join(tmp, "town"))("gastown")
	mr := closedMR("main", landed)
	if reason := beads.UnverifiedReason(mr, p); reason == "" || !strings.Contains(reason, "shallow") {
		t.Fatalf("landed commit below the shallow boundary must stay unverified (shallow), got reason %q landed=%v", reason, beads.MRWorkLanded(mr, p))
	}
}

func TestCombinePatchFallback(t *testing.T) {
	uncertain := errors.New("unable to fetch")
	cases := []struct {
		tOK     bool
		tErr    error
		dOK     bool
		dErr    error
		wantOK  bool
		wantErr bool
	}{
		{false, nil, true, nil, true, false},
		{false, nil, false, nil, false, false},
		{false, nil, false, uncertain, false, true},
		{false, uncertain, false, nil, false, true},
		{false, uncertain, true, nil, true, false},
	}
	for i, c := range cases {
		ok, err := combinePatchFallback(c.tOK, c.tErr, c.dOK, c.dErr)
		if ok != c.wantOK || (err != nil) != c.wantErr {
			t.Errorf("case %d: combinePatchFallback = (%v, %v), want (%v, err=%v)", i, ok, err, c.wantOK, c.wantErr)
		}
	}
}
