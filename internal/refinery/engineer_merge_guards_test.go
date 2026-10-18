package refinery

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofrs/flock"
	beadsdk "github.com/steveyegge/beads"
	"github.com/steveyegge/gastown/internal/beads"
)

// Review round 1 (PR-A): security and Go review findings.

// H1: a gate is branch-controlled code. Whatever it does to HEAD, only the
// verified merge commit may be pushed.
func TestMergeOne_GateThatMovesHEADIsRefusedAndNothingIsPushed(t *testing.T) {
	for name, gate := range map[string]string{
		"amend":        "git commit --amend -q -m tampered",
		"extra commit": "echo evil > evil.txt && git add evil.txt && git commit -q -m evil",
		"reset away":   "git reset -q --hard HEAD^1",
	} {
		t.Run(name, func(t *testing.T) {
			f := newMergeOneFixture(t)
			f.settings(t, map[string]any{"test_command": gate})
			before := f.originMain(t)
			out := f.mergeOne(t)
			if out.Kind != OutcomeGatesFailed || !strings.Contains(out.Message, "gate changed") {
				t.Fatalf("outcome = %s %q, want gates_failed (gate changed the merge)", out.Kind, out.Message)
			}
			if f.originMain(t) != before {
				t.Fatal("a tampered merge was pushed")
			}
			if local := run(t, f.workDir, "git", "rev-parse", "main"); local != before {
				t.Fatalf("local main = %s, want reset to the pre-merge tip %s", local, before)
			}
			f.assertOpenAndUnclaimed(t)
		})
	}
}

// H1/M3: gates run without agent identity or credentials in their env.
func TestMergeOne_GatesRunWithScrubbedEnvironment(t *testing.T) {
	f := newMergeOneFixture(t)
	envFile := filepath.Join(t.TempDir(), "env.txt")
	for k, v := range map[string]string{
		"GITHUB_TOKEN": "s1", "GH_TOKEN": "s2", "GT_ROLE": "testrig/refinery", "BD_ACTOR": "testrig/refinery",
		"BEADS_DIR": "/x", "ANTHROPIC_API_KEY": "s3", "SSH_AUTH_SOCK": "/tmp/agent", "MY_SERVICE_PASSWORD": "s4",
		"GIT_ASKPASS": "/bin/echo",
	} {
		t.Setenv(k, v)
	}
	f.settings(t, map[string]any{"test_command": "env > " + envFile})
	if out := f.mergeOne(t); out.Kind != OutcomeMerged {
		t.Fatalf("outcome = %s %q, want merged", out.Kind, out.Message)
	}
	env := readLog(t, envFile)
	for _, leaked := range []string{"GITHUB_TOKEN=", "GH_TOKEN=", "GT_ROLE=", "BD_ACTOR=", "BEADS_DIR=", "ANTHROPIC_API_KEY=", "SSH_AUTH_SOCK=", "MY_SERVICE_PASSWORD=", "GIT_ASKPASS="} {
		if strings.Contains(env, "\n"+leaked) || strings.HasPrefix(env, leaked) {
			t.Errorf("gate environment still has %s", strings.TrimSuffix(leaked, "="))
		}
	}
	for _, kept := range []string{"PATH=", "HOME="} {
		if !strings.Contains(env, kept) {
			t.Errorf("gate environment lost %s", strings.TrimSuffix(kept, "="))
		}
	}
}

// H2: one merge at a time per rig worktree.
func TestMergeOne_RefusedWhileAnotherMergeHoldsTheLock(t *testing.T) {
	f := newMergeOneFixture(t)
	lockDir := filepath.Join(f.rigPath, ".runtime", "locks")
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatal(err)
	}
	held := flock.New(filepath.Join(lockDir, "refinery-merge.lock"))
	if ok, err := held.TryLock(); err != nil || !ok {
		t.Fatalf("test could not take the lock: %v", err)
	}
	defer func() { _ = held.Unlock() }()
	before := f.originMain(t)

	out := f.mergeOne(t)
	if out.Kind != OutcomePrecondition || !strings.Contains(out.Message, "another merge is running") {
		t.Fatalf("outcome = %s %q, want precondition (another merge is running)", out.Kind, out.Message)
	}
	if f.originMain(t) != before {
		t.Fatal("origin main moved while another merge held the lock")
	}
	f.assertOpenAndUnclaimed(t)
}

// stealingClaimStore lets another claimant win the race for the MR.
type stealingClaimStore struct{ *memStore }

func (s *stealingClaimStore) UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error {
	if v, ok := updates["assignee"]; ok && v != "" {
		updates = map[string]interface{}{"assignee": "otherrig/crew/joe"}
	}
	return s.memStore.UpdateIssue(ctx, id, updates, actor)
}

// H2: the claim is compare-and-set: losing the race refuses, and the
// winner's claim is never released by the loser.
func TestMergeOne_LostClaimRaceRefusesAndKeepsTheOtherClaim(t *testing.T) {
	f := newMergeOneFixture(t)
	e := f.engineer(t)
	e.beads = beads.NewWithBeadsDirAndStore(f.workDir, f.rigBeads, &stealingClaimStore{memStore: f.store})
	before := f.originMain(t)
	out := e.MergeOne(context.Background(), fixtureMR)
	if out.Kind != OutcomeNotEligible || !strings.Contains(out.Message, "otherrig/crew/joe") {
		t.Fatalf("outcome = %s %q, want not_eligible (claim lost)", out.Kind, out.Message)
	}
	if got := f.store.issue(t, fixtureMR).Assignee; got != "otherrig/crew/joe" {
		t.Fatalf("assignee = %q: the winner's claim was released or overwritten", got)
	}
	if f.originMain(t) != before {
		t.Fatal("origin main moved after losing the claim")
	}
}

// H3: a bead field close_reason: merged is not proof.
func TestRecheckMRStillMergeable_ForgedMergedCloseReasonClosesNothing(t *testing.T) {
	f := newMergeOneFixture(t)
	f.store.issues[fixtureMR].Description += "\nclose_reason: merged"
	e := f.engineer(t)
	mr := &MRInfo{ID: fixtureMR, Branch: f.branch, Target: "main", SourceIssue: fixtureSource, CommitSHA: f.commit}
	res := e.recheckMRStillMergeable(mr, "main")
	if res.Success || res.NoMerge {
		t.Fatalf("recheck = %+v, want a refusal that is not NoMerge (nothing may be closed)", res)
	}
	if !strings.Contains(res.Error, "close_reason") {
		t.Fatalf("error %q does not explain the unproven close_reason", res.Error)
	}
	if closes := f.store.closeCalls(); len(closes) != 0 {
		t.Fatalf("closed %v on a forged close_reason", closes)
	}
	if src := f.store.issue(t, fixtureSource); src.Status != beadsdk.StatusOpen {
		t.Fatalf("source status = %s, want open", src.Status)
	}
}

func TestRecheckMRStillMergeable_ProvenMergedCloseReasonClosesAsMerged(t *testing.T) {
	f := newMergeOneFixture(t)
	run(t, f.workDir, "git", "fetch", "origin")
	run(t, f.workDir, "git", "merge", "--no-ff", "-m", "landed by hand", "origin/"+f.branch)
	run(t, f.workDir, "git", "push", "origin", "main")
	f.store.issues[fixtureMR].Description += "\nclose_reason: merged"
	res := f.engineer(t).recheckMRStillMergeable(&MRInfo{ID: fixtureMR, Branch: f.branch, Target: "main", SourceIssue: fixtureSource, CommitSHA: f.commit}, "main")
	if !res.NoMerge {
		t.Fatalf("recheck = %+v, want NoMerge after closing the proven-merged MR", res)
	}
	if mr := f.store.issue(t, fixtureMR); mr.Status != beadsdk.StatusClosed || mr.CloseReason != string(CloseReasonMerged) {
		t.Fatalf("MR = %s/%q, want closed merged", mr.Status, mr.CloseReason)
	}
}

func TestMergeOne_RefusesMRWithACloseReasonField(t *testing.T) {
	f := newMergeOneFixture(t)
	f.store.issues[fixtureMR].Description += "\nclose_reason: merged"
	out := f.mergeOne(t)
	if out.Kind != OutcomeNotEligible || !strings.Contains(out.Message, "close_reason") {
		t.Fatalf("outcome = %s %q, want not_eligible (close_reason set)", out.Kind, out.Message)
	}
	if closes := f.store.closeCalls(); len(closes) != 0 {
		t.Fatalf("closed %v", closes)
	}
}

// M5: source_issue is handed to bd: it must be a plain bead id.
func TestMergeOne_RefusesUnsafeSourceIssue(t *testing.T) {
	f := newMergeOneFixture(t)
	f.store.issues[fixtureMR] = memMRIssue(fixtureMR, f.branch, "main", "--db=/tmp/x", f.commit)
	if out := f.mergeOne(t); out.Kind != OutcomeNotEligible || !strings.Contains(out.Message, "source_issue") {
		t.Fatalf("outcome = %s %q, want not_eligible (unsafe source_issue)", out.Kind, out.Message)
	}
	if closes := f.store.closeCalls(); len(closes) != 0 {
		t.Fatalf("closed %v: an unsafe source_issue must be refused before any bead write", closes)
	}
}

// M2/L1: no gates is refused unless run_tests=false says so explicitly.
func TestMergeOne_NoGatesConfiguredIsRefused(t *testing.T) {
	f := newMergeOneFixture(t)
	writeRigSettingsMQ(t, f.rigPath, map[string]any{"lint_command": "  "})
	before := f.originMain(t)
	out := f.mergeOne(t)
	if out.Kind != OutcomePrecondition || !strings.Contains(out.Message, "no merge gates configured") {
		t.Fatalf("outcome = %s %q, want precondition (no gates)", out.Kind, out.Message)
	}
	if f.originMain(t) != before {
		t.Fatal("merged without gates")
	}
	f.assertOpenAndUnclaimed(t)
}

func TestMergeOne_RunTestsFalseDisablesEveryGateAndWarns(t *testing.T) {
	f := newMergeOneFixture(t)
	writeRigConfigMQ(t, f.rigPath, map[string]any{"gates": map[string]any{"explicit": map[string]any{"cmd": "exit 1"}}})
	writeRigSettingsMQ(t, f.rigPath, map[string]any{"run_tests": false, "test_command": "exit 1"})
	out := f.mergeOne(t)
	if out.Kind != OutcomeMerged {
		t.Fatalf("outcome = %s %q, want merged (run_tests=false disables all gates)", out.Kind, out.Message)
	}
	if !strings.Contains(strings.Join(out.Warnings, "\n"), "run_tests=false") {
		t.Fatalf("warnings = %v, want the run_tests=false notice", out.Warnings)
	}
}

// M3 (go): a failing MR is reported once per submitted commit_sha.
func TestMergeOne_FixNeededOncePerCommit(t *testing.T) {
	f := newMergeOneFixture(t)
	f.settings(t, map[string]any{"test_command": "exit 1"})
	for i := 0; i < 2; i++ {
		if out := f.mergeOne(t); out.Kind != OutcomeGatesFailed {
			t.Fatalf("run %d: outcome = %s %q, want gates_failed", i, out.Kind, out.Message)
		}
	}
	n := 0
	for _, s := range f.sender.subjects() {
		if strings.HasPrefix(s, "FIX_NEEDED") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("FIX_NEEDED sent %d times for the same commit_sha, want 1", n)
	}
}

// noopCloseStore acknowledges closes of the MR without closing it.
type noopCloseStore struct{ *memStore }

func (s *noopCloseStore) CloseIssue(ctx context.Context, id, reason, actor, session string) error {
	if id == fixtureMR {
		return nil
	}
	return s.memStore.CloseIssue(ctx, id, reason, actor, session)
}

// M4 (go): after a NoMerge the claim is released if the MR is still open.
func TestMergeOne_NoMergeWithMROpenStillReleasesTheClaim(t *testing.T) {
	f := newMergeOneFixture(t)
	f.store.issues[fixtureSource].Status = beadsdk.StatusClosed // the recheck rejects it
	e := f.engineer(t)
	e.beads = beads.NewWithBeadsDirAndStore(f.workDir, f.rigBeads, &noopCloseStore{memStore: f.store})
	out := e.MergeOne(context.Background(), fixtureMR)
	if out.Kind != OutcomeNotEligible {
		t.Fatalf("outcome = %s %q, want not_eligible", out.Kind, out.Message)
	}
	if mr := f.store.issue(t, fixtureMR); mr.Status != beadsdk.StatusOpen || mr.Assignee != "" {
		t.Fatalf("MR status/assignee = %s/%q, want open and unclaimed", mr.Status, mr.Assignee)
	}
}

// failingSourceCloseStore fails to close the source bead.
type failingSourceCloseStore struct{ *memStore }

func (s *failingSourceCloseStore) CloseIssue(ctx context.Context, id, reason, actor, session string) error {
	if id == fixtureSource {
		return errors.New("store down")
	}
	return s.memStore.CloseIssue(ctx, id, reason, actor, session)
}

// M4 (security): a merge whose source was not closed "Merged in <mr>" never
// exits as merged.
func TestMergeOne_SourceCloseFailureIsCleanupFailed(t *testing.T) {
	f := newMergeOneFixture(t)
	e := f.engineer(t)
	e.beads = beads.NewWithBeadsDirAndStore(f.workDir, f.rigBeads, &failingSourceCloseStore{memStore: f.store})
	out := e.MergeOne(context.Background(), fixtureMR)
	if out.Kind != OutcomeCleanupFailed || !strings.Contains(out.Message, fixtureSource) {
		t.Fatalf("outcome = %s %q, want cleanup_failed naming the source", out.Kind, out.Message)
	}
}

// L3: the default branch and integration/* are never deleted as a "merged
// source branch".
func TestCleanupMergedBranch_NeverDeletesProtectedBranches(t *testing.T) {
	workDir, g, _ := testGitRepo(t)
	e, _ := finalizeTestEngineer(t, workDir, g)
	e.config.DeleteMergedBranches = true
	commit, _ := landedOnMain(t, workDir, "integration/epic-1")
	run(t, workDir, "git", "checkout", "--detach")
	mainSHA := run(t, workDir, "git", "rev-parse", "main")
	for branch, sha := range map[string]string{"integration/epic-1": commit, "main": mainSHA} {
		c := e.cleanupMergedBranch(&MRInfo{Branch: branch, CommitSHA: sha})
		if c.LocalDeleted || c.RemoteDeleted {
			t.Errorf("%s: cleanup %+v deleted a protected branch", branch, c)
		}
		if ok, _ := g.BranchExists(branch); !ok {
			t.Errorf("%s was deleted", branch)
		}
	}
}

// L4a: the merge slot follows the same default branch as eligibility and
// proof (the remote's HEAD when default_branch is unset), not "main".
func TestDoMerge_SlotUsesTheTrustedDefaultBranch(t *testing.T) {
	installFakeGT(t)
	workDir, g, _ := testGitRepo(t)
	run(t, workDir, "git", "push", "origin", "main:trunk")
	bare := run(t, workDir, "git", "remote", "get-url", "origin")
	run(t, workDir, "git", "--git-dir="+bare, "symbolic-ref", "HEAD", "refs/heads/trunk")
	run(t, workDir, "git", "fetch", "origin")
	run(t, workDir, "git", "checkout", "-b", "trunk", "origin/trunk")
	createFeatureBranch(t, workDir, "polecat/t/x", "x.txt", "x\n")
	run(t, workDir, "git", "checkout", "trunk")
	commit := run(t, workDir, "git", "rev-parse", "polecat/t/x")

	e, _, _ := scopedTestEngineer(t, workDir, nil)
	e.git = g
	acquired := false
	e.mergeSlotAcquire = func(holder string, _ bool) (*beads.MergeSlotStatus, error) {
		acquired = true
		return &beads.MergeSlotStatus{Available: true, Holder: holder}, nil
	}
	if res := e.doMerge(context.Background(), &MRInfo{ID: "mr-trunk", Branch: "polecat/t/x", Target: "trunk", CommitSHA: commit}); !res.Success {
		t.Fatalf("doMerge: %s", res.Error)
	}
	if !acquired {
		t.Fatal("push to the remote's default branch (trunk) did not take the merge slot")
	}
}

// L2 (go): LoadConfig starts from defaults every time.
func TestLoadConfig_ReloadDropsGatesNoLongerConfigured(t *testing.T) {
	rigPath := t.TempDir()
	writeRigSettingsMQ(t, rigPath, map[string]any{"test_command": "go test ./..."})
	e, _ := loadTestConfig(t, rigPath)
	if len(e.Config().Gates) != 1 {
		t.Fatalf("gates = %v, want the generated test gate", e.Config().Gates)
	}
	writeRigSettingsMQ(t, rigPath, map[string]any{"lint_command": ""})
	if err := e.LoadConfig(); err != nil {
		t.Fatal(err)
	}
	if len(e.Config().Gates) != 0 {
		t.Fatalf("gates after reload = %v, want none (stale generated gate kept)", e.Config().Gates)
	}
}
