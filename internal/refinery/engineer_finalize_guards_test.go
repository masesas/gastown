package refinery

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
)

// M4 (security): which merged-source outcomes are by design and which fail.
func TestCloseMergedWorkBead_ClassifiesTheSourceOutcome(t *testing.T) {
	closedWith := func(reason string) *beads.Issue {
		is := workIssue("gt-src", string(beads.StatusClosed))
		is.CloseReason = reason
		return is
	}
	noMerge := workIssue("gt-src", string(beads.StatusOpen))
	noMerge.Description = "no_merge: true"
	nonConcrete := workIssue("gt-src", string(beads.StatusOpen))
	nonConcrete.Labels = []string{"gt:merge-request"}

	for name, tc := range map[string]struct {
		issue    *beads.Issue // nil: no source bead in the store
		source   string
		closeErr error
		skip     bool
		fail     bool
	}{
		"closed now":                        {issue: workIssue("gt-src", string(beads.StatusOpen)), source: "gt-src"},
		"no source bead (by design)":        {source: "", skip: true},
		"no_merge source (by design)":       {issue: noMerge, source: "gt-src", skip: true},
		"already closed Merged in other MR": {issue: closedWith("Merged in gt-mr-old"), source: "gt-src"},
		"already closed with free text":     {issue: closedWith("landed on main as 9473f11 via MR x"), source: "gt-src", fail: true},
		"named source missing":              {source: "gt-src", fail: true},
		"not a concrete work bead":          {issue: nonConcrete, source: "gt-src", fail: true},
		"close fails":                       {issue: workIssue("gt-src", string(beads.StatusOpen)), source: "gt-src", closeErr: errors.New("down"), fail: true},
	} {
		t.Run(name, func(t *testing.T) {
			work := newFakeWorkBeadStore()
			if tc.issue != nil {
				work.add(tc.issue)
			}
			work.closeErr = tc.closeErr
			r := closeMergedWorkBead(work, nil, nil, mergedWorkBeadCloseRequest{MRID: "gt-mr", SourceIssue: tc.source, Target: "main"})
			err := r.mergedErr()
			if tc.fail != (err != nil) {
				t.Fatalf("mergedErr = %v, want failure=%v (result %+v)", err, tc.fail, r)
			}
			if tc.skip != (r.Skip != "") {
				t.Fatalf("Skip = %q, want skip=%v", r.Skip, tc.skip)
			}
		})
	}
}

func TestFinalizeReport_SourceOutcome(t *testing.T) {
	failed := FinalizeReport{SourceErr: errors.New("source not closed")}
	if failed.OK() || !failed.MRClosed() || !strings.Contains(failed.Err().Error(), "Merged in") {
		t.Fatalf("source failure: OK=%v MRClosed=%v Err=%v, want not OK, MR closed", failed.OK(), failed.MRClosed(), failed.Err())
	}
	if skipped := (FinalizeReport{SourceSkip: "no source bead"}); !skipped.OK() {
		t.Fatal("a by-design skip made the report fail")
	}
}

func TestMergeOne_ReportsGatesRan(t *testing.T) {
	f := newMergeOneFixture(t)
	f.settings(t, map[string]any{"lint_command": "true"})
	out := f.mergeOne(t)
	if out.Kind != OutcomeMerged || out.GatesRan != 2 {
		t.Fatalf("outcome = %s gates_ran=%d, want merged with 2 gates (lint, test)", out.Kind, out.GatesRan)
	}
	data, err := json.Marshal(out)
	if err != nil || !strings.Contains(string(data), `"gates_ran":2`) {
		t.Fatalf("JSON %s lacks gates_ran", data)
	}
}

// M3 (go): a new commit is reported again; L1: the text is labeled and clean.
func TestSendFixNeeded_NewCommitMailsAgainAndTextIsSanitized(t *testing.T) {
	f := newMergeOneFixture(t)
	e := f.engineer(t)
	mr, err := e.LoadMRForMerge(fixtureMR)
	if err != nil {
		t.Fatal(err)
	}
	if sent, err := e.sendFixNeeded(mr, "tests", "boom\x1b[31m\nsecond line"); !sent || err != nil {
		t.Fatalf("first send = %v, %v", sent, err)
	}
	if sent, _ := e.sendFixNeeded(mr, "tests", "boom"); sent {
		t.Fatal("same commit_sha mailed twice")
	}
	if fields := beads.ParseMRFields(&beads.Issue{Description: f.store.issue(t, fixtureMR).Description}); fields.FixNeededFor != f.commit+":tests" {
		t.Fatalf("fix_needed_for = %q, want %s:tests", fields.FixNeededFor, f.commit)
	}
	if sent, _ := e.sendFixNeeded(mr, "scope", "outside"); !sent {
		t.Fatal("a different failure type on the same commit_sha was not reported")
	}
	mr.CommitSHA = strings.Repeat("a", 40)
	if sent, _ := e.sendFixNeeded(mr, "tests", "boom"); !sent {
		t.Fatal("a new commit_sha was not reported")
	}
	body := f.sender.find("FIX_NEEDED").Body
	if !strings.Contains(body, "Error: "+untrustedErrorLabel+"boom[31m second line") || strings.ContainsAny(body, "\x1b") {
		t.Fatalf("FIX_NEEDED body not labeled/sanitized:\n%q", body)
	}
}

func TestPolecatNameFromWorker_PlainNamesOnly(t *testing.T) {
	for _, bad := range []string{"polecats/-x", "polecats/a+b", "polecats/a@b", "polecats/.x", "polecats/a\x1bb"} {
		if got := polecatNameFromWorker(bad); got != "" {
			t.Errorf("polecatNameFromWorker(%q) = %q, want \"\"", bad, got)
		}
	}
}

func TestVerifyMergeShape_RefusesOctopusAndForeignBase(t *testing.T) {
	workDir, g, _ := testGitRepo(t)
	e, _ := finalizeTestEngineer(t, workDir, g)
	base := run(t, workDir, "git", "rev-parse", "main")
	createFeatureBranch(t, workDir, "polecat/a/1", "a1.txt", "1\n")
	createFeatureBranch(t, workDir, "polecat/a/2", "a2.txt", "2\n")
	h1 := run(t, workDir, "git", "rev-parse", "polecat/a/1")
	run(t, workDir, "git", "merge", "--no-ff", "-q", "-m", "octopus", "polecat/a/1", "polecat/a/2")
	if err := e.verifyMergeShape(run(t, workDir, "git", "rev-parse", "HEAD"), base, []string{h1}); err == nil {
		t.Fatal("octopus merge accepted")
	}
	run(t, workDir, "git", "reset", "-q", "--hard", base)
	run(t, workDir, "git", "merge", "--no-ff", "-q", "-m", "m1", "polecat/a/1")
	tip := run(t, workDir, "git", "rev-parse", "HEAD")
	if err := e.verifyMergeShape(tip, base, []string{h1}); err != nil {
		t.Fatalf("a correct merge was refused: %v", err)
	}
	if err := e.verifyMergeShape(tip, h1, []string{h1}); err == nil {
		t.Fatal("merge accepted on the wrong base")
	}
	if err := e.verifyMergeShape(base, base, []string{h1}); err == nil {
		t.Fatal("a tip that does not contain the head was accepted")
	}
}

func TestMergeOne_ContextAndLockReleasedForNextRun(t *testing.T) {
	f := newMergeOneFixture(t)
	f.settings(t, map[string]any{"test_command": "exit 1"})
	_ = f.mergeOne(t)
	f.settings(t, nil)
	if out := f.engineer(t).MergeOne(context.Background(), fixtureMR); out.Kind != OutcomeMerged {
		t.Fatalf("second run = %s %q: the lock or claim of the first run was not released", out.Kind, out.Message)
	}
}
