package cmd

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
	gitpkg "github.com/steveyegge/gastown/internal/git"
)

// gt-pd1c.78 tahap 1, review round 2.

// (a) gt mq post-merge / the refinery delete the polecat branch after the MR
// landed: with the target tip present locally the git proof alone passes; a
// recorded merged verdict falls back to git instead of refusing.
func TestPrePushGuardRefineryPostMergeDelete(t *testing.T) {
	f := newPrePushFixture(t)
	plain := &prePushRig{name: "myrig", defaultBranch: "main"}
	refinery := []string{"GT_ROLE", "myrig/refinery", "BD_ACTOR", "myrig/refinery"}
	const branch = "polecat/nux/gt-1"
	del := func(tip string) []prePushUpdate {
		return []prePushUpdate{prePushUpd("refs/heads/"+branch, testZeroSHA, tip)}
	}

	// Post-merge shape: MR merged by the refinery (recorded), its commit is
	// the tip and is on the remote main, whose tip is in this repository.
	runGitForMQSubmitTest(t, f.repo, "push", "-q", f.remote, f.good+":refs/heads/main")
	calls := 0
	merged := mrIssue("mr1", "closed", branch, f.good, "close_reason: merged\nmerge_commit: "+f.good+"\n")
	queue := func() *beads.MergeQueue { calls++; return mqWith(merged)() }
	if err := coordinatorGuard(f, plain, queue, refinery...).check(del(f.good)); err != nil || calls != 0 {
		t.Fatalf("post-merge delete: err=%v queue reads=%d; the git proof must pass alone", err, calls)
	}

	// Target tip not provable locally (tip f.bad is not on any remote
	// target): the recorded verdict falls back to git.
	recorded := mrIssue("mr2", "closed", branch, f.bad, "close_reason: merged\n")
	if err := coordinatorGuard(f, plain, mqWithProver(gitLanded{f.bad: true}, recorded), refinery...).check(del(f.bad)); err != nil {
		t.Fatalf("recorded verdict that git confirms: %v", err)
	}
	// tahap 2 (F7): git disproves the record, so the landing is unverified.
	err := coordinatorGuard(f, plain, mqWithProver(gitLanded{}, recorded), refinery...).check(del(f.bad))
	if err == nil || !strings.Contains(err.Error(), "recorded as merged") {
		t.Fatalf("recorded verdict that git does not confirm: %v", err)
	}
}

// (b) An unreadable remote does not silently skip the git proof: the merge
// queue decides on the local tip and refusals say why git could not help.
func TestPolecatRepoBranchDeletableRemoteUnreadable(t *testing.T) {
	f := newPrePushFixture(t)
	runGitForMQSubmitTest(t, f.repo, "branch", "polecat/nux", f.good)
	runGitForMQSubmitTest(t, f.repo, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing.git"))
	repo := gitpkg.NewGit(f.repo)
	err := polecatRepoBranchDeletable(repo, "gastown", "polecat/nux", "main", mqWith(mrIssue("gt-mr1", "open", "polecat/nux", f.good, "")))
	if err == nil || !strings.Contains(err.Error(), "git proof unavailable") {
		t.Fatalf("open MR, remote unreadable: %v", err)
	}
	// tahap 2: without an MR the work is not proven safe either.
	err = polecatRepoBranchDeletable(repo, "gastown", "polecat/nux", "main", mqWith())
	if err == nil || !strings.Contains(err.Error(), "no MR recorded this tip") || !strings.Contains(err.Error(), "git proof unavailable") {
		t.Fatalf("no MR, remote unreadable: %v", err)
	}
	// No local branch and origin unreadable: the tip is unknown, refused.
	if err := polecatRepoBranchDeletable(repo, "gastown", "polecat/ghost", "main", mqWith()); err == nil ||
		!strings.Contains(err.Error(), "tip is unknown") {
		t.Fatalf("no local branch, remote unreadable: %v", err)
	}
	// Rejected work is offered abandon, never deleted directly.
	rejected := mqWithProver(gitLanded{}, mrIssue("gt-mr2", "closed", "polecat/nux", f.good, "close_reason: rejected\n"))
	if err := polecatRepoBranchDeletable(repo, "gastown", "polecat/nux", "main", rejected); err == nil ||
		!strings.Contains(err.Error(), "gt polecat abandon gastown/polecat/nux") {
		t.Fatalf("rejected MR, remote unreadable: %v", err)
	}
}

// (d) The --force target check stops at a refused branch.
func TestForceDiscardCheckShortCircuits(t *testing.T) {
	err := forceDiscardCheck(polecatTarget{rigName: "gastown", polecatName: "nux"})
	if err == nil || strings.Count(err.Error(), "fail closed") != 1 || strings.Contains(err.Error(), "\n") {
		t.Fatalf("want only the branch refusal, got %q", err)
	}
}

// (c) Branch ownership: Go (gtCaller.ownsPolecatBranch) and the bash fallback
// (owns_branch) agree on every row.
var ownershipMatrix = []struct {
	polecat, branch string
	owner           bool
}{
	{"nux", "polecat/nux", true},
	{"nux", "polecat/nux/gt-1+abc", true},
	{"nux", "polecat/nux/gt-1", true},
	{"nux", "polecat/nux-abc123", true},
	{"nux", "polecat/nux-2-abc", false},
	{"nux-2", "polecat/nux-2-abc", true},
	{"nux-2", "polecat/nux-2", true},
	{"nux", "polecat/nuxx", false},
	{"nux", "polecat/max/gt-1", false},
	{"nux", "polecat/max-nux", false},
	{"nux", "polecat/nux2", false},
}

func TestOwnershipMatrixGo(t *testing.T) {
	for _, c := range ownershipMatrix {
		got := gtCallerFrom(prePushEnv("GT_ROLE", "gastown/polecats/"+c.polecat, "GT_POLECAT", c.polecat)).ownsPolecatBranch(c.branch)
		if got != c.owner {
			t.Errorf("polecat %s, branch %s: owner=%v, want %v", c.polecat, c.branch, got, c.owner)
		}
	}
	if gtCallerFrom(prePushEnv("GT_ROLE", "gastown/witness")).ownsPolecatBranch("polecat/witness") {
		t.Error("only a polecat owns a polecat branch")
	}
}

func TestOwnershipMatrixBashParity(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	hook, err := filepath.Abs(filepath.Join("..", "..", ".githooks", "pre-push"))
	if err != nil {
		t.Fatal(err)
	}
	f := newPrePushFixture(t)
	nogt := t.TempDir()
	for _, tool := range []string{"git", "sed", "awk", "cat"} {
		p, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("%s not available", tool)
		}
		if err := os.Symlink(p, filepath.Join(nogt, tool)); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range ownershipMatrix {
		// A rewrite (f.good is not an ancestor of main) passes only for the owner.
		cmd := exec.Command(bash, hook, "origin")
		cmd.Dir = f.repo
		cmd.Env = []string{"PATH=" + nogt, "HOME=" + t.TempDir(), "GT_ROLE=gastown/polecats/" + c.polecat, "GT_POLECAT=" + c.polecat}
		cmd.Stdin = strings.NewReader("HEAD " + f.mainSHA + " refs/heads/" + c.branch + " " + f.good + "\n")
		out, runErr := cmd.CombinedOutput()
		if got := runErr == nil; got != c.owner {
			var exitErr *exec.ExitError
			if runErr != nil && !errors.As(runErr, &exitErr) {
				t.Fatalf("running the hook: %v", runErr)
			}
			t.Errorf("bash, polecat %s, branch %s: allowed=%v, want %v\n%s", c.polecat, c.branch, got, c.owner, out)
		}
	}
}
