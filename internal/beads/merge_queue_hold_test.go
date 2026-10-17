package beads

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMergeQueueHoldFor_ClassifiesEveryMRShape(t *testing.T) {
	installMergedMRBDStub(t, pendingMRRows, closedMRRows)
	cases := []struct {
		issue    string
		landed   []string
		wantHold MergeQueueHold
		wantMR   string
	}{
		{"gt-work1", nil, HoldLanded, "gt-wisp-merged"},           // close_reason merged
		{"gt-work4", []string{"c4"}, HoldLanded, "gt-wisp-bare"},  // bare bd close, commit on target
		{"gt-work4", nil, HoldNone, ""},                           // bare close, not on target
		{"gt-work2", nil, HoldNone, ""},                           // rejected, never landed
		{"gt-work3", nil, HoldPending, "gt-wisp-open"},            // open MR
		{"gt-work3", []string{"c3"}, HoldPending, "gt-wisp-open"}, // open MR: the refinery owns it even if its commit landed
		{"gt-unknown", nil, HoldNone, ""},                         // no MR at all
	}
	for _, c := range cases {
		hold, mrID, err := queueFor(t, c.landed...).Hold(c.issue)
		if err != nil || hold != c.wantHold || mrID != c.wantMR {
			t.Errorf("MergeQueueHoldFor(%s, landed=%v) = (%v, %q, %v), want (%v, %q)",
				c.issue, c.landed, hold, mrID, err, c.wantHold, c.wantMR)
		}
	}
}

func TestMergeQueueHoldFor_BareClosedMRNeedsAProver(t *testing.T) {
	installMergedMRBDStub(t, pendingMRRows, closedMRRows)
	idx, _ := New(t.TempDir()).MergeRequestsBySource()
	if hold, _, err := NewMergeQueue(idx, nil, nil).Hold("gt-work4"); err != nil || hold != HoldUnverified {
		t.Fatalf("without a prover a bare-closed MR is neither landed nor free, got (%v, %v)", hold, err)
	}
}

func TestMergeQueueHoldFor_ReportsUnreadableQueue(t *testing.T) {
	installMergedMRBDStub(t, "", "")
	if _, _, err := queueFor(t).Hold("gt-work1"); err == nil {
		t.Fatal("an unreadable merge queue must be an error so callers can fail closed")
	}
}

func TestMergeQueueHold_HeldAndString(t *testing.T) {
	if HoldNone.Held() || !HoldPending.Held() || !HoldLanded.Held() {
		t.Fatal("only HoldNone may be dispatched again")
	}
	if !HoldUnverified.Held() {
		t.Fatal("unverified landing must hold (fail closed)")
	}
	for hold, want := range map[MergeQueueHold]string{HoldNone: "none", HoldPending: "pending", HoldLanded: "landed", HoldUnverified: "unverified"} {
		if hold.String() != want {
			t.Errorf("%d.String() = %q, want %q", hold, hold.String(), want)
		}
	}
}

func TestParkWorkBead_ClearsAssigneeAndKeepsItOutOfReady(t *testing.T) {
	log := installMergedMRBDStub(t, pendingMRRows, closedMRRows)
	if parked, err := New(t.TempDir()).ParkWorkBead("gt-work3"); err != nil || !parked {
		t.Fatalf("ParkWorkBead = (%v, %v), want parked", parked, err)
	}
	if calls := readCalls(t, log); !strings.Contains(calls, "update gt-work3 --status=in_progress --assignee=") {
		t.Fatalf("expected park update, calls:\n%s", calls)
	}
}

func TestSettleHeldWorkBead(t *testing.T) {
	log := installMergedMRBDStub(t, pendingMRRows, closedMRRows)
	b := New(t.TempDir())
	if outcome, err := b.SettleHeldWorkBead("gt-work1", HoldLanded, "gt-wisp-merged"); err != nil || outcome != ReleaseClosed {
		t.Fatalf("landed: (%v, %v), want closed", outcome, err)
	}
	if outcome, err := b.SettleHeldWorkBead("gt-work3", HoldPending, "gt-wisp-open"); err != nil || outcome != ReleaseParked {
		t.Fatalf("pending: (%v, %v), want parked", outcome, err)
	}
	if outcome, err := b.SettleHeldWorkBead("gt-work2", HoldNone, ""); err == nil || outcome != "" {
		t.Fatalf("none: (%v, %v), want an error and no outcome", outcome, err)
	}
	calls := readCalls(t, log)
	if strings.Contains(calls, "gt-work2") {
		t.Fatalf("a bead that is not held must not be touched, calls:\n%s", calls)
	}
	if !strings.Contains(calls, "Merged in gt-wisp-merged") {
		t.Fatalf("landed close must name the MR, calls:\n%s", calls)
	}
}

func TestTownMergeQueueIndex_ReadsEveryBeadsDBOnce(t *testing.T) {
	log := installMergedMRBDStub(t, pendingMRRows, closedMRRows)
	town := t.TempDir()
	for _, dir := range []string{".beads", "gastown/.beads", "gastown/mayor/rig/.beads", "settings", ".hidden/.beads"} {
		if err := os.MkdirAll(filepath.Join(town, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	dirs, err := TownBeadsSearchDirs(town)
	if err != nil {
		t.Fatalf("TownBeadsSearchDirs: %v", err)
	}
	want := []string{town, filepath.Join(town, "gastown"), filepath.Join(town, "gastown", "mayor", "rig")}
	if strings.Join(dirs, "|") != strings.Join(want, "|") {
		t.Fatalf("dirs = %v, want %v", dirs, want)
	}
	idx, err := TownMergeQueueIndex(town)
	if err != nil {
		t.Fatalf("TownMergeQueueIndex: %v", err)
	}
	if hold, mrID := idx.Hold("gt-work3", resolverFor()); hold != HoldPending || mrID != "gt-wisp-open" {
		t.Fatalf("Hold(gt-work3) = (%v, %q), want pending gt-wisp-open", hold, mrID)
	}
	if h, _ := idx.Hold("gt-work1", resolverFor()); !h.Held() {
		t.Fatal("index must hold merged work")
	}
	if h, _ := idx.Hold("gt-work2", resolverFor()); h.Held() {
		t.Fatal("index must release rejected work whose commit is provably not on the target")
	}
	// Three distinct beads DBs, two queries (pending + closed) each.
	if n := strings.Count(readCalls(t, log), "sql"); n != 6 {
		t.Fatalf("expected 6 merge-queue queries (3 DBs x 2), got %d", n)
	}
}

func TestTownMergeQueueIndex_FailsWhenAnyQueueUnreadable(t *testing.T) {
	installMergedMRBDStub(t, "", "")
	if _, err := TownMergeQueueIndex(t.TempDir()); err == nil {
		t.Fatal("an unreadable merge queue must be an error")
	}
}

// writeShow makes the bd stub answer `show <id>` with the given status.
func writeShow(t *testing.T, id, status string) {
	t.Helper()
	bdPath, err := exec.LookPath("bd")
	if err != nil {
		t.Fatal(err)
	}
	body := `[{"id":"` + id + `","status":"` + status + `"}]`
	if err := os.WriteFile(filepath.Join(filepath.Dir(bdPath), "show-"+id+".json"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

// gt-pd1c.59 review #6: a bead the refinery just closed is never reopened or
// parked again by a late release.
func TestParkAndRelease_LeaveTerminalBeadAlone(t *testing.T) {
	log := installMergedMRBDStub(t, pendingMRRows, closedMRRows)
	writeShow(t, "gt-work3", "closed")
	writeShow(t, "gt-work2", "closed")
	b := New(t.TempDir())
	if parked, err := b.ParkWorkBead("gt-work3"); err != nil || parked {
		t.Fatalf("ParkWorkBead(closed) = (%v, %v), want no-op", parked, err)
	}
	if outcome, err := b.ReleaseWorkBead("gt-work3", queueFor(t)); err != nil || outcome != ReleaseUnchanged {
		t.Fatalf("pending release of closed bead = (%v, %v), want unchanged", outcome, err)
	}
	if outcome, err := b.ReleaseWorkBead("gt-work2", queueFor(t)); err != nil || outcome != ReleaseUnchanged {
		t.Fatalf("reopen of closed bead = (%v, %v), want unchanged", outcome, err)
	}
	if calls := readCalls(t, log); strings.Contains(calls, "update ") {
		t.Fatalf("terminal beads must not be written, calls:\n%s", calls)
	}
}

// gt-pd1c.59 review #1: the MR lives in the polecat's rig DB, the source bead
// in another DB (hq-). The town-wide queue still sees the MR, so releasing the
// source parks it instead of reopening it.
func TestReleaseWorkBead_SeesMRInAnotherDB(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bd stub is a shell script")
	}
	ResetBdAllowStaleCacheForTest()
	t.Cleanup(ResetBdAllowStaleCacheForTest)
	town := t.TempDir()
	for _, dir := range []string{".beads", "gastown/.beads"} {
		if err := os.MkdirAll(filepath.Join(town, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	bin := t.TempDir()
	logPath := filepath.Join(bin, "calls.log")
	rigDB := filepath.Join(town, "gastown", ".beads")
	script := `#!/bin/sh
if [ "${1:-}" = "--allow-stale" ]; then
  if [ "${2:-}" = "version" ]; then echo "Error: unknown flag: --allow-stale" >&2; exit 0; fi
  shift
fi
echo "$BEADS_DIR $*" >> "` + logPath + `"
case "${1:-}" in
  show) printf '[{"id":"%s","status":"in_progress"}]\n' "${2}" ;;
  sql)
    if [ "$BEADS_DIR" = "` + rigDB + `" ] && ! echo "$*" | grep -q "w.status IN ('closed'"; then
      cat "` + bin + `/mr.json"
    else
      echo '[]'
    fi ;;
  *) echo '[]' ;;
esac
exit 0
`
	if err := os.WriteFile(filepath.Join(bin, "bd"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	mr := `[{"id":"gt-wisp-mr9","status":"open","description":"branch: polecat/a/hq-work9\nsource_issue: hq-work9\n"}]`
	if err := os.WriteFile(filepath.Join(bin, "mr.json"), []byte(mr), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	// The single-DB view (the town DB holding the source) has no MR...
	if found, err := NewWithBeadsDir(town, filepath.Join(town, ".beads")).MergeRequestsBySource(); err != nil || len(found["hq-work9"]) != 0 {
		t.Fatalf("precondition: town DB must not hold the MR, got (%v, %v)", found, err)
	}
	// ...the town-wide queue does.
	q := ReadMergeQueue(town, nil)
	if hold, mrID, err := q.Hold("hq-work9"); err != nil || hold != HoldPending || mrID != "gt-wisp-mr9" {
		calls, _ := os.ReadFile(logPath)
		t.Fatalf("Hold = (%v, %q, %v), want pending gt-wisp-mr9; calls:\n%s", hold, mrID, err, calls)
	}
	outcome, err := NewWithBeadsDir(town, filepath.Join(town, ".beads")).ReleaseWorkBead("hq-work9", q)
	if err != nil || outcome != ReleaseParked {
		t.Fatalf("ReleaseWorkBead = (%v, %v), want parked", outcome, err)
	}
	data, _ := os.ReadFile(logPath)
	if strings.Contains(string(data), "--status=open") {
		t.Fatalf("source with a pending MR in another DB must not be reopened:\n%s", data)
	}
}

// gt-pd1c.59 review #2: one guard for gt done and gt mq submit.
func TestCheckSubmission(t *testing.T) {
	installMergedMRBDStub(t, pendingMRRows, closedMRRows)
	q := queueFor(t)
	if err := q.CheckSubmission("gt-work3", "polecat/b/gt-work3"); err == nil {
		t.Fatal("another branch must not submit work held by a pending MR")
	}
	if err := q.CheckSubmission("gt-work3", "polecat/c/gt-work3"); err != nil {
		t.Fatalf("the owner's branch may resubmit: %v", err)
	}
	if err := q.CheckSubmission("gt-work1", "polecat/z/gt-work1"); err == nil {
		t.Fatal("landed work must not be submitted again from another branch")
	}
	if err := q.CheckSubmission("gt-work2", "polecat/z/gt-work2"); err != nil {
		t.Fatalf("rejected work is free: %v", err)
	}
	if err := q.CheckSubmission("gt-new", "polecat/z/gt-new"); err != nil {
		t.Fatalf("unheld work is free: %v", err)
	}
	if err := NewMergeQueue(nil, os.ErrPermission, nil).CheckSubmission("gt-new", "b"); err == nil {
		t.Fatal("an unreadable queue must refuse (fail closed)")
	}
}

// gt-pd1c.59 round 3 #1: an MR closed by hand (no close_reason) whose landing
// git cannot decide (no prover, or the prover fails) leaves its bead parked,
// never reopened; the release says why.
func TestReleaseWorkBead_ParksWhenLandingCannotBeDisproved(t *testing.T) {
	for name, resolve := range map[string]ProverResolver{
		"no prover":     nil,
		"prover errors": func(string) MergeProver { return failingProver{} },
	} {
		t.Run(name, func(t *testing.T) {
			log := installMergedMRBDStub(t, pendingMRRows, closedMRRows)
			idx, _ := New(t.TempDir()).MergeRequestsBySource()
			q := NewMergeQueue(idx, nil, resolve)
			if hold, mrID, err := q.Hold("gt-work4"); err != nil || hold != HoldUnverified || mrID != "gt-wisp-bare" {
				t.Fatalf("Hold = (%v, %q, %v), want unverified gt-wisp-bare", hold, mrID, err)
			}
			outcome, err := New(t.TempDir()).ReleaseWorkBead("gt-work4", q)
			if outcome != ReleaseParked || !errors.Is(err, ErrLandingUnverified) {
				t.Fatalf("ReleaseWorkBead = (%v, %v), want parked with ErrLandingUnverified", outcome, err)
			}
			calls := readCalls(t, log)
			if strings.Contains(calls, "--status=open") || strings.Contains(calls, "\nclose ") {
				t.Fatalf("unverifiable work must be neither reopened nor closed:\n%s", calls)
			}
		})
	}
}

// round 3 #4: a landed bead that is already closed is not force-closed again.
func TestSettleHeldWorkBead_LandedLeavesClosedBeadAlone(t *testing.T) {
	log := installMergedMRBDStub(t, pendingMRRows, closedMRRows)
	writeShow(t, "gt-work1", "closed")
	if outcome, err := New(t.TempDir()).SettleHeldWorkBead("gt-work1", HoldLanded, "gt-wisp-merged"); err != nil || outcome != ReleaseUnchanged {
		t.Fatalf("Settle(closed) = (%v, %v), want unchanged", outcome, err)
	}
	if calls := readCalls(t, log); strings.Contains(calls, "Merged in") {
		t.Fatalf("closed bead must not be closed again:\n%s", calls)
	}
}

func TestIsCommitNotOnTarget(t *testing.T) {
	if !IsCommitNotOnTarget(notOnTarget{}) || IsCommitNotOnTarget(os.ErrDeadlineExceeded) || IsCommitNotOnTarget(nil) {
		t.Fatal("only a definite not-on-target answer counts")
	}
}

// round 4 (B1): an unverified hold explains itself and points to the exit.
func TestMergeQueue_UnverifiedExplainsAndGuardPointsToExit(t *testing.T) {
	installMergedMRBDStub(t, pendingMRRows, closedMRRows)
	idx, _ := New(t.TempDir()).MergeRequestsBySource()
	q := NewMergeQueue(idx, nil, nil)
	mrID, rig, reason := q.Unverified("gt-work4")
	if mrID != "gt-wisp-bare" || rig != "gastown" || !strings.Contains(reason, "no git prover") {
		t.Fatalf("Unverified = (%q, %q, %q)", mrID, rig, reason)
	}
	if mrID, _, _ := q.Unverified("gt-work3"); mrID != "" {
		t.Fatal("a pending hold is not unverified")
	}
	err := q.CheckSubmission("gt-work4", "polecat/x/gt-work4")
	if err == nil || strings.Contains(err.Error(), "until the MR merges") || !strings.Contains(err.Error(), "--confirm-not-landed") {
		t.Fatalf("unverified refusal = %v, want the manual exit, not 'until the MR merges'", err)
	}
}

// round 4 (B1): a recorded human verdict is proof for the one rule.
func TestMergeQueueHold_HumanVerdictDecides(t *testing.T) {
	notLanded := []*Issue{{ID: "mr1", Status: "closed", Description: "source_issue: gt-x\ntarget: main\ncommit_sha: c1\nlanding: not_landed\n"}}
	landed := []*Issue{{ID: "mr1", Status: "closed", Description: "source_issue: gt-x\ntarget: main\ncommit_sha: c1\nlanding: landed\n"}}
	if hold, _ := MergeQueueHoldOf(notLanded, nil); hold != HoldNone {
		t.Fatalf("not_landed verdict: hold = %v, want none", hold)
	}
	if hold, _ := MergeQueueHoldOf(landed, nil); hold != HoldLanded {
		t.Fatalf("landed verdict: hold = %v, want landed", hold)
	}
	open := []*Issue{{ID: "mr1", Status: "open", Description: "source_issue: gt-x\nlanding: not_landed\n"}}
	if hold, _ := MergeQueueHoldOf(open, nil); hold != HoldPending {
		t.Fatalf("a verdict never overrides an open MR, got %v", hold)
	}
	if out := ParseMRFields(&Issue{Description: FormatMRFields(&MRFields{Branch: "b", Landing: LandingLanded, LandingBy: "overseer"})}); out == nil || out.Landing != "landed" || out.LandingBy != "overseer" {
		t.Fatalf("landing fields do not round-trip: %+v", out)
	}
}
