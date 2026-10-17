package cmd

import (
	"os"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
)

func stubSubmitMergeQueue(t *testing.T, idx beads.MergeQueueIndex) {
	t.Helper()
	old := submitMergeQueue
	submitMergeQueue = func(string) *beads.MergeQueue { return beads.NewMergeQueue(idx, nil, nil) }
	t.Cleanup(func() { submitMergeQueue = old })
}

// gt-pd1c.59 review #2: a parked source has no assignee, so the ownership
// guard alone lets any polecat submit it. The merge queue refuses a second MR
// from another branch and allows the owner's resubmit (FIX_NEEDED).
func TestRefuseHeldSubmission(t *testing.T) {
	stubSubmitMergeQueue(t, beads.MergeQueueIndex{
		"gt-work1": {{ID: "gt-wisp-mr1", Status: "open", Description: "branch: polecat/alpha/gt-work1\nsource_issue: gt-work1\n"}},
	})
	err := refuseHeldSubmission("/town", "gt-work1", "polecat/bravo/gt-work1")
	if err == nil || !strings.Contains(err.Error(), "held by the merge queue") || !strings.Contains(err.Error(), "Nothing was pushed") {
		t.Fatalf("polecat B must be refused before any push, got %v", err)
	}
	if err := refuseHeldSubmission("/town", "gt-work1", "polecat/alpha/gt-work1"); err != nil {
		t.Fatalf("polecat A resubmitting its own branch must be allowed: %v", err)
	}
	if err := refuseHeldSubmission("/town", "gt-other", "polecat/bravo/gt-other"); err != nil {
		t.Fatalf("unheld work must be allowed: %v", err)
	}
}

// round 3 #2: still fail closed town-wide, but the refusal says the work is safe.
func TestRefuseHeldSubmission_UnreadableQueueSaysWorkIsSafe(t *testing.T) {
	old := submitMergeQueue
	submitMergeQueue = func(string) *beads.MergeQueue { return beads.NewMergeQueue(nil, os.ErrPermission, nil) }
	t.Cleanup(func() { submitMergeQueue = old })
	err := refuseHeldSubmission("/town", "gt-work1", "polecat/alpha/gt-work1")
	if err == nil || !strings.Contains(err.Error(), "safe on the local branch polecat/alpha/gt-work1") || !strings.Contains(err.Error(), "rerun") {
		t.Fatalf("refusal = %v, want fail-closed with a safe-to-retry note", err)
	}
}

// gt-pd1c.59 review #7: a bead parked because the queue was unreadable was
// released from its worker; it is not reported as a failed reset.
func TestResetStaleOutcome(t *testing.T) {
	cases := []struct {
		outcome beads.ReleaseOutcome
		err     error
		wantOK  bool
		want    string
	}{
		{beads.ReleaseParked, os.ErrPermission, true, "parked, not reopened"},
		{"", os.ErrPermission, false, "Failed to reset"},
		{beads.ReleaseReopened, nil, true, ""},
		{beads.ReleaseParked, nil, true, "held by merge queue: parked"},
		{beads.ReleaseClosed, nil, true, "held by merge queue: closed"},
	}
	for _, c := range cases {
		msg, ok := resetStaleOutcome("gt-1", "gastown/polecats/nux", c.outcome, c.err)
		if ok != c.wantOK || (c.want == "" && msg != "") || !strings.Contains(msg, c.want) {
			t.Errorf("resetStaleOutcome(%q, %v) = (%q, %v), want ok=%v containing %q", c.outcome, c.err, msg, ok, c.wantOK, c.want)
		}
		if c.err != nil && c.wantOK && strings.Contains(msg, "Failed") {
			t.Errorf("parked-with-error must not read as a failed reset: %q", msg)
		}
	}
}
