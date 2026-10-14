package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
)

func formulaVarsSource(vars ...string) *beads.Issue {
	quoted := make([]string, len(vars))
	for i, v := range vars {
		quoted[i] = fmt.Sprintf("%q", v)
	}
	return &beads.Issue{ID: "gt-src", Description: "formula_vars: [" + strings.Join(quoted, ",") + "]\n"}
}

func TestResolveDoneTargetPriority(t *testing.T) {
	detect := func(branch string) (func() string, *int) {
		calls := 0
		return func() string { calls++; return branch }, &calls
	}
	withBase := formulaVarsSource("issue=gt-src", "base_branch=release/v2")

	tests := []struct {
		name        string
		explicit    string
		source      *beads.Issue
		integration string
		disabled    bool
		want        string
		wantOrigin  doneTargetOrigin
		wantDetect  bool
	}{
		{"explicit beats everything", "integration/kp-iwm", withBase, "integration/other", false, "integration/kp-iwm", doneTargetFromExplicit, false},
		{"formula_vars beats integration", "", withBase, "integration/other", false, "release/v2", doneTargetFromFormulaVars, false},
		{"formula_vars equal to default is ignored", "", formulaVarsSource("base_branch=main"), "integration/epic", false, "integration/epic", doneTargetFromIntegration, true},
		{"integration branch of parent epic", "", nil, "integration/epic", false, "integration/epic", doneTargetFromIntegration, true},
		{"integration detection disabled", "", nil, "integration/epic", true, "main", doneTargetFromDefault, false},
		{"nothing found falls back to default", "", nil, "", false, "main", doneTargetFromDefault, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn, calls := detect(tt.integration)
			if tt.disabled {
				fn = nil
			}
			got, origin := resolveDoneTarget("main", tt.explicit, tt.source, fn)
			if got != tt.want || origin != tt.wantOrigin {
				t.Fatalf("resolveDoneTarget = (%q, %v), want (%q, %v)", got, origin, tt.want, tt.wantOrigin)
			}
			if (*calls > 0) != tt.wantDetect {
				t.Fatalf("integration detection called %d times, want called=%v", *calls, tt.wantDetect)
			}
		})
	}
}

// The kp-iwm.3 bead stored base_branch twice: the rig default first, the
// epic's integration branch (from --base-branch) after it. The later,
// dispatch-specific value must win.
func TestExtractFormulaVarLastAssignmentWins(t *testing.T) {
	vars := "feature=x\nbase_branch=main\nmerge_strategy=direct\nbase_branch=integration/kp-iwm"
	if got := extractFormulaVar(vars, "base_branch"); got != "integration/kp-iwm" {
		t.Fatalf("extractFormulaVar = %q, want integration/kp-iwm", got)
	}
	if got := extractFormulaVar(vars, "missing"); got != "" {
		t.Fatalf("extractFormulaVar(missing) = %q, want empty", got)
	}
	src := formulaVarsSource("base_branch=main", "base_branch=integration/kp-iwm")
	if got, origin := resolveDoneTarget("main", "", src, nil); got != "integration/kp-iwm" || origin != doneTargetFromFormulaVars {
		t.Fatalf("resolveDoneTarget(kp-iwm.3 vars) = (%q, %v), want integration/kp-iwm from formula_vars", got, origin)
	}
}

func TestRigIntegrationDetectorRespectsSettings(t *testing.T) {
	townRoot := t.TempDir()
	writeRigSettingsJSON(t, townRoot, "gastown", `{"type":"rig-settings","version":1,"merge_queue":{"enabled":true,"integration_branch_refinery_enabled":false}}`)
	if fn := rigIntegrationDetector(townRoot, "gastown", beads.New(townRoot), nil, "gt-1", nil); fn != nil {
		t.Fatal("detector returned although integration_branch_refinery_enabled=false")
	}
	if fn := rigIntegrationDetector(townRoot, "gastown", nil, nil, "gt-1", nil); fn != nil {
		t.Fatal("detector returned without a source beads handle")
	}
}

// ---------------------------------------------------------------------------
// runDone end-to-end: convoy strategy "direct" (reproduces kp-iwm.3)
// ---------------------------------------------------------------------------

// installDoneSourceRecorder installs a bd stub that serves bd-source with the
// given description from the owner rig and records every call.
func installDoneSourceRecorder(t *testing.T, currentBeadsDir, ownerBeadsDir, description string) string {
	t.Helper()
	dataDir := t.TempDir()
	source, err := json.Marshal([]map[string]any{{
		"id": "bd-source", "title": "owner source", "status": "open", "priority": 1,
		"issue_type": "task", "description": description,
	}})
	if err != nil {
		t.Fatalf("marshal source: %v", err)
	}
	sourcePath := filepath.Join(dataDir, "source.json")
	if err := os.WriteFile(sourcePath, source, 0o644); err != nil {
		t.Fatalf("write source json: %v", err)
	}
	logPath := filepath.Join(dataDir, "bd.log")
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "--allow-stale" ]; then
  shift
fi
if [ "$1" = "version" ]; then
  echo "bd stub"
  exit 0
fi
printf '%%s\t%%s\n' "$BEADS_DIR" "$*" >> %q
if [ "$1" = "show" ] && [ "$2" = "bd-source" ]; then
  if [ "$BEADS_DIR" = %q ] || [ "$BEADS_DIR" = %q ]; then
    cat %q
    exit 0
  fi
  echo "Issue not found in $BEADS_DIR" >&2
  exit 1
fi
if [ "$1" = "show" ] && [ "$2" = "gt-mr" ]; then
  echo '[{"id":"gt-mr","title":"Merge: bd-source","status":"open","priority":1,"issue_type":"task","labels":["gt:merge-request"],"description":"branch: feature/routed-submit\ntarget: integration/kp-iwm\nsource_issue: bd-source\nrig: gastown"}]'
  exit 0
fi
case "$1" in
  list|sql) echo '[]'; exit 0 ;;
  create) echo '{"id":"gt-mr","title":"Merge: bd-source","status":"open","priority":1,"issue_type":"task","labels":["gt:merge-request"]}'; exit 0 ;;
  comments|close|update) exit 0 ;;
esac
echo "unexpected bd command: $*" >&2
exit 1
`, logPath, currentBeadsDir, ownerBeadsDir, sourcePath)
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "bd"), []byte(script), 0o755); err != nil {
		t.Fatalf("write bd recorder: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	beads.ResetBdAllowStaleCacheForTest()
	t.Cleanup(beads.ResetBdAllowStaleCacheForTest)
	return logPath
}

type doneDirectFixture struct {
	workDir string
	remote  string
	branch  string
	logPath string
}

// setupDoneDirectFixture builds a polecat worktree whose source bead belongs to
// a convoy with merge strategy "direct", on a rig with the given settings.
// The remote already has main and integration/kp-iwm.
func setupDoneDirectFixture(t *testing.T, rigSettings, sourceDescription string) doneDirectFixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell bd stub")
	}
	workDir, currentBeadsDir, ownerBeadsDir := setupRoutedSourceTestTown(t)
	setupRoutedSubmitCommandTown(t, workDir)
	branch := setupRoutedSubmitGitRepo(t, workDir, false)
	townRoot := routedSourceTestTownRoot(workDir)
	writeRigSettingsJSON(t, townRoot, "gastown", rigSettings)

	remote := gitOutputForDoneTest(t, workDir, "remote", "get-url", "origin")
	runGitForMQSubmitTest(t, workDir, "push", "origin", "main:refs/heads/integration/kp-iwm")

	logPath := installDoneSourceRecorder(t, currentBeadsDir, ownerBeadsDir, sourceDescription)
	resetDoneFlagsForTest(t)
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
	return doneDirectFixture{workDir: workDir, remote: remote, branch: branch, logPath: logPath}
}

func gitOutputForDoneTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out))
}

func remoteRef(t *testing.T, remote, ref string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", ref)
	cmd.Dir = remote
	out, _ := cmd.Output()
	return strings.TrimSpace(string(out))
}

const directConvoySource = "convoy_id: hq-cv-s735e\nmerge_strategy: direct\n" +
	`formula_vars: ["issue=bd-source","base_branch=main","merge_strategy=direct","base_branch=integration/kp-iwm"]` + "\n"

// Reproduces kp-iwm.3: an epic child dispatched with --merge=direct on a rig
// whose merge queue is active. gt done must submit an MR to the epic's
// integration branch and never push to main.
func TestRunDoneDirectConvoyOnMergeQueueRigSubmitsMRToTarget(t *testing.T) {
	fx := setupDoneDirectFixture(t, settingsMQEnabled, directConvoySource)
	mainBefore := remoteRef(t, fx.remote, "refs/heads/main")
	integrationBefore := remoteRef(t, fx.remote, "refs/heads/integration/kp-iwm")
	doneTarget = "integration/kp-iwm"
	donePreVerified = true

	if err := runDone(nil, nil); err != nil {
		t.Fatalf("runDone: %v", err)
	}

	if got := remoteRef(t, fx.remote, "refs/heads/main"); got != mainBefore {
		t.Fatalf("remote main moved %s -> %s: direct push bypassed the merge queue", mainBefore, got)
	}
	if got := remoteRef(t, fx.remote, "refs/heads/integration/kp-iwm"); got != integrationBefore {
		t.Fatalf("remote integration/kp-iwm moved %s -> %s: direct push bypassed the merge queue", integrationBefore, got)
	}
	head := gitOutputForDoneTest(t, fx.workDir, "rev-parse", "HEAD")
	if got := remoteRef(t, fx.remote, "refs/heads/"+fx.branch); got != head {
		t.Fatalf("feature branch on remote = %q, want %q (MR source must be pushed)", got, head)
	}
	log := readSubmitSourceBDLog(t, fx.logPath)
	if !strings.Contains(log, "--title=Merge: bd-source") || !strings.Contains(log, "target: integration/kp-iwm") {
		t.Fatalf("MR bead to integration/kp-iwm not created; bd log:\n%s", log)
	}
	if strings.Contains(log, "close bd-source") || strings.Contains(log, "Direct merge to") {
		t.Fatalf("source closed as a direct merge; bd log:\n%s", log)
	}
}

// Same epic child, but the polecat did not pass --target: the resolver must
// still find integration/kp-iwm (from formula_vars), as the MR path does.
func TestRunDoneDirectConvoyOnMergeQueueRigUsesResolvedTargetWithoutFlag(t *testing.T) {
	fx := setupDoneDirectFixture(t, settingsMQEnabled, directConvoySource)
	mainBefore := remoteRef(t, fx.remote, "refs/heads/main")

	if err := runDone(nil, nil); err != nil {
		t.Fatalf("runDone: %v", err)
	}
	if got := remoteRef(t, fx.remote, "refs/heads/main"); got != mainBefore {
		t.Fatalf("remote main moved: %s -> %s", mainBefore, got)
	}
	log := readSubmitSourceBDLog(t, fx.logPath)
	if !strings.Contains(log, "target: integration/kp-iwm") {
		t.Fatalf("MR target is not integration/kp-iwm; bd log:\n%s", log)
	}
}

// On a rig without a merge queue, direct is still legitimate, but it must land
// on the resolved target (here --target), never blindly on the default branch.
func TestRunDoneDirectConvoyWithoutMergeQueuePushesToResolvedTarget(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target string
	}{
		{"explicit --target", "integration/kp-iwm"},
		{"formula_vars base_branch", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := setupDoneDirectFixture(t, settingsMQDisabled, directConvoySource)
			mainBefore := remoteRef(t, fx.remote, "refs/heads/main")
			doneTarget = tc.target

			if err := runDone(nil, nil); err != nil {
				t.Fatalf("runDone: %v", err)
			}
			head := gitOutputForDoneTest(t, fx.workDir, "rev-parse", "HEAD")
			if got := remoteRef(t, fx.remote, "refs/heads/integration/kp-iwm"); got != head {
				t.Fatalf("integration/kp-iwm on remote = %q, want pushed HEAD %q", got, head)
			}
			if got := remoteRef(t, fx.remote, "refs/heads/main"); got != mainBefore {
				t.Fatalf("remote main moved %s -> %s; direct must use the resolved target", mainBefore, got)
			}
			log := readSubmitSourceBDLog(t, fx.logPath)
			if !strings.Contains(log, "Direct merge to integration/kp-iwm (convoy strategy)") {
				t.Fatalf("source not closed as direct merge to integration/kp-iwm; bd log:\n%s", log)
			}
			if strings.Contains(log, "--title=Merge: bd-source") {
				t.Fatalf("MR bead created for a direct merge; bd log:\n%s", log)
			}
		})
	}
}

// Without --target, the rebase/contamination base must be the resolved target
// too. Otherwise gt done rebases the branch onto a main that moved on and the
// direct push drags main's new commits into the integration branch.
func TestRunDoneDirectWithoutTargetFlagRebasesOntoResolvedTarget(t *testing.T) {
	fx := setupDoneDirectFixture(t, settingsMQDisabled, directConvoySource)

	// main moves on after the polecat branched (and after integration/kp-iwm was cut).
	runGitForMQSubmitTest(t, fx.workDir, "checkout", "main")
	writeMQSubmitTestFile(t, fx.workDir, "main-only.txt", "main moved\n")
	runGitForMQSubmitTest(t, fx.workDir, "add", "main-only.txt")
	runGitForMQSubmitTest(t, fx.workDir, "commit", "-m", "main moved")
	runGitForMQSubmitTest(t, fx.workDir, "push", "origin", "main")
	mainOnly := gitOutputForDoneTest(t, fx.workDir, "rev-parse", "HEAD")
	runGitForMQSubmitTest(t, fx.workDir, "checkout", fx.branch)

	if err := runDone(nil, nil); err != nil {
		t.Fatalf("runDone: %v", err)
	}
	head := gitOutputForDoneTest(t, fx.workDir, "rev-parse", "HEAD")
	if got := remoteRef(t, fx.remote, "refs/heads/integration/kp-iwm"); got != head {
		t.Fatalf("integration/kp-iwm on remote = %q, want pushed HEAD %q", got, head)
	}
	cmd := exec.Command("git", "merge-base", "--is-ancestor", mainOnly, "refs/heads/integration/kp-iwm")
	cmd.Dir = fx.remote
	if cmd.Run() == nil {
		t.Fatalf("main-only commit %s leaked into integration/kp-iwm: branch was rebased onto main instead of the resolved target", mainOnly)
	}
}
