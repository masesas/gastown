package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// doneHandoffTown builds a town whose bd stub serves a polecat agent bead, a
// hooked source bead gt-base-123 (attached molecule gt-wisp-xyz with one open
// step) and logs every close/update invocation. It returns the rig path, the
// town root and the call log path.
func doneHandoffTown(t *testing.T, sourceLabels string) (string, string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell script bd stub not supported on Windows")
	}
	townRoot := t.TempDir()
	for _, dir := range []string{"mayor", ".beads/locks", "gastown", "bin"} {
		if err := os.MkdirAll(filepath.Join(townRoot, dir), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(townRoot, ".beads", "routes.jsonl"),
		[]byte(`{"prefix":"gt-","path":"gastown"}`+"\n"), 0644); err != nil {
		t.Fatalf("write routes.jsonl: %v", err)
	}
	callsLog := filepath.Join(townRoot, "calls.log")
	script := fmt.Sprintf(`#!/bin/sh
while [ "$1" = "--allow-stale" ]; do shift; done
cmd="$1"
shift || true
case "$cmd" in
  show)
    case "$1" in
      gt-gastown-polecat-nux)
        echo '[{"id":"gt-gastown-polecat-nux","title":"Polecat nux","status":"open","agent_state":"working"}]' ;;
      gt-base-123)
        echo '[{"id":"gt-base-123","title":"Base bead","status":"hooked","assignee":"gastown/polecats/nux","labels":[%s],"description":"attached_molecule: gt-wisp-xyz"}]' ;;
      gt-wisp-xyz)
        echo '[{"id":"gt-wisp-xyz","title":"mol-polecat-work","status":"open","ephemeral":true}]' ;;
    esac
    ;;
  list)
    if echo "$*" | grep -q "parent=gt-wisp-xyz"; then
      echo '[{"id":"gt-step-1","title":"Step 1","status":"open"}]'
    else
      echo '[]'
    fi
    ;;
  close|update)
    echo "$cmd $*" >> "%s"
    ;;
esac
exit 0
`, sourceLabels, callsLog)
	if err := os.WriteFile(filepath.Join(townRoot, "bin", "bd"), []byte(script), 0755); err != nil {
		t.Fatalf("write bd stub: %v", err)
	}
	t.Setenv("PATH", filepath.Join(townRoot, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GT_ROLE", "polecat")
	t.Setenv("GT_RIG", "gastown")
	t.Setenv("GT_POLECAT", "nux")
	t.Setenv("GT_CREW", "")
	t.Setenv("TMUX_PANE", "")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	rigPath := filepath.Join(townRoot, "gastown")
	if err := os.Chdir(rigPath); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	return rigPath, townRoot, callsLog
}

func readDoneCalls(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read calls: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

// gt-pd1c.59: on the MR path the refinery owns the source bead and closes it
// when the MR merges, so closed_at is the merge time. gt done closes only the
// polecat's molecule and parks the source bead (in_progress, no assignee).
func TestUpdateAgentStateOnDone_MRPathParksSourceInsteadOfClosing(t *testing.T) {
	rigPath, townRoot, callsLog := doneHandoffTown(t, "")

	if err := updateAgentStateOnDone(rigPath, townRoot, ExitCompleted, "gt-base-123", "gt-wisp-mr1"); err != nil {
		t.Fatalf("updateAgentStateOnDone: %v", err)
	}

	calls := readDoneCalls(t, callsLog)
	var closedStep, closedWisp, parked bool
	for _, call := range calls {
		switch {
		case strings.HasPrefix(call, "close") && strings.Contains(call, "gt-base-123"):
			t.Errorf("source bead must not be closed by gt done on the MR path: %s", call)
		case strings.HasPrefix(call, "close") && strings.Contains(call, "gt-step-1"):
			closedStep = true
		case strings.HasPrefix(call, "close") && strings.Contains(call, "gt-wisp-xyz"):
			closedWisp = true
		case strings.HasPrefix(call, "update gt-base-123") &&
			strings.Contains(call, "--status=in_progress") && strings.Contains(call, "--assignee="):
			parked = true
		case strings.HasPrefix(call, "update gt-base-123") && strings.Contains(call, "--status=open"):
			t.Errorf("source bead must not be made dispatchable while its MR is pending: %s", call)
		}
	}
	if !closedStep || !closedWisp {
		t.Errorf("polecat molecule must still be closed at done (step=%v wisp=%v), calls:\n%s", closedStep, closedWisp, strings.Join(calls, "\n"))
	}
	if !parked {
		t.Errorf("source bead must be parked for the merge queue, calls:\n%s", strings.Join(calls, "\n"))
	}
}

// Without an MR (no-MR completion) gt done still closes the source bead.
func TestUpdateAgentStateOnDone_NoMRStillClosesSource(t *testing.T) {
	rigPath, townRoot, callsLog := doneHandoffTown(t, "")

	if err := updateAgentStateOnDone(rigPath, townRoot, ExitCompleted, "gt-base-123", ""); err != nil {
		t.Fatalf("updateAgentStateOnDone: %v", err)
	}
	var closedBase bool
	for _, call := range readDoneCalls(t, callsLog) {
		if strings.HasPrefix(call, "close") && strings.Contains(call, "gt-base-123") {
			closedBase = true
		}
		if strings.Contains(call, "--status=in_progress") {
			t.Errorf("no-MR completion must not park the source: %s", call)
		}
	}
	if !closedBase {
		t.Error("no-MR completion must close the source bead")
	}
}

// A rig identity bead used as a hook is never closed and never parked.
func TestUpdateAgentStateOnDone_MRPathNeverParksRigBead(t *testing.T) {
	rigPath, townRoot, callsLog := doneHandoffTown(t, `"gt:rig"`)

	if err := updateAgentStateOnDone(rigPath, townRoot, ExitCompleted, "gt-base-123", "gt-wisp-mr1"); err != nil {
		t.Fatalf("updateAgentStateOnDone: %v", err)
	}
	for _, call := range readDoneCalls(t, callsLog) {
		if strings.Contains(call, "gt-base-123") {
			t.Errorf("rig identity bead must not be touched: %s", call)
		}
	}
}

func TestUpdateAgentStateAfterSubmission_PassesMRToHandoff(t *testing.T) {
	old := updateAgentStateOnDoneFn
	t.Cleanup(func() { updateAgentStateOnDoneFn = old })
	var gotMR string
	updateAgentStateOnDoneFn = func(cwd, townRoot, exitType, issueID, mrID string) error {
		gotMR = mrID
		return nil
	}
	if err := updateAgentStateAfterSubmission("/work", "/town", ExitCompleted, "gt-abc", "gt-wisp-mr1", false, false); err != nil {
		t.Fatalf("updateAgentStateAfterSubmission: %v", err)
	}
	if gotMR != "gt-wisp-mr1" {
		t.Fatalf("mrID = %q, want gt-wisp-mr1", gotMR)
	}
}
