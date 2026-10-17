package cmd

import (
	"fmt"
	"github.com/steveyegge/gastown/internal/beads"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/steveyegge/gastown/internal/tmux"
)

// strandedWithMRQuery runs findStrandedConvoys against a fake bd whose convoy
// tracks one ready-looking issue (gt-work1 with the given status/assignee) and
// whose merge-request wisp SQL is answered by queryScript.
func strandedWithMRQuery(t *testing.T, queryScript string) strandedConvoyInfo {
	return strandedWithIssueAndMRQuery(t, "open", "", queryScript)
}

func strandedWithIssueAndMRQuery(t *testing.T, status, assignee, queryScript string) strandedConvoyInfo {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("skipping convoy test on Windows")
	}

	binDir := t.TempDir()
	townRoot := t.TempDir()
	beadsDir := filepath.Join(townRoot, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("mkdir .beads: %v", err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "routes.jsonl"), []byte(`{"prefix":"gt-","path":"gastown/mayor/rig"}`+"\n"), 0644); err != nil {
		t.Fatalf("write routes: %v", err)
	}

	script := `#!/bin/sh
i=0
for arg in "$@"; do
  case "$arg" in
    --*) ;;
    *) eval "pos$i=\"$arg\""; i=$((i+1)) ;;
  esac
done

case "$pos0" in
  list)
    echo '[{"id":"hq-cv1","title":"Convoy"}]'
    ;;
  sql)
    case "$*" in
      *"issue_id = 'hq-cv1'"*) echo '[{"depends_on_id":"gt-work1"}]' ;;
      *gt:merge-request*)
` + queryScript + `
        ;;
      *) echo '[]' ;;
    esac
    ;;
  dep)
    echo '[{"id":"gt-work1","title":"Work","status":"` + status + `","issue_type":"task","assignee":"` + assignee + `","dependency_type":"tracks"}]'
    ;;
  show)
    echo '[{"id":"gt-work1","title":"Work","status":"` + status + `","issue_type":"task","assignee":"` + assignee + `","blocked_by":[],"blocked_by_count":0,"dependencies":[]}]'
    ;;
  query)
    echo '[]'
    ;;
esac
exit 0
`
	if err := os.WriteFile(filepath.Join(binDir, "bd"), []byte(script), 0755); err != nil {
		t.Fatalf("write mock bd: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	stranded, err := findStrandedConvoys(townRoot)
	if err != nil {
		t.Fatalf("findStrandedConvoys() error: %v", err)
	}
	if len(stranded) != 1 {
		t.Fatalf("expected 1 stranded convoy, got %d", len(stranded))
	}
	return stranded[0]
}

// gt-ka3d: an issue whose MR is still in the merge queue is owned by the
// refinery; the feeder must not dispatch it to another polecat.
func TestFindStrandedConvoys_HoldsIssueWithPendingMR(t *testing.T) {
	s := strandedWithMRQuery(t, `    printf '%s\n' '[{"id":"gt-wisp-mr1","title":"Merge: gt-work1","status":"open","description":"branch: polecat/rust/gt-work1\ntarget: main\nsource_issue: gt-work1\nrig: gastown\n","labels":["gt:merge-request"]}]'`)
	if s.ReadyCount != 0 {
		t.Fatalf("ReadyCount = %d (%v), want 0 while MR is pending", s.ReadyCount, s.ReadyIssues)
	}
}

// The reported shape: gt done left the issue hooked to the polecat whose
// session it retired, while the MR waits in the queue.
func TestFindStrandedConvoys_HoldsHookedIssueOfExitedPolecatWithPendingMR(t *testing.T) {
	prev := tmux.GetDefaultSocket()
	tmux.SetDefaultSocket("gt-test-ka3d-no-server") // assignee session is gone; never the default server
	t.Cleanup(func() { tmux.SetDefaultSocket(prev) })

	mr := `    printf '%s\n' '[{"id":"gt-wisp-mr1","status":"open","description":"source_issue: gt-work1\n"}]'`
	if s := strandedWithIssueAndMRQuery(t, "hooked", "gastown/polecats/rust", mr); s.ReadyCount != 0 {
		t.Fatalf("ReadyCount = %d (%v), want 0 while MR is pending", s.ReadyCount, s.ReadyIssues)
	}
	if s := strandedWithIssueAndMRQuery(t, "hooked", "gastown/polecats/rust", `    echo '[]'`); s.ReadyCount != 1 {
		t.Fatalf("ReadyCount = %d, want 1: no MR and the polecat session is gone", s.ReadyCount)
	}
}

// Once the MR is merged or rejected (closed), the issue is feedable again.
func TestFindStrandedConvoys_FeedsIssueAfterMRClosed(t *testing.T) {
	proveNotLanded(t)
	s := strandedWithMRQuery(t, `    printf '%s\n' '[{"id":"gt-wisp-mr1","title":"Merge: gt-work1","status":"closed","description":"branch: polecat/rust/gt-work1\ntarget: main\nsource_issue: gt-work1\nrig: gastown\ncommit_sha: c1\n","labels":["gt:merge-request"]}]'`)
	if s.ReadyCount != 1 {
		t.Fatalf("ReadyCount = %d, want 1 after MR closed", s.ReadyCount)
	}
}

// Pilot kp-4az.5: the MR merged and left the queue but the source bead was not
// closed (still open/hooked). A merged MR means the work is done, so the
// feeder must not sling it again.
func TestFindStrandedConvoys_HoldsIssueWhoseMRMerged(t *testing.T) {
	merged := `    printf '%s\n' '[{"id":"gt-wisp-mr1","title":"Merge: gt-work1","status":"closed","description":"branch: polecat/rust/gt-work1\ntarget: integration/gt-epic\nsource_issue: gt-work1\nclose_reason: merged\nmerge_commit: abc123\n","labels":["gt:merge-request"]}]'`
	if s := strandedWithMRQuery(t, merged); s.ReadyCount != 0 {
		t.Fatalf("ReadyCount = %d (%v), want 0: MR merged, issue is done", s.ReadyCount, s.ReadyIssues)
	}
	if s := strandedWithIssueAndMRQuery(t, "hooked", "gastown/polecats/rust", merged); s.ReadyCount != 0 {
		t.Fatalf("hooked: ReadyCount = %d (%v), want 0: MR merged", s.ReadyCount, s.ReadyIssues)
	}
}

// Pilot kp-4az.5 exactly: the refinery agent pushed to the target and ran a
// bare `bd close` on the MR (no close_reason: merged, no gt mq post-merge).
// Git is the proof: the MR's commit is on origin/<target>, so the issue is done.
func TestFindStrandedConvoys_HoldsIssueWhoseMRWasBareClosedButLanded(t *testing.T) {
	bare := `    printf '%s\n' '[{"id":"gt-wisp-mr1","status":"closed","description":"branch: polecat/rust/gt-work1\ntarget: integration/gt-epic\nsource_issue: gt-work1\nrig: gastown\ncommit_sha: abc123\n","labels":["gt:merge-request"]}]'`
	orig := mergeProverResolver
	t.Cleanup(func() { mergeProverResolver = orig })

	mergeProverResolver = func(string) beads.ProverResolver {
		return func(string) beads.MergeProver { return stubProver{landed: true} }
	}
	if s := strandedWithMRQuery(t, bare); s.ReadyCount != 0 {
		t.Fatalf("ReadyCount = %d (%v), want 0: commit is on the target", s.ReadyCount, s.ReadyIssues)
	}
	if s := strandedWithIssueAndMRQuery(t, "hooked", "gastown/polecats/rust", bare); s.ReadyCount != 0 {
		t.Fatalf("hooked: ReadyCount = %d (%v), want 0", s.ReadyCount, s.ReadyIssues)
	}

	mergeProverResolver = func(string) beads.ProverResolver {
		return func(string) beads.MergeProver { return stubProver{landed: false} }
	}
	if s := strandedWithMRQuery(t, bare); s.ReadyCount != 1 {
		t.Fatalf("ReadyCount = %d, want 1: closed MR whose commit is not on the target needs rework", s.ReadyCount)
	}
}

type stubProver struct{ landed bool }

func (p stubProver) VerifyPushedCommitReachableFromPushTarget(remote, branch, commit string) error {
	if p.landed {
		return nil
	}
	return stubNotOnTarget{fmt.Sprintf("commit %s not on %s/%s", commit, remote, branch)}
}

// PatchEquivalentOnPushTarget: no rebase/squash either, so "not landed" is definite.
func (p stubProver) PatchEquivalentOnPushTarget(remote, branch, commit string) (bool, error) {
	return false, nil
}

// stubNotOnTarget is a definite "not reachable" (like git.CommitNotOnBranchError).
type stubNotOnTarget struct{ msg string }

func (e stubNotOnTarget) Error() string         { return e.msg }
func (stubNotOnTarget) CommitNotOnTarget() bool { return true }

// proveNotLanded makes the feeder's git prover answer "not on the target" for
// every commit: a closed MR then definitely did not land (gt-pd1c.59: without
// that proof the issue stays held).
func proveNotLanded(t *testing.T) {
	t.Helper()
	orig := mergeProverResolver
	t.Cleanup(func() { mergeProverResolver = orig })
	mergeProverResolver = func(string) beads.ProverResolver {
		return func(string) beads.MergeProver { return stubProver{landed: false} }
	}
}

// A rejected MR needs rework, so its issue stays feedable.
func TestFindStrandedConvoys_FeedsIssueAfterMRRejected(t *testing.T) {
	proveNotLanded(t)
	s := strandedWithMRQuery(t, `    printf '%s\n' '[{"id":"gt-wisp-mr1","status":"closed","description":"source_issue: gt-work1\ntarget: main\nrig: gastown\ncommit_sha: c1\nclose_reason: rejected\n","labels":["gt:merge-request"]}]'`)
	if s.ReadyCount != 1 {
		t.Fatalf("ReadyCount = %d, want 1 after MR rejected", s.ReadyCount)
	}
}

// A pending MR for a different issue (prefix match only) must not hold it.
func TestFindStrandedConvoys_IgnoresMRForOtherIssue(t *testing.T) {
	s := strandedWithMRQuery(t, `    printf '%s\n' '[{"id":"gt-wisp-mr2","status":"open","description":"source_issue: gt-work10\n","labels":["gt:merge-request"]}]'`)
	if s.ReadyCount != 1 {
		t.Fatalf("ReadyCount = %d, want 1 (MR belongs to gt-work10)", s.ReadyCount)
	}
}

// If the merge queue cannot be read, hold dispatch rather than risk a duplicate.
func TestFindStrandedConvoys_HoldsWhenMRLookupFails(t *testing.T) {
	s := strandedWithMRQuery(t, `    echo 'dolt: connection refused' >&2
    exit 1`)
	if s.ReadyCount != 0 {
		t.Fatalf("ReadyCount = %d, want 0 when MR lookup fails (fail closed)", s.ReadyCount)
	}
}

// gt-pd1c.59: gt done parks the source of a submitted MR (in_progress, no
// assignee). The feeder must hold it while the MR is pending and feed it again
// once the MR was rejected (the refinery reopens it; a parked bead whose MR
// was closed by hand is fed as well).
func TestFindStrandedConvoys_ParkedSourceFollowsTheMergeQueue(t *testing.T) {
	pending := `    printf '%s\n' '[{"id":"gt-wisp-mr1","status":"open","description":"source_issue: gt-work1\n"}]'`
	if s := strandedWithIssueAndMRQuery(t, "in_progress", "", pending); s.ReadyCount != 0 {
		t.Fatalf("ReadyCount = %d (%v), want 0: parked source with a pending MR", s.ReadyCount, s.ReadyIssues)
	}
	proveNotLanded(t)
	rejected := `    printf '%s\n' '[{"id":"gt-wisp-mr1","status":"closed","description":"source_issue: gt-work1\ntarget: main\nrig: gastown\ncommit_sha: c1\nclose_reason: rejected\n"}]'`
	if s := strandedWithIssueAndMRQuery(t, "in_progress", "", rejected); s.ReadyCount != 1 {
		t.Fatalf("ReadyCount = %d, want 1: rejected MR, parked source needs rework", s.ReadyCount)
	}
	if s := strandedWithIssueAndMRQuery(t, "open", "", rejected); s.ReadyCount != 1 {
		t.Fatalf("ReadyCount = %d, want 1: rejected MR, released source", s.ReadyCount)
	}
}

// gt-pd1c.59 round 3: a closed MR whose landing git cannot decide (no prover
// for its rig) keeps its issue held; the feeder does not re-dispatch it.
func TestFindStrandedConvoys_HoldsIssueWhoseLandingIsUnverifiable(t *testing.T) {
	orig := mergeProverResolver
	t.Cleanup(func() { mergeProverResolver = orig })
	mergeProverResolver = func(string) beads.ProverResolver { return func(string) beads.MergeProver { return nil } }
	bare := `    printf '%s\n' '[{"id":"gt-wisp-mr1","status":"closed","description":"source_issue: gt-work1\ntarget: main\nrig: gastown\ncommit_sha: c1\n"}]'`
	if s := strandedWithMRQuery(t, bare); s.ReadyCount != 0 {
		t.Fatalf("ReadyCount = %d, want 0: landing unverifiable, fail closed", s.ReadyCount)
	}
}
