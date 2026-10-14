package refinery

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests lock in gt-pd1c.40: a polecat's `gt done --pre-verified` claim is
// informational only. The refinery cannot verify it, so it must never replace
// the refinery's own quality gates, even when the target has not moved since
// the claimed verification base.

// Gates run on the merged tree (gt-pd1c.45), so the failing cases below put a
// FAIL_MARKER file in the MR itself and use the tree-dependent gate
// failMarkerGateCmd. Before gt-pd1c.45 these tests had to use `exit 1` because
// pre-merge gates and the legacy test command ran on the bare target checkout
// and could not see the MR's files; the marker proves the gate saw the MR.

// preVerifiedFixture builds a repo with a feature branch and returns an MR that
// claims pre-verification against the current origin/main HEAD.
func preVerifiedFixture(t *testing.T, branch, file, content string, preVerified bool) (string, *Engineer, *MRInfo) {
	t.Helper()
	workDir, g, cleanup := testGitRepo(t)
	t.Cleanup(cleanup)
	installNoPRGH(t)

	createFeatureBranch(t, workDir, branch, file, content)
	commit := run(t, workDir, "git", "rev-parse", branch)
	targetHead := run(t, workDir, "git", "rev-parse", "origin/main")

	e := newTestEngineer(t, workDir, g)
	mr := &MRInfo{
		ID:        "mr-" + strings.ReplaceAll(branch, "/", "-"),
		Branch:    branch,
		Target:    "main",
		CommitSHA: commit,
	}
	if preVerified {
		mr.PreVerified = true
		mr.PreVerifiedBase = targetHead
	}
	return workDir, e, mr
}

func remoteMainHead(t *testing.T, workDir string) string {
	t.Helper()
	out := run(t, workDir, "git", "ls-remote", "origin", "refs/heads/main")
	fields := strings.Fields(out)
	if len(fields) == 0 {
		t.Fatalf("origin has no main branch: %q", out)
	}
	return fields[0]
}

// gateRecorderCmd returns a gate command that appends a line to a file outside
// the work tree each time it runs, plus the path to read the count from.
func gateRecorderCmd(t *testing.T) (cmd string, logPath string) {
	t.Helper()
	logPath = filepath.Join(t.TempDir(), "gate-runs.log")
	return fmt.Sprintf("echo ran >> %q", logPath), logPath
}

func gateRunCount(t *testing.T, logPath string) int {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatalf("read gate log: %v", err)
	}
	return strings.Count(string(data), "ran")
}

func TestProcessMRInfo_PreVerifiedUnchangedTarget_StillRunsPreMergeGates(t *testing.T) {
	workDir, e, mr := preVerifiedFixture(t, "polecat/test/pv-gate-fail", "FAIL_MARKER", "breaks the gate\n", true)
	e.config.Gates = map[string]*GateConfig{"check": {Cmd: failMarkerGateCmd()}}
	before := remoteMainHead(t, workDir)

	result := e.ProcessMRInfo(context.Background(), mr)

	if result.Success {
		t.Fatal("pre-verified MR merged although the refinery gate fails; the claim must not skip gates")
	}
	if !result.TestsFailed {
		t.Fatalf("expected TestsFailed for gate failure, got %+v", result)
	}
	if after := remoteMainHead(t, workDir); after != before {
		t.Fatalf("origin/main moved from %s to %s despite gate failure", before, after)
	}
}

func TestProcessMRInfo_PreVerifiedUnchangedTarget_StillRunsLegacyTestCommand(t *testing.T) {
	workDir, e, mr := preVerifiedFixture(t, "polecat/test/pv-legacy-fail", "FAIL_MARKER", "breaks the tests\n", true)
	e.config.Gates = nil
	e.config.RunTests = true
	e.config.TestCommand = failMarkerGateCmd()
	before := remoteMainHead(t, workDir)

	result := e.ProcessMRInfo(context.Background(), mr)

	if result.Success {
		t.Fatal("pre-verified MR merged although the refinery test command fails")
	}
	if !result.TestsFailed {
		t.Fatalf("expected TestsFailed for test command failure, got %+v", result)
	}
	if after := remoteMainHead(t, workDir); after != before {
		t.Fatalf("origin/main moved from %s to %s despite test failure", before, after)
	}
}

func TestProcessMRInfo_PreVerifiedUnchangedTarget_StillRunsPostSquashGates(t *testing.T) {
	workDir, e, mr := preVerifiedFixture(t, "polecat/test/pv-post-fail", "FAIL_MARKER", "breaks post-squash\n", true)
	e.config.Gates = map[string]*GateConfig{
		"post": {Cmd: failMarkerGateCmd(), Phase: GatePhasePostSquash},
	}
	before := remoteMainHead(t, workDir)

	result := e.ProcessMRInfo(context.Background(), mr)

	if result.Success {
		t.Fatal("pre-verified MR merged although the post-squash gate fails")
	}
	if !result.TestsFailed || !strings.Contains(result.Error, "post") {
		t.Fatalf("expected post-squash gate failure, got %+v", result)
	}
	if after := remoteMainHead(t, workDir); after != before {
		t.Fatalf("origin/main moved from %s to %s despite post-squash gate failure", before, after)
	}
}

func TestProcessMRInfo_PreVerifiedUnchangedTarget_PassingGateRunsThenMerges(t *testing.T) {
	workDir, e, mr := preVerifiedFixture(t, "polecat/test/pv-gate-pass", "ok.txt", "fine\n", true)
	cmd, logPath := gateRecorderCmd(t)
	e.config.Gates = map[string]*GateConfig{"record": {Cmd: cmd}}
	before := remoteMainHead(t, workDir)

	result := e.ProcessMRInfo(context.Background(), mr)

	if !result.Success {
		t.Fatalf("ProcessMRInfo failed: %s", result.Error)
	}
	if got := gateRunCount(t, logPath); got != 1 {
		t.Fatalf("gate ran %d times for pre-verified MR, want 1", got)
	}
	if after := remoteMainHead(t, workDir); after == before {
		t.Fatal("origin/main did not advance after a passing merge")
	}
}

// A failing gate must be reported identically whether or not the MR carries a
// pre-verified claim, so the MR is rejected/returned exactly like any other MR.
func TestProcessMRInfo_GateFailureSameWithAndWithoutPreVerifiedClaim(t *testing.T) {
	outcome := func(preVerified bool) ProcessResult {
		_, e, mr := preVerifiedFixture(t, "polecat/test/pv-compare", "FAIL_MARKER", "breaks the gate\n", preVerified)
		e.config.Gates = map[string]*GateConfig{"check": {Cmd: failMarkerGateCmd()}}
		return e.ProcessMRInfo(context.Background(), mr)
	}

	plain := outcome(false)
	claimed := outcome(true)

	if plain.Success || claimed.Success {
		t.Fatalf("expected both to fail: plain=%+v claimed=%+v", plain, claimed)
	}
	if plain.TestsFailed != claimed.TestsFailed || plain.Conflict != claimed.Conflict ||
		plain.NoMerge != claimed.NoMerge || plain.Error != claimed.Error {
		t.Fatalf("pre-verified claim changed the failure outcome:\nplain=  %+v\nclaimed=%+v", plain, claimed)
	}
}

func TestProcessMRInfo_NotPreVerified_PassingGateUnchanged(t *testing.T) {
	_, e, mr := preVerifiedFixture(t, "polecat/test/plain-pass", "ok.txt", "fine\n", false)
	cmd, logPath := gateRecorderCmd(t)
	e.config.Gates = map[string]*GateConfig{"record": {Cmd: cmd}}

	result := e.ProcessMRInfo(context.Background(), mr)

	if !result.Success {
		t.Fatalf("ProcessMRInfo failed: %s", result.Error)
	}
	if got := gateRunCount(t, logPath); got != 1 {
		t.Fatalf("gate ran %d times, want 1", got)
	}
}

func TestProcessBatch_SinglePreVerifiedMR_StillRunsGates(t *testing.T) {
	workDir, e, mr := preVerifiedFixture(t, "polecat/test/pv-batch-one", "FAIL_MARKER", "breaks the gate\n", true)
	e.config.Gates = map[string]*GateConfig{"check": {Cmd: failMarkerGateCmd()}}
	before := remoteMainHead(t, workDir)

	result := e.ProcessBatch(context.Background(), []*MRInfo{mr}, "main", nil)

	if len(result.Merged) != 0 {
		t.Fatalf("pre-verified MR merged in single-MR batch despite failing gate: %v", stackedIDs(result.Merged))
	}
	if result.Error != nil {
		t.Fatalf("unexpected batch error (gate should have been the failure): %v", result.Error)
	}
	if len(result.Culprits) != 1 || result.Culprits[0].ID != mr.ID {
		t.Fatalf("expected %s as gate culprit, got %v", mr.ID, stackedIDs(result.Culprits))
	}
	if after := remoteMainHead(t, workDir); after != before {
		t.Fatalf("origin/main moved from %s to %s despite gate failure", before, after)
	}
}

func TestProcessBatch_PreVerifiedMRs_StillRunGatesAndBisect(t *testing.T) {
	workDir, g, cleanup := testGitRepo(t)
	defer cleanup()

	createFeatureBranch(t, workDir, "feature-a", "a.txt", "hello a\n")
	createFeatureBranch(t, workDir, "feature-b", "FAIL_MARKER", "this causes gate failure\n")
	base := run(t, workDir, "git", "rev-parse", "origin/main")

	e := newTestEngineer(t, workDir, g)
	e.config.Gates = map[string]*GateConfig{"check": {Cmd: failMarkerGateCmd()}}
	e.config.GatesParallel = false

	batch := []*MRInfo{makeMR("mr-a", "feature-a", "main"), makeMR("mr-b", "feature-b", "main")}
	for _, mr := range batch {
		mr.PreVerified = true
		mr.PreVerifiedBase = base
	}

	result := e.ProcessBatch(context.Background(), batch, "main", &BatchConfig{MaxBatchSize: 5})

	if len(result.Culprits) != 1 || result.Culprits[0].ID != "mr-b" {
		t.Fatalf("expected culprit mr-b, got %v", stackedIDs(result.Culprits))
	}
	for _, mr := range result.Merged {
		if mr.ID == "mr-b" {
			t.Fatal("pre-verified culprit mr-b was merged")
		}
	}
}

// The claim must not change anything when the base is stale, empty, or shorter
// than the 8-character log prefix (done.go records no base when it cannot
// resolve the target ref).
func TestProcessMRInfo_PreVerifiedOddBases_StillRunGatesWithoutPanic(t *testing.T) {
	for _, tc := range []struct{ name, base string }{
		{"stale", "0123456789abcdef0123456789abcdef01234567"},
		{"empty", ""},
		{"short", "abc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workDir, e, mr := preVerifiedFixture(t, "polecat/test/pv-odd-"+tc.name, "FAIL_MARKER", "breaks the gate\n", true)
			mr.PreVerifiedBase = tc.base
			e.config.Gates = map[string]*GateConfig{"check": {Cmd: failMarkerGateCmd()}}
			before := remoteMainHead(t, workDir)

			result := e.ProcessMRInfo(context.Background(), mr)

			if result.Success || !result.TestsFailed {
				t.Fatalf("expected gate failure, got %+v", result)
			}
			if after := remoteMainHead(t, workDir); after != before {
				t.Fatalf("origin/main moved from %s to %s despite gate failure", before, after)
			}
		})
	}
}

// With no gates and no test command configured, a pre-verified MR merges just
// like any other MR: removing the fast path must not be what blocks it.
func TestProcessMRInfo_PreVerifiedNoGatesConfigured_Merges(t *testing.T) {
	workDir, e, mr := preVerifiedFixture(t, "polecat/test/pv-no-gates", "ok.txt", "fine\n", true)
	e.config.Gates = nil
	e.config.RunTests = false
	before := remoteMainHead(t, workDir)

	result := e.ProcessMRInfo(context.Background(), mr)

	if !result.Success {
		t.Fatalf("ProcessMRInfo failed: %s", result.Error)
	}
	if after := remoteMainHead(t, workDir); after == before {
		t.Fatal("origin/main did not advance")
	}
}
