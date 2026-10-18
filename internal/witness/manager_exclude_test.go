package witness

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

// newWitnessRig lays out <town>/<rig> with the rig beads redirect that
// prepareWitnessDir needs.
func newWitnessRig(t *testing.T, townRoot string) string {
	t.Helper()
	rigPath := filepath.Join(townRoot, "gastown")
	for _, dir := range []string{filepath.Join(rigPath, ".beads"), filepath.Join(rigPath, "mayor", "rig", ".beads")} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(rigPath, ".beads", "redirect"), []byte("mayor/rig/.beads\n"), 0644); err != nil {
		t.Fatal(err)
	}
	return rigPath
}

func writeArtifacts(t *testing.T, base string, rel ...string) {
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

// gt-pd1c.90: a witness/rig worktree of the shared repo must stay clean
// without touching the repo's tracked .gitignore.
func TestPrepareWitnessDir_WorktreeStaysCleanWithoutTouchingTrackedGitignore(t *testing.T) {
	townRoot := t.TempDir()
	rigPath := newWitnessRig(t, townRoot)

	src := filepath.Join(townRoot, "src")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	const gitignore = "node_modules/\n"
	writeArtifacts(t, src, "main.go")
	if err := os.WriteFile(filepath.Join(src, ".gitignore"), []byte(gitignore), 0644); err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, src, "init", "-q", "-b", "main")
	fixtureGit(t, src, "-c", "user.email=t@example.com", "-c", "user.name=t", "add", ".")
	fixtureGit(t, src, "-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q", "-m", "init")
	bare := filepath.Join(rigPath, ".repo.git")
	fixtureGit(t, townRoot, "clone", "-q", "--bare", src, bare)
	fixtureGit(t, bare, "worktree", "add", "-q", filepath.Join(rigPath, "witness", "rig"), "main")

	mgr := NewManager(&rig.Rig{Name: "gastown", Path: rigPath})
	witnessDir, err := mgr.prepareWitnessDir(townRoot)
	if err != nil {
		t.Fatalf("prepareWitnessDir: %v", err)
	}
	if want := filepath.Join(rigPath, "witness", "rig"); witnessDir != want {
		t.Fatalf("witnessDir = %q, want %q", witnessDir, want)
	}
	writeArtifacts(t, witnessDir, ".runtime/session.json", ".claude/settings.local.json", ".logs/witness.log", "state.json")

	if status := fixtureGit(t, witnessDir, "status", "--porcelain", "--untracked-files=all"); strings.TrimSpace(status) != "" {
		t.Fatalf("witness worktree must stay clean, git status:\n%s", status)
	}
	got, err := os.ReadFile(filepath.Join(witnessDir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != gitignore {
		t.Errorf("tracked .gitignore was modified:\n%q\nwant %q", got, gitignore)
	}
}

// The default witness dir is not a git worktree. When the town itself is a git
// repo, the excludes must not be written into the town repo (where the root
// anchors would point at the town root), and no .gitignore may be created.
func TestPrepareWitnessDir_PlainDirInsideTownRepoWritesNoExcludes(t *testing.T) {
	townRoot := t.TempDir()
	rigPath := newWitnessRig(t, townRoot)
	fixtureGit(t, townRoot, "init", "-q", "-b", "main")
	excludePath := filepath.Join(townRoot, ".git", "info", "exclude")
	before, _ := os.ReadFile(excludePath)

	mgr := NewManager(&rig.Rig{Name: "gastown", Path: rigPath})
	witnessDir, err := mgr.prepareWitnessDir(townRoot)
	if err != nil {
		t.Fatalf("prepareWitnessDir: %v", err)
	}
	if want := filepath.Join(rigPath, "witness"); witnessDir != want {
		t.Fatalf("witnessDir = %q, want %q", witnessDir, want)
	}

	after, _ := os.ReadFile(excludePath)
	if string(after) != string(before) {
		t.Errorf("town repo info/exclude was modified:\n%s", after)
	}
	if _, err := os.Stat(filepath.Join(witnessDir, ".gitignore")); !os.IsNotExist(err) {
		t.Errorf("witness dir must not get a .gitignore (stat err=%v)", err)
	}
}
