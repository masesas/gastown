package rig

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runGitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	// core.excludesFile=/dev/null keeps the user's default global ignore file
	// (~/.config/git/ignore) from hiding files the test expects to see.
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=core.excludesFile", "GIT_CONFIG_VALUE_0=/dev/null")
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

// newRepo creates a plain repository (the crew clone layout: its own .git dir)
// with one commit. A non-empty gitignore is committed as a tracked .gitignore.
func newRepo(t *testing.T, gitignore string) string {
	t.Helper()
	repo := t.TempDir()
	runGitT(t, repo, "init", "-q", "-b", "main")
	writeFiles(t, repo, "main.go")
	if gitignore != "" {
		if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte(gitignore), 0644); err != nil {
			t.Fatal(err)
		}
	}
	runGitT(t, repo, "-c", "user.email=t@example.com", "-c", "user.name=t", "add", ".")
	runGitT(t, repo, "-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q", "-m", "init")
	return repo
}

func readExclude(t *testing.T, repo string) string {
	t.Helper()
	excludePath, err := GitInfoExcludePath(repo)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(excludePath)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(data)
}

func writeExclude(t *testing.T, repo, content string) {
	t.Helper()
	excludePath, err := GitInfoExcludePath(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(excludePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(excludePath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// The exclude contract: root-anchored runtime artifacts (the exclude file is
// shared by every worktree of the repo), with __pycache__/ left unanchored
// because Python creates it in every package directory.
func TestGasTownLocalExcludePatterns_AnchoredContract(t *testing.T) {
	want := []string{
		"/.runtime/", "/.claude/", "/.opencode/", "/.logs/", "__pycache__/",
		"/state.json", "/CLAUDE.md", "/CLAUDE.local.md", "/GEMINI.md", "/.beads/",
	}
	got := gasTownLocalExcludePatterns()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("gasTownLocalExcludePatterns() =\n%v\nwant\n%v", got, want)
	}
}

func TestEnsureLocalExcludePatterns_WritesAnchoredPatternsToCloneExclude(t *testing.T) {
	repo := newRepo(t, "")
	if err := EnsureLocalExcludePatterns(repo); err != nil {
		t.Fatalf("EnsureLocalExcludePatterns: %v", err)
	}
	content := readExclude(t, repo)
	for _, pattern := range gasTownLocalExcludePatterns() {
		if n := countOccurrences(content, pattern); n != 1 {
			t.Errorf("pattern %q appears %d times, want 1:\n%s", pattern, n, content)
		}
	}
}

// gt-pd1c.90 (and the .beads/ regression guard of PR #753/#966): the tracked
// .gitignore is never written, so it can neither dirty the tree nor gain
// ".beads/"; a repo without one does not get one.
func TestEnsureLocalExcludePatterns_NeverWritesGitignore(t *testing.T) {
	t.Run("tracked gitignore unchanged", func(t *testing.T) {
		const gitignore = "node_modules/\n"
		repo := newRepo(t, gitignore)
		if err := EnsureLocalExcludePatterns(repo); err != nil {
			t.Fatalf("EnsureLocalExcludePatterns: %v", err)
		}
		writeFiles(t, repo, ".runtime/x.json", ".claude/settings.json", ".beads/redirect", "state.json")
		got, err := os.ReadFile(filepath.Join(repo, ".gitignore"))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != gitignore {
			t.Errorf("tracked .gitignore modified: %q, want %q", got, gitignore)
		}
		if status := runGitT(t, repo, "status", "--porcelain", "--untracked-files=all"); strings.TrimSpace(status) != "" {
			t.Errorf("tree must stay clean, got:\n%s", status)
		}
	})
	t.Run("no gitignore created", func(t *testing.T) {
		repo := newRepo(t, "")
		if err := EnsureLocalExcludePatterns(repo); err != nil {
			t.Fatalf("EnsureLocalExcludePatterns: %v", err)
		}
		if _, err := os.Stat(filepath.Join(repo, ".gitignore")); !os.IsNotExist(err) {
			t.Errorf(".gitignore must not be created (stat err=%v)", err)
		}
	})
}

func TestEnsureLocalExcludePatterns_PreservesExistingContent(t *testing.T) {
	repo := newRepo(t, "")
	writeExclude(t, repo, "# local\n*.swp") // no trailing newline
	if err := EnsureLocalExcludePatterns(repo); err != nil {
		t.Fatalf("EnsureLocalExcludePatterns: %v", err)
	}
	content := readExclude(t, repo)
	if !strings.HasPrefix(content, "# local\n*.swp\n") {
		t.Errorf("existing content must be kept and newline-terminated:\n%s", content)
	}
	if !containsLine(content, "# Gas Town (added by gt)") {
		t.Errorf("missing Gas Town header:\n%s", content)
	}
}

// Lines a user (or an older gt) already wrote are recognized in their variant
// forms: without trailing slash, with or without the root anchor, or as a
// broader directory.
func TestEnsureLocalExcludePatterns_RecognizesExistingVariants(t *testing.T) {
	repo := newRepo(t, "")
	existing := ".runtime\n/.claude\n.opencode/\nstate.json\n"
	writeExclude(t, repo, existing)
	if err := EnsureLocalExcludePatterns(repo); err != nil {
		t.Fatalf("EnsureLocalExcludePatterns: %v", err)
	}
	content := readExclude(t, repo)
	for _, added := range []string{"/.runtime/", "/.claude/", "/.opencode/", "/state.json"} {
		if containsLine(content, added) {
			t.Errorf("%q must not be added when an equivalent line exists:\n%s", added, content)
		}
	}
	for _, missing := range []string{"/.logs/", "__pycache__/", "/CLAUDE.md", "/.beads/"} {
		if !containsLine(content, missing) {
			t.Errorf("missing pattern %q:\n%s", missing, content)
		}
	}
}

// A narrow .claude/commands/ line (old installs) does not cover .claude/.
func TestEnsureLocalExcludePatterns_NarrowClaudeDoesNotCoverBroad(t *testing.T) {
	repo := newRepo(t, "")
	writeExclude(t, repo, ".claude/commands/\n")
	if err := EnsureLocalExcludePatterns(repo); err != nil {
		t.Fatalf("EnsureLocalExcludePatterns: %v", err)
	}
	if content := readExclude(t, repo); !containsLine(content, "/.claude/") {
		t.Errorf("/.claude/ must be added when only .claude/commands/ exists:\n%s", content)
	}
}

func TestEnsureLocalExcludePatterns_SecondCallLeavesFileUntouched(t *testing.T) {
	repo := newRepo(t, "")
	if err := EnsureLocalExcludePatterns(repo); err != nil {
		t.Fatal(err)
	}
	first := readExclude(t, repo)
	if err := EnsureLocalExcludePatterns(repo); err != nil {
		t.Fatal(err)
	}
	if second := readExclude(t, repo); second != first {
		t.Errorf("second call modified the exclude file:\n%q\nwant %q", second, first)
	}
}

// The patterns are root-anchored, so they must only be written for the root of
// a checkout. A plain subdirectory (e.g. a witness dir inside a town repo)
// must fail closed instead of writing into the enclosing repo's exclude file.
func TestEnsureLocalExcludePatterns_RejectsNonWorktreeRoot(t *testing.T) {
	repo := newRepo(t, "")
	before := readExclude(t, repo)
	sub := filepath.Join(repo, "witness")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}

	for _, dir := range []string{sub, t.TempDir()} {
		err := EnsureLocalExcludePatterns(dir)
		if !errors.Is(err, ErrNotWorktreeRoot) {
			t.Errorf("EnsureLocalExcludePatterns(%s) error = %v, want ErrNotWorktreeRoot", dir, err)
		}
	}
	if after := readExclude(t, repo); after != before {
		t.Errorf("enclosing repo exclude modified:\n%s", after)
	}
}
