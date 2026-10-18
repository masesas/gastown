package refinery

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
)

// installTerminalMRBDStub installs a bd stub where `show <id>` prints
// <dir>/<id>.json and every invocation is appended to the returned call log.
func installTerminalMRBDStub(t *testing.T, issues map[string]string) (logPath string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("bd stub is a shell script")
	}
	beads.ResetBdAllowStaleCacheForTest()
	t.Cleanup(beads.ResetBdAllowStaleCacheForTest)

	dir := t.TempDir()
	logPath = filepath.Join(dir, "calls.log")
	for id, body := range issues {
		if err := os.WriteFile(filepath.Join(dir, id+".json"), []byte(body), 0644); err != nil {
			t.Fatalf("write %s: %v", id, err)
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
    if [ -f "` + dir + `/${2}.json" ]; then cat "` + dir + `/${2}.json"; exit 0; fi
    echo "Error: no issue found matching ${2}" >&2
    exit 1
    ;;
  comments)
    if [ -f "` + dir + `/${2}.comments.json" ]; then cat "` + dir + `/${2}.comments.json"; exit 0; fi
    printf '%s\n' '[]'
    exit 0
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

func terminalMRIssues(mrStatus, mrFields string) map[string]string {
	return map[string]string{
		"gt-wisp-mr1": `[{"id":"gt-wisp-mr1","title":"Merge: gt-work1","status":"` + mrStatus + `","issue_type":"task","ephemeral":true,"labels":["gt:merge-request"],"description":"branch: polecat/rust/gt-work1\ntarget: integration/gt-epic\nsource_issue: gt-work1\n` + mrFields + `"}]`,
		"gt-work1":    `[{"id":"gt-work1","title":"Work","status":"hooked","issue_type":"task","description":""}]`,
		// The back-link gt done wrote when it created the MR (gt-pd1c.89).
		"gt-work1.comments": `[{"id":"c1","issue_id":"gt-work1","author":"rig/polecats/rust","text":"MR created: gt-wisp-mr1"}]`,
	}
}

func sourceWasClosed(t *testing.T, logPath string) bool {
	t.Helper()
	data, _ := os.ReadFile(logPath)
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "close") && strings.Contains(line, "gt-work1") {
			return true
		}
	}
	return false
}

// The single merged-MR rule: closing an MR as merged closes its source bead,
// whichever caller (post-merge, engineer success, already-merged recheck).
func TestCloseTerminalMR_MergedClosesSourceBead(t *testing.T) {
	logPath := installTerminalMRBDStub(t, terminalMRIssues("open", ""))
	var out bytes.Buffer

	res, err := closeTerminalMR(beads.New(t.TempDir()), "gt-wisp-mr1", terminalMRCloseOptions{
		Reason: string(CloseReasonMerged), MergeCommit: "abc123", Out: &out,
	})
	if err != nil {
		t.Fatalf("closeTerminalMR: %v", err)
	}
	if !res.Closed || !res.SourceCloseAttempted || !res.SourceClose.Closed || res.SourceClose.WorkBeadID != "gt-work1" {
		t.Fatalf("result = %+v, want MR closed and source gt-work1 closed", res)
	}
	if !sourceWasClosed(t, logPath) {
		data, _ := os.ReadFile(logPath)
		t.Fatalf("source gt-work1 was not closed; bd calls:\n%s", data)
	}
}

// Retry heals a source whose first close failed: the MR is already terminal
// with close_reason=merged but the source is still hooked.
func TestCloseTerminalMR_AlreadyMergedMRStillClosesSource(t *testing.T) {
	logPath := installTerminalMRBDStub(t, terminalMRIssues("closed", "close_reason: merged\\nmerge_commit: abc123\\n"))

	res, err := closeTerminalMR(beads.New(t.TempDir()), "gt-wisp-mr1", terminalMRCloseOptions{
		Reason: string(CloseReasonMerged),
	})
	if err != nil {
		t.Fatalf("closeTerminalMR: %v", err)
	}
	if !res.AlreadyTerminal || !res.SourceClose.Closed {
		t.Fatalf("result = %+v, want already-terminal MR with source closed", res)
	}
	if !sourceWasClosed(t, logPath) {
		t.Fatal("source gt-work1 was not closed on retry")
	}
}

// Rejected work is not done: its source bead must stay open for rework.
func TestCloseTerminalMR_RejectedDoesNotCloseSource(t *testing.T) {
	for name, issues := range map[string]map[string]string{
		"open MR rejected now":    terminalMRIssues("open", ""),
		"closed MR was rejected":  terminalMRIssues("closed", "close_reason: rejected\\n"),
		"closed MR no reason set": terminalMRIssues("closed", ""),
	} {
		t.Run(name, func(t *testing.T) {
			logPath := installTerminalMRBDStub(t, issues)
			reason := "rejected: policy failed"
			if strings.Contains(name, "merged") {
				reason = string(CloseReasonMerged)
			}
			res, err := closeTerminalMR(beads.New(t.TempDir()), "gt-wisp-mr1", terminalMRCloseOptions{Reason: reason})
			if err != nil {
				t.Fatalf("closeTerminalMR: %v", err)
			}
			if res.SourceCloseAttempted || sourceWasClosed(t, logPath) {
				t.Fatalf("source must stay open for a non-merged MR (result %+v)", res)
			}
		})
	}
}

// A merged-close request on an MR that terminated without close_reason=merged
// (e.g. rejected earlier) must not close the source.
func TestCloseTerminalMR_MergedRequestOnRejectedMRDoesNotCloseSource(t *testing.T) {
	logPath := installTerminalMRBDStub(t, terminalMRIssues("closed", "close_reason: rejected\\n"))
	res, err := closeTerminalMR(beads.New(t.TempDir()), "gt-wisp-mr1", terminalMRCloseOptions{Reason: string(CloseReasonMerged)})
	if err != nil {
		t.Fatalf("closeTerminalMR: %v", err)
	}
	if res.SourceCloseAttempted || sourceWasClosed(t, logPath) {
		t.Fatalf("source closed for a rejected MR (result %+v)", res)
	}
}

type fakeLanded bool

func (f fakeLanded) VerifyPushedCommitReachableFromPushTarget(remote, branch, commit string) error {
	if f {
		return nil
	}
	return fmt.Errorf("commit %s not on %s/%s", commit, remote, branch)
}

// Pilot kp-4az.5: the MR was closed by a bare `bd close` (no close_reason) and
// its commit is on the target. Running post-merge (closeTerminalMR) heals the
// source; if the commit is not on the target the source stays open.
func TestCloseTerminalMR_BareClosedMRWithLandedCommitClosesSource(t *testing.T) {
	fields := "commit_sha: abc1234000000000000000000000000000000000\\n"
	logPath := installTerminalMRBDStub(t, terminalMRIssues("closed", fields))
	res, err := closeTerminalMR(beads.New(t.TempDir()), "gt-wisp-mr1", terminalMRCloseOptions{
		Reason: string(CloseReasonMerged), Prover: fakeLanded(true),
	})
	if err != nil || !res.SourceClose.Closed || !sourceWasClosed(t, logPath) {
		t.Fatalf("result = %+v, err = %v; want source closed", res, err)
	}

	logPath = installTerminalMRBDStub(t, terminalMRIssues("closed", fields))
	res, err = closeTerminalMR(beads.New(t.TempDir()), "gt-wisp-mr1", terminalMRCloseOptions{
		Reason: string(CloseReasonMerged), Prover: fakeLanded(false),
	})
	if err != nil || res.SourceCloseAttempted || sourceWasClosed(t, logPath) {
		t.Fatalf("result = %+v, err = %v; source must stay open when commit is not on target", res, err)
	}
}
