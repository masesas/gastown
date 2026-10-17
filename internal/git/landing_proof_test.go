package git

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func isNotOnBranch(err error) bool {
	var e *CommitNotOnBranchError
	return errors.As(err, &e)
}

// gt-pd1c.59 round 4 (B2): after the target was fetched, a commit whose object
// is absent from the clone cannot be on the target: a definite "not landed".
// A failure to read or fetch the target stays uncertain.
func TestVerifyPushedCommitReachable_MissingObjectIsDefinite(t *testing.T) {
	localDir, _, mainBranch := initTestRepoWithRemote(t)
	g := NewGit(localDir)
	if err := os.WriteFile(filepath.Join(localDir, "f.txt"), []byte("v1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := g.Add("f.txt"); err != nil {
		t.Fatal(err)
	}
	if err := g.Commit("v1"); err != nil {
		t.Fatal(err)
	}
	if err := g.Push("origin", mainBranch, false); err != nil {
		t.Fatal(err)
	}

	absent := "0123456789abcdef0123456789abcdef01234567"
	err := g.VerifyPushedCommitReachableFromPushTarget("origin", mainBranch, absent)
	if err == nil || !isNotOnBranch(err) {
		t.Fatalf("absent commit object: err = %v, want CommitNotOnBranchError", err)
	}

	// The local-only commit exists but is not on the target: also definite.
	if err := os.WriteFile(filepath.Join(localDir, "f.txt"), []byte("local\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := g.Add("f.txt"); err != nil {
		t.Fatal(err)
	}
	if err := g.Commit("local"); err != nil {
		t.Fatal(err)
	}
	local, _ := g.Rev("HEAD")
	if err := g.VerifyPushedCommitReachableFromPushTarget("origin", mainBranch, local); err == nil || !isNotOnBranch(err) {
		t.Fatalf("unpushed commit: err = %v, want CommitNotOnBranchError", err)
	}

	// Remote unreachable: cannot read or fetch the target, so uncertain.
	cmd := exec.Command("git", "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))
	cmd.Dir = localDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("set-url: %v\n%s", err, out)
	}
	if err := g.VerifyPushedCommitReachableFromPushTarget("origin", mainBranch, absent); err == nil || isNotOnBranch(err) {
		t.Fatalf("unreachable remote: err = %v, want an uncertain error", err)
	}
}

// LOW: "exit status 128" contains "exit status 1"; the exit code decides.
func TestIsNotAncestorExit(t *testing.T) {
	exitWith := func(code string) error {
		return exec.Command("sh", "-c", "exit "+code).Run()
	}
	if !isNotAncestorExit(&GitError{Command: "merge-base", Err: exitWith("1")}) {
		t.Fatal("exit 1 means not an ancestor")
	}
	if isNotAncestorExit(&GitError{Command: "merge-base", Err: exitWith("128")}) {
		t.Fatal("exit 128 without stderr is an error, not 'not an ancestor'")
	}
	if isNotAncestorExit(errors.New("exit status 1")) {
		t.Fatal("only a real exit code counts, not text")
	}
}

func TestIsAncestor_Exit128IsAnError(t *testing.T) {
	localDir, _, _ := initTestRepoWithRemote(t)
	if _, err := NewGit(localDir).IsAncestor("0123456789abcdef0123456789abcdef01234567", "HEAD"); err == nil {
		t.Fatal("an unknown commit must be an error, not 'not an ancestor'")
	}
}
