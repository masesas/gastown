package git

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gt-pd1c.78 S-H1: ls-remote matches a pattern against the tail of a ref
// name, so a branch an agent named "refs/heads/integration/pwn" (full name
// refs/heads/refs/heads/integration/pwn) must not pass as an integration
// branch, nor a branch ending in "main" as main.
func TestListRemoteRefsWithHashesExactPrefix(t *testing.T) {
	tmp := t.TempDir()
	remote := filepath.Join(tmp, "remote.git")
	repo := filepath.Join(tmp, "repo")
	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(cmd.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run(tmp, "init", "-q", "--bare", "-b", "main", remote)
	run(tmp, "init", "-q", "-b", "main", repo)
	run(repo, "commit", "-q", "--allow-empty", "-m", "base")
	run(repo, "remote", "add", "origin", remote)
	run(repo, "push", "-q", "origin", "HEAD:refs/heads/main")
	run(repo, "push", "-q", "origin", "HEAD:refs/heads/integration/real")
	run(repo, "push", "-q", "origin", "HEAD:refs/heads/refs/heads/integration/pwn")
	run(repo, "push", "-q", "origin", "HEAD:refs/heads/x/refs/gt/archive/polecat/a/1-abcdef0")
	run(repo, "push", "-q", "origin", "HEAD:refs/heads/polecat/main")
	g := NewGit(repo)

	refs, err := g.ListRemoteRefsWithHashes("origin", "refs/heads/integration/")
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].Name != "refs/heads/integration/real" {
		t.Fatalf("integration refs = %+v, want only refs/heads/integration/real", refs)
	}
	if refs, _ := g.ListRemoteRefsWithHashes("origin", "refs/gt/archive/"); len(refs) != 0 {
		t.Fatalf("archive refs = %+v, want none", refs)
	}
	if ok, err := g.RemoteBranchExists("origin", "integration/pwn"); err != nil || ok {
		t.Fatalf("RemoteBranchExists(integration/pwn) = %v, %v; want false", ok, err)
	}
	if ok, _ := g.RemoteBranchExists("origin", "main"); !ok {
		t.Fatal("main exists")
	}
	if ok, _ := g.PushRemoteBranchExists("origin", "integration/pwn"); ok {
		t.Fatal("PushRemoteBranchExists must match exactly")
	}
}
