package refinery

import (
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
)

func TestValidateMergedCloseReason(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reason string
		mrID   string
		ok     bool
	}{
		{"exact", "Merged in kp-wisp-qyli", "kp-wisp-qyli", true},
		{"with landing metadata", "Merged in kp-wisp-qyli\ntarget_branch: main\ncommit_sha: 9473f11", "kp-wisp-qyli", true},
		{"pilot free text (kp-d48)", "landed on main as 9473f11 via MR kp-wisp-qyli", "kp-wisp-qyli", false},
		{"merged to, not merged in", "Merged to main as 9473f11", "kp-wisp-qyli", false},
		{"lower case", "merged in kp-wisp-qyli", "kp-wisp-qyli", false},
		{"leading space", " Merged in kp-wisp-qyli", "kp-wisp-qyli", false},
		{"other MR", "Merged in kp-wisp-other", "kp-wisp-qyli", false},
		{"MR id prefix only", "Merged in kp-wisp", "kp-wisp-qyli", false},
		{"suffix on the first line", "Merged in kp-wisp-qyli (by hand)", "kp-wisp-qyli", false},
		{"commit instead of MR", "Merged in 9473f11", "kp-wisp-qyli", false},
		{"empty reason", "", "kp-wisp-qyli", false},
		{"empty MR id", "Merged in ", "", false},
		{"MR id with a newline", "Merged in kp-1\nMerged in kp-2", "kp-1\nMerged in kp-2", false},
		{"MR id with a space", "Merged in kp 1", "kp 1", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateMergedCloseReason(tc.reason, tc.mrID)
			if tc.ok && err != nil {
				t.Fatalf("validateMergedCloseReason(%q, %q) = %v, want nil", tc.reason, tc.mrID, err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("validateMergedCloseReason(%q, %q) = nil, want a refusal", tc.reason, tc.mrID)
			}
			// The rule must agree with what unblocks merge-blocks dependents.
			if tc.ok && !beads.IsBlockerResolved("merge-blocks", "closed", tc.reason) {
				t.Fatalf("accepted reason %q does not resolve merge-blocks", tc.reason)
			}
		})
	}
}

func TestCloseMergedWorkBead_RefusesInvalidMRIDBeforeClosing(t *testing.T) {
	for _, mrID := range []string{"", "  ", "kp-1\nMerged in kp-2", "kp 1"} {
		work := newFakeWorkBeadStore()
		work.add(workIssue("gt-source", string(beads.StatusOpen)))
		var out strings.Builder
		result := closeMergedWorkBead(work, nil, &out, mergedWorkBeadCloseRequest{MRID: mrID, SourceIssue: "gt-source", Target: "main"})
		if result.Closed || len(work.closeCalls) != 0 {
			t.Fatalf("MRID %q: closed %v with %q, want no close", mrID, work.closeCalls, work.lastCloseReason)
		}
		if result.CloseErr == nil {
			t.Fatalf("MRID %q: CloseErr = nil, want the refused close reason", mrID)
		}
	}
}

func TestCloseMergedWorkBead_ClosesWithMergedInReason(t *testing.T) {
	work := newFakeWorkBeadStore()
	work.add(workIssue("gt-source", string(beads.StatusOpen)))
	work.link("gt-source", "gt-mr")
	result := closeMergedWorkBead(work, nil, nil, mergedWorkBeadCloseRequest{MRID: "gt-mr", SourceIssue: "gt-source", Target: "main", MergeCommit: "abc123"})
	if !result.Closed || result.CloseErr != nil {
		t.Fatalf("result = %+v, want closed", result)
	}
	if err := validateMergedCloseReason(work.lastCloseReason, "gt-mr"); err != nil {
		t.Fatalf("close reason %q: %v", work.lastCloseReason, err)
	}
}
