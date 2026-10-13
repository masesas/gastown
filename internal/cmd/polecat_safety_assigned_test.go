package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/polecat"
	"github.com/steveyegge/gastown/internal/rig"
)

func TestAssignedWorkBlocker(t *testing.T) {
	if got := assignedWorkBlocker(nil, nil); got != "" {
		t.Errorf("no assigned work: got %q, want no blocker", got)
	}
	if got := assignedWorkBlocker(&beads.Issue{ID: "gt-1", Status: "hooked"}, nil); !strings.Contains(got, "gt-1") {
		t.Errorf("assigned work: got %q, want blocker naming gt-1", got)
	}
	if got := assignedWorkBlocker(nil, errors.New("database is locked")); !strings.Contains(got, "unknown") {
		t.Errorf("lookup error: got %q, want unknown blocker", got)
	}
}

func TestAgentBeadLookupBlocker(t *testing.T) {
	if got := agentBeadLookupBlocker(nil); got != "" {
		t.Errorf("no error: got %q", got)
	}
	if got := agentBeadLookupBlocker(errors.New("connection refused")); !strings.Contains(got, "agent bead lookup failed") {
		t.Errorf("error: got %q", got)
	}
}

// installSafetyBDStub answers the agent-bead show with a clean polecat and
// "bd list --assignee=..." according to listMode: "work", "none" or "fail".
func installSafetyBDStub(t *testing.T, listMode string) {
	t.Helper()
	listCase := `echo '[]'; exit 0`
	switch listMode {
	case "work":
		listCase = `case "$*" in *--status=hooked*) echo '[{"id":"gt-work","status":"hooked","assignee":"gastown/polecats/toast"}]';; *) echo '[]';; esac; exit 0`
	case "fail":
		listCase = `echo "database is locked" >&2; exit 1`
	}
	script := `#!/bin/sh
if [ "$1" = "--allow-stale" ]; then shift; fi
case "$1" in
  version) echo "bd stub"; exit 0;;
  show) printf '%s\n' '[{"id":"'"$2"'","title":"agent","status":"open","issue_type":"agent","labels":["gt:agent"],"description":"role_type: polecat\nrig: gastown\nagent_state: idle\ncleanup_status: clean"}]'; exit 0;;
  list) ` + listCase + `;;
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

func newSafetyTestTarget(t *testing.T) polecatTarget {
	t.Helper()
	townRoot := t.TempDir()
	rigPath := filepath.Join(townRoot, "gastown")
	if err := os.MkdirAll(filepath.Join(rigPath, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := &rig.Rig{Name: "gastown", Path: rigPath}
	return polecatTarget{
		rigName:     "gastown",
		polecatName: "toast",
		mgr:         polecat.NewManager(r, git.NewGit(rigPath), nil),
		r:           r,
	}
}

func TestCheckPolecatSafety_AssignedWork(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses Unix shell script bd stub")
	}
	tests := []struct {
		listMode    string
		wantBlocked bool
		wantReason  string
	}{
		{listMode: "none", wantBlocked: false},
		{listMode: "work", wantBlocked: true, wantReason: "gt-work"},
		{listMode: "fail", wantBlocked: true, wantReason: "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.listMode, func(t *testing.T) {
			installSafetyBDStub(t, tt.listMode)
			result := checkPolecatSafety(newSafetyTestTarget(t))
			if result.Blocked != tt.wantBlocked {
				t.Fatalf("Blocked = %v, want %v (reasons: %v)", result.Blocked, tt.wantBlocked, result.Reasons)
			}
			if tt.wantReason != "" && !strings.Contains(strings.Join(result.Reasons, "; "), tt.wantReason) {
				t.Errorf("reasons %v should mention %q", result.Reasons, tt.wantReason)
			}
		})
	}
}
