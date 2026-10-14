package refinery

import (
	"strconv"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/testutil"
)

// gt-ka3d: the convoy feeder holds issues that have a pending MR. Verify the
// lookup against real bd + Dolt with MR wisps created the way gt done creates
// them (ephemeral, gt:merge-request label, source_issue in the description).
func TestListPendingMergeRequests_RealBeads(t *testing.T) {
	_, rigPath := setupTestManager(t)
	testutil.RequireDoltContainer(t)
	port, _ := strconv.Atoi(testutil.DoltContainerPort())
	testutil.ResetDoltDatabases(t, "gt")
	b := beads.NewIsolatedWithPort(rigPath, port)
	if err := b.Init("gt"); err != nil {
		t.Skipf("bd init unavailable in test environment: %v", err)
	}

	pending, err := b.Create(beads.CreateOptions{
		Title:       "Merge: gt-work1",
		Labels:      []string{"gt:merge-request"},
		Description: "branch: polecat/rust/gt-work1\ntarget: main\nsource_issue: gt-work1\nrig: gastown\n",
		Ephemeral:   true,
	})
	if err != nil {
		t.Fatalf("create pending MR wisp: %v", err)
	}
	merged, err := b.Create(beads.CreateOptions{
		Title:       "Merge: gt-work2",
		Labels:      []string{"gt:merge-request"},
		Description: "branch: polecat/nux/gt-work2\ntarget: main\nsource_issue: gt-work2\nrig: gastown\n",
		Ephemeral:   true,
	})
	if err != nil {
		t.Fatalf("create merged MR wisp: %v", err)
	}
	if err := b.CloseWithReason("Merged in test", merged.ID); err != nil {
		t.Fatalf("close merged MR wisp: %v", err)
	}

	mrs, err := b.ListPendingMergeRequests()
	if err != nil {
		t.Fatalf("ListPendingMergeRequests: %v", err)
	}
	var sources []string
	for _, mr := range mrs {
		if mr.ID == merged.ID {
			t.Fatalf("closed MR %s reported as pending", merged.ID)
		}
		if fields := beads.ParseMRFields(mr); fields != nil {
			sources = append(sources, fields.SourceIssue)
		}
	}
	if len(sources) != 1 || sources[0] != "gt-work1" {
		t.Fatalf("pending MR source issues = %v, want [gt-work1] (pending MR %s)", sources, pending.ID)
	}
}
