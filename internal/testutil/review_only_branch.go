package testutil

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// NewUnpushedPolecatBranch creates a bare "origin" remote and a clone whose
// checked-out branch carries one commit that origin does not have — the state
// of a polecat worktree before nuke/removal. Returns the clone and remote paths.
func NewUnpushedPolecatBranch(t *testing.T, branch string) (clonePath, remotePath string) {
	t.Helper()
	remotePath = filepath.Join(t.TempDir(), "origin.git")
	clonePath = filepath.Join(t.TempDir(), "clone")
	reviewOnlyFixtureGit(t, "", "init", "--bare", remotePath)
	reviewOnlyFixtureGit(t, "", "init", clonePath)
	reviewOnlyFixtureGit(t, clonePath, "config", "user.email", "test@example.com")
	reviewOnlyFixtureGit(t, clonePath, "config", "user.name", "Test User")
	reviewOnlyFixtureGit(t, clonePath, "remote", "add", "origin", remotePath)
	reviewOnlyFixtureWrite(t, filepath.Join(clonePath, "file.txt"), "main\n")
	reviewOnlyFixtureGit(t, clonePath, "add", "file.txt")
	reviewOnlyFixtureGit(t, clonePath, "commit", "-m", "main")
	reviewOnlyFixtureGit(t, clonePath, "branch", "-M", "main")
	reviewOnlyFixtureGit(t, clonePath, "push", "-u", "origin", "main")
	reviewOnlyFixtureGit(t, clonePath, "checkout", "-b", branch)
	reviewOnlyFixtureWrite(t, filepath.Join(clonePath, "notes.md"), "review notes\n")
	reviewOnlyFixtureGit(t, clonePath, "add", "notes.md")
	reviewOnlyFixtureGit(t, clonePath, "commit", "-m", "review notes")
	return clonePath, remotePath
}

// RemoteHasBranch reports whether the bare remote has refs/heads/<branch>.
func RemoteHasBranch(t *testing.T, remotePath, branch string) bool {
	t.Helper()
	cmd := exec.Command("git", "--git-dir", remotePath, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return cmd.Run() == nil
}

// InstallBDShowStub puts a bd stub on PATH that answers `bd show <id> --json`
// with the given descriptions (id -> description) and bd's real
// "Issue <id> not found" error for any other id. With failAll every bd call
// fails with a non-"not found" error (beads unavailable).
func InstallBDShowStub(t *testing.T, descriptions map[string]string, failAll bool) {
	t.Helper()
	dir := t.TempDir()
	for id, desc := range descriptions {
		data, err := json.Marshal([]map[string]string{{
			"id": id, "title": id, "status": "hooked", "issue_type": "task", "description": desc,
		}})
		if err != nil {
			t.Fatalf("marshal %s: %v", id, err)
		}
		reviewOnlyFixtureWrite(t, filepath.Join(dir, "show-"+id+".json"), string(data))
	}
	fail := ""
	if failAll {
		fail = "echo 'database is locked' >&2\nexit 1\n"
	}
	script := fmt.Sprintf(`#!/bin/sh
while [ "$1" = "--allow-stale" ]; do shift; done
if [ "$1" = "version" ]; then
  echo "bd stub"
  exit 0
fi
%sif [ "$1" = "show" ] && [ -f %q/"show-$2.json" ]; then
  cat %q/"show-$2.json"
  exit 0
fi
if [ "$1" = "show" ]; then
  echo "Issue $2 not found" >&2
  exit 1
fi
echo '[]'
exit 0
`, fail, dir, dir)
	binDir := t.TempDir()
	reviewOnlyFixtureWrite(t, filepath.Join(binDir, "bd"), script)
	if err := os.Chmod(filepath.Join(binDir, "bd"), 0o755); err != nil {
		t.Fatalf("chmod bd stub: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func reviewOnlyFixtureGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func reviewOnlyFixtureWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
