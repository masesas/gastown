package beads

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// installTwoTableMRStub installs a bd stub that answers `bd sql` like Dolt
// would for a DB holding MR beads in both tables: issueRows when the query
// reads the issues table (joined with labels), wispRows when it reads the
// wisps table (joined with wisp_labels), split into open and terminal rows by
// the query's status filter. Anything else answers [].
func installTwoTableMRStub(t *testing.T, issueOpen, issueClosed, wispOpen, wispClosed string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("bd stub is a shell script")
	}
	ResetBdAllowStaleCacheForTest()
	t.Cleanup(ResetBdAllowStaleCacheForTest)
	dir := t.TempDir()
	write := func(name, body string) {
		if body == "" {
			body = "[]"
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("issue-open.json", issueOpen)
	write("issue-closed.json", issueClosed)
	write("wisp-open.json", wispOpen)
	write("wisp-closed.json", wispClosed)
	// The stub emits one JSON array: the rows of every table the query reads.
	script := `#!/bin/sh
if [ "${1:-}" = "--allow-stale" ]; then
  if [ "${2:-}" = "version" ]; then echo "Error: unknown flag: --allow-stale" >&2; exit 0; fi
  shift
fi
[ "${1:-}" = "sql" ] || { echo '[]'; exit 0; }
q="$*"
state=open
case "$q" in *"status IN ('closed'"*) state=closed ;; esac
files=""
case "$q" in *"FROM issues i JOIN labels l"*) files="$files ` + dir + `/issue-$state.json" ;; esac
case "$q" in *"FROM wisps w JOIN wisp_labels l"*) files="$files ` + dir + `/wisp-$state.json" ;; esac
[ -n "$files" ] || { echo '[]'; exit 0; }
# Concatenate the arrays into one.
cat $files | tr -d '\n' | sed -e 's/\]\[/,/g' -e 's/\[,/[/g' -e 's/,\]/]/g' -e 's/,,*/,/g'
echo
`
	if err := os.WriteFile(filepath.Join(dir, "bd"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// The merge queue (holds, TipDisposition) reads the same MRs the refinery
// processes (ListMergeRequests: issues and wisps tables). An MR bead created
// without --ephemeral (bd create --labels gt:merge-request, or one written
// before MRs became wisps) lives in the issues table; the queue must see it,
// or its open MR reads as "no MR recorded this tip" (pilot: witness delete of
// polecat/s2test/kp-s2b+t1) and its source bead as not held.
func TestMergeQueueReadsMRsFromBothTables(t *testing.T) {
	tip := "e" + strings.Repeat("1", 39)
	old := "f" + strings.Repeat("2", 39)
	branch := "polecat/s2test/kp-s2b+t1"
	issueOpen := `[{"id":"kp-mr1","status":"open","description":"branch: ` + branch +
		`\ntarget: main\nsource_issue: kp-wak\ncommit_sha: ` + tip + `\n"}]`
	wispClosed := `[{"id":"kp-wisp-mr0","status":"closed","description":"branch: ` + branch +
		`\ntarget: main\nsource_issue: kp-wak\ncommit_sha: ` + old + `\nclose_reason: superseded\n"}]`
	installTwoTableMRStub(t, issueOpen, "", "", wispClosed)

	idx, err := New(t.TempDir()).MergeRequestsBySource()
	if err != nil {
		t.Fatalf("MergeRequestsBySource: %v", err)
	}
	ids := map[string]bool{}
	for _, mr := range idx["kp-wak"] {
		ids[mr.ID] = true
	}
	if !ids["kp-mr1"] || !ids["kp-wisp-mr0"] || len(idx["kp-wak"]) != 2 {
		t.Fatalf("index = %v, want the issues-table MR kp-mr1 and the wisp kp-wisp-mr0", ids)
	}
	q := NewMergeQueue(idx, nil, nil)
	d, err := q.TipDisposition("refs/heads/"+branch, tip)
	if err != nil || d.Class != TipOpen || d.MRID != "kp-mr1" {
		t.Fatalf("TipDisposition = %+v %v, want open (MR kp-mr1), never no MR recorded", d, err)
	}
	if hold, mrID, err := q.Hold("kp-wak"); err != nil || hold != HoldPending || mrID != "kp-mr1" {
		t.Fatalf("Hold(kp-wak) = %v %q %v, want pending on kp-mr1", hold, mrID, err)
	}
}

// One MR bead listed by both tables (mid-migration copy) is indexed once.
func TestMergeQueueDedupsMRSeenInBothTables(t *testing.T) {
	row := `[{"id":"kp-mr2","status":"open","description":"branch: polecat/a/kp-x+t1\ntarget: main\nsource_issue: kp-x\n"}]`
	installTwoTableMRStub(t, row, "", row, "")
	idx, err := New(t.TempDir()).MergeRequestsBySource()
	if err != nil {
		t.Fatal(err)
	}
	if n := len(idx["kp-x"]); n != 1 {
		t.Fatalf("kp-mr2 indexed %d times, want once", n)
	}
}
