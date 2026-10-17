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
	old := mergeQueueHoldForBead
	mergeQueueHoldForBead = func(workDir, beadID string) (beads.MergeQueueHold, string, error) {
		return hold, mrID, err
	}
	t.Cleanup(func() { mergeQueueHoldForBead = old })
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
