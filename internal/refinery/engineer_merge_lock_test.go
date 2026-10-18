package refinery

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gofrs/flock"
	"github.com/steveyegge/gastown/internal/rig"
)

// Review round 2.

func holdMergeLock(t *testing.T, rigPath string) {
	t.Helper()
	dir := filepath.Join(rigPath, ".runtime", "locks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	held := flock.New(filepath.Join(dir, mergeLockName))
	if ok, err := held.TryLock(); err != nil || !ok {
		t.Fatalf("test could not take the lock: %v", err)
	}
	t.Cleanup(func() { _ = held.Unlock() })
}

// 1: every engine path that drives the worktree takes the same lock.
func TestProcessBatch_RefusedWhileAnotherMergeHoldsTheLock(t *testing.T) {
	installFakeGT(t)
	workDir, g, _ := testGitRepo(t)
	createFeatureBranch(t, workDir, "polecat/a/1", "a1.txt", "1\n")
	createFeatureBranch(t, workDir, "polecat/b/2", "b2.txt", "2\n")
	e := newTestEngineer(t, workDir, g)
	e.rig = &rig.Rig{Name: "test-rig", Path: t.TempDir()}
	holdMergeLock(t, e.rig.Path)
	before := run(t, workDir, "git", "ls-remote", "origin", "refs/heads/main")
	for _, batch := range [][]*MRInfo{
		{makeMR("mr-1", "polecat/a/1", "main")},
		{makeMR("mr-1", "polecat/a/1", "main"), makeMR("mr-2", "polecat/b/2", "main")},
	} {
		res := e.ProcessBatch(context.Background(), batch, "main", DefaultBatchConfig())
		if res.Error == nil || !strings.Contains(res.Error.Error(), "another merge is running") || len(res.Merged) != 0 {
			t.Fatalf("ProcessBatch(%d MRs) = %+v, want refused (lock held)", len(batch), res)
		}
	}
	if got := run(t, workDir, "git", "ls-remote", "origin", "refs/heads/main"); got != before {
		t.Fatal("origin main moved while another merge held the lock")
	}
}

func TestMergeLockFileIsPrivate(t *testing.T) {
	f := newMergeOneFixture(t)
	_ = f.mergeOne(t)
	info, err := os.Stat(filepath.Join(f.rigPath, ".runtime", "locks", mergeLockName))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("lock file mode = %o, want 600", perm)
	}
}

// 3: only the rig's refinery identity and its MergeOne run claimants count
// as ours.
func TestOwnClaim(t *testing.T) {
	e := &Engineer{rig: &rig.Rig{Name: "testrig"}}
	for assignee, want := range map[string]bool{
		"":                             true,
		"testrig/refinery":             true,
		"testrig/refinery/merge-12-3":  true,
		"testrig/refinery/x":           false,
		"testrig/refinery/merge-a-b":   false,
		"testrig/refinery/merge-1-2/x": false,
		"testrig/refineryx":            false,
		"otherrig/refinery":            false,
		"testrig/crew/joe":             false,
	} {
		if got := e.ownClaim(assignee); got != want {
			t.Errorf("ownClaim(%q) = %v, want %v", assignee, got, want)
		}
	}
	if !e.ownClaim(e.newClaimant()) {
		t.Fatal("ownClaim refuses this engine's own run claimant")
	}
}

// 5: a long multi-byte stderr is cut on a rune boundary.
func TestRunGate_StderrCapIsUTF8Safe(t *testing.T) {
	e := NewEngineer(&rig.Rig{Name: "test-rig", Path: t.TempDir()})
	e.workDir = t.TempDir()
	res := e.runGate(context.Background(), "g", &GateConfig{Cmd: "i=0; while [ $i -lt 400 ]; do printf 'é' >&2; i=$((i+1)); done; printf 'x' >&2; exit 1"})
	if res.Success || !utf8.ValidString(res.Error) || !strings.HasSuffix(res.Error, "…") {
		t.Fatalf("gate error %q: want valid UTF-8 cut with an ellipsis", res.Error)
	}
}
