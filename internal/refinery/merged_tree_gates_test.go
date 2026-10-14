package refinery

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These tests lock in gt-pd1c.45: every refinery gate (pre-merge, post-squash,
// and the legacy test_command) runs on the tree that results from merging the
// MR into the target, never on the bare target checkout. A gate that inspects
// the MR's own files (FAIL_MARKER) must therefore see them.

// mergedTreeFixture builds a repo with one feature branch carrying file and
// returns an MR for it plus the origin/main SHA before processing.
func mergedTreeFixture(t *testing.T, branch, file, content string) (string, *Engineer, *MRInfo, string) {
	t.Helper()
	workDir, e, mr := preVerifiedFixture(t, branch, file, content, false)
	return workDir, e, mr, remoteMainHead(t, workDir)
}

func assertNoMergeLeftovers(t *testing.T, workDir, before string) {
	t.Helper()
	assertOriginMainUnchangedAndReset(t, workDir, before)
	if _, err := os.Stat(filepath.Join(workDir, "FAIL_MARKER")); !os.IsNotExist(err) {
		t.Fatalf("FAIL_MARKER still present in the refinery worktree after a rejected merge (err=%v)", err)
	}
}

func TestDoMerge_PreMergeGateSeesMRContent(t *testing.T) {
	workDir, e, mr, before := mergedTreeFixture(t, "polecat/test/mt-pre", "FAIL_MARKER", "breaks the gate\n")
	e.config.Gates = map[string]*GateConfig{"check": {Cmd: failMarkerGateCmd()}}

	result := e.ProcessMRInfo(context.Background(), mr)

	if result.Success {
		t.Fatal("MR carrying FAIL_MARKER merged: the pre-merge gate did not see the MR's files")
	}
	if !result.TestsFailed {
		t.Fatalf("expected TestsFailed, got %+v", result)
	}
	assertNoMergeLeftovers(t, workDir, before)
}

func TestDoMerge_DefaultPhaseGateSeesMRContent_Parallel(t *testing.T) {
	workDir, e, mr, before := mergedTreeFixture(t, "polecat/test/mt-par", "FAIL_MARKER", "breaks the gate\n")
	e.config.GatesParallel = true
	e.config.Gates = map[string]*GateConfig{
		"a-check":  {Cmd: failMarkerGateCmd()},
		"b-passes": {Cmd: "true"},
	}

	result := e.ProcessMRInfo(context.Background(), mr)

	if result.Success || !result.TestsFailed {
		t.Fatalf("expected parallel gate failure on the merged tree, got %+v", result)
	}
	assertNoMergeLeftovers(t, workDir, before)
}

func TestDoMerge_LegacyTestCommandSeesMRContent(t *testing.T) {
	workDir, e, mr, before := mergedTreeFixture(t, "polecat/test/mt-legacy", "FAIL_MARKER", "breaks the tests\n")
	e.config.Gates = nil
	e.config.RunTests = true
	e.config.TestCommand = failMarkerGateCmd()

	result := e.ProcessMRInfo(context.Background(), mr)

	if result.Success {
		t.Fatal("MR carrying FAIL_MARKER merged: the legacy test_command did not see the MR's files")
	}
	if !result.TestsFailed {
		t.Fatalf("expected TestsFailed, got %+v", result)
	}
	assertNoMergeLeftovers(t, workDir, before)
}

// A clean MR merges and pushes as before, every gate sees the MR's file, and
// each phase runs exactly once.
func TestDoMerge_CleanMR_GatesSeeMRAndRunOncePerPhase(t *testing.T) {
	workDir, e, mr, before := mergedTreeFixture(t, "polecat/test/mt-clean", "feature.txt", "ok\n")
	preCmd, preLog := gateRecorderCmd(t)
	postCmd, postLog := gateRecorderCmd(t)
	e.config.Gates = map[string]*GateConfig{
		"pre-sees-mr":  {Cmd: "test -f feature.txt"},
		"pre-record":   {Cmd: preCmd},
		"post-sees-mr": {Cmd: "test -f feature.txt", Phase: GatePhasePostSquash},
		"post-record":  {Cmd: postCmd, Phase: GatePhasePostSquash},
	}

	result := e.ProcessMRInfo(context.Background(), mr)

	if !result.Success {
		t.Fatalf("clean MR failed: %s", result.Error)
	}
	if got := gateRunCount(t, preLog); got != 1 {
		t.Fatalf("pre-merge gate ran %d times, want 1", got)
	}
	if got := gateRunCount(t, postLog); got != 1 {
		t.Fatalf("post-squash gate ran %d times, want 1", got)
	}
	after := remoteMainHead(t, workDir)
	if after == before {
		t.Fatal("origin/main did not advance after a passing merge")
	}
	if after != result.MergeCommit {
		t.Fatalf("origin/main = %s, want merge commit %s", after, result.MergeCommit)
	}
}

func TestDoMerge_CleanMR_LegacyTestCommandSeesMR(t *testing.T) {
	workDir, e, mr, before := mergedTreeFixture(t, "polecat/test/mt-legacy-ok", "feature.txt", "ok\n")
	e.config.Gates = nil
	e.config.RunTests = true
	e.config.TestCommand = "test -f feature.txt"

	result := e.ProcessMRInfo(context.Background(), mr)

	if !result.Success {
		t.Fatalf("clean MR failed under legacy test_command: %s", result.Error)
	}
	if after := remoteMainHead(t, workDir); after == before {
		t.Fatal("origin/main did not advance after a passing merge")
	}
}

// merge_strategy=pr: gates must test the merged tree before the provider merge
// is requested, and the local trial merge must be undone first.
func TestDoMerge_PRStrategy_GateSeesMRContentAndBlocksProviderMerge(t *testing.T) {
	workDir, e, mr, before := mergedTreeFixture(t, "polecat/test/mt-pr-fail", "FAIL_MARKER", "breaks the gate\n")
	e.config.MergeStrategy = "pr"
	e.config.Gates = map[string]*GateConfig{"check": {Cmd: failMarkerGateCmd()}}
	e.prProvider = &recordingPRProvider{mergeFunc: func(method string) (string, error) {
		t.Fatalf("MergePR called although the gate must fail on the merged tree (method %s)", method)
		return "", nil
	}}

	result := e.doMerge(context.Background(), mr)

	if result.Success || !result.TestsFailed {
		t.Fatalf("expected gate failure before PR merge, got %+v", result)
	}
	assertNoMergeLeftovers(t, workDir, before)
}

func TestDoMerge_PRStrategy_CleanMR_GatesSeeMRThenProviderMerges(t *testing.T) {
	workDir, e, mr, before := mergedTreeFixture(t, "polecat/test/mt-pr-ok", "feature.txt", "ok\n")
	e.config.MergeStrategy = "pr"
	postCmd, postLog := gateRecorderCmd(t)
	e.config.Gates = map[string]*GateConfig{
		"pre-sees-mr": {Cmd: "test -f feature.txt"},
		"post-record": {Cmd: postCmd, Phase: GatePhasePostSquash},
	}
	provider := &recordingPRProvider{mergeFunc: func(string) (string, error) {
		// The local trial merge must be gone before the provider merges, so the
		// refinery worktree does not carry an unpushed commit of its own.
		if local := run(t, workDir, "git", "rev-parse", "main"); local != before {
			t.Fatalf("local main = %s at provider merge time, want origin/main %s", local, before)
		}
		run(t, workDir, "git", "merge", "--no-ff", "-m", "merge PR", mr.Branch)
		run(t, workDir, "git", "push", "origin", "main")
		return run(t, workDir, "git", "rev-parse", "main"), nil
	}}
	e.prProvider = provider

	result := e.doMerge(context.Background(), mr)

	if !result.Success {
		t.Fatalf("clean PR-strategy MR failed: %s", result.Error)
	}
	if provider.mergeMethod != "merge" {
		t.Fatalf("MergePR not called with merge method (got %q)", provider.mergeMethod)
	}
	if got := gateRunCount(t, postLog); got != 1 {
		t.Fatalf("post-squash gate ran %d times on the PR path, want 1", got)
	}
}

// Batch: post-squash gates must run on the combined stack too, not only the
// default-phase gates.
func TestProcessBatch_PostSquashGateRunsOnCombinedStack(t *testing.T) {
	workDir, g, cleanup := testGitRepo(t)
	defer cleanup()

	createFeatureBranch(t, workDir, "feature-a", "a.txt", "hello a\n")
	createFeatureBranch(t, workDir, "feature-b", "FAIL_MARKER", "this causes gate failure\n")
	before := remoteMainHead(t, workDir)

	e := newTestEngineer(t, workDir, g)
	e.config.Gates = map[string]*GateConfig{
		"post": {Cmd: failMarkerGateCmd(), Phase: GatePhasePostSquash},
	}

	batch := []*MRInfo{makeMR("mr-a", "feature-a", "main"), makeMR("mr-b", "feature-b", "main")}
	result := e.ProcessBatch(context.Background(), batch, "main", &BatchConfig{MaxBatchSize: 5})

	if len(result.Culprits) != 1 || result.Culprits[0].ID != "mr-b" {
		t.Fatalf("expected culprit mr-b from the post-squash gate, got culprits=%v merged=%v err=%v",
			stackedIDs(result.Culprits), stackedIDs(result.Merged), result.Error)
	}
	for _, mr := range result.Merged {
		if mr.ID == "mr-b" {
			t.Fatal("mr-b merged although the post-squash gate fails on it")
		}
	}
	if after := remoteMainHead(t, workDir); after == before && len(result.Merged) > 0 {
		t.Fatal("good MRs reported merged but origin/main did not move")
	}
}

// Batch: default-phase gates see the combined stack (already true; locked in).
func TestProcessBatch_PreMergeGateSeesCombinedStack(t *testing.T) {
	workDir, g, cleanup := testGitRepo(t)
	defer cleanup()

	createFeatureBranch(t, workDir, "feature-a", "a.txt", "hello a\n")
	createFeatureBranch(t, workDir, "feature-b", "b.txt", "hello b\n")

	e := newTestEngineer(t, workDir, g)
	e.config.Gates = map[string]*GateConfig{"both": {Cmd: "test -f a.txt && test -f b.txt"}}

	batch := []*MRInfo{makeMR("mr-a", "feature-a", "main"), makeMR("mr-b", "feature-b", "main")}
	result := e.ProcessBatch(context.Background(), batch, "main", &BatchConfig{MaxBatchSize: 5})

	if result.Error != nil || len(result.Merged) != 2 {
		t.Fatalf("expected both MRs merged after the gate saw the combined stack: merged=%v culprits=%v err=%v",
			stackedIDs(result.Merged), stackedIDs(result.Culprits), result.Error)
	}
}

// A failing pre-merge gate stops the run: post-squash gates never start.
func TestDoMerge_PreMergeFailure_SkipsPostSquash(t *testing.T) {
	workDir, e, mr, before := mergedTreeFixture(t, "polecat/test/mt-skip-post", "FAIL_MARKER", "breaks the gate\n")
	postCmd, postLog := gateRecorderCmd(t)
	e.config.Gates = map[string]*GateConfig{
		"pre":  {Cmd: failMarkerGateCmd()},
		"post": {Cmd: postCmd, Phase: GatePhasePostSquash},
	}

	result := e.ProcessMRInfo(context.Background(), mr)

	if result.Success || !result.TestsFailed {
		t.Fatalf("expected pre-merge gate failure, got %+v", result)
	}
	if got := gateRunCount(t, postLog); got != 0 {
		t.Fatalf("post-squash gate ran %d times after a pre-merge failure, want 0", got)
	}
	assertNoMergeLeftovers(t, workDir, before)
}

// A canceled context must not be classified as a test failure (the MR would
// be rejected as a culprit during refinery shutdown), and the local merge is
// still undone.
func TestDoMerge_CanceledGateRun_NotTestsFailed(t *testing.T) {
	workDir, e, mr, before := mergedTreeFixture(t, "polecat/test/mt-cancel", "feature.txt", "ok\n")
	started := filepath.Join(t.TempDir(), "gate-started")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The gate signals that it is running (on the merged tree), then blocks.
	e.config.Gates = map[string]*GateConfig{"slow": {Cmd: "touch " + started + " && exec sleep 30"}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(started); err == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		cancel()
	}()

	result := e.doMerge(ctx, mr)
	<-done

	if result.Success {
		t.Fatal("canceled gate run reported success")
	}
	if result.TestsFailed {
		t.Fatalf("canceled gate run classified as TestsFailed: %+v", result)
	}
	if !strings.Contains(result.Error, "canceled") {
		t.Fatalf("expected cancellation in error, got %q", result.Error)
	}
	assertOriginMainUnchangedAndReset(t, workDir, before)
}

// With auto_push=false a rejected MR must not discard an earlier merge that
// was committed locally but never pushed: the reset goes to the pre-merge tip,
// not to origin/<target>.
func TestDoMerge_AutoPushOff_GateFailureKeepsEarlierLocalMerge(t *testing.T) {
	workDir, g, cleanup := testGitRepo(t)
	defer cleanup()
	installNoPRGH(t)
	createFeatureBranch(t, workDir, "feature-good", "good.txt", "ok\n")
	createFeatureBranch(t, workDir, "feature-bad", "FAIL_MARKER", "breaks the gate\n")
	before := remoteMainHead(t, workDir)

	e := newTestEngineer(t, workDir, g)
	e.config.AutoPush = false
	e.config.Gates = map[string]*GateConfig{"check": {Cmd: failMarkerGateCmd()}}

	first := e.doMerge(context.Background(), makeMR("mr-good", "feature-good", "main"))
	if !first.Success {
		t.Fatalf("first MR failed: %s", first.Error)
	}
	second := e.doMerge(context.Background(), makeMR("mr-bad", "feature-bad", "main"))
	if second.Success || !second.TestsFailed {
		t.Fatalf("expected gate failure for the second MR, got %+v", second)
	}

	if local := run(t, workDir, "git", "rev-parse", "main"); local != first.MergeCommit {
		t.Fatalf("local main = %s, want the earlier unpushed merge %s", local, first.MergeCommit)
	}
	if after := remoteMainHead(t, workDir); after != before {
		t.Fatalf("origin/main moved with auto_push=false: %s -> %s", before, after)
	}
}

// Batch where only one MR survives stack construction goes through
// verifyAndPush; its post-squash gates must see the merged MR too.
func TestProcessBatch_SingleSurvivor_PostSquashGateSeesMR(t *testing.T) {
	workDir, g, cleanup := testGitRepo(t)
	defer cleanup()

	createFeatureBranch(t, workDir, "feature-a", "shared.txt", "version a\n")
	run(t, workDir, "git", "checkout", "feature-a")
	writeFile(t, workDir, "FAIL_MARKER", "breaks the gate\n")
	run(t, workDir, "git", "add", ".")
	run(t, workDir, "git", "commit", "-m", "feat: marker")
	run(t, workDir, "git", "checkout", "main")
	createConflictingBranch(t, workDir, "feature-b", "shared.txt", "version b\n")
	before := remoteMainHead(t, workDir)

	e := newTestEngineer(t, workDir, g)
	e.config.Gates = map[string]*GateConfig{
		"post": {Cmd: failMarkerGateCmd(), Phase: GatePhasePostSquash},
	}

	batch := []*MRInfo{makeMR("mr-a", "feature-a", "main"), makeMR("mr-b", "feature-b", "main")}
	result := e.ProcessBatch(context.Background(), batch, "main", &BatchConfig{MaxBatchSize: 5})

	if len(result.Merged) != 0 {
		t.Fatalf("expected nothing merged, got %v (culprits=%v conflicts=%v err=%v)",
			stackedIDs(result.Merged), stackedIDs(result.Culprits), stackedIDs(result.Conflicts), result.Error)
	}
	if len(result.Culprits) != 1 || result.Culprits[0].ID != "mr-a" {
		t.Fatalf("expected mr-a as post-squash culprit, got culprits=%v conflicts=%v err=%v",
			stackedIDs(result.Culprits), stackedIDs(result.Conflicts), result.Error)
	}
	if after := remoteMainHead(t, workDir); after != before {
		t.Fatalf("origin/main moved from %s to %s despite gate failure", before, after)
	}
}
