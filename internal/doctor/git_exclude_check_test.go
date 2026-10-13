package doctor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// gt-zjlv: when mayor/rig is a linked worktree of the rig's shared repo, the
// check must read and fix the common info/exclude that git actually uses.
func TestGitExcludeConfiguredCheck_MayorRigWorktreeUsesCommonExclude(t *testing.T) {
	town := t.TempDir()
	rigPath := filepath.Join(town, "myrig")
	src := filepath.Join(town, "src")
	repo := filepath.Join(rigPath, ".repo.git") // bare shared repo, as gt rig add creates
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	gitT(t, src, "init", "-q", "-b", "main")
	gitT(t, src, "-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init")
	gitT(t, town, "clone", "-q", "--bare", src, repo)
	gitT(t, repo, "worktree", "add", "-q", "-b", "mayor", filepath.Join(rigPath, "mayor", "rig"))

	check := NewGitExcludeConfiguredCheck()
	ctx := &CheckContext{TownRoot: town, RigName: "myrig"}

	if res := check.Run(ctx); res.Status != StatusWarning {
		t.Fatalf("expected warning before fix, got %v: %s", res.Status, res.Message)
	}
	if err := check.Fix(ctx); err != nil {
		t.Fatalf("Fix: %v", err)
	}

	common, err := os.ReadFile(filepath.Join(repo, "info", "exclude"))
	if err != nil {
		t.Fatalf("reading common exclude: %v", err)
	}
	if !strings.Contains(string(common), "/polecats/") {
		t.Fatalf("Fix must write the common exclude git reads, got:\n%s", common)
	}
	if res := check.Run(ctx); res.Status != StatusOK {
		t.Fatalf("expected OK after fix, got %v: %s", res.Status, res.Message)
	}
}
