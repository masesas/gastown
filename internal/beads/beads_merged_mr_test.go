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
	return os.ErrNotExist
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

func readCalls(t *testing.T, logPath string) string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read call log: %v", err)
	}
	return string(data)
}

func TestMergedMergeRequestFor_GitIsTheProof(t *testing.T) {
	installMergedMRBDStub(t, pendingMRRows, closedMRRows)
	b := New(t.TempDir())
	cases := []struct {
		issue, want string
		landed      []string
	}{
		{"gt-work1", "gt-wisp-merged", nil},          // close_reason merged: fast path, no git needed
		{"gt-work4", "gt-wisp-bare", []string{"c4"}}, // pilot: bd close with no reason, commit on target
		{"gt-work4", "", nil},                        // same MR, commit not on target
		{"gt-work2", "", nil},                        // rejected, never landed
		{"gt-work3", "gt-wisp-open", []string{"c3"}}, // pending MR whose commit already landed
		{"gt-work3", "", nil},                        // pending, not landed
	}
	for _, c := range cases {
		got, err := b.MergedMergeRequestFor(c.issue, resolverFor(c.landed...))
		if err != nil || got != c.want {
			t.Errorf("MergedMergeRequestFor(%s, landed=%v) = (%q, %v), want %q", c.issue, c.landed, got, err, c.want)
		}
	}
	if got, _ := b.MergedMergeRequestFor("gt-work4", nil); got != "" {
		t.Errorf("without a prover a bare-closed MR must not count as landed, got %q", got)
	}
}

func TestReleaseWorkBead_ClosesWorkWhoseMRMerged(t *testing.T) {
	log := installMergedMRBDStub(t, pendingMRRows, closedMRRows)
	// Pilot shape: MR closed via bare bd close, commit already on the target.
	closed, err := New(t.TempDir()).ReleaseWorkBead("gt-work4", resolverFor("c4"))
	if err != nil || !closed {
		t.Fatalf("ReleaseWorkBead = (%v, %v), want closed", closed, err)
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
	closed, err := New(t.TempDir()).ReleaseWorkBead("gt-work2", resolverFor()) // rejected
	if err != nil || closed {
		t.Fatalf("ReleaseWorkBead = (%v, %v), want reopened", closed, err)
	}
	calls := readCalls(t, log)
	if !strings.Contains(calls, "update gt-work2") || !strings.Contains(calls, "--status=open") {
		t.Fatalf("expected gt-work2 reopened, calls:\n%s", calls)
	}
}

func TestReleaseWorkBead_ReleasesWorkerWhenQueueUnreadable(t *testing.T) {
	log := installMergedMRBDStub(t, "", "")
	closed, err := New(t.TempDir()).ReleaseWorkBead("gt-work1", resolverFor())
	if err == nil {
		t.Fatal("expected the merge-queue lookup error to be reported")
	}
	if closed {
		t.Fatal("must not close without merge proof")
	}
	if calls := readCalls(t, log); !strings.Contains(calls, "--status=open") {
		t.Fatalf("worker must still be released, calls:\n%s", calls)
	}
}
