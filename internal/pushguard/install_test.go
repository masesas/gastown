package pushguard

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// newRepo creates a non-bare repository with one commit.
func newRepo(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitT(t, dir, "init", "-q", "-b", "main")
	gitT(t, dir, "config", "user.email", "t@example.com")
	gitT(t, dir, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitT(t, dir, "add", ".")
	gitT(t, dir, "commit", "-q", "-m", "init")
	return dir
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func writeExec(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestScriptContract(t *testing.T) {
	s := Script()
	for _, want := range []string{"#!/bin/sh", Marker, "gt hooks pre-push", ChainedHookName, "command -v gt"} {
		if !strings.Contains(s, want) {
			t.Errorf("script missing %q", want)
		}
	}
	if strings.Contains(s, "GT_INTEGRATION_LAND") {
		t.Error("script must not mention GT_INTEGRATION_LAND")
	}
}

func TestInstallFreshRepoIsIdempotent(t *testing.T) {
	repo := newRepo(t, filepath.Join(t.TempDir(), "r"))
	hook := filepath.Join(repo, ".git", "hooks", "pre-push")

	st, err := Inspect(repo)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StateMissing || !st.Fixable() || st.OK() {
		t.Fatalf("fresh repo: got %+v, want missing+fixable", st)
	}

	st, err = Install(repo)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StateInstalled || !st.Changed {
		t.Fatalf("install: got %+v", st)
	}
	if got := readFile(t, hook); got != Script() {
		t.Fatalf("hook content mismatch:\n%s", got)
	}
	info, err := os.Stat(hook)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("hook not executable: %v", info.Mode())
	}

	st, err = Install(repo)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StateInstalled || st.Changed {
		t.Fatalf("second install should be a no-op, got %+v", st)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "hooks", ChainedHookName)); !os.IsNotExist(err) {
		t.Fatalf("no chained hook expected, stat err=%v", err)
	}
	if out := gitT(t, repo, "status", "--porcelain"); out != "" {
		t.Fatalf("install dirtied the worktree: %q", out)
	}
}

func TestInstallChainsProjectHook(t *testing.T) {
	repo := newRepo(t, filepath.Join(t.TempDir(), "r"))
	hooks := filepath.Join(repo, ".git", "hooks")
	project := "#!/bin/sh\necho project-hook\n"
	writeExec(t, filepath.Join(hooks, "pre-push"), project)

	st, err := Inspect(repo)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StateForeign || !st.Fixable() {
		t.Fatalf("foreign hook: got %+v", st)
	}

	st, err = Install(repo)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StateInstalled || !st.Changed || !st.Chained {
		t.Fatalf("install over project hook: got %+v", st)
	}
	if got := readFile(t, filepath.Join(hooks, ChainedHookName)); got != project {
		t.Fatalf("project hook not preserved: %q", got)
	}
	if got := readFile(t, filepath.Join(hooks, "pre-push")); got != Script() {
		t.Fatal("managed hook not installed")
	}

	// Idempotent: a second run keeps the chain untouched.
	st, err = Install(repo)
	if err != nil {
		t.Fatal(err)
	}
	if st.Changed || !st.Chained {
		t.Fatalf("second install: got %+v", st)
	}
	if got := readFile(t, filepath.Join(hooks, ChainedHookName)); got != project {
		t.Fatal("chained hook changed on re-install")
	}
}

func TestInstallRefusesToClobberOccupiedChainSlot(t *testing.T) {
	repo := newRepo(t, filepath.Join(t.TempDir(), "r"))
	hooks := filepath.Join(repo, ".git", "hooks")
	writeExec(t, filepath.Join(hooks, "pre-push"), "#!/bin/sh\necho new-project\n")
	writeExec(t, filepath.Join(hooks, ChainedHookName), "#!/bin/sh\necho old-project\n")

	st, err := Install(repo)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("want ErrConflict, got %v (%+v)", err, st)
	}
	if st.State != StateConflict || st.Fixable() {
		t.Fatalf("got %+v", st)
	}
	if got := readFile(t, filepath.Join(hooks, "pre-push")); !strings.Contains(got, "new-project") {
		t.Fatal("project pre-push was overwritten")
	}
}

func TestInstallUpgradesOutdatedManagedHook(t *testing.T) {
	repo := newRepo(t, filepath.Join(t.TempDir(), "r"))
	hook := filepath.Join(repo, ".git", "hooks", "pre-push")
	writeExec(t, hook, "#!/bin/sh\n# "+Marker+" v0\nexit 0\n")

	st, err := Inspect(repo)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StateOutdated || !st.Fixable() {
		t.Fatalf("got %+v", st)
	}
	st, err = Install(repo)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StateInstalled || !st.Changed {
		t.Fatalf("got %+v", st)
	}
	if readFile(t, hook) != Script() {
		t.Fatal("outdated hook not upgraded")
	}
}

func TestInstallProjectHooksPath(t *testing.T) {
	repo := newRepo(t, filepath.Join(t.TempDir(), "r"))
	gitT(t, repo, "config", "core.hooksPath", ".githooks")
	projectHook := filepath.Join(repo, ".githooks", "pre-push")

	// No pre-push in the project hooks dir: conflict, nothing written.
	st, err := Install(repo)
	if !errors.Is(err, ErrConflict) || st.State != StateConflict {
		t.Fatalf("missing project pre-push: got %+v err=%v", st, err)
	}
	if _, err := os.Stat(projectHook); !os.IsNotExist(err) {
		t.Fatal("must not write into a project-owned hooks dir")
	}

	// Project pre-push that does not delegate: conflict.
	writeExec(t, projectHook, "#!/bin/sh\nexit 0\n")
	if st, err = Install(repo); !errors.Is(err, ErrConflict) {
		t.Fatalf("non-delegating project hook: got %+v err=%v", st, err)
	}
	if !strings.Contains(st.Detail, "core.hooksPath") {
		t.Fatalf("detail should name core.hooksPath: %q", st.Detail)
	}

	// Project pre-push that delegates to gt hooks pre-push: OK, untouched.
	content := "#!/bin/sh\ngt hooks pre-push \"$@\"\n"
	writeExec(t, projectHook, content)
	st, err = Install(repo)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StateDelegated || !st.OK() || st.Changed {
		t.Fatalf("delegating project hook: got %+v", st)
	}
	if readFile(t, projectHook) != content {
		t.Fatal("project hook modified")
	}
}

func TestInstallSharedRepoCoversWorktrees(t *testing.T) {
	root := t.TempDir()
	src := newRepo(t, filepath.Join(root, "src"))
	bare := filepath.Join(root, "rig", ".repo.git")
	gitT(t, root, "clone", "-q", "--bare", src, bare)
	wt := filepath.Join(root, "rig", "polecats", "nux", "rig")
	gitT(t, bare, "worktree", "add", "-q", "-b", "polecat/nux", wt)

	st, err := Install(wt)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StateInstalled {
		t.Fatalf("got %+v", st)
	}
	wantDir, _ := filepath.EvalSymlinks(filepath.Join(bare, "hooks"))
	gotDir, _ := filepath.EvalSymlinks(st.HooksDir)
	if gotDir != wantDir {
		t.Fatalf("hooks dir = %s, want shared %s", gotDir, wantDir)
	}
	if st, _ := Inspect(bare); st.State != StateInstalled {
		t.Fatalf("bare repo should see the shared hook: %+v", st)
	}
}

func TestInspectNotARepo(t *testing.T) {
	if _, err := Inspect(t.TempDir()); err == nil {
		t.Fatal("want error for a non-repository")
	}
}

func TestEnsureRigDedupesSharedHooksDir(t *testing.T) {
	root := t.TempDir()
	src := newRepo(t, filepath.Join(root, "src"))
	rig := filepath.Join(root, "town", "myrig")
	bare := filepath.Join(rig, ".repo.git")
	gitT(t, root, "clone", "-q", "--bare", src, bare)
	gitT(t, bare, "worktree", "add", "-q", filepath.Join(rig, "refinery", "rig"), "main")
	gitT(t, bare, "worktree", "add", "-q", "-b", "polecat/a", filepath.Join(rig, "polecats", "a", "myrig"))
	gitT(t, root, "clone", "-q", src, filepath.Join(rig, "mayor", "rig"))
	gitT(t, root, "clone", "-q", src, filepath.Join(rig, "crew", "dom"))
	if err := os.MkdirAll(filepath.Join(rig, "crew", "notarepo"), 0o755); err != nil {
		t.Fatal(err)
	}

	before, err := InspectRig(rig)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 3 { // .repo.git (shared by refinery+polecat), mayor, crew/dom
		t.Fatalf("want 3 distinct hook dirs, got %d: %+v", len(before), before)
	}
	for _, st := range before {
		if st.State != StateMissing {
			t.Fatalf("before: %+v", st)
		}
	}

	after, err := EnsureRig(rig)
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range after {
		if st.State != StateInstalled {
			t.Fatalf("after: %+v", st)
		}
	}
	for _, p := range []string{
		filepath.Join(bare, "hooks", "pre-push"),
		filepath.Join(rig, "mayor", "rig", ".git", "hooks", "pre-push"),
		filepath.Join(rig, "crew", "dom", ".git", "hooks", "pre-push"),
	} {
		if readFile(t, p) != Script() {
			t.Fatalf("%s not installed", p)
		}
	}
}

func TestEnsureRigReportsConflictButInstallsOthers(t *testing.T) {
	root := t.TempDir()
	src := newRepo(t, filepath.Join(root, "src"))
	rig := filepath.Join(root, "town", "myrig")
	gitT(t, root, "clone", "-q", src, filepath.Join(rig, "mayor", "rig"))
	crew := filepath.Join(rig, "crew", "dom")
	gitT(t, root, "clone", "-q", src, crew)
	gitT(t, crew, "config", "core.hooksPath", ".githooks")

	sts, err := EnsureRig(rig)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
	var installed, conflict int
	for _, st := range sts {
		switch st.State {
		case StateInstalled:
			installed++
		case StateConflict:
			conflict++
		}
	}
	if installed != 1 || conflict != 1 {
		t.Fatalf("got %+v", sts)
	}
}

func TestInspectBareWithHooksPathIsNotApplicable(t *testing.T) {
	root := t.TempDir()
	src := newRepo(t, filepath.Join(root, "src"))
	bare := filepath.Join(root, "b.git")
	gitT(t, root, "clone", "-q", "--bare", src, bare)
	gitT(t, bare, "config", "core.hooksPath", ".githooks")
	st, err := Install(bare)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StateNotApplicable || !st.OK() || st.Changed {
		t.Fatalf("got %+v", st)
	}
}

func TestInspectAbsoluteHooksPathDelegated(t *testing.T) {
	repo := newRepo(t, filepath.Join(t.TempDir(), "r"))
	dir := filepath.Join(t.TempDir(), "shared-hooks")
	writeExec(t, filepath.Join(dir, "pre-push"), "#!/bin/sh\ngt hooks pre-push \"$@\" || exit 1\n")
	gitT(t, repo, "config", "core.hooksPath", dir)
	st, err := Inspect(repo)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StateDelegated || st.HooksDir != dir {
		t.Fatalf("got %+v", st)
	}
}

func TestInstallRestoresMissingManagedHookKeepingChain(t *testing.T) {
	repo := newRepo(t, filepath.Join(t.TempDir(), "r"))
	hooks := filepath.Join(repo, ".git", "hooks")
	writeExec(t, filepath.Join(hooks, ChainedHookName), "#!/bin/sh\nexit 0\n")
	st, err := Install(repo)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StateInstalled || !st.Chained || !st.Changed {
		t.Fatalf("got %+v", st)
	}
}

func TestEnsureRigLegacyPolecatAndBrokenRepo(t *testing.T) {
	root := t.TempDir()
	src := newRepo(t, filepath.Join(root, "src"))
	rig := filepath.Join(root, "town", "r")
	gitT(t, root, "clone", "-q", src, filepath.Join(rig, "polecats", "old"))
	// A directory with a bogus .git entry is listed but fails to resolve.
	writeExec(t, filepath.Join(rig, "crew", "broken", ".git"), "gitdir: /nonexistent\n")
	sts, err := EnsureRig(rig)
	if err == nil {
		t.Fatal("want error for the broken repo")
	}
	if len(sts) != 1 || sts[0].State != StateInstalled {
		t.Fatalf("legacy polecat clone should be installed: %+v", sts)
	}
}

func TestInspectNonExecutableManagedHookIsOutdated(t *testing.T) {
	repo := newRepo(t, filepath.Join(t.TempDir(), "r"))
	hook := filepath.Join(repo, ".git", "hooks", "pre-push")
	if err := os.WriteFile(hook, []byte(Script()), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := Inspect(repo)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StateOutdated || !st.Fixable() {
		t.Fatalf("non-executable hook git would ignore: got %+v", st)
	}
	if st, err = Install(repo); err != nil || st.State != StateInstalled {
		t.Fatalf("install: %+v %v", st, err)
	}
	info, _ := os.Stat(hook)
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatal("install must restore the exec bit")
	}
}

func TestDelegationIgnoresComments(t *testing.T) {
	repo := newRepo(t, filepath.Join(t.TempDir(), "r"))
	gitT(t, repo, "config", "core.hooksPath", ".githooks")
	hook := filepath.Join(repo, ".githooks", "pre-push")
	writeExec(t, hook, "#!/bin/sh\n# TODO: gt hooks pre-push\necho hi # gt hooks pre-push\nexit 0\n")
	if st, _ := Inspect(repo); st.State != StateConflict {
		t.Fatalf("comment-only mention must not count as delegation: %+v", st)
	}
	writeExec(t, hook, "#!/bin/sh\nprintf '%s\\n' \"$input\" | gt hooks pre-push \"$@\" || exit 1\n")
	if st, _ := Inspect(repo); st.State != StateDelegated {
		t.Fatalf("real delegation: %+v", st)
	}
	if err := os.Chmod(hook, 0o644); err != nil {
		t.Fatal(err)
	}
	if st, _ := Inspect(repo); st.State != StateConflict {
		t.Fatalf("non-executable project hook does not run: %+v", st)
	}
}
