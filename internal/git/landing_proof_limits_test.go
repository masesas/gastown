package git

import (
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// gt-pd1c.81 review round 1: companion signals, base bound, merge topology
// and execution limits of the landing proof.

// A rebase that resolved a conflict changes the patch-id, but keeps the
// subject: the answer must stay open instead of a definite "not landed".
func TestLandingProofConflictResolvedRebaseIsUncertain(t *testing.T) {
	_, work := lpOrigin(t)
	lpCommit(t, work, "f.txt", "a\nb\nc\n")
	lpGit(t, work, "push", "-q", "origin", "main")
	lpGit(t, work, "checkout", "-q", "-b", "polecat/feat")
	head := lpCommit(t, work, "f.txt", "a\nB-polecat\nc\n")
	lpGit(t, work, "checkout", "-q", "main")
	lpCommit(t, work, "f.txt", "a\nB-main\nc\n")
	// The refinery's conflict resolution: same subject, a different patch.
	lpGit(t, work, "checkout", "-q", "-b", "resolved")
	lpCommit(t, work, "f.txt", "a\nB-main-and-polecat\nc\n")
	lpGit(t, work, "commit", "-q", "--amend", "-m", "change f.txt")
	lpGit(t, work, "push", "-q", "origin", "resolved:main")
	g := NewGit(work)

	if err := g.VerifyPushedCommitReachableFromPushTarget("origin", "main", head); !isNotOnBranch(err) {
		t.Fatalf("precondition: head not on main, got %v", err)
	}
	ok, err := g.PatchEquivalentOnPushTarget("origin", "main", head, "")
	if err == nil || !strings.Contains(err.Error(), "subject") {
		t.Fatalf("conflict-resolved rebase: want uncertain (subject signal), got (%v, %v)", ok, err)
	}
}

// A trivial patch that another bead landed before the MR was submitted is not
// proof of this MR's landing once base_sha bounds the target range.
func TestLandingProofBaseBoundsTheTargetRange(t *testing.T) {
	_, work := lpOrigin(t)
	lpGit(t, work, "checkout", "-q", "-b", "polecat/feat")
	head := lpCommit(t, work, "trivial.txt", "1\n")
	lpGit(t, work, "checkout", "-q", "main")
	lpCommit(t, work, "trivial.txt", "1\n")
	lpGit(t, work, "commit", "-q", "--amend", "-m", "other bead, same one-liner")
	base := lpGit(t, work, "rev-parse", "HEAD") // target tip at submission
	lpCommit(t, work, "m.txt", "later\n")
	lpGit(t, work, "push", "-q", "origin", "main")
	g := NewGit(work)

	if ok, err := g.PatchEquivalentOnPushTarget("origin", "main", head, ""); err != nil || !ok {
		t.Fatalf("precondition: unbounded range matches the older patch, got (%v, %v)", ok, err)
	}
	ok, err := g.PatchEquivalentOnPushTarget("origin", "main", head, base)
	if err != nil || ok {
		t.Fatalf("bounded by base_sha: want (false, nil), got (%v, %v)", ok, err)
	}

	// A base that is not on the branch (history rewritten) cannot bound it.
	lpGit(t, work, "checkout", "-q", "-b", "elsewhere", "polecat/feat")
	stray := lpCommit(t, work, "stray.txt", "s\n")
	if ok, err := g.PatchEquivalentOnPushTarget("origin", "main", head, stray); err == nil {
		t.Fatalf("base off the branch: want uncertain, got (%v, nil)", ok)
	}
}

// Merge topology with two shallow boundaries (a --depth 2 clone of a merge):
// a commit on top of the merge is decidable, a commit on only one parent is not.
func TestLandingProofShallowMergeTopology(t *testing.T) {
	origin, work := lpOrigin(t)
	lpGit(t, work, "checkout", "-q", "-b", "side")
	lpCommit(t, work, "s.txt", "s\n")
	lpGit(t, work, "checkout", "-q", "main")
	lpCommit(t, work, "a.txt", "a\n")
	lpGit(t, work, "merge", "-q", "--no-ff", "-m", "merge side", "side")
	lpGit(t, work, "push", "-q", "origin", "main")

	shallow := filepath.Join(t.TempDir(), "sh")
	lpGit(t, work, "clone", "-q", "--single-branch", "--depth", "2", "file://"+origin, shallow)
	if n := len(strings.Fields(lpGit(t, shallow, "rev-parse", "--git-path", "shallow"))); n == 0 {
		t.Fatal("precondition: no shallow file")
	}
	g := NewGit(shallow)
	boundaries, err := g.shallowBoundaries()
	if err != nil || len(boundaries) != 2 {
		t.Fatalf("precondition: want 2 shallow boundaries, got %v %v", boundaries, err)
	}

	onMerge := lpCommit(t, shallow, "c.txt", "on merge\n")
	if ok, why := g.ancestryDecidable("origin/main", onMerge); !ok {
		t.Fatalf("commit on top of the merge must be decidable: %s", why)
	}
	lpGit(t, shallow, "checkout", "-q", "HEAD~1^1") // the main-side parent only
	onParent := lpCommit(t, shallow, "d.txt", "on one parent\n")
	if ok, _ := g.ancestryDecidable("origin/main", onParent); ok {
		t.Fatal("the side boundary is reachable from the tip but not an ancestor of the commit: must be undecidable")
	}
}

func TestRunCmdLimits(t *testing.T) {
	_, work := lpOrigin(t)
	g := NewGit(work)

	if _, err := g.runCmd(runOptions{maxOutput: 8}, "log"); !errors.Is(err, ErrGitOutputTooLarge) {
		t.Fatalf("output limit: want ErrGitOutputTooLarge, got %v", err)
	}
	if out, err := g.runCmd(runOptions{maxOutput: 1 << 20}, "rev-parse", "--is-inside-work-tree"); err != nil || strings.TrimSpace(out) != "true" {
		t.Fatalf("under the limit: got %q %v", out, err)
	}
	if _, err := g.runCmd(runOptions{timeout: time.Nanosecond}, "log"); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout: want a timed-out error, got %v", err)
	}
	in := "hello\n"
	out, err := g.runCmd(runOptions{stdin: &in}, "hash-object", "--stdin")
	if err != nil || strings.TrimSpace(out) != "ce013625030ba8dba906f756967f9e9ca394464a" {
		t.Fatalf("stdin: got %q %v", out, err)
	}
	if out, err := g.runCmd(runOptions{env: []string{"GIT_AUTHOR_NAME=limits"}}, "var", "GIT_AUTHOR_IDENT"); err != nil || !strings.HasPrefix(out, "limits ") {
		t.Fatalf("env: got %q %v", out, err)
	}
}

// gt-pd1c.81 review round 2: on timeout the whole process group dies, so a
// child that keeps git's stdout open (ssh, remote helper, alias shell) cannot
// hold runCmd past the deadline.
func TestRunCmdTimeoutKillsChildProcesses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process groups are unix-only here")
	}
	_, work := lpOrigin(t)
	g := NewGit(work)
	start := time.Now()
	_, err := g.runCmd(runOptions{timeout: 300 * time.Millisecond}, "-c", "alias.hang=!sleep 30", "hang")
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("runCmd returned after %v: the child kept the pipe open past the timeout", elapsed)
	}
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want a timed-out error, got %v", err)
	}
}

func TestWithProofLimits(t *testing.T) {
	g := NewGitWithDir("/x.git", "")
	b := g.WithProofLimits()
	if g.HasProofLimits() || !b.HasProofLimits() {
		t.Fatal("WithProofLimits must bound a copy, not the original")
	}
	if b.limits.timeout != proofTimeout || b.limits.maxOutput != proofMaxOutput || b.gitDir != "/x.git" {
		t.Fatalf("unexpected limits %+v", b.limits)
	}
	got := b.limits.under(runOptions{timeout: time.Second, env: []string{"A=1"}})
	if got.timeout != time.Second || got.maxOutput != proofMaxOutput || len(got.env) != 2 || got.env[1] != "A=1" {
		t.Fatalf("explicit options must win, defaults fill the rest: %+v", got)
	}
	if (runOptions{}).under(runOptions{}).timeout != 0 {
		t.Fatal("an unbounded Git must stay unbounded")
	}
	// Limits apply to every command of the bounded copy.
	_, work := lpOrigin(t)
	small := NewGit(work).WithProofLimits()
	small.limits.maxOutput = 8
	if _, err := small.runCmd(runOptions{}, "log"); !errors.Is(err, ErrGitOutputTooLarge) {
		t.Fatalf("bounded Git: want ErrGitOutputTooLarge, got %v", err)
	}
}
