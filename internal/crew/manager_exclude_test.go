package crew

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/git"
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

// gt-pd1c.90: a crew clone must stay clean without touching the repo's tracked
// .gitignore. Appending Gas Town patterns there left " M .gitignore", an edit a
// crew worker could commit and push upstream.
func TestManagerAdd_CrewCloneStaysCleanWithoutTouchingTrackedGitignore(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	const gitignore = "node_modules/\n"
	if err := os.WriteFile(filepath.Join(src, ".gitignore"), []byte(gitignore), 0644); err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, src, "init", "-q", "-b", "main")
	fixtureGit(t, src, "-c", "user.email=t@example.com", "-c", "user.name=t", "add", ".")
	fixtureGit(t, src, "-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q", "-m", "init")
	bare := filepath.Join(root, "origin.git")
	fixtureGit(t, root, "clone", "-q", "--bare", src, bare)

	rigPath := filepath.Join(root, "test-rig")
	if err := os.MkdirAll(rigPath, 0755); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(&rig.Rig{Name: "test-rig", Path: rigPath, GitURL: bare}, git.NewGit(rigPath))

	worker, err := mgr.Add("dave", false)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	// What a running crew session leaves at its clone root.
	for _, rel := range []string{".runtime/session.json", ".claude/settings.local.json", ".logs/crew.log"} {
		p := filepath.Join(worker.ClonePath, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	if status := fixtureGit(t, worker.ClonePath, "status", "--porcelain", "--untracked-files=all"); strings.TrimSpace(status) != "" {
		t.Fatalf("crew clone must stay clean, git status:\n%s", status)
	}
	got, err := os.ReadFile(filepath.Join(worker.ClonePath, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != gitignore {
		t.Errorf("tracked .gitignore was modified:\n%q\nwant %q", got, gitignore)
	}
}
