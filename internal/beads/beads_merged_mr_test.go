package beads

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const mergedMRRows = `[` +
	`{"id":"gt-wisp-merged","status":"closed","description":"branch: polecat/a/gt-work1\ntarget: integration/gt-epic\nsource_issue: gt-work1\nclose_reason: merged\nmerge_commit: abc123\n"},` +
	`{"id":"gt-wisp-rejected","status":"closed","description":"branch: polecat/b/gt-work2\ntarget: main\nsource_issue: gt-work2\nclose_reason: rejected\n"},` +
	`{"id":"gt-wisp-open","status":"open","description":"branch: polecat/c/gt-work3\ntarget: main\nsource_issue: gt-work3\n"}` +
	`]`

// installMergedMRBDStub installs a bd stub that answers `sql` with sqlOut (or
// fails when sqlOut is empty) and appends every invocation to the returned log.
func installMergedMRBDStub(t *testing.T, sqlOut string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("bd stub is a shell script")
	}
	ResetBdAllowStaleCacheForTest()
	t.Cleanup(ResetBdAllowStaleCacheForTest)

	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls.log")
	sqlPath := filepath.Join(dir, "sql.json")
	if sqlOut != "" {
		if err := os.WriteFile(sqlPath, []byte(sqlOut), 0644); err != nil {
			t.Fatalf("write sql output: %v", err)
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
    if [ -f "` + sqlPath + `" ]; then cat "` + sqlPath + `"; exit 0; fi
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

func TestListMergedMergeRequests_OnlyMergedCloseReason(t *testing.T) {
	installMergedMRBDStub(t, mergedMRRows)
	mrs, err := New(t.TempDir()).ListMergedMergeRequests()
	if err != nil {
		t.Fatalf("ListMergedMergeRequests: %v", err)
	}
	got := MergedSourceIssues(mrs)
	if len(got) != 1 || got["gt-work1"] != "gt-wisp-merged" {
		t.Fatalf("MergedSourceIssues = %v, want only gt-work1 -> gt-wisp-merged (rejected/open MRs excluded)", got)
	}
}

func TestReleaseWorkBead_ClosesWorkWhoseMRMerged(t *testing.T) {
	log := installMergedMRBDStub(t, mergedMRRows)
	closed, err := New(t.TempDir()).ReleaseWorkBead("gt-work1")
	if err != nil || !closed {
		t.Fatalf("ReleaseWorkBead = (%v, %v), want closed", closed, err)
	}
	calls := readCalls(t, log)
	if !strings.Contains(calls, "close") || !strings.Contains(calls, "gt-work1") {
		t.Fatalf("expected gt-work1 to be closed, calls:\n%s", calls)
	}
	if strings.Contains(calls, "--status") {
		t.Fatalf("merged work must not be reopened, calls:\n%s", calls)
	}
}

func TestReleaseWorkBead_ReopensWorkWithoutMergedMR(t *testing.T) {
	log := installMergedMRBDStub(t, mergedMRRows)
	closed, err := New(t.TempDir()).ReleaseWorkBead("gt-work2") // its MR was rejected
	if err != nil || closed {
		t.Fatalf("ReleaseWorkBead = (%v, %v), want reopened", closed, err)
	}
	calls := readCalls(t, log)
	if !strings.Contains(calls, "update gt-work2") || !strings.Contains(calls, "--status=open") {
		t.Fatalf("expected gt-work2 reopened, calls:\n%s", calls)
	}
}

func TestReleaseWorkBead_ReleasesWorkerWhenQueueUnreadable(t *testing.T) {
	log := installMergedMRBDStub(t, "")
	closed, err := New(t.TempDir()).ReleaseWorkBead("gt-work1")
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
