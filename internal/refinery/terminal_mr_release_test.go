package refinery

import (
	"os"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
)

// parkedSourceIssues is terminalMRIssues with the source bead in the shape gt
// done leaves it on the MR path (gt-pd1c.59): in_progress, no assignee.
func parkedSourceIssues(mrStatus, mrFields, assignee string) map[string]string {
	issues := terminalMRIssues(mrStatus, mrFields)
	issues["gt-work1"] = `[{"id":"gt-work1","title":"Work","status":"in_progress","assignee":"` + assignee + `","issue_type":"task","description":""}]`
	return issues
}

func sourceUpdates(t *testing.T, logPath string) []string {
	t.Helper()
	data, _ := os.ReadFile(logPath)
	var updates []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "update gt-work1") {
			updates = append(updates, line)
		}
	}
	return updates
}

// (c) The refinery is the one owner of a parked source bead: merging the MR
// closes it.
func TestCloseTerminalMR_MergedClosesParkedSource(t *testing.T) {
	logPath := installTerminalMRBDStub(t, parkedSourceIssues("open", "", ""))
	res, err := closeTerminalMR(beads.New(t.TempDir()), "gt-wisp-mr1", terminalMRCloseOptions{
		Reason: string(CloseReasonMerged), MergeCommit: "abc123",
	})
	if err != nil {
		t.Fatalf("closeTerminalMR: %v", err)
	}
	if !res.SourceClose.Closed || !sourceWasClosed(t, logPath) {
		t.Fatalf("parked source must be closed on merge (result %+v)", res)
	}
	if res.SourceRelease != "" {
		t.Fatalf("a merged close must not release the source, got %q", res.SourceRelease)
	}
}

// (d) An MR closed without landing gives a parked source back for rework:
// open, no assignee, so the convoy feeder or a dispatcher can pick it up.
func TestCloseTerminalMR_RejectedReleasesParkedSource(t *testing.T) {
	logPath := installTerminalMRBDStub(t, parkedSourceIssues("open", "", ""))
	res, err := closeTerminalMR(beads.New(t.TempDir()), "gt-wisp-mr1", terminalMRCloseOptions{
		Reason: "rejected: gate failed",
	})
	if err != nil {
		t.Fatalf("closeTerminalMR: %v", err)
	}
	if res.SourceRelease != beads.ReleaseReopened || res.SourceReleaseErr != nil {
		t.Fatalf("SourceRelease = (%q, %v), want reopened", res.SourceRelease, res.SourceReleaseErr)
	}
	if sourceWasClosed(t, logPath) {
		t.Fatal("rejected work must not be closed")
	}
	updates := sourceUpdates(t, logPath)
	if len(updates) != 1 || !strings.Contains(updates[0], "--status=open") || !strings.Contains(updates[0], "--assignee=") {
		t.Fatalf("expected one reopen of gt-work1, got %v", updates)
	}
}

// A source that a live worker already claimed again (assignee set), or that is
// already closed, is not touched by the release.
func TestCloseTerminalMR_RejectedLeavesOwnedOrClosedSourceAlone(t *testing.T) {
	cases := map[string]map[string]string{
		"claimed by a new worker": parkedSourceIssues("open", "", "gastown/polecats/fresh"),
		"already closed": func() map[string]string {
			issues := terminalMRIssues("open", "")
			issues["gt-work1"] = `[{"id":"gt-work1","title":"Work","status":"closed","issue_type":"task","description":""}]`
			return issues
		}(),
	}
	for name, issues := range cases {
		t.Run(name, func(t *testing.T) {
			logPath := installTerminalMRBDStub(t, issues)
			res, err := closeTerminalMR(beads.New(t.TempDir()), "gt-wisp-mr1", terminalMRCloseOptions{Reason: "rejected: policy"})
			if err != nil {
				t.Fatalf("closeTerminalMR: %v", err)
			}
			if res.SourceRelease != "" || len(sourceUpdates(t, logPath)) != 0 || sourceWasClosed(t, logPath) {
				t.Fatalf("source must be left alone (release %q, updates %v)", res.SourceRelease, sourceUpdates(t, logPath))
			}
		})
	}
}

// A retry on an MR that is already terminal (not closed by this call) does not
// release again: only the close that ends the MR hands the bead back.
func TestCloseTerminalMR_AlreadyRejectedDoesNotReleaseAgain(t *testing.T) {
	logPath := installTerminalMRBDStub(t, parkedSourceIssues("closed", "close_reason: rejected\\n", ""))
	res, err := closeTerminalMR(beads.New(t.TempDir()), "gt-wisp-mr1", terminalMRCloseOptions{Reason: "rejected: again"})
	if err != nil {
		t.Fatalf("closeTerminalMR: %v", err)
	}
	if res.SourceRelease != "" || len(sourceUpdates(t, logPath)) != 0 {
		t.Fatalf("already-terminal MR must not release (release %q, updates %v)", res.SourceRelease, sourceUpdates(t, logPath))
	}
}

func TestReportSourceRelease(t *testing.T) {
	var out strings.Builder
	reportSourceRelease(&out, "[Engineer] ", &terminalMRCloseResult{MRID: "gt-wisp-mr1", SourceIssue: "gt-work1", SourceRelease: beads.ReleaseReopened})
	reportSourceRelease(&out, "", &terminalMRCloseResult{MRID: "gt-wisp-mr2", SourceIssue: "gt-work2", SourceReleaseErr: os.ErrPermission})
	reportSourceRelease(&out, "", &terminalMRCloseResult{MRID: "gt-wisp-mr3"})
	reportSourceRelease(nil, "", nil)
	got := out.String()
	for _, want := range []string{"[Engineer] Source gt-work1 of MR gt-wisp-mr1 reopened", "Warning: source gt-work2 of MR gt-wisp-mr2 not released"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output %q lacks %q", got, want)
		}
	}
	if strings.Contains(got, "gt-wisp-mr3") {
		t.Fatalf("nothing to report for an MR without release, got %q", got)
	}
}
