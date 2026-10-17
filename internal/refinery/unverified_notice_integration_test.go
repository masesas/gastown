package refinery

import (
	"strconv"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/testutil"
)

// gt-pd1c.82 (F-4): the witness marks unverified_notified_at on an MR that is
// already closed. Verify against real bd + Dolt that a closed MR wisp accepts
// the description update and keeps its other fields (otherwise every patrol
// would resend the notice).
func TestMarkUnverifiedNotified_ClosedMRWisp_RealBeads(t *testing.T) {
	_, rigPath := setupTestManager(t)
	testutil.RequireDoltContainer(t)
	port, _ := strconv.Atoi(testutil.DoltContainerPort())
	testutil.ResetDoltDatabases(t, "gt")
	b := beads.NewIsolatedWithPort(rigPath, port)
	if err := b.Init("gt"); err != nil {
		t.Skipf("bd init unavailable in test environment: %v", err)
	}

	mr, err := b.Create(beads.CreateOptions{
		Title:       "Merge: gt-work1",
		Labels:      []string{"gt:merge-request"},
		Description: "branch: polecat/rust/gt-work1\ntarget: main\nsource_issue: gt-work1\nrig: gastown\n",
		Ephemeral:   true,
	})
	if err != nil {
		t.Fatalf("create MR wisp: %v", err)
	}
	if err := b.CloseWithReason("closed by hand", mr.ID); err != nil {
		t.Fatalf("close MR wisp: %v", err)
	}

	at := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	if err := b.MarkUnverifiedNotified(mr.ID, at); err != nil {
		t.Fatalf("MarkUnverifiedNotified on a closed MR: %v", err)
	}
	got, err := b.Show(mr.ID)
	if err != nil {
		t.Fatalf("show MR: %v", err)
	}
	if !beads.IssueStatus(got.Status).IsTerminal() {
		t.Fatalf("marking must not reopen the MR, status = %q", got.Status)
	}
	f := beads.ParseMRFields(got)
	if f == nil || f.UnverifiedNotifiedAt != "2026-10-16T10:00:00Z" || f.SourceIssue != "gt-work1" || f.Rig != "gastown" {
		t.Fatalf("fields after mark = %+v (description %q)", f, got.Description)
	}
}
