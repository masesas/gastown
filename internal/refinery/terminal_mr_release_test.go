package refinery

import (
	"errors"
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

// stubTownMergeQueue makes the release see the given town merge queue.
func stubTownMergeQueue(t *testing.T, idx beads.MergeQueueIndex, err error) {
	t.Helper()
	old := readTownMergeQueue
	readTownMergeQueue = func(string) *beads.MergeQueue { return beads.NewMergeQueue(idx, err, nil) }
	t.Cleanup(func() { readTownMergeQueue = old })
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
	stubTownMergeQueue(t, beads.MergeQueueIndex{}, nil)
	res, err := closeTerminalMR(beads.New(t.TempDir()), "gt-wisp-mr1", terminalMRCloseOptions{
		Reason: "rejected: gate failed", TownRoot: t.TempDir(),
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
			stubTownMergeQueue(t, beads.MergeQueueIndex{}, nil)
			res, err := closeTerminalMR(beads.New(t.TempDir()), "gt-wisp-mr1", terminalMRCloseOptions{Reason: "rejected: policy", TownRoot: t.TempDir()})
			if err != nil {
				t.Fatalf("closeTerminalMR: %v", err)
			}
			if res.SourceRelease != "" || len(sourceUpdates(t, logPath)) != 0 || sourceWasClosed(t, logPath) {
				t.Fatalf("source must be left alone (release %q, updates %v)", res.SourceRelease, sourceUpdates(t, logPath))
			}
		})
	}
}

// gt-pd1c.59 review #4: a repeated `gt mq reject`, or an MR that was closed
// by hand without landing, still hands a parked source back.
func TestCloseTerminalMR_AlreadyRejectedStillReleasesParkedSource(t *testing.T) {
	for name, fields := range map[string]string{
		"rejected again":       "close_reason: rejected\\n",
		"bare close, unlanded": "",
	} {
		t.Run(name, func(t *testing.T) {
			logPath := installTerminalMRBDStub(t, parkedSourceIssues("closed", fields, ""))
			stubTownMergeQueue(t, beads.MergeQueueIndex{}, nil)
			res, err := closeTerminalMR(beads.New(t.TempDir()), "gt-wisp-mr1", terminalMRCloseOptions{Reason: "rejected: again", TownRoot: t.TempDir()})
			if err != nil {
				t.Fatalf("closeTerminalMR: %v", err)
			}
			if !res.AlreadyTerminal || res.SourceRelease != beads.ReleaseReopened {
				t.Fatalf("result = %+v, want already-terminal MR with the source reopened", res)
			}
			if updates := sourceUpdates(t, logPath); len(updates) != 1 || !strings.Contains(updates[0], "--status=open") {
				t.Fatalf("expected one reopen, got %v", updates)
			}
		})
	}
}

// A pending MR of the same source in another DB keeps it parked; an unreadable
// town queue parks it and reports the error (fail closed).
func TestCloseTerminalMR_RejectedReleaseUsesTownQueue(t *testing.T) {
	logPath := installTerminalMRBDStub(t, parkedSourceIssues("open", "", ""))
	stubTownMergeQueue(t, beads.MergeQueueIndex{"gt-work1": {{ID: "gt-wisp-mr2", Status: "open", Description: "source_issue: gt-work1\n"}}}, nil)
	res, err := closeTerminalMR(beads.New(t.TempDir()), "gt-wisp-mr1", terminalMRCloseOptions{Reason: "rejected: superseded", TownRoot: t.TempDir()})
	if err != nil || res.SourceRelease != beads.ReleaseParked {
		t.Fatalf("result = (%+v, %v), want parked: another MR is pending", res, err)
	}
	for _, u := range sourceUpdates(t, logPath) {
		if strings.Contains(u, "--status=open") {
			t.Fatalf("source with a pending MR reopened: %s", u)
		}
	}

	installTerminalMRBDStub(t, parkedSourceIssues("open", "", ""))
	stubTownMergeQueue(t, nil, errors.New("dolt: connection refused"))
	res, _ = closeTerminalMR(beads.New(t.TempDir()), "gt-wisp-mr1", terminalMRCloseOptions{Reason: "rejected: x", TownRoot: t.TempDir()})
	if res.SourceReleaseErr == nil || res.SourceRelease == beads.ReleaseReopened {
		t.Fatalf("unreadable queue: result = %+v, want an error and no reopen", res)
	}
}

// gt-pd1c.59 review #5: a missing source or an unknown town is reported.
func TestCloseTerminalMR_ReleaseReportsMissingSourceAndTown(t *testing.T) {
	issues := terminalMRIssues("open", "")
	delete(issues, "gt-work1")
	installTerminalMRBDStub(t, issues)
	stubTownMergeQueue(t, beads.MergeQueueIndex{}, nil)
	res, err := closeTerminalMR(beads.New(t.TempDir()), "gt-wisp-mr1", terminalMRCloseOptions{Reason: "rejected: x", TownRoot: t.TempDir()})
	if err != nil || res.SourceReleaseErr == nil || !strings.Contains(res.SourceReleaseErr.Error(), "gt-work1") || res.SourceRelease != "" {
		t.Fatalf("missing source: (%+v, %v), want a not-found release error", res, err)
	}

	logPath := installTerminalMRBDStub(t, parkedSourceIssues("open", "", ""))
	res, _ = closeTerminalMR(beads.New(t.TempDir()), "gt-wisp-mr1", terminalMRCloseOptions{Reason: "rejected: x"})
	if res.SourceReleaseErr == nil || len(sourceUpdates(t, logPath)) != 0 {
		t.Fatalf("no town root: result = %+v, want an error and no write", res)
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

// useIsolatedMergeQueue makes the release read the merge queue of the
// isolated test DB b (a container-backed Dolt the town reader cannot reach):
// in a real town that DB is one of the town's beads DBs.
func useIsolatedMergeQueue(t *testing.T, b *beads.Beads) {
	t.Helper()
	useIsolatedMergeQueueWithProvers(t, b, nil)
}

// useIsolatedMergeQueueWithProvers is useIsolatedMergeQueue with git provers
// for the MRs' rigs (nil: none, so git cannot decide any landing).
func useIsolatedMergeQueueWithProvers(t *testing.T, b *beads.Beads, resolve beads.ProverResolver) {
	t.Helper()
	old := readTownMergeQueue
	readTownMergeQueue = func(string) *beads.MergeQueue {
		idx, err := b.MergeRequestsBySource()
		return beads.NewMergeQueue(idx, err, resolve)
	}
	t.Cleanup(func() { readTownMergeQueue = old })
}

// notLandedMRCommit is the commit_sha of rejected test MRs that git proves
// did not land (notLandedProver).
const notLandedMRCommit = "d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0"

// notLandedProver: git definitely says no commit is on the target, not even
// as an equivalent patch (rebase/squash).
type notLandedProver struct{}

func (notLandedProver) VerifyPushedCommitReachableFromPushTarget(remote, branch, commit string) error {
	return commitNotOnTargetErr{}
}

func (notLandedProver) PatchEquivalentOnPushTarget(remote, branch, commit, base string) (bool, error) {
	return false, nil
}

type commitNotOnTargetErr struct{}

func (commitNotOnTargetErr) Error() string           { return "commit not on target" }
func (commitNotOnTargetErr) CommitNotOnTarget() bool { return true }

// useIsolatedMergeQueueNotLanded is the isolated queue with git proving that
// no MR landed, so a rejected MR (commit_sha notLandedMRCommit) releases its
// source to open.
func useIsolatedMergeQueueNotLanded(t *testing.T, b *beads.Beads) {
	t.Helper()
	useIsolatedMergeQueueWithProvers(t, b, func(string) beads.MergeProver { return notLandedProver{} })
}

// round 3 #1: the rejected MR was closed by hand earlier and git cannot decide
// whether its commit landed (no prover): the parked source is not reopened and
// the release says why.
func TestCloseTerminalMR_UnverifiableLandingKeepsSourceParked(t *testing.T) {
	logPath := installTerminalMRBDStub(t, parkedSourceIssues("closed", "", ""))
	stubTownMergeQueue(t, beads.MergeQueueIndex{"gt-work1": {{ID: "gt-wisp-mr1", Status: "closed", Description: "source_issue: gt-work1\ntarget: main\ncommit_sha: c1\n"}}}, nil)
	res, err := closeTerminalMR(beads.New(t.TempDir()), "gt-wisp-mr1", terminalMRCloseOptions{Reason: "rejected: again", TownRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("closeTerminalMR: %v", err)
	}
	if res.SourceRelease == beads.ReleaseReopened || !errors.Is(res.SourceReleaseErr, beads.ErrLandingUnverified) {
		t.Fatalf("result = %+v, want parked with ErrLandingUnverified", res)
	}
	for _, u := range sourceUpdates(t, logPath) {
		if strings.Contains(u, "--status=open") {
			t.Fatalf("unverifiable source reopened: %s", u)
		}
	}
}
