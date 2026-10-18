package cmd

import (
	"path/filepath"
	"strings"
	"testing"
)

// gt-pd1c.86: a remote that cannot prove the branch preserved is never read
// as "nothing at risk".

func commitPolecatStopLocalWork(t *testing.T, repo string) {
	t.Helper()
	writePolecatStopTestFile(t, repo, "work.go", "package main\n")
	runPolecatStopTestGit(t, repo, "add", "work.go")
	runPolecatStopTestGit(t, repo, "commit", "-m", "local work")
}

func TestPolecatStopPendingWorkRemoteUnreachableIsPending(t *testing.T) {
	repo := initPolecatStopTestRepo(t)
	commitPolecatStopLocalWork(t, repo)
	// A forged tracking ref must not help either.
	runPolecatStopTestGit(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD")
	runPolecatStopTestGit(t, repo, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))

	pending, reason, err := polecatStopPendingWork(repo, polecatStopTestBranch)
	if err != nil {
		t.Fatalf("polecatStopPendingWork: %v", err)
	}
	if !pending || !strings.Contains(reason, "not proven") {
		t.Fatalf("pending = %v (%q), want pending: not proven on the remote", pending, reason)
	}
}

func TestPolecatStopPendingWorkForgedOriginHEADIsPending(t *testing.T) {
	repo := initPolecatStopTestRepo(t)
	commitPolecatStopLocalWork(t, repo)
	runPolecatStopTestGit(t, repo, "update-ref", "refs/remotes/origin/zzz", "HEAD")
	runPolecatStopTestGit(t, repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/zzz")

	pending, reason, err := polecatStopPendingWork(repo, polecatStopTestBranch)
	if err != nil {
		t.Fatalf("polecatStopPendingWork: %v", err)
	}
	if !pending {
		t.Fatalf("pending = false (%q), want the forged origin/HEAD refused", reason)
	}
}

func TestGetGitStateRemoteUnreachableFailsClosed(t *testing.T) {
	repo := initPolecatStopTestRepo(t)
	commitPolecatStopLocalWork(t, repo)
	runPolecatStopTestGit(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD")
	runPolecatStopTestGit(t, repo, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))

	state, err := getGitState(repo)
	if err == nil {
		t.Fatalf("getGitState = %+v, want an error (no remote proof)", state)
	}
	for _, want := range []string{"cannot prove", "git push origin " + polecatStopTestBranch, "--force"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not tell the human %q", err, want)
		}
	}
}

func TestGetGitStateForgedOriginMainIsDirty(t *testing.T) {
	repo := initPolecatStopTestRepo(t)
	commitPolecatStopLocalWork(t, repo)
	runPolecatStopTestGit(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD")

	state, err := getGitState(repo)
	if err != nil {
		t.Fatalf("getGitState: %v", err)
	}
	if state.Clean || state.UnpushedCommits == 0 {
		t.Fatalf("getGitState = %+v, want unpushed work (forged origin/main ignored)", state)
	}
}
