package refinery

import (
	"errors"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
)

// gt-pd1c.89: the source bead is closed "Merged in <mr>" (with --force, which
// also unblocks merge-blocks dependents) only when it is bound to that MR:
// it carries the back-link gt done / gt mq submit wrote when they created the
// MR ("MR created: <mr>"). An MR that merely names a bead as source_issue
// (any bd writer can create one) cannot close it.
func TestCloseMergedWorkBead_RefusesASourceNotBoundToTheMR(t *testing.T) {
	for name, tc := range map[string]struct {
		comments    []beads.Comment
		commentsErr error
	}{
		"no back-link":              {},
		"back-link of another MR":   {comments: []beads.Comment{{Text: "MR created: gt-mr-other"}}},
		"back-link text with extra": {comments: []beads.Comment{{Text: "MR created: gt-mr (forged)"}}},
		"comments unreadable":       {commentsErr: errors.New("dolt down")},
	} {
		t.Run(name, func(t *testing.T) {
			work := newFakeWorkBeadStore()
			work.add(workIssue("gt-victim", string(beads.StatusOpen)))
			work.comments["gt-victim"] = tc.comments
			work.commentsErr = tc.commentsErr

			r := closeMergedWorkBead(work, nil, nil, mergedWorkBeadCloseRequest{MRID: "gt-mr", SourceIssue: "gt-victim", Target: "main", MergeCommit: "abc"})
			if r.Closed || len(work.closeCalls) != 0 {
				t.Fatalf("closed %v (result %+v): the source is not bound to gt-mr", work.closeCalls, r)
			}
			if r.CloseErr == nil || !strings.Contains(r.CloseErr.Error(), "MR gt-mr") {
				t.Fatalf("CloseErr = %v, want a refusal naming the missing back-link", r.CloseErr)
			}
			if r.mergedErr() == nil {
				t.Fatal("an unbound source must make the finalize fail (cleanup_failed), not pass silently")
			}
		})
	}
}

func TestCloseMergedWorkBead_ClosesABoundSource(t *testing.T) {
	work := newFakeWorkBeadStore()
	work.add(workIssue("gt-src", string(beads.StatusOpen)))
	work.link("gt-src", "gt-mr")
	r := closeMergedWorkBead(work, nil, nil, mergedWorkBeadCloseRequest{MRID: "gt-mr", SourceIssue: "gt-src", Target: "main"})
	if !r.Closed || r.CloseErr != nil || len(work.closeCalls) != 1 {
		t.Fatalf("result %+v closes %v: a bound source must be closed", r, work.closeCalls)
	}
}
