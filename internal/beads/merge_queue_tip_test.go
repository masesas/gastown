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
	// A real generated polecat branch (polecat/<name>/<issue>+<suffix>) is
	// offered as is, never as the <polecat-branch> placeholder.
	if got := AbandonCommand("posin", "polecat/s2test/kp-s2b+t1"); got != `gt polecat abandon posin/polecat/s2test/kp-s2b+t1 --reason "<why>"` {
		t.Fatalf("AbandonCommand(real branch) = %q", got)
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

// The pilot shape: an open MR recorded the exact tip of a real generated
// branch (with '+'); the queue says open, never "no MR recorded", however
// the branch is spelled (refs/heads/ prefix, surrounding space).
func TestTipDispositionRealBranchName(t *testing.T) {
	tip := "c" + strings.Repeat("d", 39)
	branch := "polecat/s2test/kp-s2b+t1"
	open := &Issue{ID: "kp-wisp-mr1", Status: "open", Description: "branch: " + branch + "\ntarget: main\nsource_issue: kp-wak\ncommit_sha: " + tip + "\n"}
	q := NewMergeQueue(MergeQueueIndex{"kp-wak": {open}}, nil, nil)
	for _, spelled := range []string{branch, "refs/heads/" + branch, " " + branch + " "} {
		d, err := q.TipDisposition(spelled, tip)
		if err != nil || d.Class != TipOpen || d.MRID != "kp-wisp-mr1" || !d.BranchOpen() {
			t.Fatalf("TipDisposition(%q) = %+v %v, want open (MR kp-wisp-mr1)", spelled, d, err)
		}
	}
}

// noNewWorkProver: the commit is an ancestor of base_sha (no new work) and
// reachable from the target.
type noNewWorkProver struct{}

func (noNewWorkProver) VerifyPushedCommitReachableFromPushTarget(remote, branch, commit string) error {
	return nil
}
func (noNewWorkProver) IsAncestor(ancestor, descendant string) (bool, error) { return true, nil }

// F7 round 1: an MR that records a merge but carried no new work (its commit
// already in base_sha) is not a contradiction: reachability confirms the
// record. Without a record the no-new-work guard still says not landed.
func TestLandingVerdictRecordedNoNewWork(t *testing.T) {
	sha := "c" + strings.Repeat("0", 39)
	base := "b" + strings.Repeat("0", 39)
	desc := "branch: polecat/x\ntarget: main\nsource_issue: gt-x\nrig: gastown\ncommit_sha: " + sha + "\nbase_sha: " + base + "\n"
	recorded := ParseMRFields(&Issue{Description: desc + "close_reason: merged\n"})
	if v, reason, ev := mrLandingVerdictReason(StatusClosed, recorded, noNewWorkProver{}); v != verdictLanded || ev != EvidenceGit {
		t.Fatalf("recorded, no new work: %v %q %q, want landed by git", v, reason, ev)
	}
	bare := ParseMRFields(&Issue{Description: desc})
	if v, _, ev := mrLandingVerdictReason(StatusClosed, bare, noNewWorkProver{}); v != verdictNotLanded || ev != EvidenceGit {
		t.Fatalf("bare close, no new work: %v %q, want not landed", v, ev)
	}
}

// gt-pd1c.78 N2: classification is per tip. A rejected MR for T1 stays
// rejected while MR2 for T2 is open on the same branch (but the branch is
// open, so not abandonable); an MR without source_issue is still seen.
func TestTipDispositionPerTip(t *testing.T) {
	t1 := "1" + strings.Repeat("a", 39)
	t2 := "2" + strings.Repeat("a", 39)
	mr := func(id, status, commit, extra string) *Issue {
		return &Issue{ID: id, Status: status, Description: "branch: polecat/n\ntarget: main\nsource_issue: gt-src\nrig: r\ncommit_sha: " + commit + "\n" + extra}
	}
	q := NewMergeQueue(MergeQueueIndex{"gt-src": {mr("mr-1", "closed", t1, "close_reason: rejected\n"), mr("mr-2", "open", t2, "")}},
		nil, func(string) MergeProver { return fakeProver{} })
	d1, err := q.TipDisposition("polecat/n", t1)
	if err != nil || d1.Class != TipRejected || !d1.BranchOpen() || d1.Abandonable() || d1.MRID != "mr-1" {
		t.Fatalf("T1: %+v %v, want rejected, branch open, not abandonable", d1, err)
	}
	if d2, _ := q.TipDisposition("polecat/n", t2); d2.Class != TipOpen || d2.MRID != "mr-2" {
		t.Fatalf("T2: %+v, want open", d2)
	}
	if d0, _ := q.TipDisposition("polecat/n", strings.Repeat("3", 40)); d0.Class != TipNoMR || !d0.BranchOpen() {
		t.Fatalf("unsubmitted tip on an open branch: %+v", d0)
	}

	// An MR without source_issue (indexed under sourcelessKey) still counts.
	installMergedMRBDStub(t, `[{"id":"mr-ns","status":"open","description":"branch: polecat/ns\ntarget: main\ncommit_sha: `+t1+`\n"}]`, `[]`)
	idx, err := New(t.TempDir()).MergeRequestsBySource()
	if err != nil {
		t.Fatal(err)
	}
	if d, _ := NewMergeQueue(idx, nil, nil).TipDisposition("polecat/ns", t1); d.Class != TipOpen || d.MRID != "mr-ns" {
		t.Fatalf("sourceless MR: %+v, want open", d)
	}
	if hold, _, _ := NewMergeQueue(idx, nil, nil).Hold(""); hold != HoldNone {
		t.Fatal("sourceless MRs are no bead's hold")
	}
}
