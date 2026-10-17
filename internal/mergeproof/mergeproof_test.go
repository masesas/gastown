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
