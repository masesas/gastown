package beads

import (
	"os"
	"testing"
)

// gt-pd1c.78: the merge queue's verdict on a polecat branch is the same
// predicate (MergeQueueHoldOf) over the MRs submitted from that branch, in any
// source bead's group.
func TestMergeQueueBranchHold(t *testing.T) {
	mr := func(id, status, desc string) *Issue { return &Issue{ID: id, Status: status, Description: desc} }
	idx := MergeQueueIndex{
		"gt-a": {
			mr("mr-open", "open", "branch: polecat/a/gt-a\ntarget: main\nsource_issue: gt-a\ncommit_sha: ca\n"),
		},
		"gt-b": {
			mr("mr-merged", "closed", "branch: polecat/b/gt-b\ntarget: main\nsource_issue: gt-b\ncommit_sha: cb\nclose_reason: merged\n"),
		},
		"gt-c": {
			mr("mr-rejected", "closed", "branch: polecat/c/gt-c\ntarget: main\nsource_issue: gt-c\ncommit_sha: cc\nclose_reason: rejected\nlanding: not_landed\n"),
		},
		"gt-d": {
			mr("mr-bare", "closed", "branch: polecat/d/gt-d\ntarget: main\nsource_issue: gt-d\ncommit_sha: cd\n"),
		},
		// Same branch submitted for two source beads: one pending MR holds it.
		"gt-e1": {mr("mr-e1", "closed", "branch: polecat/e\ntarget: main\nsource_issue: gt-e1\ncommit_sha: ce1\nclose_reason: rejected\nlanding: not_landed\n")},
		"gt-e2": {mr("mr-e2", "open", "branch: polecat/e\ntarget: main\nsource_issue: gt-e2\ncommit_sha: ce2\n")},
	}
	q := NewMergeQueue(idx, nil, nil)
	cases := []struct {
		branch string
		want   MergeQueueHold
		mrID   string
	}{
		{"polecat/a/gt-a", HoldPending, "mr-open"},
		{"polecat/b/gt-b", HoldLanded, "mr-merged"},
		{"polecat/c/gt-c", HoldNone, ""},
		{"polecat/d/gt-d", HoldUnverified, "mr-bare"},
		{"polecat/e", HoldPending, "mr-e2"},
		{"polecat/never-submitted", HoldNone, ""},
		{" polecat/a/gt-a ", HoldPending, "mr-open"},
	}
	for _, c := range cases {
		d, err := q.BranchHold(c.branch)
		if err != nil {
			t.Fatalf("%s: %v", c.branch, err)
		}
		if d.Hold != c.want || d.MRID != c.mrID || (c.mrID != "" && (d.MR == nil || d.MR.ID != c.mrID)) {
			t.Errorf("BranchHold(%q) = (%v, %q), want (%v, %q)", c.branch, d.Hold, d.MRID, c.want, c.mrID)
		}
		if d.Hold == HoldUnverified && d.Reason == "" {
			t.Errorf("BranchHold(%q): an unverified hold must explain itself", c.branch)
		}
	}
	if _, err := q.BranchHold(""); err == nil {
		t.Error("an empty branch must be an error (fail closed)")
	}
	if _, err := NewMergeQueue(nil, os.ErrPermission, nil).BranchHold("polecat/a/gt-a"); err == nil {
		t.Error("an unreadable queue must be an error (fail closed)")
	}
	var nilQueue *MergeQueue
	if _, err := nilQueue.BranchHold("polecat/a/gt-a"); err == nil {
		t.Error("a nil queue must be an error (fail closed)")
	}
}
