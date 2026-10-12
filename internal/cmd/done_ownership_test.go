package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
)

func TestCheckDoneOwnership(t *testing.T) {
	const me = "gastown/polecats/refuge"
	tests := []struct {
		name      string
		issue     *beads.Issue
		lookupErr error
		wantErr   string
	}{
		{name: "assigned to me", issue: &beads.Issue{ID: "gt-1", Assignee: me, Status: "hooked"}},
		{name: "assigned to me, in progress after resume", issue: &beads.Issue{ID: "gt-1", Assignee: me, Status: "in_progress"}},
		{name: "unassigned is allowed", issue: &beads.Issue{ID: "gt-1", Status: "open"}},
		{name: "assigned to another agent", issue: &beads.Issue{ID: "gt-1", Assignee: "gastown/polecats/nux"}, wantErr: "assigned to gastown/polecats/nux"},
		{name: "lookup failed", lookupErr: errors.New("dolt: connection refused"), wantErr: "cannot verify ownership"},
		{name: "issue missing", wantErr: "cannot verify ownership"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkDoneOwnership("gt-1", tt.issue, tt.lookupErr, me)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

// installOwnershipBDStub answers "show bd-source" with the given assignee (or
// fails when failShow is set) and logs every bd invocation.
func installOwnershipBDStub(t *testing.T, assignee string, failShow bool) string {
	t.Helper()
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "bd.log")
	show := fmt.Sprintf(`  echo '[{"id":"bd-source","title":"owner source","status":"hooked","assignee":"%s","priority":1,"issue_type":"task"}]'
  exit 0`, assignee)
	if failShow {
		show = `  echo "database is locked" >&2
  exit 1`
	}
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "--allow-stale" ]; then
  shift
fi
if [ "$1" = "version" ]; then
  echo "bd stub"
  exit 0
fi
printf '%%s\n' "$*" >> %q
if [ "$1" = "show" ] && [ "$2" = "bd-source" ]; then
%s
fi
echo '[]'
exit 0
`, logPath, show)
	if err := os.WriteFile(filepath.Join(binDir, "bd"), []byte(script), 0o755); err != nil {
		t.Fatalf("write bd stub: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	beads.ResetBdAllowStaleCacheForTest()
	return logPath
}

func runDoneForOwnershipTest(t *testing.T, assignee string, failShow bool) (error, string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses Unix shell script bd stub")
	}
	workDir, _, _ := setupRoutedSourceTestTown(t)
	setupRoutedSubmitCommandTown(t, workDir)
	branch := setupRoutedSubmitGitRepo(t, workDir, false)
	logPath := installOwnershipBDStub(t, assignee, failShow)
	resetDoneFlagsForTest(t)
	townRoot := routedSourceTestTownRoot(workDir)
	t.Setenv("GT_TEST_NUDGE_LOG", filepath.Join(t.TempDir(), "nudge.log"))
	t.Setenv("GT_TOWN_ROOT", townRoot)
	t.Setenv("GT_ROOT", townRoot)
	t.Setenv("GT_ROLE", "gastown/polecats/refuge")
	t.Setenv("GT_RIG", "gastown")
	t.Setenv("GT_POLECAT", "refuge")
	t.Setenv("BD_ACTOR", "gastown/polecats/refuge")
	t.Chdir(workDir)

	doneIssue = "bd-source"
	doneCleanupStatus = "unpushed"
	doneSkipVerify = true
	updateAgentStateOnDoneFn = func(cwd, townRoot, exitType, issueID string) error { return nil }
	err := runDone(nil, nil)

	logData, _ := os.ReadFile(logPath)
	remote, lsErr := exec.Command("git", "-C", workDir, "ls-remote", "origin", branch).Output()
	if lsErr != nil {
		t.Fatalf("git ls-remote: %v", lsErr)
	}
	return err, string(logData), strings.TrimSpace(string(remote))
}

func TestRunDone_NotAssignee_NoPushNoMR(t *testing.T) {
	err, bdLog, remoteRef := runDoneForOwnershipTest(t, "gastown/polecats/nux", false)
	if err == nil || !strings.Contains(err.Error(), "assigned to gastown/polecats/nux") {
		t.Fatalf("runDone error = %v, want ownership rejection", err)
	}
	if strings.Contains(bdLog, "gt:merge-request") {
		t.Errorf("no MR bead may be created, bd log:\n%s", bdLog)
	}
	if remoteRef != "" {
		t.Errorf("branch must not be pushed, remote has: %s", remoteRef)
	}
}

func TestRunDone_OwnershipLookupError_NoPushNoMR(t *testing.T) {
	err, bdLog, remoteRef := runDoneForOwnershipTest(t, "", true)
	if err == nil || !strings.Contains(err.Error(), "cannot verify ownership") {
		t.Fatalf("runDone error = %v, want ownership unknown rejection", err)
	}
	if strings.Contains(bdLog, "gt:merge-request") {
		t.Errorf("no MR bead may be created, bd log:\n%s", bdLog)
	}
	if remoteRef != "" {
		t.Errorf("branch must not be pushed, remote has: %s", remoteRef)
	}
}

func TestQueryAssignedBeadsE_ErrorNotMaskedByEmpty(t *testing.T) {
	issues, err := mergeAssignedLookups(nil, errors.New("hooked query failed"), []*beads.Issue{}, nil)
	if err == nil {
		t.Fatalf("expected error when the hooked query failed and in_progress was empty, got issues=%v", issues)
	}
	found := []*beads.Issue{{ID: "gt-1"}}
	issues, err = mergeAssignedLookups(nil, errors.New("hooked query failed"), found, nil)
	if err != nil || len(issues) != 1 {
		t.Fatalf("work found by another query must win: issues=%v err=%v", issues, err)
	}
	issues, err = mergeAssignedLookups([]*beads.Issue{}, nil, []*beads.Issue{}, nil)
	if err != nil || len(issues) != 0 {
		t.Fatalf("both empty: issues=%v err=%v", issues, err)
	}
}
