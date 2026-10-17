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

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// round 5 #2: in a shallow clone (gastown's .repo.git is cloned --depth 1) an
// old commit below the shallow boundary is absent although it is on the
// target, so "absent" proves nothing there. Same for a partial clone.
func TestVerifyPushedCommitReachable_AbsentIsUncertainInIncompleteClones(t *testing.T) {
	localDir, remoteDir, mainBranch := initTestRepoWithRemote(t)
	g := NewGit(localDir)
	for _, v := range []string{"v1", "v2", "v3"} {
		if err := os.WriteFile(filepath.Join(localDir, "f.txt"), []byte(v+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := g.Add("f.txt"); err != nil {
			t.Fatal(err)
		}
		if err := g.Commit(v); err != nil {
			t.Fatal(err)
		}
	}
	old, _ := g.Rev("HEAD~2") // landed, but below the shallow boundary
	if err := g.Push("origin", mainBranch, false); err != nil {
		t.Fatal(err)
	}

	shallowDir := filepath.Join(t.TempDir(), "shallow")
	if out, err := exec.Command("git", "clone", "--depth", "1", "file://"+remoteDir, shallowDir).CombinedOutput(); err != nil {
		t.Fatalf("shallow clone: %v\n%s", err, out)
	}
	if got := gitIn(t, shallowDir, "rev-parse", "--is-shallow-repository"); got != "true\n" {
		t.Fatalf("precondition: clone is not shallow (%q)", got)
	}
	err := NewGit(shallowDir).VerifyPushedCommitReachableFromPushTarget("origin", mainBranch, old)
	if err == nil || isNotOnBranch(err) {
		t.Fatalf("shallow clone, landed commit below the boundary: err = %v, want uncertain (never 'not on branch')", err)
	}

	// A full clone flagged as a partial clone (promisor): absent is uncertain too.
	fullDir := filepath.Join(t.TempDir(), "full")
	if out, err := exec.Command("git", "clone", remoteDir, fullDir).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	absent := "0123456789abcdef0123456789abcdef01234567"
	if err := NewGit(fullDir).VerifyPushedCommitReachableFromPushTarget("origin", mainBranch, absent); !isNotOnBranch(err) {
		t.Fatalf("complete clone: err = %v, want definite not-on-branch", err)
	}
	gitIn(t, fullDir, "config", "remote.origin.promisor", "true")
	if err := NewGit(fullDir).VerifyPushedCommitReachableFromPushTarget("origin", mainBranch, absent); err == nil || isNotOnBranch(err) {
		t.Fatalf("promisor clone: err = %v, want uncertain", err)
	}
	gitIn(t, fullDir, "config", "--unset", "remote.origin.promisor")
	gitIn(t, fullDir, "config", "extensions.partialclone", "origin")
	if err := NewGit(fullDir).VerifyPushedCommitReachableFromPushTarget("origin", mainBranch, absent); err == nil || isNotOnBranch(err) {
		t.Fatalf("partialclone extension: err = %v, want uncertain", err)
	}
}
