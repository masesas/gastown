package refinery

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	beadsdk "github.com/steveyegge/beads"
	"github.com/steveyegge/gastown/internal/beads"
	gitpkg "github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/rig"
)

// deadPID is the pid of a process that has exited.
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

// landByHand merges the MR branch into origin main outside the engine (as a
// run that pushed and then failed to finalize did) and returns the new tip.
func (f *mergeOneFixture) landByHand(t *testing.T) string {
	t.Helper()
	run(t, f.workDir, "git", "fetch", "origin")
	run(t, f.workDir, "git", "merge", "--no-ff", "-m", "landed earlier", "origin/"+f.branch)
	run(t, f.workDir, "git", "push", "origin", "main")
	return f.originMain(t)
}

// gateMarker makes the test gate append to a file, to count gate runs.
func (f *mergeOneFixture) gateMarker(t *testing.T) string {
	t.Helper()
	marker := filepath.Join(t.TempDir(), "gate-runs")
	f.settings(t, map[string]any{"test_command": "echo run >> " + marker})
	return marker
}

func (f *mergeOneFixture) assertFinalized(t *testing.T, out MergeOutcome) {
	t.Helper()
	mr := f.store.issue(t, fixtureMR)
	if mr.Status != beadsdk.StatusClosed || mr.CloseReason != string(CloseReasonMerged) {
		t.Fatalf("MR status/close reason = %s/%q, want closed/merged", mr.Status, mr.CloseReason)
	}
	src := f.store.issue(t, fixtureSource)
	if src.Status != beadsdk.StatusClosed || validateMergedCloseReason(src.CloseReason, fixtureMR) != nil {
		t.Fatalf("source = %s/%q, want closed \"Merged in %s\"", src.Status, src.CloseReason, fixtureMR)
	}
	if remote := run(t, f.workDir, "git", "ls-remote", "--heads", "origin", f.branch); remote != "" {
		t.Fatalf("remote branch still exists: %q", remote)
	}
	if f.sender.find("MERGED") == nil {
		t.Fatal("no MERGED mail")
	}
	if out.BranchCleanup == nil || !out.BranchCleanup.RemoteDeleted {
		t.Fatalf("BranchCleanup = %+v, want the remote branch deleted", out.BranchCleanup)
	}
}

// gt-pd1c.93: bd refuses to close a bead whose assignee is not the actor.
// The run claims the MR as <rig>/refinery/merge-<pid>-<n> and must close it
// as that claimant, not as the caller (a human, or <rig>/refinery).
func TestMergeOne_ClosesTheMRAsItsRunClaimant(t *testing.T) {
	for name, callerActor := range map[string]string{"human": "", "refinery session": "testrig/refinery"} {
		t.Run(name, func(t *testing.T) {
			f := newMergeOneFixture(t)
			t.Setenv("BD_ACTOR", callerActor)
			out := f.mergeOne(t)
			if out.Kind != OutcomeMerged {
				t.Fatalf("Kind = %s (%s), want merged", out.Kind, out.Message)
			}
			if out.AlreadyLanded {
				t.Fatal("AlreadyLanded on a fresh merge")
			}
			e := f.engineer(t)
			var mrClose *closeCall
			for _, c := range f.store.closeCalls() {
				if c.ID == fixtureMR {
					c := c
					mrClose = &c
				}
			}
			if mrClose == nil {
				t.Fatal("MR never closed")
			}
			runID, ok := strings.CutPrefix(mrClose.Actor, "testrig/refinery/")
			if !ok || !runClaimantPattern.MatchString(runID) || !e.ownClaim(mrClose.Actor) {
				t.Fatalf("MR closed by actor %q, want this run's claimant", mrClose.Actor)
			}
			if got := f.store.issue(t, fixtureMR).Assignee; got != mrClose.Actor {
				t.Fatalf("MR assignee = %q, want the claimant %q that closed it", got, mrClose.Actor)
			}
			f.assertFinalized(t, out)
		})
	}
}

// guardRefusingMRCloseStore refuses the first close of the MR with bd's
// ownership error, as bd 1.3.0 did for the pilot run (kp-wisp-6lwd).
type guardRefusingMRCloseStore struct {
	*memStore
	refused bool
}

func (s *guardRefusingMRCloseStore) CloseIssue(ctx context.Context, id, reason, actor, session string) error {
	if id == fixtureMR && !s.refused {
		s.refused = true
		return fmt.Errorf("cannot close %s: assignee is %q, actor is %q; reclaim or use --force to override", id, "testrig/refinery/merge-1-1", "Human")
	}
	return s.memStore.CloseIssue(ctx, id, reason, actor, session)
}

// The pilot failure and its recovery: the merge is pushed, the MR close
// fails (cleanup_failed: MR open, close_reason merged recorded, claim
// released), and re-running gt mq merge finalizes it without merging again.
func TestMergeOne_RerunFinalizesALandedMRWithoutMergingAgain(t *testing.T) {
	f := newMergeOneFixture(t)
	marker := f.gateMarker(t)
	e := f.engineer(t)
	e.beads = beads.NewWithBeadsDirAndStore(f.workDir, f.rigBeads, &guardRefusingMRCloseStore{memStore: f.store})
	first := e.MergeOne(context.Background(), fixtureMR)
	t.Logf("first: %+v\n%s", first, f.out.String())
	if first.Kind != OutcomeCleanupFailed {
		t.Fatalf("first run = %s %q, want cleanup_failed", first.Kind, first.Message)
	}
	if want := "gt mq merge testrig " + fixtureMR; first.Next != want || !strings.Contains(first.Message, want) {
		t.Fatalf("first run next = %q, message %q: want the re-run command %q", first.Next, first.Message, want)
	}
	mr := f.store.issue(t, fixtureMR)
	if mr.Status != beadsdk.StatusOpen || mr.Assignee != "" {
		t.Fatalf("after cleanup_failed MR = %s/%q, want open and unclaimed", mr.Status, mr.Assignee)
	}
	landed := f.originMain(t)

	second := f.mergeOne(t)
	if second.Kind != OutcomeMerged || !second.AlreadyLanded {
		t.Fatalf("re-run = %s %q (already_landed=%v), want merged and already_landed", second.Kind, second.Message, second.AlreadyLanded)
	}
	if f.originMain(t) != landed {
		t.Fatal("the re-run merged and pushed again")
	}
	if second.MergeCommit != landed || second.GatesRan != 0 {
		t.Fatalf("re-run merge_commit=%s gates_ran=%d, want the recorded %s and 0", second.MergeCommit, second.GatesRan, landed)
	}
	if runs := strings.Count(readLog(t, marker), "run"); runs != 1 {
		t.Fatalf("gates ran %d times over both runs, want once (only the real merge)", runs)
	}
	f.assertFinalized(t, second)
}

// A landed MR still claimed by a run whose process died is taken over under
// the merge lock and finalized.
func TestMergeOne_LandedMRWithDeadRunClaimIsFinalized(t *testing.T) {
	f := newMergeOneFixture(t)
	marker := f.gateMarker(t)
	tip := f.landByHand(t)
	f.store.issues[fixtureMR].Description += "\nclose_reason: merged\nmerge_commit: " + tip
	f.store.issues[fixtureMR].Assignee = fmt.Sprintf("testrig/refinery/merge-%d-1", deadPID(t))

	out := f.mergeOne(t)
	if out.Kind != OutcomeMerged || !out.AlreadyLanded {
		t.Fatalf("outcome = %s %q (already_landed=%v), want merged and already_landed", out.Kind, out.Message, out.AlreadyLanded)
	}
	if f.originMain(t) != tip {
		t.Fatal("origin main moved: a landed MR was merged again")
	}
	if log := readLog(t, marker); log != "" {
		t.Fatalf("gates ran for an already-landed MR: %q", log)
	}
	f.assertFinalized(t, out)
}

// H1 (security review): commit_sha on the target alone does not bind the MR.
// Only a recorded merge_commit that git proves on the target, with exactly
// two parents and commit_sha as the second, lets a run finalize without
// merging. Anything else is refused: nothing merged, closed or mailed.
func TestMergeOne_LandedWithoutAProvenMergeOfThisMRIsRefused(t *testing.T) {
	for name, tc := range map[string]struct {
		setup func(t *testing.T, f *mergeOneFixture) // mutates the MR
		want  string
	}{
		"forged: commit_sha = main HEAD, no merge_commit": {func(t *testing.T, f *mergeOneFixture) {
			head := f.originMain(t)
			f.store.issues[fixtureMR] = memMRIssue(fixtureMR, f.branch, "main", fixtureSource, head)
		}, "no recorded merge"},
		"landed by hand, no merge_commit": {func(t *testing.T, f *mergeOneFixture) {
			f.landByHand(t)
		}, "no recorded merge"},
		"merge_commit whose second parent is not commit_sha": {func(t *testing.T, f *mergeOneFixture) {
			f.landByHand(t)
			run(t, f.workDir, "git", "checkout", "-q", "-b", "side", "origin/main~1")
			writeFile(t, f.workDir, "side.txt", "side\n")
			run(t, f.workDir, "git", "add", ".")
			run(t, f.workDir, "git", "commit", "-q", "-m", "side")
			run(t, f.workDir, "git", "checkout", "-q", "main")
			run(t, f.workDir, "git", "merge", "--no-ff", "-q", "-m", "merge side", "side")
			run(t, f.workDir, "git", "push", "-q", "origin", "main")
			run(t, f.workDir, "git", "branch", "-D", "side")
			f.store.issues[fixtureMR].Description += "\nclose_reason: merged\nmerge_commit: " + f.originMain(t)
		}, "second parent"},
		"merge_commit not on the target": {func(t *testing.T, f *mergeOneFixture) {
			f.landByHand(t)
			run(t, f.workDir, "git", "checkout", "-q", "-b", "elsewhere", f.originMain(t)+"~1")
			run(t, f.workDir, "git", "merge", "--no-ff", "-q", "-m", "unpushed merge", "origin/"+f.branch)
			unpushed := run(t, f.workDir, "git", "rev-parse", "HEAD")
			run(t, f.workDir, "git", "checkout", "-q", "main")
			run(t, f.workDir, "git", "branch", "-D", "elsewhere")
			f.store.issues[fixtureMR].Description += "\nmerge_commit: " + unpushed
		}, "not proven on origin/main"},
		"merge_commit not a commit id": {func(t *testing.T, f *mergeOneFixture) {
			f.landByHand(t)
			f.store.issues[fixtureMR].Description += "\nmerge_commit: " + strings.Repeat("a", 40)
		}, "not proven on origin/main"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newMergeOneFixture(t)
			tc.setup(t, f)
			before := f.originMain(t)
			out := f.mergeOne(t)
			if out.Kind != OutcomeNotEligible || out.AlreadyLanded || !strings.Contains(out.Message, tc.want) || !strings.Contains(out.Message, "resolve") {
				t.Fatalf("outcome = %s %q (already_landed=%v), want not_eligible containing %q", out.Kind, out.Message, out.AlreadyLanded, tc.want)
			}
			if f.originMain(t) != before {
				t.Fatal("origin main moved")
			}
			if closes := f.store.closeCalls(); len(closes) != 0 {
				t.Fatalf("closed %v without a proven merge of this MR", closes)
			}
			if f.sender.find("MERGED") != nil {
				t.Fatal("MERGED mailed for an unproven landing")
			}
			if mr := f.store.issue(t, fixtureMR); mr.Status != beadsdk.StatusOpen || mr.Assignee != "" {
				t.Fatalf("MR = %s/%q, want open and unclaimed", mr.Status, mr.Assignee)
			}
		})
	}
}

// The merge commit is recorded on the MR right after the verified push,
// before any finalize step that can fail, so a failed finalize always leaves
// the evidence a re-run needs.
func TestMergeOne_RecordsMergeCommitBeforeFinalize(t *testing.T) {
	f := newMergeOneFixture(t)
	e := f.engineer(t)
	e.beads = beads.NewWithBeadsDirAndStore(f.workDir, f.rigBeads, &failingMRDescriptionAfterFirstStore{memStore: f.store})
	out := e.MergeOne(context.Background(), fixtureMR)
	if out.Kind != OutcomeCleanupFailed {
		t.Fatalf("outcome = %s %q, want cleanup_failed (close metadata write fails)", out.Kind, out.Message)
	}
	fields := beads.ParseMRFields(&beads.Issue{Description: f.store.issue(t, fixtureMR).Description})
	if fields == nil || fields.MergeCommit != f.originMain(t) {
		t.Fatalf("MR fields = %+v, want merge_commit %s recorded before the failed finalize", fields, f.originMain(t))
	}
	second := f.mergeOne(t)
	if second.Kind != OutcomeMerged || !second.AlreadyLanded {
		t.Fatalf("re-run = %s %q, want merged already_landed", second.Kind, second.Message)
	}
}

// failingMRDescriptionAfterFirstStore lets the first MR description write
// (the merge_commit record) through and fails the next one (closeTerminalMR's
// close metadata), once.
type failingMRDescriptionAfterFirstStore struct {
	*memStore
	writes int
}

func (s *failingMRDescriptionAfterFirstStore) UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error {
	if _, ok := updates["description"]; ok && id == fixtureMR {
		s.writes++
		if s.writes == 2 {
			return errors.New("store down")
		}
	}
	return s.memStore.UpdateIssue(ctx, id, updates, actor)
}

// A claim held by a run whose process is alive is never taken.
func TestMergeOne_LiveRunClaimIsRefused(t *testing.T) {
	f := newMergeOneFixture(t)
	tip := f.landByHand(t)
	live := fmt.Sprintf("testrig/refinery/merge-%d-7", os.Getppid())
	f.store.issues[fixtureMR].Assignee = live

	out := f.mergeOne(t)
	if out.Kind != OutcomeNotEligible || !strings.Contains(out.Message, live) || !strings.Contains(out.Message, "running") {
		t.Fatalf("outcome = %s %q, want not_eligible naming the live claimant", out.Kind, out.Message)
	}
	if got := f.store.issue(t, fixtureMR); got.Assignee != live || got.Status != beadsdk.StatusOpen {
		t.Fatalf("MR = %s/%q: the live claim was taken or released", got.Status, got.Assignee)
	}
	if f.originMain(t) != tip || len(f.store.closeCalls()) != 0 {
		t.Fatal("a live run's MR was merged or closed")
	}
}

// close_reason: merged without git proof is refused (nothing closed, claim
// released), as before.
func TestMergeOne_RecordedMergedWithoutLandingIsRefused(t *testing.T) {
	f := newMergeOneFixture(t)
	f.store.issues[fixtureMR].Description += "\nclose_reason: merged"
	before := f.originMain(t)
	out := f.mergeOne(t)
	if out.Kind != OutcomeNotEligible || !strings.Contains(out.Message, "close_reason") || !strings.Contains(out.Message, "not on origin/main") {
		t.Fatalf("outcome = %s %q, want not_eligible: close_reason merged but not landed", out.Kind, out.Message)
	}
	if f.originMain(t) != before || len(f.store.closeCalls()) != 0 {
		t.Fatal("merged or closed an MR whose recorded merge is not proven")
	}
	f.assertOpenAndUnclaimed(t)
}

func TestLandingState(t *testing.T) {
	notOn := &gitpkg.CommitNotOnBranchError{}
	for name, tc := range map[string]struct {
		err  error
		want landing
	}{
		"proven":        {nil, landingProven},
		"definite no":   {fmt.Errorf("target main does not contain x: %w", notOn), landingAbsent},
		"cannot decide": {errors.New("verified_push_failed: unable to read origin/main"), landingUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			if got := landingFromProof(tc.err); got != tc.want {
				t.Fatalf("landingFromProof(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// The run's claimant pid decides whether a leftover claim is stale.
func TestOwnClaim_RunClaimantLiveness(t *testing.T) {
	e := &Engineer{rig: &rig.Rig{Name: "testrig"}}
	for assignee, want := range map[string]bool{
		fmt.Sprintf("testrig/refinery/merge-%d-1", deadPID(t)):   true,
		fmt.Sprintf("testrig/refinery/merge-%d-1", os.Getpid()):  true, // this process holds the merge lock
		fmt.Sprintf("testrig/refinery/merge-%d-1", os.Getppid()): false,
		"testrig/refinery/merge-99999999999999999999999-1":       false,
		"testrig/refinery/merge-0-1":                             false,
	} {
		if got := e.ownClaim(assignee); got != want {
			t.Errorf("ownClaim(%q) = %v, want %v", assignee, got, want)
		}
	}
}

func TestLandingStep(t *testing.T) {
	f := newMergeOneFixture(t)
	e := f.engineer(t)
	notLanded := &MRInfo{ID: fixtureMR, Branch: f.branch, Target: "main", CommitSHA: f.commit}
	unknown := &MRInfo{ID: fixtureMR, Branch: f.branch, Target: "integration/missing", CommitSHA: f.commit}
	merged := &beads.MRFields{CloseReason: "merged"}
	for name, tc := range map[string]struct {
		mr       *MRInfo
		fields   *beads.MRFields
		want     mergeStep
		wantKind MergeOutcomeKind
	}{
		"not landed merges":                   {notLanded, &beads.MRFields{}, stepMerge, ""},
		"undecidable without verdict merges":  {unknown, &beads.MRFields{}, stepMerge, ""},
		"recorded merged, not landed refuses": {notLanded, merged, stepRefuse, OutcomeNotEligible},
		"recorded merged, undecidable errors": {unknown, merged, stepRefuse, OutcomeError},
	} {
		t.Run(name, func(t *testing.T) {
			step, out := e.landingStep(MergeOutcome{}, tc.mr, tc.fields)
			if step != tc.want || out.Kind != tc.wantKind {
				t.Fatalf("landingStep = %v %s %q, want %v %s", step, out.Kind, out.Message, tc.want, tc.wantKind)
			}
		})
	}
	tip := f.landByHand(t)
	if step, out := e.landingStep(MergeOutcome{}, notLanded, merged); step != stepRefuse || out.Kind != OutcomeNotEligible {
		t.Fatalf("landed MR without merge_commit: step = %v %s, want refuse not_eligible", step, out.Kind)
	}
	withMerge := &beads.MRFields{CloseReason: "merged", MergeCommit: tip}
	if step, out := e.landingStep(MergeOutcome{}, notLanded, withMerge); step != stepFinalize || out.MergeCommit != tip {
		t.Fatalf("landed MR with its merge: step = %v merge_commit %q, want finalize with %s", step, out.MergeCommit, tip)
	}
}

// CheckWorktreeReady stays the whole check: base checks plus the local
// source branch created at commit_sha from origin.
func TestCheckWorktreeReady_CreatesTheLocalSourceBranch(t *testing.T) {
	f := newMergeOneFixture(t)
	mr := &MRInfo{ID: fixtureMR, Branch: f.branch, Target: "main", CommitSHA: f.commit}
	if err := f.engineer(t).CheckWorktreeReady(mr); err != nil {
		t.Fatalf("CheckWorktreeReady: %v", err)
	}
	if got := run(t, f.workDir, "git", "rev-parse", "refs/heads/"+f.branch); got != f.commit {
		t.Fatalf("local %s = %s, want commit_sha %s", f.branch, got, f.commit)
	}
	writeFile(t, f.workDir, "dirty.txt", "x\n")
	if err := f.engineer(t).CheckWorktreeReady(mr); !errors.Is(err, ErrWorktreeNotReady) {
		t.Fatalf("dirty tree: CheckWorktreeReady = %v, want ErrWorktreeNotReady", err)
	}
}
