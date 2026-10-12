package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// setupReadinessSlingTown creates a town with a rig whose readiness mode is
// set, and a bd stub that serves a bead with the given type/description/AC and
// logs every invocation to the returned log path.
func setupReadinessSlingTown(t *testing.T, mode, issueType, desc, ac string) (townRoot, logPath string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}
	townRoot = t.TempDir()
	if err := os.MkdirAll(filepath.Join(townRoot, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	settingsDir := filepath.Join(townRoot, "testrig", "settings")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf(`{"type":"rig-settings","version":1,"readiness":%q}`, mode)
	if err := os.WriteFile(filepath.Join(settingsDir, "config.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	binDir := filepath.Join(townRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath = filepath.Join(townRoot, "bd.log")
	bdScript := fmt.Sprintf(`#!/bin/sh
echo "$@" >> %q
case "$1" in
  show)
    echo '[{"title":"Work","status":"open","assignee":"","issue_type":%q,"description":%q,"acceptance_criteria":%q}]'
    ;;
esac
exit 0
`, logPath, issueType, desc, ac)
	writeBDStub(t, binDir, bdScript, "")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return townRoot, logPath
}

func readBDLog(t *testing.T, logPath string) string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(data)
}

func TestExecuteSling_NotReady_Enforce_Rejected_NoStateChange(t *testing.T) {
	townRoot, logPath := setupReadinessSlingTown(t, "enforce", "feature", "has a description", "")

	result, err := executeSling(SlingParams{BeadID: "tr-notready1", RigName: "testrig", TownRoot: townRoot})
	if err == nil {
		t.Fatal("expected rejection in enforce mode")
	}
	if !strings.Contains(err.Error(), "missing_acceptance_criteria") {
		t.Errorf("error should name the problem: %v", err)
	}
	if result == nil || result.ErrMsg != "not_ready" {
		t.Errorf("ErrMsg = %+v, want not_ready", result)
	}
	log := readBDLog(t, logPath)
	for _, forbidden := range []string{"update", "comments", "create"} {
		if strings.Contains(log, forbidden+" ") {
			t.Errorf("bead state must not change on rejection; bd log has %q:\n%s", forbidden, log)
		}
	}
}

func TestExecuteSling_NotReady_Warn_Dispatched(t *testing.T) {
	townRoot, _ := setupReadinessSlingTown(t, "warn", "feature", "has a description", "")

	_, err := executeSling(SlingParams{BeadID: "tr-warn1", RigName: "testrig", TownRoot: townRoot})
	// Dispatch proceeds past the readiness check; it may fail later in this
	// stubbed town, but never because of readiness.
	if err != nil && (strings.Contains(err.Error(), "not ready") || strings.Contains(err.Error(), "missing_acceptance_criteria")) {
		t.Fatalf("warn mode must not reject: %v", err)
	}
}

func TestExecuteSling_ForceUnready_RecordsComment(t *testing.T) {
	townRoot, logPath := setupReadinessSlingTown(t, "enforce", "task", "", "")

	_, err := executeSling(SlingParams{
		BeadID: "tr-force1", RigName: "testrig", TownRoot: townRoot,
		ForceUnready: true, UnreadyReason: "prod hotfix",
	})
	if err != nil && strings.Contains(err.Error(), "not ready") {
		t.Fatalf("override must pass the readiness check: %v", err)
	}
	log := readBDLog(t, logPath)
	if !strings.Contains(log, "comments add tr-force1 readiness override by ") || !strings.Contains(log, ": prod hotfix") {
		t.Errorf("override comment not recorded; bd log:\n%s", log)
	}
}

func TestExecuteSling_ForceUnready_WithoutReason_Rejected(t *testing.T) {
	townRoot, _ := setupReadinessSlingTown(t, "off", "task", "", "")

	_, err := executeSling(SlingParams{BeadID: "tr-force2", RigName: "testrig", TownRoot: townRoot, ForceUnready: true})
	if err == nil || !strings.Contains(err.Error(), "--reason") {
		t.Fatalf("want --reason error, got %v", err)
	}
}

func TestExecuteSling_EpicType_NotChecked(t *testing.T) {
	townRoot, _ := setupReadinessSlingTown(t, "enforce", "epic", "", "")

	_, err := executeSling(SlingParams{BeadID: "tr-epic1", RigName: "testrig", TownRoot: townRoot})
	if err != nil && (strings.Contains(err.Error(), "not ready") || strings.Contains(err.Error(), "missing_")) {
		t.Fatalf("epic must not be readiness-checked: %v", err)
	}
}
