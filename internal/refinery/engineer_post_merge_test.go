package refinery

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	beadsdk "github.com/steveyegge/beads"
)

// closedMerged puts the fixture MR in the state a gt mq merge run leaves it
// in when it pushed, recorded merge_commit, closed the MR "merged" and then
// failed to finish (cleanup_failed, next = gt mq post-merge): closed, claimed
// by a run that has exited, the source still open, the branch still on origin.
func (f *mergeOneFixture) closedMerged(t *testing.T, mergeCommit string) {
	t.Helper()
	mr := f.store.issues[fixtureMR]
	if mergeCommit != "" {
		mr.Description += "\nmerge_commit: " + mergeCommit
	}
	mr.Description += "\nclose_reason: merged"
	mr.Status = beadsdk.StatusClosed
	mr.CloseReason = string(CloseReasonMerged)
	mr.Assignee = fmt.Sprintf("testrig/refinery/merge-%d-1", deadPID(t))
}

func (f *mergeOneFixture) postMerge(t *testing.T, opts PostMergeOptions) (*MRInfo, FinalizeReport, error) {
	t.Helper()
	mr, report, err := f.engineer(t).PostMerge(fixtureMR, opts)
	t.Logf("post-merge: err=%v report=%+v\nengine output:\n%s", err, report, f.out.String())
	return mr, report, err
}

func (f *mergeOneFixture) assertNothingFinalized(t *testing.T) {
	t.Helper()
	if src := f.store.issue(t, fixtureSource); src.Status == beadsdk.StatusClosed {
		t.Fatalf("source closed (%q) without a bound landing proof", src.CloseReason)
	}
	if remote := run(t, f.workDir, "git", "ls-remote", "--heads", "origin", f.branch); remote == "" {
		t.Fatal("branch deleted without a bound landing proof")
	}
	if f.sender.find("MERGED") != nil {
		t.Fatal("MERGED mailed without a bound landing proof")
	}
}

// gt mq post-merge is a thin wrapper over the engine's one finalize: for an
// MR closed as merged whose recorded merge is proven, it closes the source
// "Merged in <mr>", deletes the branch and mails MERGED.
func TestPostMerge_FinishesAClosedMergedMR(t *testing.T) {
	f := newMergeOneFixture(t)
	f.closedMerged(t, f.landByHand(t))

	mr, report, err := f.postMerge(t, PostMergeOptions{})
	if err != nil || !report.OK() {
		t.Fatalf("PostMerge err=%v report err=%v, want a finished finalize", err, report.Err())
	}
	if mr == nil || mr.ID != fixtureMR {
		t.Fatalf("MRInfo = %+v", mr)
	}
	src := f.store.issue(t, fixtureSource)
	if src.Status != beadsdk.StatusClosed || validateMergedCloseReason(src.CloseReason, fixtureMR) != nil {
		t.Fatalf("source = %s/%q, want closed \"Merged in %s\"", src.Status, src.CloseReason, fixtureMR)
	}
	if !report.BranchCleanup.RemoteDeleted {
		t.Fatalf("BranchCleanup = %+v, want the remote branch deleted", report.BranchCleanup)
	}
	if f.sender.find("MERGED") == nil {
		t.Fatal("no MERGED mail")
	}
}

func TestPostMerge_SkipBranchDeleteKeepsTheBranch(t *testing.T) {
	f := newMergeOneFixture(t)
	f.closedMerged(t, f.landByHand(t))
	_, report, err := f.postMerge(t, PostMergeOptions{SkipBranchDelete: true})
	if err != nil || !report.OK() {
		t.Fatalf("PostMerge err=%v report err=%v", err, report.Err())
	}
	if !report.BranchCleanup.Skipped || report.BranchCleanup.RemoteDeleted || report.BranchCleanup.LocalDeleted {
		t.Fatalf("BranchCleanup = %+v, want skipped", report.BranchCleanup)
	}
	if remote := run(t, f.workDir, "git", "ls-remote", "--heads", "origin", f.branch); remote == "" {
		t.Fatal("--skip-branch-delete deleted the branch")
	}
}

// gt-pd1c.89: "commit_sha is reachable from the target" does not bind the
// landing to this MR (any commit on main passes). Like gt mq merge's
// already-landed path, post-merge needs the MR's recorded merge_commit M,
// proven on the target, with exactly two parents and commit_sha second.
func TestPostMerge_RefusesALandingProofNotBoundToTheMR(t *testing.T) {
	for name, tc := range map[string]struct {
		setup func(t *testing.T, f *mergeOneFixture)
		want  string
	}{
		"forged MR: commit_sha = a commit already on main, no merge_commit": {func(t *testing.T, f *mergeOneFixture) {
			f.store.issues[fixtureMR] = memMRIssue(fixtureMR, f.branch, "main", fixtureSource, f.originMain(t))
			f.closedMerged(t, "")
		}, "no recorded merge"},
		"landed by hand, no merge_commit": {func(t *testing.T, f *mergeOneFixture) {
			f.landByHand(t)
			f.closedMerged(t, "")
		}, "no recorded merge"},
		"merge_commit is a historical merge of another commit": {func(t *testing.T, f *mergeOneFixture) {
			// M is a real merge on main, but of another commit: the MR names
			// M's first parent (already on main) as its commit_sha.
			tip := f.landByHand(t)
			f.store.issues[fixtureMR] = memMRIssue(fixtureMR, f.branch, "main", fixtureSource, run(t, f.workDir, "git", "rev-parse", tip+"^1"))
			f.closedMerged(t, tip)
		}, "second parent"},
		"octopus merge_commit": {func(t *testing.T, f *mergeOneFixture) {
			run(t, f.workDir, "git", "fetch", "-q", "origin")
			run(t, f.workDir, "git", "checkout", "-q", "-b", "other", "origin/main")
			writeFile(t, f.workDir, "other.txt", "other\n")
			run(t, f.workDir, "git", "add", ".")
			run(t, f.workDir, "git", "commit", "-q", "-m", "other")
			run(t, f.workDir, "git", "checkout", "-q", "main")
			run(t, f.workDir, "git", "merge", "-q", "--no-ff", "-m", "octopus", "origin/"+f.branch, "other")
			run(t, f.workDir, "git", "push", "-q", "origin", "main")
			f.closedMerged(t, f.originMain(t))
		}, "parents"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newMergeOneFixture(t)
			tc.setup(t, f)
			_, _, err := f.postMerge(t, PostMergeOptions{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("PostMerge err = %v, want a refusal containing %q", err, tc.want)
			}
			f.assertNothingFinalized(t)
		})
	}
}

// gt-pd1c.89: the source is force-closed only when it is bound to the MR
// (the back-link gt done wrote). A real merge of an MR that names someone
// else's bead as source_issue leaves that bead alone.
func TestPostMerge_RefusesASourceNotBoundToTheMR(t *testing.T) {
	f := newMergeOneFixture(t)
	f.store.comments[fixtureSource] = nil // no "MR created: gt-mr"
	f.closedMerged(t, f.landByHand(t))

	_, report, err := f.postMerge(t, PostMergeOptions{})
	if err != nil {
		t.Fatalf("PostMerge err = %v, want the finalize to run and report the source", err)
	}
	if report.OK() || report.SourceErr == nil || !strings.Contains(report.SourceErr.Error(), "not bound") {
		t.Fatalf("report = %+v, want SourceErr: source not bound to the MR", report)
	}
	if src := f.store.issue(t, fixtureSource); src.Status == beadsdk.StatusClosed {
		t.Fatalf("unbound source closed with %q", src.CloseReason)
	}
}

func TestPostMerge_RefusesOpenAndUnmergedMRs(t *testing.T) {
	for name, tc := range map[string]struct {
		setup func(t *testing.T, f *mergeOneFixture)
		want  string
	}{
		"open MR: gt mq merge finalizes it": {func(t *testing.T, f *mergeOneFixture) {
			tip := f.landByHand(t)
			f.store.issues[fixtureMR].Description += "\nmerge_commit: " + tip
		}, "gt mq merge testrig " + fixtureMR},
		"closed as rejected": {func(t *testing.T, f *mergeOneFixture) {
			f.closedMerged(t, f.landByHand(t))
			mr := f.store.issues[fixtureMR]
			mr.Description = strings.Replace(mr.Description, "close_reason: merged", "close_reason: rejected", 1)
		}, "rejected"},
		"not a merge request": {func(t *testing.T, f *mergeOneFixture) {
			f.store.issues[fixtureMR].Labels = nil
		}, "not a merge request"},
		"another rig's MR": {func(t *testing.T, f *mergeOneFixture) {
			f.closedMerged(t, f.landByHand(t))
			mr := f.store.issues[fixtureMR]
			mr.Description = strings.Replace(mr.Description, "rig: testrig", "rig: otherrig", 1)
		}, "otherrig"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newMergeOneFixture(t)
			tc.setup(t, f)
			_, _, err := f.postMerge(t, PostMergeOptions{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("PostMerge err = %v, want a refusal containing %q", err, tc.want)
			}
			f.assertNothingFinalized(t)
		})
	}
	f := newMergeOneFixture(t)
	if _, _, err := f.engineer(t).PostMerge("gt-mr; rm -rf /", PostMergeOptions{}); err == nil {
		t.Fatal("an MR id that is not a plain bead id must be refused")
	}
}

// F12 / gt-pd1c.64: on a remote that hosts no pull requests (the pilot
// mirror is a file:// repository) the merged branch is deleted after the
// landing proof; an unknowable PR state no longer keeps it forever.
func TestMergeOne_DeletesTheMergedBranchOnAFileRemote(t *testing.T) {
	f := newMergeOneFixture(t)
	bare := strings.TrimSpace(run(t, f.workDir, "git", "remote", "get-url", "origin"))
	run(t, f.workDir, "git", "remote", "set-url", "origin", "file://"+bare)
	// gh would answer "open PR" for anything; it must not be consulted.
	dir := t.TempDir()
	marker := filepath.Join(dir, "gh-called")
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\ntouch "+marker+"\necho '{\"number\":1,\"state\":\"OPEN\"}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	out := f.engineer(t).MergeOne(context.Background(), fixtureMR)
	t.Logf("outcome: %+v\n%s", out, f.out.String())
	if out.Kind != OutcomeMerged {
		t.Fatalf("outcome = %s %q, want merged", out.Kind, out.Message)
	}
	if out.BranchCleanup == nil || !out.BranchCleanup.RemoteDeleted || out.BranchCleanup.OpenPR {
		t.Fatalf("BranchCleanup = %+v, want the branch deleted on the file:// remote", out.BranchCleanup)
	}
	if remote := run(t, f.workDir, "git", "ls-remote", "--heads", "origin", f.branch); remote != "" {
		t.Fatalf("branch still on origin: %q", remote)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("gh was consulted for a file:// remote")
	}
}
