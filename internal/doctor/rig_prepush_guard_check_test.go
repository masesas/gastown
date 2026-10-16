package doctor

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/pushguard"
)

func prepushGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// newPrePushTown builds a town with one rig: .repo.git + refinery worktree,
// mayor/rig clone and crew/dom clone.
func newPrePushTown(t *testing.T) (town, rig string) {
	t.Helper()
	root := t.TempDir()
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	prepushGit(t, src, "init", "-q", "-b", "main")
	prepushGit(t, src, "-c", "user.email=t@e", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init")
	town = filepath.Join(root, "town")
	rig = filepath.Join(town, "myrig")
	prepushGit(t, root, "clone", "-q", "--bare", src, filepath.Join(rig, ".repo.git"))
	prepushGit(t, filepath.Join(rig, ".repo.git"), "worktree", "add", "-q", filepath.Join(rig, "refinery", "rig"), "main")
	prepushGit(t, root, "clone", "-q", src, filepath.Join(rig, "mayor", "rig"))
	prepushGit(t, root, "clone", "-q", src, filepath.Join(rig, "crew", "dom"))
	return town, rig
}

func TestRigPrePushGuardCheck_MissingThenFixed(t *testing.T) {
	town, rig := newPrePushTown(t)
	c := NewRigPrePushGuardCheck()
	ctx := &CheckContext{TownRoot: town}

	res := c.Run(ctx)
	if res.Status != StatusWarning || !strings.Contains(res.Message, "3") {
		t.Fatalf("want warning for 3 unguarded repos, got %+v", res)
	}
	if !c.CanFix() {
		t.Fatal("check must be fixable")
	}
	if err := c.Fix(ctx); err != nil {
		t.Fatal(err)
	}
	if res := c.Run(ctx); res.Status != StatusOK {
		t.Fatalf("after fix: %+v", res)
	}
	if b, err := os.ReadFile(filepath.Join(rig, ".repo.git", "hooks", "pre-push")); err != nil || string(b) != pushguard.Script() {
		t.Fatalf("shared hook not installed: %v", err)
	}
}

func TestRigPrePushGuardCheck_ConflictNotFixable(t *testing.T) {
	town, rig := newPrePushTown(t)
	prepushGit(t, filepath.Join(rig, "crew", "dom"), "config", "core.hooksPath", ".githooks")
	c := NewRigPrePushGuardCheck()
	ctx := &CheckContext{TownRoot: town}
	if err := c.Fix(ctx); err != nil {
		t.Fatalf("fix should install what it can and not fail on conflicts: %v", err)
	}
	res := c.Run(ctx)
	if res.Status != StatusWarning {
		t.Fatalf("conflict must stay visible: %+v", res)
	}
	joined := strings.Join(res.Details, "\n")
	if !strings.Contains(joined, "crew/dom") || !strings.Contains(joined, "core.hooksPath") {
		t.Fatalf("details should name the repo and the cause: %q", joined)
	}
}

func TestRigPrePushGuardCheck_RigScopeAndNoRigs(t *testing.T) {
	c := NewRigPrePushGuardCheck()
	if res := c.Run(&CheckContext{TownRoot: t.TempDir()}); res.Status != StatusOK {
		t.Fatalf("no rigs: %+v", res)
	}
	town, _ := newPrePushTown(t)
	if res := c.Run(&CheckContext{TownRoot: town, RigName: "other"}); res.Status != StatusOK {
		t.Fatalf("other rig selected: %+v", res)
	}
	if res := c.Run(&CheckContext{TownRoot: town, RigName: "myrig"}); res.Status != StatusWarning {
		t.Fatalf("myrig selected: %+v", res)
	}
}

func TestWithoutConflicts(t *testing.T) {
	other := errors.New("disk full")
	conflict := fmt.Errorf("x: %w", pushguard.ErrConflict)
	if withoutConflicts(conflict) != nil {
		t.Fatal("single conflict should be dropped")
	}
	if !errors.Is(withoutConflicts(other), other) {
		t.Fatal("other errors are kept")
	}
	got := withoutConflicts(errors.Join(conflict, other))
	if !errors.Is(got, other) || errors.Is(got, pushguard.ErrConflict) {
		t.Fatalf("joined: %v", got)
	}
}
