package beads

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeQueueHoldFor_ClassifiesEveryMRShape(t *testing.T) {
	installMergedMRBDStub(t, pendingMRRows, closedMRRows)
	b := New(t.TempDir())
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
		hold, mrID, err := b.MergeQueueHoldFor(c.issue, resolverFor(c.landed...))
		if err != nil || hold != c.wantHold || mrID != c.wantMR {
			t.Errorf("MergeQueueHoldFor(%s, landed=%v) = (%v, %q, %v), want (%v, %q)",
				c.issue, c.landed, hold, mrID, err, c.wantHold, c.wantMR)
		}
	}
}

func TestMergeQueueHoldFor_BareClosedMRNeedsAProver(t *testing.T) {
	installMergedMRBDStub(t, pendingMRRows, closedMRRows)
	if hold, _, err := New(t.TempDir()).MergeQueueHoldFor("gt-work4", nil); err != nil || hold != HoldNone {
		t.Fatalf("without a prover a bare-closed MR must not count as landed, got (%v, %v)", hold, err)
	}
}

func TestMergeQueueHoldFor_ReportsUnreadableQueue(t *testing.T) {
	installMergedMRBDStub(t, "", "")
	if _, _, err := New(t.TempDir()).MergeQueueHoldFor("gt-work1", resolverFor()); err == nil {
		t.Fatal("an unreadable merge queue must be an error so callers can fail closed")
	}
}

func TestMergeQueueHold_HeldAndString(t *testing.T) {
	if HoldNone.Held() || !HoldPending.Held() || !HoldLanded.Held() {
		t.Fatal("only HoldNone may be dispatched again")
	}
	for hold, want := range map[MergeQueueHold]string{HoldNone: "none", HoldPending: "pending", HoldLanded: "landed"} {
		if hold.String() != want {
			t.Errorf("%d.String() = %q, want %q", hold, hold.String(), want)
		}
	}
}

func TestParkWorkBead_ClearsAssigneeAndKeepsItOutOfReady(t *testing.T) {
	log := installMergedMRBDStub(t, pendingMRRows, closedMRRows)
	if err := New(t.TempDir()).ParkWorkBead("gt-work3"); err != nil {
		t.Fatalf("ParkWorkBead: %v", err)
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
	if !idx.Held("gt-work1", resolverFor()) || idx.Held("gt-work2", resolverFor()) {
		t.Fatal("index must hold merged work and release rejected work")
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
