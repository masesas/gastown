package doctor

import (
	"errors"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
)

func testQueue(idx beads.MergeQueueIndex, err error) func(string) *beads.MergeQueue {
	return func(string) *beads.MergeQueue { return beads.NewMergeQueue(idx, err, notLandedResolver) }
}

// notLandedProver answers every commit with a definite "not on the target".
type notLandedProver struct{}

func (notLandedProver) VerifyPushedCommitReachableFromPushTarget(remote, branch, commit string) error {
	return notOnTarget{}
}

type notOnTarget struct{}

func (notOnTarget) Error() string           { return "commit not on target" }
func (notOnTarget) CommitNotOnTarget() bool { return true }

var notLandedResolver beads.ProverResolver = func(string) beads.MergeProver { return notLandedProver{} }

var doctorTestIndex = beads.MergeQueueIndex{
	"gt-parked": {{ID: "gt-wisp-mr1", Status: "open", Description: "source_issue: gt-parked\n"}},
	"gt-landed": {{ID: "gt-wisp-mr2", Status: "closed", Description: "source_issue: gt-landed\nclose_reason: merged\n"}},
	"gt-broken": {{ID: "gt-wisp-mr3", Status: "closed", Description: "source_issue: gt-broken\ntarget: main\ncommit_sha: c3\nclose_reason: rejected\n"}},
}

// gt-pd1c.59: gt done parks the source of a submitted MR as in_progress with
// no assignee. A pending one waits for its merge and is never touched; a
// landed one (MR closed by hand) is closed by the fix; the rest is reopened.
func TestSplitMergeQueueHeld(t *testing.T) {
	rows := []nullAssigneeRow{{ID: "gt-parked", RigDB: "gastown"}, {ID: "gt-broken", RigDB: "gastown"}, {ID: "gt-landed", RigDB: "gastown"}}
	reopen, landed, pending := splitMergeQueueHeld(rows, beads.NewMergeQueue(doctorTestIndex, nil, notLandedResolver))
	if len(reopen) != 1 || reopen[0].ID != "gt-broken" {
		t.Fatalf("reopen = %+v, want only gt-broken (rejected MR, needs re-dispatch)", reopen)
	}
	if len(landed) != 1 || landed[0].ID != "gt-landed" || landed[0].MRID != "gt-wisp-mr2" {
		t.Fatalf("landed = %+v, want gt-landed via gt-wisp-mr2", landed)
	}
	if len(pending) != 1 || pending[0].ID != "gt-parked" {
		t.Fatalf("pending = %+v, want gt-parked", pending)
	}
}

func TestNullAssigneeCheck_FailsClosedWhenMergeQueueUnreadable(t *testing.T) {
	c := NewNullAssigneeCheck()
	res := c.classify(&CheckContext{TownRoot: t.TempDir()},
		[]nullAssigneeRow{{ID: "gt-parked", RigDB: "gastown"}},
		testQueue(nil, errors.New("dolt: connection refused")))
	if res.Status != StatusWarning || !strings.Contains(res.Message, "merge queue") {
		t.Fatalf("result = %+v, want a warning about the unreadable merge queue", res)
	}
	if len(c.affected) != 0 || len(c.landed) != 0 {
		t.Fatalf("nothing may be fixed without the merge queue, affected = %+v landed = %+v", c.affected, c.landed)
	}
}

func TestNullAssigneeCheck_ReportsOnlyFixableBeads(t *testing.T) {
	c := NewNullAssigneeCheck()
	res := c.classify(&CheckContext{TownRoot: t.TempDir()},
		[]nullAssigneeRow{{ID: "gt-parked", RigDB: "gastown"}}, testQueue(doctorTestIndex, nil))
	if res.Status != StatusOK || len(c.affected) != 0 || len(c.landed) != 0 {
		t.Fatalf("result = %+v, want OK: the only bead waits for its merge", res)
	}

	res = c.classify(&CheckContext{TownRoot: t.TempDir()},
		[]nullAssigneeRow{{ID: "gt-parked", RigDB: "gastown"}, {ID: "gt-broken", RigDB: "gastown"}, {ID: "gt-landed", RigDB: "gastown"}},
		testQueue(doctorTestIndex, nil))
	if res.Status != StatusWarning || len(c.affected) != 1 || len(c.landed) != 1 {
		t.Fatalf("result = %+v (affected %+v landed %+v), want a warning for gt-broken and gt-landed", res, c.affected, c.landed)
	}
	if !strings.Contains(strings.Join(res.Details, "\n"), "MR gt-wisp-mr2 landed") {
		t.Fatalf("details must name the landed MR: %v", res.Details)
	}
}

// The fix resets exactly the reported beads; IDs are validated, not escaped.
func TestNullAssigneeFixQueryFor(t *testing.T) {
	q, err := nullAssigneeFixQueryFor([]string{"gt-a", "hq-b.1_x"})
	if err != nil {
		t.Fatalf("valid IDs rejected: %v", err)
	}
	for _, want := range []string{"id IN ('gt-a', 'hq-b.1_x')", "status = 'in_progress'", "assignee IS NULL OR assignee = ''"} {
		if !strings.Contains(q, want) {
			t.Fatalf("query %q lacks %q", q, want)
		}
	}
	for _, bad := range []string{"gt-b'c", "gt a", "", "gt;drop", "-gt-a", ".gt"} {
		if _, err := nullAssigneeFixQueryFor([]string{"gt-a", bad}); err == nil {
			t.Fatalf("invalid ID %q accepted", bad)
		}
	}
}

// round 3 #6: the landed close path validates IDs too, before any bd call.
func TestNullAssigneeCheck_FixRefusesInvalidLandedID(t *testing.T) {
	c := NewNullAssigneeCheck()
	c.landed = []nullAssigneeRow{{ID: "--force", RigDB: "gastown", MRID: "gt-wisp-mr2"}}
	err := c.Fix(&CheckContext{TownRoot: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "refusing bead ID") {
		t.Fatalf("Fix = %v, want the invalid ID refused", err)
	}
}

// round 3 #1: a bead whose MR landing cannot be decided is never reopened.
func TestSplitMergeQueueHeld_UnverifiedIsNotReopened(t *testing.T) {
	idx := beads.MergeQueueIndex{"gt-bare": {{ID: "gt-wisp-mr4", Status: "closed", Description: "source_issue: gt-bare\ntarget: main\ncommit_sha: c4\n"}}}
	reopen, landed, pending := splitMergeQueueHeld([]nullAssigneeRow{{ID: "gt-bare"}}, beads.NewMergeQueue(idx, nil, nil))
	if len(reopen) != 0 || len(landed) != 0 || len(pending) != 1 {
		t.Fatalf("reopen=%v landed=%v pending=%v, want it left alone", reopen, landed, pending)
	}
}
