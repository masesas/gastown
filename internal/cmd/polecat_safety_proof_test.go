package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/polecat"
)

// gt-pd1c.96: gt polecat nuke (and the witness, which runs it) decides from
// the git state gt computes now. installSafetyBDStub's agent bead always says
// cleanup_status=clean; that claim must not skip the git check.

func safetyProofGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// newSafetyProofTarget is newSafetyTestTarget with a real polecat worktree:
// a clone of a bare origin on branch polecat/toast/gt-work with one commit
// that origin does not have.
func newSafetyProofTarget(t *testing.T) (polecatTarget, string) {
	t.Helper()
	target := newSafetyTestTarget(t)
	clone := filepath.Join(target.r.Path, "polecats", "toast", "gastown")
	origin := filepath.Join(t.TempDir(), "origin.git")
	safetyProofGit(t, "", "init", "--bare", "-b", "main", origin)
	safetyProofGit(t, "", "init", "-b", "main", clone)
	safetyProofGit(t, clone, "remote", "add", "origin", origin)
	if err := os.WriteFile(filepath.Join(clone, "README.md"), []byte("# rig\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	safetyProofGit(t, clone, "add", "README.md")
	safetyProofGit(t, clone, "commit", "-m", "base")
	safetyProofGit(t, clone, "push", "origin", "main")
	safetyProofGit(t, clone, "checkout", "-b", "polecat/toast/gt-work")
	if err := os.WriteFile(filepath.Join(clone, "work.go"), []byte("package work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	safetyProofGit(t, clone, "add", "work.go")
	safetyProofGit(t, clone, "commit", "-m", "work")
	return target, clone
}

func TestCheckPolecatSafetyUsesGitStateWhenBeadSaysClean(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses Unix shell script bd stub")
	}
	tests := []struct {
		name        string
		mutate      func(t *testing.T, clone string)
		wantBlocked bool
		wantReason  string
	}{
		{name: "unpushed commit", mutate: func(*testing.T, string) {}, wantBlocked: true, wantReason: "unpushed"},
		{name: "uncommitted file", mutate: func(t *testing.T, clone string) {
			safetyProofGit(t, clone, "push", "origin", "HEAD")
			if err := os.WriteFile(filepath.Join(clone, "draft.go"), []byte("package draft\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, wantBlocked: true, wantReason: "uncommitted"},
		{name: "git status error", mutate: func(t *testing.T, clone string) {
			safetyProofGit(t, clone, "push", "origin", "HEAD")
			if err := os.WriteFile(filepath.Join(clone, ".git", "index"), []byte("not a git index\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, wantBlocked: true, wantReason: "cannot check git state"},
		{name: "clean and pushed", mutate: func(t *testing.T, clone string) {
			safetyProofGit(t, clone, "push", "origin", "HEAD")
		}, wantBlocked: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			installSafetyBDStub(t, "none")
			target, clone := newSafetyProofTarget(t)
			tt.mutate(t, clone)

			result := checkPolecatSafety(target)
			if result.CleanupStatus != "clean" {
				t.Fatalf("precondition: bead cleanup_status = %q, want clean", result.CleanupStatus)
			}
			if result.Blocked != tt.wantBlocked {
				t.Fatalf("Blocked = %v, want %v (reasons: %v)", result.Blocked, tt.wantBlocked, result.Reasons)
			}
			if tt.wantReason != "" && !strings.Contains(strings.Join(result.Reasons, "; "), tt.wantReason) {
				t.Errorf("reasons %v should mention %q", result.Reasons, tt.wantReason)
			}
		})
	}
}

// makeCleanPushedParent makes dir a clean git repository pushed to its own
// bare origin that ignores everything else in dir.
func makeCleanPushedParent(t *testing.T, dir string) {
	t.Helper()
	origin := filepath.Join(t.TempDir(), "parent.git")
	safetyProofGit(t, "", "init", "--bare", "-b", "main", origin)
	safetyProofGit(t, dir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("/*\n!.gitignore\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	safetyProofGit(t, dir, "add", ".gitignore")
	safetyProofGit(t, dir, "commit", "-m", "parent")
	safetyProofGit(t, dir, "remote", "add", "origin", origin)
	safetyProofGit(t, dir, "push", "-u", "origin", "main")
}

// Security review M1: a polecat worktree without .git inside a clean, pushed
// town repository must not be judged by that repository.
func TestCheckPolecatSafetyRefusesWorktreeWithoutGitInsideCleanParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses Unix shell script bd stub")
	}
	installSafetyBDStub(t, "none")
	target, clone := newSafetyProofTarget(t)
	safetyProofGit(t, clone, "push", "origin", "HEAD")
	makeCleanPushedParent(t, filepath.Dir(target.r.Path))
	if err := os.RemoveAll(filepath.Join(clone, ".git")); err != nil {
		t.Fatal(err)
	}

	result := checkPolecatSafety(target)
	if !result.Blocked || !strings.Contains(strings.Join(result.Reasons, "; "), "cannot check git state") {
		t.Fatalf("checkPolecatSafety = %+v, want blocked: cannot check git state", result)
	}
}

// Security review M2: gt polecat stale --cleanup goes through the nuke safety
// check; a polecat whose git state cannot be read is not nuked.
func TestCleanupStalePolecatsUsesNukeSafetyGate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses Unix shell script bd stub")
	}
	installSafetyBDStub(t, "none")
	broken, brokenClone := newSafetyProofTarget(t)
	if err := os.RemoveAll(filepath.Join(brokenClone, ".git")); err != nil {
		t.Fatal(err)
	}
	clean, cleanClone := newSafetyProofTarget(t)
	safetyProofGit(t, cleanClone, "push", "origin", "HEAD")

	targets := map[string]polecatTarget{"broken": broken, "clean": clean}
	infos := []*polecat.StalenessInfo{
		{Name: "broken", IsStale: true},
		{Name: "clean", IsStale: true},
		{Name: "active", IsStale: false},
	}
	var nuked []string
	got := cleanupStalePolecats(infos, func(name string) polecatTarget { return targets[name] }, func(name string, _ bool) error {
		nuked = append(nuked, name)
		return nil
	})
	if got != 1 || len(nuked) != 1 || nuked[0] != "clean" {
		t.Fatalf("cleanupStalePolecats nuked %v (count %d), want only clean", nuked, got)
	}
}
