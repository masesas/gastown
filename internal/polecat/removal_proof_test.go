package polecat

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/rig"
	"github.com/steveyegge/gastown/internal/tmux"
)

// gt-pd1c.96: the agent bead's cleanup_status is written by the agent's own
// gt done run (and can be rewritten with bd), so it is never proof that a
// worktree is safe to discard. These tests run with installMockBd, whose agent
// bead always says cleanup_status=clean, against a real bare origin so the
// remote preservation proof (gt-pd1c.86) answers from the remote itself.

// setupRemoteProofManagerTest builds a rig whose mayor/rig is a clone of a
// separate bare origin. A polecat commit is then unpushed until it is pushed
// to that origin (unlike setupCanonicalBranchManagerTest, whose origin is the
// repo base itself and so lists every local branch).
func setupRemoteProofManagerTest(t *testing.T) (*Manager, string) {
	t.Helper()
	installMockBd(t)

	root := t.TempDir()
	origin := filepath.Join(t.TempDir(), "origin.git")
	seed := filepath.Join(t.TempDir(), "seed")
	mayorRig := filepath.Join(root, "mayor", "rig")
	removalProofGit(t, "", "init", "--bare", "-b", "main", origin)
	removalProofGit(t, "", "init", "-b", "main", seed)
	removalProofWrite(t, filepath.Join(seed, "README.md"), "# Test\n")
	removalProofGit(t, seed, "add", "README.md")
	removalProofGit(t, seed, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "Initial commit")
	removalProofGit(t, seed, "remote", "add", "origin", origin)
	removalProofGit(t, seed, "push", "origin", "main")
	if err := os.MkdirAll(filepath.Dir(mayorRig), 0o755); err != nil {
		t.Fatal(err)
	}
	removalProofGit(t, "", "clone", origin, mayorRig)

	for _, dir := range []string{filepath.Join(root, ".beads"), filepath.Join(mayorRig, ".beads")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	removalProofWrite(t, filepath.Join(root, ".beads", "redirect"), "mayor/rig/.beads\n")

	r := &rig.Rig{Name: "rig", Path: root}
	return NewManager(r, git.NewGit(root), nil), origin
}

func removalProofGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func removalProofWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// addBeadCleanPolecat adds polecat toast and checks the precondition every
// test relies on: its agent bead claims cleanup_status=clean.
func addBeadCleanPolecat(t *testing.T, mgr *Manager) *Polecat {
	t.Helper()
	p, err := mgr.AddWithOptions("toast", AddOptions{})
	if err != nil {
		t.Fatalf("AddWithOptions: %v", err)
	}
	if got := mgr.getCleanupStatusFromBead("toast"); got != CleanupClean {
		t.Fatalf("precondition: agent bead cleanup_status = %q, want clean", got)
	}
	return p
}

func commitUnpushed(t *testing.T, clonePath string) {
	t.Helper()
	removalProofWrite(t, filepath.Join(clonePath, "work.go"), "package work\n")
	removalProofGit(t, clonePath, "add", "work.go")
	removalProofGit(t, clonePath, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "unpushed work")
}

// corruptIndex makes `git status` fail in clonePath (a local git error, not a
// remote-proof gap).
func corruptIndex(t *testing.T, clonePath string) {
	t.Helper()
	out, err := exec.Command("git", "-C", clonePath, "rev-parse", "--path-format=absolute", "--git-path", "index").Output()
	if err != nil {
		t.Fatalf("git rev-parse --git-path index: %v", err)
	}
	removalProofWrite(t, strings.TrimSpace(string(out)), "not a git index\n")
	if _, err := git.NewGit(clonePath).CheckUncommittedWork(); err == nil {
		t.Fatal("precondition: CheckUncommittedWork should fail on a corrupt index")
	}
}

func assertWorktreeKept(t *testing.T, clonePath string) {
	t.Helper()
	if _, err := os.Stat(clonePath); err != nil {
		t.Fatalf("worktree removed although its work was not proven safe: %v", err)
	}
}

func TestRemoveRefusesUnpushedCommitsAlthoughBeadSaysClean(t *testing.T) {
	mgr, _ := setupRemoteProofManagerTest(t)
	p := addBeadCleanPolecat(t, mgr)
	commitUnpushed(t, p.ClonePath)

	err := mgr.RemoveWithOptions("toast", false, false, false)
	var uwe *UncommittedWorkError
	if !errors.As(err, &uwe) || !uwe.Status.CommitsAtRisk() {
		t.Fatalf("RemoveWithOptions err = %v, want UncommittedWorkError with commits at risk", err)
	}
	assertWorktreeKept(t, p.ClonePath)
}

func TestRemoveRefusesUncommittedChangesAlthoughBeadSaysClean(t *testing.T) {
	mgr, _ := setupRemoteProofManagerTest(t)
	p := addBeadCleanPolecat(t, mgr)
	removalProofWrite(t, filepath.Join(p.ClonePath, "draft.go"), "package draft\n")

	err := mgr.RemoveWithOptions("toast", false, false, false)
	var uwe *UncommittedWorkError
	if !errors.As(err, &uwe) || !uwe.Status.HasUncommittedChanges {
		t.Fatalf("RemoveWithOptions err = %v, want UncommittedWorkError with uncommitted changes", err)
	}
	assertWorktreeKept(t, p.ClonePath)
}

func TestRemoveAllowsCleanWorktreeProvenOnRemote(t *testing.T) {
	mgr, origin := setupRemoteProofManagerTest(t)
	p := addBeadCleanPolecat(t, mgr)
	commitUnpushed(t, p.ClonePath)
	// --no-verify: the Gas Town pre-push guard needs gt on PATH.
	removalProofGit(t, p.ClonePath, "push", "--no-verify", origin, "HEAD:refs/heads/"+p.Branch)

	if err := mgr.RemoveWithOptions("toast", false, false, false); err != nil {
		t.Fatalf("RemoveWithOptions on a clean, pushed worktree: %v", err)
	}
	if _, err := os.Stat(p.ClonePath); !os.IsNotExist(err) {
		t.Fatalf("worktree still present after removal: %v", err)
	}
}

// gt-pd1c.95 (L2): a local git error is not "nothing to lose".
func TestRemoveRefusesWhenGitStatusFails(t *testing.T) {
	mgr, _ := setupRemoteProofManagerTest(t)
	p := addBeadCleanPolecat(t, mgr)
	corruptIndex(t, p.ClonePath)

	err := mgr.RemoveWithOptions("toast", false, false, false)
	var unproven *UnprovenWorkError
	if !errors.As(err, &unproven) || !errors.Is(err, ErrWorkUnproven) {
		t.Fatalf("RemoveWithOptions err = %v, want UnprovenWorkError", err)
	}
	if !errors.Is(err, ErrHasUncommittedWork) {
		t.Fatalf("UnprovenWorkError must count as work at risk: %v", err)
	}
	assertWorktreeKept(t, p.ClonePath)
}

func TestRemoveForceDiscardsUnprovenAndUnpushedWork(t *testing.T) {
	t.Run("unpushed", func(t *testing.T) {
		mgr, _ := setupRemoteProofManagerTest(t)
		p := addBeadCleanPolecat(t, mgr)
		commitUnpushed(t, p.ClonePath)
		if err := mgr.RemoveWithOptions("toast", true, false, false); err != nil {
			t.Fatalf("RemoveWithOptions --force: %v", err)
		}
	})
	t.Run("git status error", func(t *testing.T) {
		mgr, _ := setupRemoteProofManagerTest(t)
		p := addBeadCleanPolecat(t, mgr)
		corruptIndex(t, p.ClonePath)
		if err := mgr.RemoveWithOptions("toast", true, false, false); err != nil {
			t.Fatalf("RemoveWithOptions --force: %v", err)
		}
	})
}

func TestRepairRefusesUnprovenOrUnpushedWork(t *testing.T) {
	t.Run("git status error", func(t *testing.T) {
		mgr, _ := setupRemoteProofManagerTest(t)
		p := addBeadCleanPolecat(t, mgr)
		corruptIndex(t, p.ClonePath)
		_, err := mgr.RepairWorktree("toast", false)
		if !errors.Is(err, ErrWorkUnproven) {
			t.Fatalf("RepairWorktree err = %v, want ErrWorkUnproven", err)
		}
		assertWorktreeKept(t, p.ClonePath)
	})
	t.Run("unpushed", func(t *testing.T) {
		mgr, _ := setupRemoteProofManagerTest(t)
		p := addBeadCleanPolecat(t, mgr)
		commitUnpushed(t, p.ClonePath)
		_, err := mgr.RepairWorktree("toast", false)
		var uwe *UncommittedWorkError
		if !errors.As(err, &uwe) {
			t.Fatalf("RepairWorktree err = %v, want UncommittedWorkError", err)
		}
	})
}

// Slot reuse (FindIdlePolecat / ReuseIdlePolecat) resets the worktree: the
// bead's clean claim must not make an unpushed commit reusable.
func TestReuseDecisionRefusesUnpushedAlthoughBeadSaysClean(t *testing.T) {
	mgr, _ := setupRemoteProofManagerTest(t)
	p := addBeadCleanPolecat(t, mgr)
	commitUnpushed(t, p.ClonePath)

	if d := mgr.ReuseDecisionForPolecat("toast", StateIdle); d.Reusable {
		t.Fatalf("reuse decision = %+v, want not reusable (unpushed commit)", d)
	}
}

// A structurally broken worktree cannot be read by git, so gt has no proof
// its files are safe to discard: reclaim refuses (an operator can still
// gt polecat nuke --force), whatever the bead claims.
func TestReclaimBrokenIdlePolecatRefusesWithoutGitProof(t *testing.T) {
	mgr, _ := setupRemoteProofManagerTest(t)
	mgr.tmux = tmux.NewTmux()
	if !mgr.tmux.IsAvailable() {
		t.Skip("tmux is required to prove no live polecat session")
	}
	p := addBeadCleanPolecat(t, mgr)
	removalProofWrite(t, filepath.Join(p.ClonePath, "draft.go"), "package draft\n")
	if err := os.Remove(filepath.Join(p.ClonePath, ".git")); err != nil {
		t.Fatalf("break worktree .git: %v", err)
	}

	err := mgr.ReclaimBrokenIdlePolecat("toast")
	if !errors.Is(err, ErrWorkUnproven) {
		t.Fatalf("ReclaimBrokenIdlePolecat err = %v, want ErrWorkUnproven", err)
	}
	if _, statErr := os.Stat(filepath.Join(p.ClonePath, "draft.go")); statErr != nil {
		t.Fatalf("uncommitted file of a broken worktree was discarded: %v", statErr)
	}
}

// makeCleanPushedParentRepo turns dir into a clean git repository pushed to
// its own bare origin; it ignores everything else in dir, so a nested polecat
// worktree whose .git is gone resolves to this clean, pushed repository.
func makeCleanPushedParentRepo(t *testing.T, dir string) {
	t.Helper()
	origin := filepath.Join(t.TempDir(), "parent.git")
	removalProofGit(t, "", "init", "--bare", "-b", "main", origin)
	removalProofGit(t, dir, "init", "-b", "main")
	removalProofWrite(t, filepath.Join(dir, ".gitignore"), "/*\n!.gitignore\n")
	removalProofGit(t, dir, "add", ".gitignore")
	removalProofGit(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "parent")
	removalProofGit(t, dir, "remote", "add", "origin", origin)
	removalProofGit(t, dir, "push", "--no-verify", "-u", "origin", "main")
}

// Security review M1: with its .git deleted, git run in the worktree would
// answer for the enclosing (clean, pushed) repository. That is no proof.
func TestRemoveRefusesWorktreeWithoutGitInsideCleanParentRepo(t *testing.T) {
	mgr, _ := setupRemoteProofManagerTest(t)
	p := addBeadCleanPolecat(t, mgr)
	commitUnpushed(t, p.ClonePath)
	makeCleanPushedParentRepo(t, mgr.rig.Path)
	if err := os.Remove(filepath.Join(p.ClonePath, ".git")); err != nil {
		t.Fatalf("delete worktree .git: %v", err)
	}
	if ws, err := git.NewGit(p.ClonePath).CheckUncommittedWork(); err != nil || !ws.Clean() {
		t.Fatalf("precondition: git in the worktree should read the clean parent, got %+v, %v", ws, err)
	}

	err := mgr.RemoveWithOptions("toast", false, false, false)
	if !errors.Is(err, ErrWorkUnproven) || !errors.Is(err, git.ErrNotWorktreeRoot) {
		t.Fatalf("RemoveWithOptions err = %v, want ErrWorkUnproven (not the worktree root)", err)
	}
	assertWorktreeKept(t, p.ClonePath)
}

// Security review M2: a git error is not "no uncommitted work" for stale
// detection either.
func TestDetectStalePolecatsBrokenGitIsNotStale(t *testing.T) {
	mgr, _ := setupRemoteProofManagerTest(t)
	p := addBeadCleanPolecat(t, mgr)
	commitUnpushed(t, p.ClonePath)
	if err := os.Remove(filepath.Join(p.ClonePath, ".git")); err != nil {
		t.Fatalf("delete worktree .git: %v", err)
	}

	infos, err := mgr.DetectStalePolecats(1)
	if err != nil {
		t.Fatalf("DetectStalePolecats: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("infos = %+v, want one polecat", infos)
	}
	if infos[0].IsStale {
		t.Fatalf("polecat with unreadable git state reported stale: %+v", infos[0])
	}
	if infos[0].GitCheckError == "" {
		t.Fatalf("GitCheckError empty: %+v", infos[0])
	}
}
