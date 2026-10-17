package beads

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const (
	// Closed MRs: gt-work1 closed_reason merged; gt-work4 is the pilot shape
	// (bare `bd close`, no close_reason) with its commit on the target; gt-work2
	// was rejected and never landed.
	closedMRRows = `[` +
		`{"id":"gt-wisp-merged","status":"closed","description":"branch: polecat/a/gt-work1\ntarget: integration/gt-epic\nsource_issue: gt-work1\nrig: gastown\ncommit_sha: c1\nclose_reason: merged\n"},` +
		`{"id":"gt-wisp-bare","status":"closed","description":"branch: polecat/d/gt-work4\ntarget: integration/gt-epic\nsource_issue: gt-work4\nrig: gastown\ncommit_sha: c4\n"},` +
		`{"id":"gt-wisp-rejected","status":"closed","description":"branch: polecat/b/gt-work2\ntarget: main\nsource_issue: gt-work2\nrig: gastown\ncommit_sha: c2\nclose_reason: rejected\n"}` +
		`]`
	pendingMRRows = `[{"id":"gt-wisp-open","status":"open","description":"branch: polecat/c/gt-work3\ntarget: main\nsource_issue: gt-work3\nrig: gastown\ncommit_sha: c3\n"}]`
)

// fakeProver reports commits in landed as reachable from any target.
type fakeProver map[string]bool

func (f fakeProver) VerifyPushedCommitReachableFromPushTarget(remote, branch, commit string) error {
	if f[commit] {
		return nil
	}
	return notOnTarget{}
}

// notOnTarget is a definite "commit not reachable" (like git.CommitNotOnBranchError).
type notOnTarget struct{}

func (notOnTarget) Error() string           { return "commit not on target" }
func (notOnTarget) CommitNotOnTarget() bool { return true }

// failingProver cannot find out (network, fetch, missing repo).
type failingProver struct{}

func (failingProver) VerifyPushedCommitReachableFromPushTarget(remote, branch, commit string) error {
	return os.ErrDeadlineExceeded
}

func resolverFor(landed ...string) ProverResolver {
	p := fakeProver{}
	for _, c := range landed {
		p[c] = true
	}
	return func(string) MergeProver { return p }
}

// installMergedMRBDStub installs a bd stub that answers the pending-MR query
// with pendingOut and the closed-MR query with closedOut (both fail when
// closedOut is empty) and appends every invocation to the returned log.
func installMergedMRBDStub(t *testing.T, pendingOut, closedOut string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("bd stub is a shell script")
	}
	ResetBdAllowStaleCacheForTest()
	t.Cleanup(ResetBdAllowStaleCacheForTest)

	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls.log")
	for name, body := range map[string]string{"pending.json": pendingOut, "closed.json": closedOut} {
		if body == "" {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	script := `#!/bin/sh
if [ "${1:-}" = "--allow-stale" ]; then
  if [ "${2:-}" = "version" ]; then
    echo "Error: unknown flag: --allow-stale" >&2
    exit 0
  fi
  shift
fi
echo "$*" >> "` + logPath + `"
case "${1:-}" in
  show)
    if [ -f "` + dir + `/show-${2}.json" ]; then cat "` + dir + `/show-${2}.json"; exit 0; fi
    printf '[{"id":"%s","status":"in_progress"}]\n' "${2}"
    exit 0
    ;;
  sql)
    case "$*" in
      *"w.status IN ('closed'"*) f="` + dir + `/closed.json" ;;
      *) f="` + dir + `/pending.json" ;;
    esac
    if [ -f "$f" ]; then cat "$f"; exit 0; fi
    echo "dolt: connection refused" >&2
    exit 1
    ;;
  *)
    printf '%s\n' '[]'
    exit 0
    ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "bd"), []byte(script), 0755); err != nil {
		t.Fatalf("write bd stub: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

// queueFor reads the stub's merge queue into a MergeQueue with the given
// commits proven landed.
func queueFor(t *testing.T, landed ...string) *MergeQueue {
	t.Helper()
	idx, err := New(t.TempDir()).MergeRequestsBySource()
	return NewMergeQueue(idx, err, resolverFor(landed...))
}

func readCalls(t *testing.T, logPath string) string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read call log: %v", err)
	}
	return string(data)
}

func TestReleaseWorkBead_ClosesWorkWhoseMRMerged(t *testing.T) {
	log := installMergedMRBDStub(t, pendingMRRows, closedMRRows)
	// Pilot shape: MR closed via bare bd close, commit already on the target.
	outcome, err := New(t.TempDir()).ReleaseWorkBead("gt-work4", queueFor(t, "c4"))
	if err != nil || outcome != ReleaseClosed {
		t.Fatalf("ReleaseWorkBead = (%v, %v), want closed", outcome, err)
	}
	calls := readCalls(t, log)
	if !strings.Contains(calls, "close") || !strings.Contains(calls, "gt-work4") {
		t.Fatalf("expected gt-work4 to be closed, calls:\n%s", calls)
	}
	if strings.Contains(calls, "--status") {
		t.Fatalf("merged work must not be reopened, calls:\n%s", calls)
	}
}

func TestReleaseWorkBead_ReopensWorkWithoutLandedMR(t *testing.T) {
	log := installMergedMRBDStub(t, pendingMRRows, closedMRRows)
	outcome, err := New(t.TempDir()).ReleaseWorkBead("gt-work2", queueFor(t)) // rejected
	if err != nil || outcome != ReleaseReopened {
		t.Fatalf("ReleaseWorkBead = (%v, %v), want reopened", outcome, err)
	}
	calls := readCalls(t, log)
	if !strings.Contains(calls, "update gt-work2") || !strings.Contains(calls, "--status=open") {
		t.Fatalf("expected gt-work2 reopened, calls:\n%s", calls)
	}
}

// gt-pd1c.59: a bead whose MR is still in the queue belongs to the refinery.
// Releasing its worker parks it (in_progress, no assignee) instead of making
// it dispatchable again.
func TestReleaseWorkBead_ParksWorkWithPendingMR(t *testing.T) {
	log := installMergedMRBDStub(t, pendingMRRows, closedMRRows)
	outcome, err := New(t.TempDir()).ReleaseWorkBead("gt-work3", queueFor(t))
	if err != nil || outcome != ReleaseParked {
		t.Fatalf("ReleaseWorkBead = (%v, %v), want parked", outcome, err)
	}
	calls := readCalls(t, log)
	if !strings.Contains(calls, "update gt-work3 --status=in_progress --assignee=") {
		t.Fatalf("expected gt-work3 parked, calls:\n%s", calls)
	}
	if strings.Contains(calls, "--status=open") || strings.Contains(calls, "\nclose ") {
		t.Fatalf("work held by the merge queue must be neither reopened nor closed, calls:\n%s", calls)
	}
}

// Fail closed: with the merge queue unreadable the worker is still released,
// but the bead is parked, not reopened for dispatch.
func TestReleaseWorkBead_ParksWhenQueueUnreadable(t *testing.T) {
	log := installMergedMRBDStub(t, "", "")
	outcome, err := New(t.TempDir()).ReleaseWorkBead("gt-work1", queueFor(t))
	if err == nil {
		t.Fatal("expected the merge-queue lookup error to be reported")
	}
	if outcome != ReleaseParked {
		t.Fatalf("outcome = %v, want parked", outcome)
	}
	calls := readCalls(t, log)
	if !strings.Contains(calls, "update gt-work1 --status=in_progress --assignee=") {
		t.Fatalf("worker must still be released (parked), calls:\n%s", calls)
	}
	if strings.Contains(calls, "--status=open") {
		t.Fatalf("must not reopen without reading the merge queue, calls:\n%s", calls)
	}
}

type ancestorProver struct {
	fakeProver
	ancestorOfBase bool
}

func (a ancestorProver) IsAncestor(ancestor, descendant string) (bool, error) {
	return a.ancestorOfBase, nil
}

// An MR whose commit was already in the target at submit (base_sha) carried no
// new work: being reachable from the target must not read as "landed".
func TestMRFieldsLanded_CommitAlreadyInBaseIsNotLanded(t *testing.T) {
	fields := &MRFields{Target: "main", CommitSHA: "c1", BaseSHA: "b1"}
	if MRFieldsLanded(StatusClosed, fields, ancestorProver{fakeProver{"c1": true}, true}) {
		t.Fatal("commit that is an ancestor of base_sha must not count as landed")
	}
	if !MRFieldsLanded(StatusClosed, fields, ancestorProver{fakeProver{"c1": true}, false}) {
		t.Fatal("new commit reachable from target must count as landed")
	}
	noBase := &MRFields{Target: "main", CommitSHA: "c1"}
	if !MRFieldsLanded(StatusClosed, noBase, ancestorProver{fakeProver{"c1": true}, true}) {
		t.Fatal("without base_sha the guard does not apply (older MRs)")
	}
}

func TestMRFields_BaseSHARoundTrip(t *testing.T) {
	in := &MRFields{Branch: "b", Target: "main", SourceIssue: "gt-1", CommitSHA: "c1", BaseSHA: "b1"}
	out := ParseMRFields(&Issue{Description: FormatMRFields(in)})
	if out == nil || out.BaseSHA != "b1" {
		t.Fatalf("round trip lost base_sha: %+v", out)
	}
}
