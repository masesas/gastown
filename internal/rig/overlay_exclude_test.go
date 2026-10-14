package rig

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// newRepoWithWorktree creates a repo with one commit and a linked worktree,
// mirroring a polecat worktree of the rig's shared repo.
func newRepoWithWorktree(t *testing.T) (repo, worktree string) {
	t.Helper()
	root := t.TempDir()
	repo = filepath.Join(root, "repo")
	worktree = filepath.Join(root, "wt")
	if err := os.MkdirAll(filepath.Join(repo, "apps", "api"), 0755); err != nil {
		t.Fatal(err)
	}
	runGitT(t, repo, "init", "-q", "-b", "main")
	runGitT(t, repo, "config", "user.email", "t@example.com")
	runGitT(t, repo, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(repo, "apps", "api", "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGitT(t, repo, "add", ".")
	runGitT(t, repo, "commit", "-q", "-m", "init")
	runGitT(t, repo, "worktree", "add", "-q", "-b", "polecat", worktree)
	return repo, worktree
}

func writeFiles(t *testing.T, base string, rel ...string) {
	t.Helper()
	for _, r := range rel {
		p := filepath.Join(base, r)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

// gt-zjlv: git reads info/exclude only from the common dir, so the patterns
// must land there for a linked worktree to stay clean.
func TestEnsureLocalExcludePatterns_LinkedWorktreeStaysClean(t *testing.T) {
	_, wt := newRepoWithWorktree(t)

	if err := EnsureLocalExcludePatterns(wt); err != nil {
		t.Fatalf("EnsureLocalExcludePatterns: %v", err)
	}
	writeFiles(t, wt,
		"CLAUDE.md", "state.json", ".beads/issues.jsonl",
		".claude/commands/done.md", ".runtime/agent.json", ".logs/a.log",
		"apps/api/__pycache__/x.pyc",
	)

	if status := runGitT(t, wt, "status", "--porcelain", "--untracked-files=all"); strings.TrimSpace(status) != "" {
		t.Fatalf("worktree should be clean after EnsureLocalExcludePatterns, got:\n%s", status)
	}
}

// The exclude file is shared by every worktree of the repo, so root-only
// runtime names must not hide same-named source files in subdirectories.
func TestEnsureLocalExcludePatterns_DoesNotHideNestedSourceFiles(t *testing.T) {
	_, wt := newRepoWithWorktree(t)

	if err := EnsureLocalExcludePatterns(wt); err != nil {
		t.Fatalf("EnsureLocalExcludePatterns: %v", err)
	}
	writeFiles(t, wt, "apps/api/CLAUDE.md", "apps/api/state.json")

	status := runGitT(t, wt, "status", "--porcelain", "--untracked-files=all")
	for _, want := range []string{"apps/api/CLAUDE.md", "apps/api/state.json"} {
		if !strings.Contains(status, want) {
			t.Errorf("%s must stay visible to git, status:\n%s", want, status)
		}
	}
}

func TestGitInfoExcludePath_LinkedWorktreeUsesCommonDir(t *testing.T) {
	repo, wt := newRepoWithWorktree(t)

	got, err := GitInfoExcludePath(wt)
	if err != nil {
		t.Fatalf("GitInfoExcludePath: %v", err)
	}
	want := filepath.Join(repo, ".git", "info", "exclude")
	if resolved, err := filepath.EvalSymlinks(filepath.Dir(want)); err == nil {
		want = filepath.Join(resolved, "exclude")
	}
	if resolved, err := filepath.EvalSymlinks(filepath.Dir(got)); err == nil {
		got = filepath.Join(resolved, "exclude")
	}
	if got != want {
		t.Fatalf("GitInfoExcludePath(worktree) = %q, want %q", got, want)
	}
}

// Every polecat worktree shares one exclude file; repeated calls from several
// worktrees must not duplicate patterns.
func TestEnsureLocalExcludePatterns_SharedFileIsIdempotent(t *testing.T) {
	repo, wt := newRepoWithWorktree(t)
	wt2 := filepath.Join(filepath.Dir(wt), "wt2")
	runGitT(t, repo, "worktree", "add", "-q", "-b", "polecat2", wt2)

	for _, dir := range []string{wt, wt2, wt} {
		if err := EnsureLocalExcludePatterns(dir); err != nil {
			t.Fatalf("EnsureLocalExcludePatterns(%s): %v", dir, err)
		}
	}

	excludePath, err := GitInfoExcludePath(wt)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, pattern := range gasTownLocalExcludePatterns() {
		if n := strings.Count("\n"+string(data), "\n"+pattern+"\n"); n != 1 {
			t.Errorf("pattern %q appears %d times, want 1:\n%s", pattern, n, data)
		}
	}
}
