package git

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func worktreeRootGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// newNestedCheckout returns a clean, pushed parent repository and a checkout
// nested in it (ignored by the parent) whose branch has an unpushed commit.
func newNestedCheckout(t *testing.T) (parent, child string) {
	t.Helper()
	parent = t.TempDir()
	parentOrigin := filepath.Join(t.TempDir(), "parent.git")
	childOrigin := filepath.Join(t.TempDir(), "child.git")
	worktreeRootGit(t, parent, "init", "--bare", "-b", "main", parentOrigin)
	worktreeRootGit(t, parent, "init", "--bare", "-b", "main", childOrigin)
	worktreeRootGit(t, parent, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(parent, ".gitignore"), []byte("polecats/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	worktreeRootGit(t, parent, "add", ".gitignore")
	worktreeRootGit(t, parent, "commit", "-m", "parent")
	worktreeRootGit(t, parent, "remote", "add", "origin", parentOrigin)
	worktreeRootGit(t, parent, "push", "-u", "origin", "main")

	child = filepath.Join(parent, "polecats", "toast", "rig")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	worktreeRootGit(t, child, "init", "-b", "main")
	worktreeRootGit(t, child, "remote", "add", "origin", childOrigin)
	if err := os.WriteFile(filepath.Join(child, "work.go"), []byte("package work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	worktreeRootGit(t, child, "add", "work.go")
	worktreeRootGit(t, child, "commit", "-m", "unpushed")
	return parent, child
}

func TestCheckWorktreeWorkOnOwnRoot(t *testing.T) {
	_, child := newNestedCheckout(t)
	status, err := NewGit(child).CheckWorktreeWork()
	if err != nil {
		t.Fatalf("CheckWorktreeWork: %v", err)
	}
	if !status.CommitsAtRisk() {
		t.Fatalf("status = %+v, want the child's unpushed commit at risk", status)
	}
}

// gt-pd1c.96 security review M1: without its .git the checkout resolves to the
// clean, pushed parent repository, which must not answer for it.
func TestCheckWorktreeWorkRefusesEnclosingRepository(t *testing.T) {
	parent, child := newNestedCheckout(t)
	if err := os.RemoveAll(filepath.Join(child, ".git")); err != nil {
		t.Fatal(err)
	}
	if ws, err := NewGit(child).CheckUncommittedWork(); err != nil || !ws.Clean() {
		t.Fatalf("precondition: plain CheckUncommittedWork should read the clean parent %s, got %+v, %v", parent, ws, err)
	}

	_, err := NewGit(child).CheckWorktreeWork()
	if !errors.Is(err, ErrNotWorktreeRoot) {
		t.Fatalf("CheckWorktreeWork err = %v, want ErrNotWorktreeRoot", err)
	}
}

// A .git entry that is not the checkout's own (here: a subdirectory of the
// parent that only contains a stray .git file) is refused too.
func TestVerifyWorktreeRootRefusesSubdirectory(t *testing.T) {
	parent, _ := newNestedCheckout(t)
	sub := filepath.Join(parent, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := NewGit(sub).VerifyWorktreeRoot(); !errors.Is(err, ErrNotWorktreeRoot) {
		t.Fatalf("VerifyWorktreeRoot(sub) = %v, want ErrNotWorktreeRoot", err)
	}
	if err := NewGit(parent).VerifyWorktreeRoot(); err != nil {
		t.Fatalf("VerifyWorktreeRoot(parent) = %v, want nil", err)
	}
}

func TestVerifyWorktreeRootResolvesSymlinks(t *testing.T) {
	_, child := newNestedCheckout(t)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(child, link); err != nil {
		t.Skipf("symlink: %v", err)
	}
	if err := NewGit(link).VerifyWorktreeRoot(); err != nil {
		t.Fatalf("VerifyWorktreeRoot(symlink to root) = %v, want nil", err)
	}
}
