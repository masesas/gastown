package beads

import (
	"os"
	"strings"
	"testing"
)

// gt-pd1c.78 tahap 2 (F7): the recorded merged signal (close_reason=merged,
// merge_commit) is cross-checked with git in the one landing predicate. Bead
// fields can be edited with bd update; git decides whenever it can.
func TestLandingVerdictRecordedCrossCheckedWithGit(t *testing.T) {
	sha := "e" + strings.Repeat("0", 39)
	desc := func(extra string) string {
		return "branch: polecat/x\ntarget: main\nsource_issue: gt-x\nrig: gastown\ncommit_sha: " + sha + "\n" + extra
	}
	cases := []struct {
		name     string
		extra    string
		prover   MergeProver
		verdict  landingVerdict
		evidence LandingEvidence
		reason   string // substring of the unverified reason
	}{
		{name: "recorded merged, git confirms: landed by git",
			extra: "close_reason: merged\n", prover: fakeProver{sha: true}, verdict: verdictLanded, evidence: EvidenceGit},
		{name: "recorded merge_commit, git confirms: landed by git",
			extra: "merge_commit: abc\n", prover: fakeProver{sha: true}, verdict: verdictLanded, evidence: EvidenceGit},
		{name: "recorded merged, git proves not landed: unverified (forged or contradicted)",
			extra: "close_reason: merged\n", prover: fakeProver{}, verdict: verdictUnknown, reason: "recorded as merged"},
		{name: "recorded merge_commit, git proves not landed: unverified",
			extra: "merge_commit: abc\n", prover: fakeProver{}, verdict: verdictUnknown, reason: "recorded as merged"},
		{name: "contradiction resolved by a human verdict: not landed",
			extra: "close_reason: merged\nlanding: not_landed\n", prover: fakeProver{}, verdict: verdictNotLanded, evidence: EvidenceHumanVerdict},
		{name: "contradiction resolved by a human verdict: landed",
			extra: "close_reason: merged\nlanding: landed\n", prover: fakeProver{}, verdict: verdictLanded, evidence: EvidenceHumanVerdict},
		{name: "recorded merged, git cannot decide: landed by the record",
			extra: "close_reason: merged\n", prover: failingProver{}, verdict: verdictLanded, evidence: EvidenceRecorded},
		{name: "recorded merged, no prover: landed by the record",
			extra: "close_reason: merged\n", prover: nil, verdict: verdictLanded, evidence: EvidenceRecorded},
		{name: "not recorded, git proves not landed: not landed by git",
			extra: "", prover: fakeProver{}, verdict: verdictNotLanded, evidence: EvidenceGit},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fields := ParseMRFields(&Issue{Description: desc(c.extra)})
			verdict, reason, evidence := mrLandingVerdictReason(StatusClosed, fields, c.prover)
			if verdict != c.verdict || evidence != c.evidence {
				t.Fatalf("verdict=%v evidence=%q reason=%q, want %v/%q", verdict, evidence, reason, c.verdict, c.evidence)
			}
			if c.reason != "" && !strings.Contains(reason, c.reason) {
				t.Fatalf("reason %q must contain %q", reason, c.reason)
			}
		})
	}
}

// F7 through the shared predicate: a forged merged record on a bead whose
// commit git proves is not on the target holds the bead as unverified, so the
// witness no longer settles (closes) it as merged.
func TestMergeQueueHoldForgedMergedRecord(t *testing.T) {
	sha := "f" + strings.Repeat("0", 39)
	forged := &Issue{ID: "mr-f", Status: "closed", Description: "branch: polecat/f\ntarget: main\nsource_issue: gt-f\nrig: gastown\ncommit_sha: " + sha + "\nclose_reason: merged\n"}
	q := NewMergeQueue(MergeQueueIndex{"gt-f": {forged}}, nil, func(string) MergeProver { return fakeProver{} })
	d, err := q.Detail("gt-f")
	if err != nil || d.Hold != HoldUnverified || !strings.Contains(d.Reason, "recorded as merged") {
		t.Fatalf("forged merged record: %+v %v, want unverified with the contradiction as reason", d, err)
	}
}

func TestMergeQueueTipDisposition(t *testing.T) {
	tip := "a" + strings.Repeat("1", 39)
	other := "b" + strings.Repeat("2", 39)
	mr := func(id, status, branch, commit, extra string) *Issue {
		return &Issue{ID: id, Status: status, Description: "branch: " + branch + "\ntarget: main\nsource_issue: gt-" + id +
			"\nrig: gastown\ncommit_sha: " + commit + "\n" + extra}
	}
	idx := MergeQueueIndex{}
	add := func(issues ...*Issue) {
		for _, i := range issues {
			src := ParseMRFields(i).SourceIssue
			idx[src] = append(idx[src], i)
		}
	}
	add(
		mr("open", "open", "polecat/open", tip, ""),
		mr("landed", "closed", "polecat/landed", tip, ""),
		mr("landed-old", "closed", "polecat/later", other, ""),
		mr("rej", "closed", "polecat/rej", tip, "close_reason: rejected\n"),
		mr("sup", "closed", "polecat/sup", tip, "close_reason: superseded\n"),
		mr("con", "closed", "polecat/con", tip, "close_reason: conflict\n"),
		mr("raw", "closed", "polecat/raw", tip, ""),
		mr("unv", "closed", "polecat/unv", tip, ""),
		mr("rej-old", "closed", "polecat/moved", other, "close_reason: rejected\n"),
		mr("mixed-a", "closed", "polecat/mixed", tip, "close_reason: superseded\n"),
		mr("mixed-b", "closed", "polecat/mixed", tip, "close_reason: conflict\n"),
		mr("case", "closed", "polecat/case", strings.ToUpper(tip), "close_reason:  Rejected \n"),
	)
	// Git: "landed" and "landed-old" commits are on the target; the
	// "unv" MR's rig has no prover (git cannot decide).
	landedSHAs := fakeProver{}
	resolve := func(rig string) MergeProver { return landedSHAs }
	q := NewMergeQueue(idx, nil, resolve)
	landedSHAs[other] = true // landed-old
	cases := []struct {
		branch, tip string
		want        TipClass
		mrID        string
		abandon     bool
	}{
		{"polecat/open", tip, TipOpen, "open", false},
		{"polecat/rej", tip, TipRejected, "rej", true},
		{"polecat/sup", tip, TipSuperseded, "sup", true},
		{"polecat/con", tip, TipConflict, "con", false},
		{"polecat/raw", tip, TipNoVerdict, "raw", false},
		{"polecat/never", tip, TipNoMR, "", false},
		{"polecat/later", tip, TipNoMR, "", false},               // landed MR, later commits never submitted
		{"polecat/later", other, TipLanded, "landed-old", false}, // tip is the landed commit
		{"polecat/moved", tip, TipNoMR, "", false},               // rejected MR was for an older commit
		{"polecat/mixed", tip, TipConflict, "mixed-b", false},    // strictest verdict wins
		{"polecat/case", tip, TipRejected, "case", true},
	}
	for _, c := range cases {
		d, err := q.TipDisposition(c.branch, c.tip)
		if err != nil {
			t.Fatalf("%s: %v", c.branch, err)
		}
		if d.Class != c.want || d.MRID != c.mrID || d.Abandonable() != c.abandon {
			t.Errorf("TipDisposition(%s, %.7s) = %+v, want %s/%q abandon=%v", c.branch, c.tip, d, c.want, c.mrID, c.abandon)
		}
	}

	unverified := NewMergeQueue(MergeQueueIndex{"gt-unv": idx["gt-unv"]}, nil, nil)
	if d, err := unverified.TipDisposition("polecat/unv", tip); err != nil || d.Class != TipUnverified || d.Abandonable() {
		t.Fatalf("no prover: %+v %v, want unverified", d, err)
	}
	if _, err := NewMergeQueue(nil, os.ErrPermission, nil).TipDisposition("polecat/x", tip); err == nil {
		t.Fatal("an unread queue must be an error (fail closed)")
	}
	if _, err := q.TipDisposition("", tip); err == nil {
		t.Fatal("an empty branch must be an error")
	}
}

func TestAbandonCommand(t *testing.T) {
	if got := AbandonCommand("gastown", "refs/heads/polecat/nux/gt-1"); got != `gt polecat abandon gastown/polecat/nux/gt-1 --reason "<why>"` {
		t.Fatalf("AbandonCommand = %q", got)
	}
	for _, tc := range []struct{ rig, branch string }{
		{"gastown; rm -rf ~", "polecat/nux"},
		{"gastown", "polecat/nux; curl evil"},
		{"gastown", "main"},
		{"gastown", "polecat/$(id)"},
	} {
		got := AbandonCommand(tc.rig, tc.branch)
		if strings.ContainsAny(got, ";$()") || strings.Contains(got, "curl") || strings.Contains(got, " main") {
			t.Errorf("(%q, %q): raw value leaked into %q", tc.rig, tc.branch, got)
		}
	}
}
