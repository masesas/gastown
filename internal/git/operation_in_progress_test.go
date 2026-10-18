package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func writeAndCommit(t *testing.T, dir, file, content, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", file)
	runGit(t, dir, "commit", "-m", msg)
}

// conflictingBranches makes branch "other" and the current branch change
// README.md differently.
func conflictingBranches(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "checkout", "-b", "other")
	writeAndCommit(t, dir, "README.md", "other\n", "other")
	runGit(t, dir, "checkout", "-")
	writeAndCommit(t, dir, "README.md", "mine\n", "mine")
}

func TestOperationInProgress(t *testing.T) {
	t.Run("clean repository", func(t *testing.T) {
		dir := initTestRepo(t)
		op, err := NewGit(dir).OperationInProgress()
		if err != nil || op != "" {
			t.Fatalf("OperationInProgress() = %q, %v; want \"\", nil", op, err)
		}
	})
	t.Run("unfinished merge", func(t *testing.T) {
		dir := initTestRepo(t)
		conflictingBranches(t, dir)
		_ = exec.Command("git", "-C", dir, "merge", "other").Run() // conflicts
		op, err := NewGit(dir).OperationInProgress()
		if err != nil || op != "merge" {
			t.Fatalf("OperationInProgress() = %q, %v; want merge", op, err)
		}
	})
	t.Run("unfinished rebase", func(t *testing.T) {
		dir := initTestRepo(t)
		conflictingBranches(t, dir)
		_ = exec.Command("git", "-C", dir, "rebase", "other").Run() // conflicts
		op, err := NewGit(dir).OperationInProgress()
		if err != nil || op != "rebase" {
			t.Fatalf("OperationInProgress() = %q, %v; want rebase", op, err)
		}
	})
	t.Run("unfinished cherry-pick", func(t *testing.T) {
		dir := initTestRepo(t)
		conflictingBranches(t, dir)
		_ = exec.Command("git", "-C", dir, "cherry-pick", "other").Run() // conflicts
		op, err := NewGit(dir).OperationInProgress()
		if err != nil || op != "cherry-pick" {
			t.Fatalf("OperationInProgress() = %q, %v; want cherry-pick", op, err)
		}
	})
	t.Run("linked worktree has its own state", func(t *testing.T) {
		dir := initTestRepo(t)
		conflictingBranches(t, dir)
		wt := filepath.Join(t.TempDir(), "wt")
		runGit(t, dir, "worktree", "add", "-b", "wt-branch", wt)
		_ = exec.Command("git", "-C", dir, "merge", "other").Run() // conflict in the main worktree only
		op, err := NewGit(wt).OperationInProgress()
		if err != nil || op != "" {
			t.Fatalf("worktree OperationInProgress() = %q, %v; want \"\" (the merge is in the main worktree)", op, err)
		}
	})
	t.Run("not a repository fails", func(t *testing.T) {
		if _, err := NewGit(t.TempDir()).OperationInProgress(); err == nil {
			t.Fatal("OperationInProgress() outside a repository = nil error")
		}
	})
}
