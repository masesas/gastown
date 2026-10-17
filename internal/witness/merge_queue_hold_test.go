package witness

import (
	"errors"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
)

// stubMergeQueueHold makes the witness see the given merge-queue verdict for
// every bead (not parallel-safe: overrides a package-level hook).
func stubMergeQueueHold(t *testing.T, hold beads.MergeQueueHold, mrID string, err error) {
	t.Helper()
	old := witnessMergeQueue
	witnessMergeQueue = func(string) *beads.MergeQueue { return heldQueue(hold, mrID, err) }
	t.Cleanup(func() { witnessMergeQueue = old })
}

// heldQueue is a merge queue in which every bead has the given hold.
func heldQueue(hold beads.MergeQueueHold, mrID string, err error) *beads.MergeQueue {
	return beads.NewMergeQueue(holdIndex{hold: hold, mrID: mrID}.index(), err, nil)
}

type holdIndex struct {
	hold beads.MergeQueueHold
	mrID string
}

// index gives every bead ID used by the witness tests the configured hold.
func (h holdIndex) index() beads.MergeQueueIndex {
	idx := beads.MergeQueueIndex{}
	for _, id := range []string{"gt-work123", "gt-orphan1", "gt-hooked1", "gt-parked1", "gt-parked2", "gt-base1"} {
		if mrs := h.mrsFor(id); mrs != nil {
			idx[id] = mrs
		}
	}
	return idx
}

func (h holdIndex) mrsFor(id string) []*beads.Issue {
	switch h.hold {
	case beads.HoldPending:
		return []*beads.Issue{{ID: h.mrID, Status: "open", Description: "source_issue: " + id + "\n"}}
	case beads.HoldLanded:
		return []*beads.Issue{{ID: h.mrID, Status: "closed", Description: "source_issue: " + id + "\nclose_reason: merged\n"}}
	}
	return nil
}

func hookedBeadMock() (*BdCli, *mockBdCalls) {
	return mockBd(
		func(args []string) (string, error) {
			if len(args) >= 1 && args[0] == "show" {
				return `[{"status":"hooked"}]`, nil
			}
			return "", nil
		},
		func(args []string) error { return nil },
	)
}

// gt-pd1c.59: after gt done the polecat is gone but its MR is still in the
// queue. The witness must not reset the bead to open nor ask the deacon to
// re-dispatch it; it parks it for the refinery.
func TestResetAbandonedBead_ParksBeadWithPendingMR(t *testing.T) {
	oldVerify := verifyCommitOnMain
	verifyCommitOnMain = func(workDir, rigName, polecatName string) (bool, error) { return false, nil }
	t.Cleanup(func() { verifyCommitOnMain = oldVerify })
	stubMergeQueueHold(t, beads.HoldPending, "gt-wisp-mr1", nil)

	bd, mock := hookedBeadMock()
	if resetAbandonedBead(bd, t.TempDir(), "testrig", "gt-work123", "alpha", nil) {
		t.Fatal("a bead with a pending MR must not be recovered for re-dispatch")
	}
	var parked bool
	for _, call := range mock.calls {
		if strings.Contains(call, "--status=open") || strings.HasPrefix(call, "close") {
			t.Errorf("held work must be neither reopened nor closed, call: %s", call)
		}
		if strings.Contains(call, "update gt-work123") && strings.Contains(call, "--status=in_progress") && strings.Contains(call, "--assignee=") {
			parked = true
		}
	}
	if !parked {
		t.Errorf("expected the bead to be parked, calls: %v", mock.calls)
	}
}

// Fail closed: an unreadable merge queue never turns into a reset.
func TestResetAbandonedBead_SkipsWhenMergeQueueUnreadable(t *testing.T) {
	oldVerify := verifyCommitOnMain
	verifyCommitOnMain = func(workDir, rigName, polecatName string) (bool, error) { return false, nil }
	t.Cleanup(func() { verifyCommitOnMain = oldVerify })
	stubMergeQueueHold(t, beads.HoldNone, "", errors.New("dolt: connection refused"))

	bd, mock := hookedBeadMock()
	if resetAbandonedBead(bd, t.TempDir(), "testrig", "gt-work123", "alpha", nil) {
		t.Fatal("must not recover a bead without reading the merge queue")
	}
	for _, call := range mock.calls {
		if strings.HasPrefix(call, "update") || strings.HasPrefix(call, "close") {
			t.Errorf("no write expected when the merge queue is unreadable, call: %s", call)
		}
	}
}

// gt-pd1c.59 review #3: an MR closed by hand (bare bd close) after landing
// leaves its parked source in_progress with no assignee. The witness orphan
// scan settles it: landed work is closed; work still pending is left alone.
func TestDetectOrphanedBeads_SettlesParkedBeadWhoseMRLanded(t *testing.T) {
	installFakeTmuxNoServer(t)
	for _, tc := range []struct {
		hold      beads.MergeQueueHold
		wantClose bool
	}{{beads.HoldLanded, true}, {beads.HoldPending, false}, {beads.HoldNone, false}} {
		old := witnessMergeQueue
		witnessMergeQueue = func(string) *beads.MergeQueue { return heldQueue(tc.hold, "gt-wisp-mr1", nil) }
		bd, mock := mockBd(
			func(args []string) (string, error) {
				if len(args) > 0 && args[0] == "list" && strings.Contains(strings.Join(args, " "), "--status=in_progress") {
					return `[{"id":"gt-parked1","assignee":""}]`, nil
				}
				return "[]", nil
			},
			func(args []string) error { return nil },
		)
		DetectOrphanedBeads(bd, t.TempDir(), "testrig", nil)
		witnessMergeQueue = old
		closed := false
		for _, call := range mock.calls {
			if strings.HasPrefix(call, "close gt-parked1") {
				closed = true
			}
			if strings.Contains(call, "--status=open") {
				t.Errorf("hold %v: parked bead reopened: %s", tc.hold, call)
			}
		}
		if closed != tc.wantClose {
			t.Errorf("hold %v: closed = %v, want %v (calls %v)", tc.hold, closed, tc.wantClose, mock.calls)
		}
	}
}

// round 3 #3: outside a town the witness gets no partial merge queue.
func TestWitnessMergeQueue_FailsClosedOutsideTown(t *testing.T) {
	if err := witnessMergeQueue(t.TempDir()).Err(); err == nil {
		t.Fatal("a work dir outside any town must not yield a (partial) merge queue")
	}
}

// round 3 #1: an unverifiable landing parks the bead instead of resetting it.
func TestResetAbandonedBead_ParksWhenLandingUnverified(t *testing.T) {
	oldVerify := verifyCommitOnMain
	verifyCommitOnMain = func(workDir, rigName, polecatName string) (bool, error) { return false, nil }
	t.Cleanup(func() { verifyCommitOnMain = oldVerify })
	old := witnessMergeQueue
	witnessMergeQueue = func(string) *beads.MergeQueue {
		return beads.NewMergeQueue(beads.MergeQueueIndex{"gt-work123": {{ID: "gt-wisp-mr4", Status: "closed", Description: "source_issue: gt-work123\n"}}}, nil, nil)
	}
	t.Cleanup(func() { witnessMergeQueue = old })

	bd, mock := hookedBeadMock()
	if resetAbandonedBead(bd, t.TempDir(), "testrig", "gt-work123", "alpha", nil) {
		t.Fatal("unverifiable landing must not be recovered for re-dispatch")
	}
	for _, call := range mock.calls {
		if strings.Contains(call, "--status=open") {
			t.Fatalf("bead reopened: %s", call)
		}
	}
}
