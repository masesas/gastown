package cmd

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/git"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// gt-pd1c.70: a submit whose commit is already on origin/<target> has no new
// work; it must be rejected, and base_sha is the recorded origin/<target> tip.
func TestCheckSubmitHasNewWork(t *testing.T) {
	tmp := t.TempDir()
	origin := tmp + "/origin.git"
	gitIn(t, tmp, "init", "-q", "--bare", "-b", "main", origin)
	repo := tmp + "/repo"
	gitIn(t, tmp, "clone", "-q", origin, repo)
	gitIn(t, repo, "commit", "-q", "--allow-empty", "-m", "base")
	gitIn(t, repo, "push", "-q", "origin", "HEAD:main")
	gitIn(t, repo, "fetch", "-q", "origin")
	g := git.NewGit(repo)

	base := submitBaseSHA(g, "main")
	if base == "" || base != gitIn(t, repo, "rev-parse", "HEAD") {
		t.Fatalf("submitBaseSHA = %q, want origin/main tip", base)
	}
	if err := checkSubmitHasNewWork(g, base, base, "main"); err == nil || !strings.Contains(err.Error(), "nothing to merge") {
		t.Fatalf("commit == target tip must be rejected, got %v", err)
	}

	gitIn(t, repo, "checkout", "-q", "-b", "polecat/x/gt-1")
	gitIn(t, repo, "commit", "-q", "--allow-empty", "-m", "work")
	work := gitIn(t, repo, "rev-parse", "HEAD")
	if err := checkSubmitHasNewWork(g, work, base, "main"); err != nil {
		t.Fatalf("new commit rejected: %v", err)
	}
	if err := checkSubmitHasNewWork(g, work, "", "main"); err != nil {
		t.Fatalf("unknown base must not block submit: %v", err)
	}
	if got := submitBaseSHA(g, "no-such-branch"); got != "" {
		t.Fatalf("unknown target base = %q, want empty", got)
	}
}
