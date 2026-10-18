package polecat

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/testutil"
)

// advanceRemoteMain pushes a commit to main on remote from another clone, so
// the remote's main tip is not in clonePath's object store until it fetches.
func advanceRemoteMain(t *testing.T, remote string) {
	t.Helper()
	other := filepath.Join(t.TempDir(), "other")
	for _, args := range [][]string{
		{"clone", remote, other},
		{"-C", other, "config", "user.email", "o@example.com"},
		{"-C", other, "config", "user.name", "Other"},
		{"-C", other, "checkout", "main"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(other, "later.txt"), []byte("later\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"-C", other, "add", "later.txt"},
		{"-C", other, "commit", "-m", "later"},
		{"-C", other, "push", "origin", "main"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// gt-pd1c.86: when the remote cannot prove the branch preserved (here: its
// main moved on and was not fetched), removal must still push ordinary work
// instead of reading "cannot tell" as "nothing to push".
func TestPushBranchBeforeRemovalPushesWhenPreservationUnproven(t *testing.T) {
	const branch = "polecat/pipboy/gt-code+abc"
	m := newReviewOnlyPushManager(t, map[string]string{"gt-code": "Implement the thing."}, false)
	clone, remote := testutil.NewUnpushedPolecatBranch(t, branch)
	advanceRemoteMain(t, remote)

	m.pushBranchBeforeRemoval(clone, m.reviewOnlyDecisionForClone("pipboy", clone))

	if !testutil.RemoteHasBranch(t, remote, branch) {
		t.Fatal("removal skipped the push although the branch was not proven preserved")
	}
}

func TestPushBranchBeforeRemovalUnprovenStillSkipsReviewOnlyLeg(t *testing.T) {
	m := newReviewOnlyPushManager(t, map[string]string{"gt-review": "review_only: true\n\nReview leg."}, false)
	clone, remote := testutil.NewUnpushedPolecatBranch(t, reviewLegBranch)
	advanceRemoteMain(t, remote)

	m.pushBranchBeforeRemoval(clone, m.reviewOnlyDecisionForClone("pipboy", clone))

	if testutil.RemoteHasBranch(t, remote, reviewLegBranch) {
		t.Fatal("removal pushed the branch of a review-only leg to origin")
	}
}

// gt-pd1c.86 H2a: removing origin (shared repository config) must not turn
// unpushed commits into "clean": nuke without --force is refused.
func TestRemoveRefusedWhenOriginRemovedWithUnpushedCommit(t *testing.T) {
	mgr, mayorRig := setupCanonicalBranchManagerTest(t)
	p, err := mgr.AddWithOptions("toast", AddOptions{})
	if err != nil {
		t.Fatalf("AddWithOptions: %v", err)
	}
	if err := os.WriteFile(filepath.Join(p.ClonePath, "work.go"), []byte("package work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"-C", p.ClonePath, "add", "work.go"},
		{"-C", p.ClonePath, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "unpushed work"},
		{"-C", mayorRig, "remote", "remove", "origin"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// No self-reported cleanup_status (the agent bead cannot be read): the
	// removal falls back to git (a trusted bead status is H3, out of scope).
	testutil.InstallBDShowStubWithHooked(t, nil, nil, true)
	beads.ResetBdAllowStaleCacheForTest()
	t.Cleanup(beads.ResetBdAllowStaleCacheForTest)
	if got := mgr.getCleanupStatusFromBead("toast"); got != CleanupUnknown {
		t.Fatalf("cleanup status = %q, want unknown", got)
	}
	if ws, err := git.NewGit(p.ClonePath).CheckUncommittedWork(); err != nil || !ws.UnpushedUnverified {
		t.Fatalf("CheckUncommittedWork = %+v, %v; want unverified", ws, err)
	}
	err = mgr.RemoveWithOptions("toast", false, false, false)
	var uwe *UncommittedWorkError
	if !errors.As(err, &uwe) {
		t.Fatalf("RemoveWithOptions err = %v, want UncommittedWorkError (unpushed commits unverified)", err)
	}
	if !uwe.Status.UnpushedUnverified {
		t.Fatalf("status = %+v, want UnpushedUnverified", uwe.Status)
	}
	if _, statErr := os.Stat(p.ClonePath); statErr != nil {
		t.Fatalf("worktree removed although its commit was not proven pushed: %v", statErr)
	}
}
