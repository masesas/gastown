package refinery

import (
	"errors"
	"strings"
	"testing"

	gitpkg "github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/rig"
)

// landedOnMain creates branch with one commit, pushes it, merges it into main
// with a merge commit and pushes main. Returns (commit_sha, merge commit).
func landedOnMain(t *testing.T, workDir, branch string) (string, string) {
	t.Helper()
	createFeatureBranch(t, workDir, branch, strings.ReplaceAll(branch, "/", "_")+".txt", "landed\n")
	commit := run(t, workDir, "git", "rev-parse", branch)
	run(t, workDir, "git", "push", "origin", branch)
	run(t, workDir, "git", "checkout", "main")
	run(t, workDir, "git", "merge", "--no-ff", "-m", "merge "+branch, branch)
	run(t, workDir, "git", "push", "origin", "main")
	return commit, run(t, workDir, "git", "rev-parse", "main")
}

// finalizeTestEngineer is a synthetic-MR engineer on workDir with its own rig
// dir (no config.json: the trusted default branch is the remote's HEAD).
func finalizeTestEngineer(t *testing.T, workDir string, g *gitpkg.Git) (*Engineer, *fakeMailSender) {
	t.Helper()
	installFakeGT(t)
	e := newTestEngineer(t, workDir, g)
	e.rig = &rig.Rig{Name: "test-rig", Path: t.TempDir()}
	sender := &fakeMailSender{}
	e.router = sender
	return e, sender
}

func TestHandleMRInfoSuccess_RefusesUntrustedTargetAsProof(t *testing.T) {
	workDir, g, _ := testGitRepo(t)
	installNoPRGH(t)
	// The MR "target" is the polecat's own branch: it trivially contains the
	// submitted commit, so it must never count as landing proof.
	branch := "polecat/test/self-target"
	createFeatureBranch(t, workDir, branch, "self.txt", "self\n")
	commit := run(t, workDir, "git", "rev-parse", branch)
	run(t, workDir, "git", "push", "origin", branch)
	run(t, workDir, "git", "push", "origin", branch+":refs/heads/fake-target")

	e, sender := finalizeTestEngineer(t, workDir, g)
	report := e.HandleMRInfoSuccess(&MRInfo{ID: "mr-self", Branch: branch, Target: "fake-target", CommitSHA: commit}, ProcessResult{Success: true, MergeCommit: commit})

	if report.ProofErr == nil || !strings.Contains(report.ProofErr.Error(), "not a merge target") {
		t.Fatalf("ProofErr = %v, want an untrusted-target refusal", report.ProofErr)
	}
	if report.OK() || report.MRClosed() {
		t.Fatalf("report %+v: OK/MRClosed after a refused proof", report)
	}
	if sender.find("MERGED") != nil {
		t.Fatal("MERGED mail sent although the proof was refused")
	}
	if out := run(t, workDir, "git", "ls-remote", "--heads", "origin", branch); !strings.Contains(out, commit) {
		t.Fatalf("branch deleted after a refused proof: %q", out)
	}
}

func TestHandleMRInfoSuccess_LocalOriginHEADIsNotTrusted(t *testing.T) {
	workDir, g, _ := testGitRepo(t)
	installNoPRGH(t)
	branch := "polecat/test/head-tamper"
	createFeatureBranch(t, workDir, branch, "tamper.txt", "tamper\n")
	commit := run(t, workDir, "git", "rev-parse", branch)
	run(t, workDir, "git", "push", "origin", branch+":refs/heads/release")
	// An agent repoints the local origin/HEAD at "release"; the remote still
	// advertises main.
	run(t, workDir, "git", "fetch", "origin")
	run(t, workDir, "git", "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/release")

	e, _ := finalizeTestEngineer(t, workDir, g)
	report := e.HandleMRInfoSuccess(&MRInfo{ID: "mr-tamper", Branch: branch, Target: "release", CommitSHA: commit}, ProcessResult{Success: true})
	if report.ProofErr == nil {
		t.Fatal("ProofErr = nil: the local origin/HEAD was trusted as the default branch")
	}
}

func TestHandleMRInfoSuccess_ConfiguredDefaultBranchIsTheOnlyTrustedOne(t *testing.T) {
	workDir, g, _ := testGitRepo(t)
	installNoPRGH(t)
	commit, merge := landedOnMain(t, workDir, "polecat/test/cfg-default")

	e, _ := finalizeTestEngineer(t, workDir, g)
	writeRigJSON(t, e.rig.Path, "config.json", map[string]any{"type": "rig", "name": "test-rig", "default_branch": "develop"})
	report := e.HandleMRInfoSuccess(&MRInfo{ID: "mr-cfg", Branch: "polecat/test/cfg-default", Target: "main", CommitSHA: commit}, ProcessResult{Success: true, MergeCommit: merge})
	if report.ProofErr == nil {
		t.Fatal("ProofErr = nil: main trusted although default_branch=develop is configured")
	}
}

func TestHandleMRInfoSuccess_UnsafeBranchNameIsNeverDeleted(t *testing.T) {
	workDir, g, _ := testGitRepo(t)
	installNoPRGH(t)
	commit, merge := landedOnMain(t, workDir, "polecat/test/real")

	e, sender := finalizeTestEngineer(t, workDir, g)
	unsafe := "polecat/test/$(touch pwned)"
	report := e.HandleMRInfoSuccess(&MRInfo{ID: "mr-unsafe", Branch: unsafe, Target: "main", CommitSHA: commit}, ProcessResult{Success: true, MergeCommit: merge})

	if report.ProofErr != nil || report.CloseErr != nil {
		t.Fatalf("proof/close = %v/%v, want both nil", report.ProofErr, report.CloseErr)
	}
	if !report.BranchCleanup.UnsafeName || report.BranchCleanup.Err == nil {
		t.Fatalf("BranchCleanup = %+v, want UnsafeName with an error", report.BranchCleanup)
	}
	if report.BranchCleanup.RemoteDeleted || report.BranchCleanup.LocalDeleted {
		t.Fatalf("BranchCleanup = %+v: deleted something for an unsafe name", report.BranchCleanup)
	}
	if report.OK() || !report.MRClosed() {
		t.Fatalf("report OK=%v MRClosed=%v, want closed but not OK (cleanup failed)", report.OK(), report.MRClosed())
	}
	if sender.find("MERGED") == nil {
		t.Fatal("MERGED not sent although the MR closed as merged")
	}
}

func TestHandleMRInfoSuccess_SendsMergedMailToWitness(t *testing.T) {
	workDir, g, _ := testGitRepo(t)
	installNoPRGH(t)
	run(t, workDir, "git", "remote", "add", "upstream", "https://github.com/example/repo.git")
	branch := "polecat/nux/gt-9"
	commit, merge := landedOnMain(t, workDir, branch)

	e, sender := finalizeTestEngineer(t, workDir, g)
	report := e.HandleMRInfoSuccess(&MRInfo{ID: "mr-mail", Branch: branch, Target: "main", CommitSHA: commit, Worker: "polecats/nux"}, ProcessResult{Success: true, MergeCommit: merge})
	if !report.OK() {
		t.Fatalf("report not OK: %+v (err %v)", report, report.Err())
	}
	if !report.BranchCleanup.RemoteDeleted || !report.BranchCleanup.LocalDeleted {
		t.Fatalf("BranchCleanup = %+v, want remote and local deleted", report.BranchCleanup)
	}
	msg := sender.find("MERGED")
	if msg == nil {
		t.Fatalf("no MERGED mail; sent=%v", sender.subjects())
	}
	if msg.To != "test-rig/witness" || msg.Subject != "MERGED nux" {
		t.Errorf("MERGED to=%q subject=%q, want test-rig/witness, \"MERGED nux\"", msg.To, msg.Subject)
	}
	for _, want := range []string{"Branch: " + branch, "Target: main", "Merge-Commit: " + merge} {
		if !strings.Contains(msg.Body, want) {
			t.Errorf("MERGED body missing %q:\n%s", want, msg.Body)
		}
	}
}

func TestHandleMRInfoSuccess_MailFailureIsReportedNotFatal(t *testing.T) {
	workDir, g, _ := testGitRepo(t)
	installNoPRGH(t)
	commit, merge := landedOnMain(t, workDir, "polecat/test/mailfail")

	e, sender := finalizeTestEngineer(t, workDir, g)
	sender.err = errors.New("mail store down")
	report := e.HandleMRInfoSuccess(&MRInfo{ID: "mr-mailfail", Branch: "polecat/test/mailfail", Target: "main", CommitSHA: commit}, ProcessResult{Success: true, MergeCommit: merge})
	if report.MergedMailErr == nil {
		t.Fatal("MergedMailErr = nil, want the send error")
	}
	if !report.MRClosed() {
		t.Fatal("a mail failure undid the merged close")
	}
}

func TestFinalizeReport_OK(t *testing.T) {
	boom := errors.New("boom")
	for name, tc := range map[string]struct {
		r          FinalizeReport
		ok, closed bool
	}{
		"clean":           {FinalizeReport{}, true, true},
		"proof failed":    {FinalizeReport{ProofErr: boom}, false, false},
		"close failed":    {FinalizeReport{CloseErr: boom}, false, false},
		"cleanup failed":  {FinalizeReport{BranchCleanup: BranchCleanup{Err: boom}}, false, true},
		"mail failed":     {FinalizeReport{MergedMailErr: boom}, true, true},
		"proof and close": {FinalizeReport{ProofErr: boom, CloseErr: boom}, false, false},
	} {
		if got := tc.r.OK(); got != tc.ok {
			t.Errorf("%s: OK() = %v, want %v", name, got, tc.ok)
		}
		if got := tc.r.MRClosed(); got != tc.closed {
			t.Errorf("%s: MRClosed() = %v, want %v", name, got, tc.closed)
		}
		if (tc.r.Err() == nil) != tc.ok {
			t.Errorf("%s: Err() = %v, want nil exactly when OK", name, tc.r.Err())
		}
	}
}
