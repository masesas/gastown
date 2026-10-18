package refinery

import (
	"context"
	"errors"
	"strings"
	"testing"

	beadsdk "github.com/steveyegge/beads"
	"github.com/steveyegge/gastown/internal/beads"
)

func TestFailureOutcomeKindAndMergeFailureType(t *testing.T) {
	for _, tc := range []struct {
		result   ProcessResult
		kind     MergeOutcomeKind
		failType string
	}{
		{ProcessResult{Conflict: true}, OutcomeConflict, "conflict"},
		{ProcessResult{ScopeViolation: true}, OutcomeScopeViolation, "scope"},
		{ProcessResult{TestsFailed: true}, OutcomeGatesFailed, "tests"},
		{ProcessResult{SlotTimeout: true}, OutcomeSlotTimeout, "build"},
		{ProcessResult{BranchNotFound: true}, OutcomeBranchMissing, "build"},
		{ProcessResult{NoMerge: true}, OutcomeNotEligible, "build"},
		{ProcessResult{NeedsApproval: true}, OutcomeNeedsApproval, "build"},
		{ProcessResult{Error: "push failed"}, OutcomeError, "build"},
	} {
		if got := failureOutcomeKind(tc.result); got != tc.kind {
			t.Errorf("failureOutcomeKind(%+v) = %s, want %s", tc.result, got, tc.kind)
		}
		if got := mergeFailureType(tc.result); got != tc.failType {
			t.Errorf("mergeFailureType(%+v) = %s, want %s", tc.result, got, tc.failType)
		}
	}
}

func TestPolecatNameFromWorker(t *testing.T) {
	for worker, want := range map[string]string{
		"nux": "nux", "polecats/nux": "nux", "gastown/polecats/nux": "nux", " nux ": "nux",
		"": "", "crew/joe": "", "polecats/": "", "polecats/a b": "", "polecats/a\nb": "",
	} {
		if got := polecatNameFromWorker(worker); got != want {
			t.Errorf("polecatNameFromWorker(%q) = %q, want %q", worker, got, want)
		}
	}
}

func TestSendProtocolMail_NoRouter(t *testing.T) {
	e := &Engineer{}
	if err := e.sendProtocolMail(nil); !errors.Is(err, errNoMailRouter) {
		t.Fatalf("sendProtocolMail without router = %v, want errNoMailRouter", err)
	}
	e.waitPendingMail() // no router: must not panic
}

func TestNudgePolecatAboutConflict_UnknownWorkerIsSilent(t *testing.T) {
	gtLog := installFakeGT(t)
	e, _, _ := scopedTestEngineer(t, t.TempDir(), nil)
	e.nudgePolecatAboutConflict(&MRInfo{ID: "gt-mr", Worker: "crew/joe"}, "conflict")
	if log := readLog(t, gtLog); log != "" {
		t.Fatalf("nudged someone for an unknown worker:\n%s", log)
	}
}

func TestLoadMRForMerge_Ineligibility(t *testing.T) {
	f := newMergeOneFixture(t)
	mrWith := func(mutate func(fields *beads.MRFields), labels ...string) *beadsdk.Issue {
		fields := &beads.MRFields{Branch: f.branch, Target: "main", SourceIssue: fixtureSource, Worker: "polecats/nux", Rig: "testrig", CommitSHA: f.commit}
		mutate(fields)
		return memIssue(fixtureMR, beads.FormatMRFields(fields), append([]string{"gt:merge-request"}, labels...)...)
	}
	for name, tc := range map[string]struct {
		issue *beadsdk.Issue
		want  string
	}{
		"owned-direct":          {mrWith(func(*beads.MRFields) {}, "gt:owned-direct"), "owned-direct"},
		"other rig":             {mrWith(func(m *beads.MRFields) { m.Rig = "otherrig" }), "belongs to rig otherrig"},
		"option-looking branch": {mrWith(func(m *beads.MRFields) { m.Branch = "-x" }), "not a plain branch name"},
		"branch is its target":  {mrWith(func(m *beads.MRFields) { m.Branch = "main" }), "its own target"},
		"short commit_sha":      {mrWith(func(m *beads.MRFields) { m.CommitSHA = f.commit[:8] }), "commit_sha"},
		"no MR fields":          {memIssue(fixtureMR, "", "gt:merge-request"), ""},
	} {
		t.Run(name, func(t *testing.T) {
			f.store.issues[fixtureMR] = tc.issue
			_, err := f.engineer(t).LoadMRForMerge(fixtureMR)
			if !errors.Is(err, ErrMRNotEligible) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("LoadMRForMerge = %v, want ErrMRNotEligible containing %q", err, tc.want)
			}
		})
	}
}

func TestMergeOne_IntegrationTargetMissingOnOriginIsPrecondition(t *testing.T) {
	f := newMergeOneFixture(t)
	f.store.issues[fixtureMR] = memMRIssue(fixtureMR, f.branch, "integration/epic-1", fixtureSource, f.commit)
	out := f.mergeOne(t)
	if out.Kind != OutcomePrecondition || !strings.Contains(out.Message, "origin/integration/epic-1 does not exist") {
		t.Fatalf("outcome = %s %q, want precondition (target missing on origin)", out.Kind, out.Message)
	}
	f.assertOpenAndUnclaimed(t)
}

func TestMergeOne_LocalTargetMissingIsCheckedOutFromOrigin(t *testing.T) {
	f := newMergeOneFixture(t)
	run(t, f.workDir, "git", "checkout", "--detach")
	run(t, f.workDir, "git", "branch", "-D", "main")
	if out := f.mergeOne(t); out.Kind != OutcomeMerged {
		t.Fatalf("outcome = %s %q, want merged (local main recreated from origin)", out.Kind, out.Message)
	}
}

func TestMergeOne_BehindLocalTargetNotCheckedOutIsMoved(t *testing.T) {
	f := newMergeOneFixture(t)
	behind := run(t, f.workDir, "git", "rev-parse", "main")
	writeFile(t, f.workDir, "later.txt", "later\n")
	run(t, f.workDir, "git", "add", ".")
	run(t, f.workDir, "git", "commit", "-m", "later on main")
	run(t, f.workDir, "git", "push", "origin", "main")
	run(t, f.workDir, "git", "checkout", "--detach")
	run(t, f.workDir, "git", "branch", "-f", "main", behind)
	if out := f.mergeOne(t); out.Kind != OutcomeMerged {
		t.Fatalf("outcome = %s %q, want merged after moving the local main up to origin", out.Kind, out.Message)
	}
}

func TestFinalizeMergeOne_CleanupFailures(t *testing.T) {
	t.Run("proof failed: MR stays open, rerun post-merge", func(t *testing.T) {
		workDir, g, _ := testGitRepo(t)
		installNoPRGH(t)
		branch := "polecat/test/not-landed"
		createFeatureBranch(t, workDir, branch, "nl.txt", "nl\n")
		commit := run(t, workDir, "git", "rev-parse", branch)
		e, _ := finalizeTestEngineer(t, workDir, g)
		out, terminal := e.finalizeMergeOne(MergeOutcome{MRID: "mr-nl"}, &MRInfo{ID: "mr-nl", Branch: branch, Target: "main", CommitSHA: commit},
			ProcessResult{Success: true, MergeCommit: commit})
		if out.Kind != OutcomeCleanupFailed || terminal {
			t.Fatalf("outcome %s terminal=%v, want cleanup_failed and not terminal", out.Kind, terminal)
		}
		if !strings.Contains(out.Message, "gt mq post-merge test-rig mr-nl") {
			t.Fatalf("message %q does not say to rerun post-merge", out.Message)
		}
	})
	t.Run("branch cleanup refused: MR closed, still cleanup_failed", func(t *testing.T) {
		workDir, g, _ := testGitRepo(t)
		installNoPRGH(t)
		commit, merge := landedOnMain(t, workDir, "polecat/test/landed")
		e, sender := finalizeTestEngineer(t, workDir, g)
		sender.err = errors.New("mail down")
		out, terminal := e.finalizeMergeOne(MergeOutcome{MRID: "mr-l"}, &MRInfo{ID: "mr-l", Branch: "polecat/test/$(x)", Target: "main", CommitSHA: commit},
			ProcessResult{Success: true, MergeCommit: merge})
		if out.Kind != OutcomeCleanupFailed || !terminal {
			t.Fatalf("outcome %s terminal=%v, want cleanup_failed with the MR closed", out.Kind, terminal)
		}
		if len(out.Warnings) == 0 || !strings.Contains(out.Warnings[0], "MERGED mail") {
			t.Fatalf("warnings = %v, want the MERGED mail failure", out.Warnings)
		}
	})
}

func TestVerifyMRInfoPostMergeProof_RefusesIncompleteMR(t *testing.T) {
	workDir, g, _ := testGitRepo(t)
	e, _ := finalizeTestEngineer(t, workDir, g)
	for name, mr := range map[string]*MRInfo{
		"nil MR":             nil,
		"no target":          {ID: "m", Branch: "polecat/a/b", CommitSHA: "abc"},
		"source is target":   {ID: "m", Branch: "main", Target: "main", CommitSHA: "abc"},
		"no commit":          {ID: "m", Branch: "polecat/a/b", Target: "main"},
		"commit not on main": {ID: "m", Branch: "polecat/a/b", Target: "main", CommitSHA: strings.Repeat("e", 40)},
	} {
		if err := e.verifyMRInfoPostMergeProof(mr); err == nil {
			t.Errorf("%s: proof = nil, want a refusal", name)
		}
	}
	e.git = nil
	if err := e.verifyMRInfoPostMergeProof(&MRInfo{Target: "main"}); err == nil {
		t.Error("proof without git = nil, want a refusal")
	}
}

func TestCleanupMergedBranch_Branches(t *testing.T) {
	workDir, g, _ := testGitRepo(t)
	e, _ := finalizeTestEngineer(t, workDir, g)
	if c := e.cleanupMergedBranch(&MRInfo{}); !c.NoBranch || c.Err != nil {
		t.Errorf("no branch: %+v", c)
	}
	e.config.DeleteMergedBranches = false
	if c := e.cleanupMergedBranch(&MRInfo{Branch: "feature/x", CommitSHA: "abc"}); !c.Disabled || c.Err != nil {
		t.Errorf("non-polecat with delete_merged_branches=false: %+v", c)
	}
	if c := e.cleanupMergedBranch(&MRInfo{Branch: "polecat/a/b"}); c.Err == nil {
		t.Errorf("missing commit_sha: %+v, want an error", c)
	}
	e.config.DeleteMergedBranches = true
	commit, _ := landedOnMain(t, workDir, "feature/x")
	if c := e.cleanupMergedBranch(&MRInfo{Branch: "feature/x", CommitSHA: commit}); !c.LocalDeleted || c.RemoteDeleted || c.Err != nil {
		t.Errorf("non-polecat branch: %+v, want local-only delete", c)
	}
	if err := e.deleteLocalBranchIfAt("", "x"); err == nil {
		t.Error("deleteLocalBranchIfAt without branch = nil")
	}
	if err := e.deleteLocalBranchIfAt("x", ""); err == nil {
		t.Error("deleteLocalBranchIfAt without commit = nil")
	}
}

func TestMergeOne_ReleaseFailureIsAWarning(t *testing.T) {
	f := newMergeOneFixture(t)
	writeFile(t, f.workDir, "dirty.txt", "x\n")
	e := f.engineer(t)
	failing := &failingAssigneeStore{memStore: f.store}
	e.beads = beads.NewWithBeadsDirAndStore(f.workDir, f.rigBeads, failing)
	out := e.MergeOne(context.Background(), fixtureMR)
	if out.Kind != OutcomePrecondition || len(out.Warnings) == 0 || !strings.Contains(out.Warnings[0], "release claim") {
		t.Fatalf("outcome = %+v, want precondition with a release-claim warning", out)
	}
}

// failingAssigneeStore accepts the claim but fails to clear it.
type failingAssigneeStore struct{ *memStore }

func (s *failingAssigneeStore) UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error {
	if v, ok := updates["assignee"]; ok && v == "" {
		return errors.New("store down")
	}
	return s.memStore.UpdateIssue(ctx, id, updates, actor)
}
