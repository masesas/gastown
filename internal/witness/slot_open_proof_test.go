package witness

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
)

// gt-pd1c.96: the witness announces a reusable slot (SCHEDULER_OPEN) only
// from the git state it computes; an agent bead that claims
// cleanup_status=clean must not open a slot whose worktree has work at risk.

func slotProofGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// installSlotProofBD answers `bd show <agent-bead>` with an idle polecat whose
// bead claims cleanup_status=clean; any other show is "not found", list is [].
func installSlotProofBD(t *testing.T, agentID string) {
	t.Helper()
	script := `#!/bin/sh
while [ "$1" = "--allow-stale" ]; do shift; done
case "$1" in
  version) echo "bd stub"; exit 0;;
  show)
    if [ "$2" = "` + agentID + `" ]; then
      printf '%s\n' '[{"id":"` + agentID + `","title":"agent","status":"open","issue_type":"agent","labels":["gt:agent"],"description":"role_type: polecat\nrig: rig\nagent_state: idle\nhook_bead: null\ncleanup_status: clean"}]'
      exit 0
    fi
    echo "Issue $2 not found" >&2; exit 1;;
esac
echo '[]'
`
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "bd"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	beads.ResetBdAllowStaleCacheForTest()
}

// newSlotProofTown lays out <town>/rig/polecats/toast/rig as a clone of a bare
// origin, checked out on a polecat branch at origin's main.
func newSlotProofTown(t *testing.T) (townRoot, workDir, clone string) {
	t.Helper()
	townRoot = t.TempDir()
	workDir = filepath.Join(townRoot, "rig")
	if err := os.MkdirAll(filepath.Join(workDir, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	clone = filepath.Join(workDir, "polecats", "toast", "rig")
	origin := filepath.Join(t.TempDir(), "origin.git")
	slotProofGit(t, "", "init", "--bare", "-b", "main", origin)
	slotProofGit(t, "", "init", "-b", "main", clone)
	slotProofGit(t, clone, "remote", "add", "origin", origin)
	if err := os.WriteFile(filepath.Join(clone, "README.md"), []byte("# rig\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	slotProofGit(t, clone, "add", "README.md")
	slotProofGit(t, clone, "commit", "-m", "base")
	slotProofGit(t, clone, "push", "origin", "main")
	slotProofGit(t, clone, "checkout", "-b", "polecat/toast/gt-work")
	installSlotProofBD(t, beads.PolecatBeadIDWithPrefix(beads.GetPrefixForRig(townRoot, "rig"), "rig", "toast"))
	return townRoot, workDir, clone
}

func TestSlotOpenDecisionUsesGitStateWhenBeadSaysClean(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses Unix shell script bd stub")
	}
	tests := []struct {
		name         string
		mutate       func(t *testing.T, clone string)
		wantReusable bool
	}{
		{name: "clean at origin main", mutate: func(*testing.T, string) {}, wantReusable: true},
		{name: "unpushed commit", mutate: func(t *testing.T, clone string) {
			if err := os.WriteFile(filepath.Join(clone, "work.go"), []byte("package work\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			slotProofGit(t, clone, "add", "work.go")
			slotProofGit(t, clone, "commit", "-m", "work")
		}},
		{name: "uncommitted file", mutate: func(t *testing.T, clone string) {
			if err := os.WriteFile(filepath.Join(clone, "draft.go"), []byte("package draft\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "git status error", mutate: func(t *testing.T, clone string) {
			if err := os.WriteFile(filepath.Join(clone, ".git", "index"), []byte("not a git index\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			townRoot, workDir, clone := newSlotProofTown(t)
			tt.mutate(t, clone)
			d := slotOpenDecision(workDir, townRoot, "rig", "toast", string(ExitTypeCompleted))
			if d.Reusable != tt.wantReusable {
				t.Fatalf("slotOpenDecision = %+v, want Reusable=%v", d, tt.wantReusable)
			}
		})
	}
}
