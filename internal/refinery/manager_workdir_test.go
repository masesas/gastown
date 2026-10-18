package refinery

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/rig"
)

// fixtureGit runs git with the user's global/system config disabled so the
// fixture does not depend on the machine's git configuration.
func fixtureGit(t *testing.T, dir string, args ...string) string {
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

// newRigWithRefineryWorktree builds the real rig layout: a shared bare repo
// (<rig>/.repo.git) whose tracked .gitignore has no Gas Town patterns, and the
// refinery worktree <rig>/refinery/rig linked to it.
func newRigWithRefineryWorktree(t *testing.T) (rigPath, gitignore string) {
	t.Helper()
	root := t.TempDir()
	src := filepath.Join(root, "src")
	rigPath = filepath.Join(root, "testrig")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	gitignore = "node_modules/\n"
	if err := os.WriteFile(filepath.Join(src, ".gitignore"), []byte(gitignore), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, src, "init", "-q", "-b", "main")
	fixtureGit(t, src, "-c", "user.email=t@example.com", "-c", "user.name=t", "add", ".")
	fixtureGit(t, src, "-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q", "-m", "init")
	if err := os.MkdirAll(rigPath, 0755); err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, root, "clone", "-q", "--bare", src, filepath.Join(rigPath, ".repo.git"))
	fixtureGit(t, filepath.Join(rigPath, ".repo.git"), "worktree", "add", "-q",
		filepath.Join(rigPath, "refinery", "rig"), "main")
	return rigPath, gitignore
}

// gt-pd1c.90: preparing the refinery work dir must keep the worktree clean.
// Writing Gas Town patterns into the tracked .gitignore left it " M .gitignore",
// and the merge engine (CheckWorktreeReady) refuses a dirty tree, so the
// refinery could never merge.
func TestPrepareWorkDir_RefineryWorktreeStaysClean(t *testing.T) {
	rigPath, gitignore := newRigWithRefineryWorktree(t)
	m := NewManager(&rig.Rig{Name: "testrig", Path: rigPath})

	dir := m.prepareWorkDir()

	want := filepath.Join(rigPath, "refinery", "rig")
	if dir != want {
		t.Fatalf("prepareWorkDir() = %q, want %q", dir, want)
	}
	// What a running refinery session leaves at its worktree root.
	for _, rel := range []string{
		".runtime/session.json", ".claude/settings.local.json", ".beads/redirect",
		".logs/refinery.log", "state.json", "CLAUDE.md",
	} {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	if status := fixtureGit(t, dir, "status", "--porcelain", "--untracked-files=all"); strings.TrimSpace(status) != "" {
		t.Fatalf("refinery worktree must stay clean, git status:\n%s", status)
	}
	got, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != gitignore {
		t.Errorf("tracked .gitignore was modified:\n%q\nwant %q", got, gitignore)
	}
}
