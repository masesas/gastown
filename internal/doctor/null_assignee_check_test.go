package doctor

import (
	"errors"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
)

// gt-pd1c.59: gt done parks the source of a submitted MR as in_progress with
// no assignee. Those beads are waiting for their merge, not broken: the check
// must neither report them nor reset them to open.
func TestSplitMergeQueueHeld_ExcludesBeadsHeldByTheMergeQueue(t *testing.T) {
	rows := []nullAssigneeRow{{ID: "gt-parked", RigDB: "gastown"}, {ID: "gt-broken", RigDB: "gastown"}, {ID: "gt-landed", RigDB: "gastown"}}
	idx := beads.MergeQueueIndex{
		"gt-parked": {{ID: "gt-wisp-mr1", Status: "open", Description: "source_issue: gt-parked\n"}},
		"gt-landed": {{ID: "gt-wisp-mr2", Status: "closed", Description: "source_issue: gt-landed\nclose_reason: merged\n"}},
		"gt-broken": {{ID: "gt-wisp-mr3", Status: "closed", Description: "source_issue: gt-broken\nclose_reason: rejected\n"}},
	}
	kept, held := splitMergeQueueHeld(rows, idx, nil)
	if len(kept) != 1 || kept[0].ID != "gt-broken" {
		t.Fatalf("kept = %+v, want only gt-broken (rejected MR, needs re-dispatch)", kept)
	}
	if len(held) != 2 {
		t.Fatalf("held = %+v, want gt-parked and gt-landed", held)
	}
}

func TestNullAssigneeCheck_FailsClosedWhenMergeQueueUnreadable(t *testing.T) {
	c := NewNullAssigneeCheck()
	res := c.classify(&CheckContext{TownRoot: t.TempDir()},
		[]nullAssigneeRow{{ID: "gt-parked", RigDB: "gastown"}},
		func(string) (beads.MergeQueueIndex, error) { return nil, errors.New("dolt: connection refused") })
	if res.Status != StatusWarning || !strings.Contains(res.Message, "merge queue") {
		t.Fatalf("result = %+v, want a warning about the unreadable merge queue", res)
	}
	if len(c.affected) != 0 {
		t.Fatalf("nothing may be fixed without the merge queue, affected = %+v", c.affected)
	}
}

func TestNullAssigneeCheck_ReportsOnlyUnheldBeads(t *testing.T) {
	c := NewNullAssigneeCheck()
	idx := beads.MergeQueueIndex{"gt-parked": {{ID: "gt-wisp-mr1", Status: "open", Description: "source_issue: gt-parked\n"}}}
	res := c.classify(&CheckContext{TownRoot: t.TempDir()},
		[]nullAssigneeRow{{ID: "gt-parked", RigDB: "gastown"}},
		func(string) (beads.MergeQueueIndex, error) { return idx, nil })
	if res.Status != StatusOK || len(c.affected) != 0 {
		t.Fatalf("result = %+v (affected %+v), want OK: the only bead waits for its merge", res, c.affected)
	}

	res = c.classify(&CheckContext{TownRoot: t.TempDir()},
		[]nullAssigneeRow{{ID: "gt-parked", RigDB: "gastown"}, {ID: "gt-broken", RigDB: "gastown"}},
		func(string) (beads.MergeQueueIndex, error) { return idx, nil })
	if res.Status != StatusWarning || len(c.affected) != 1 || c.affected[0].ID != "gt-broken" {
		t.Fatalf("result = %+v (affected %+v), want a warning for gt-broken only", res, c.affected)
	}
}

// The fix resets exactly the reported beads, never every in_progress bead
// without an assignee (a parked bead may appear between Run and Fix).
func TestNullAssigneeFixQueryFor_TargetsOnlyListedIDs(t *testing.T) {
	q := nullAssigneeFixQueryFor([]string{"gt-a", "gt-b'c"})
	for _, want := range []string{"id IN ('gt-a', 'gt-b''c')", "status = 'in_progress'", "assignee IS NULL OR assignee = ''"} {
		if !strings.Contains(q, want) {
			t.Fatalf("query %q lacks %q", q, want)
		}
	}
}
