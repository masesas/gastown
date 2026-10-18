package git

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Remote proof of preservation (gt-pd1c.86): branchPreservationStatus decides
// "this work exists on the remote" from the remote itself (ls-remote), never
// from local refs/remotes/* refs, local branches or a local origin/HEAD that an
// agent can move without pushing anything.

// newUnpushedPolecat returns a repo (bare origin, main pushed) checked out on a
// polecat branch with one local-only commit and no upstream.
func newUnpushedPolecat(t *testing.T) (g *Git, localDir, remoteDir, mainBranch, branch string) {
	t.Helper()
	localDir, remoteDir, mainBranch = initTestRepoWithRemote(t)
	g = NewGit(localDir)
	branch = "polecat/proof"
	if err := g.CreateBranch(branch); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if err := g.Checkout(branch); err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	commitFile(t, g, localDir, "work.go", "package work\n", "local only work")
	return g, localDir, remoteDir, mainBranch, branch
}

func commitFile(t *testing.T, g *Git, dir, name, content, msg string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	if err := g.Add(name); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := g.Commit(msg); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	sha, err := g.Rev("HEAD")
	if err != nil {
		t.Fatalf("Rev: %v", err)
	}
	return sha
}

// assertNotPreserved checks every public view of the preservation gate.
func assertNotPreserved(t *testing.T, g *Git, branch string, targets []string) {
	t.Helper()
	st, err := g.BranchPreservationStatus(branch, "origin", targets)
	if err == nil && (st.Preserved || st.UnpreservedPatchCount == 0) {
		t.Fatalf("BranchPreservationStatus = %+v, want unpreserved work", st)
	}
	tst, err := g.BranchTargetStatus(branch, "origin", targets)
	if err == nil && (tst.Preserved || tst.UnpreservedPatchCount == 0) {
		t.Fatalf("BranchTargetStatus = %+v, want unpreserved work", tst)
	}
	if targets != nil {
		return
	}
	pushed, unpushed, err := g.BranchPushedToRemote(branch, "origin")
	if err == nil && (pushed || unpushed == 0) {
		t.Fatalf("BranchPushedToRemote = (%v, %d), want unpushed work", pushed, unpushed)
	}
	if n, err := g.UnpushedCommits(); err == nil && n == 0 {
		t.Fatal("UnpushedCommits = 0, want unpushed work")
	}
	ws, err := g.CheckUncommittedWork()
	if err != nil {
		t.Fatalf("CheckUncommittedWork must not fail open with an error: %v", err)
	}
	if ws.Clean() || ws.CleanExcludingBeads() {
		t.Fatalf("CheckUncommittedWork = %s, want not clean", ws)
	}
}

func TestPreservationRefusesForgedOriginHEAD(t *testing.T) {
	g, localDir, _, _, branch := newUnpushedPolecat(t)
	runGit(t, localDir, "update-ref", "refs/remotes/origin/zzz", "HEAD")
	runGit(t, localDir, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/zzz")

	assertNotPreserved(t, g, branch, nil)
}

func TestPreservationRefusesForgedOriginMain(t *testing.T) {
	g, localDir, _, mainBranch, branch := newUnpushedPolecat(t)
	runGit(t, localDir, "update-ref", "refs/remotes/origin/"+mainBranch, "HEAD")
	runGit(t, localDir, "update-ref", "refs/remotes/origin/master", "HEAD")
	runGit(t, localDir, "update-ref", "refs/remotes/origin/main", "HEAD")

	assertNotPreserved(t, g, branch, nil)
	assertNotPreserved(t, g, branch, []string{mainBranch})
	assertNotPreserved(t, g, branch, []string{"origin/" + mainBranch})
}

func TestPreservationRefusesForgedLocalTargetBranch(t *testing.T) {
	g, localDir, _, _, branch := newUnpushedPolecat(t)
	runGit(t, localDir, "branch", "-f", "integration/epic", "HEAD")
	runGit(t, localDir, "update-ref", "refs/remotes/upstream/integration/epic", "HEAD")

	assertNotPreserved(t, g, branch, []string{"integration/epic"})
}

func TestPreservationRefusesForgedUpstreamTrackingRef(t *testing.T) {
	g, localDir, _, _, branch := newUnpushedPolecat(t)
	runGit(t, localDir, "update-ref", "refs/remotes/origin/"+branch, "HEAD")
	runGit(t, localDir, "branch", "--set-upstream-to=origin/"+branch, branch)

	assertNotPreserved(t, g, branch, nil)

	runGit(t, localDir, "update-ref", "refs/remotes/origin/decoy", "HEAD")
	runGit(t, localDir, "branch", "--set-upstream-to=origin/decoy", branch)
	assertNotPreserved(t, g, branch, nil)
}

func TestPreservationPassesGenuinelyPushedBranch(t *testing.T) {
	g, localDir, _, _, branch := newUnpushedPolecat(t)
	runGit(t, localDir, "push", "origin", branch)
	// No local tracking ref: the proof comes from the remote alone.
	runGit(t, localDir, "update-ref", "-d", "refs/remotes/origin/"+branch)

	st, err := g.BranchPreservationStatus(branch, "origin", nil)
	if err != nil || !st.Preserved || st.UnpreservedPatchCount != 0 {
		t.Fatalf("BranchPreservationStatus = %+v, %v; want preserved", st, err)
	}
	if st.ComparisonBase != "origin/"+branch {
		t.Fatalf("ComparisonBase = %q, want origin/%s", st.ComparisonBase, branch)
	}
	pushed, unpushed, err := g.BranchPushedToRemote(branch, "origin")
	if err != nil || !pushed || unpushed != 0 {
		t.Fatalf("BranchPushedToRemote = (%v, %d, %v), want pushed", pushed, unpushed, err)
	}
	ws, err := g.CheckUncommittedWork()
	if err != nil || !ws.Clean() {
		t.Fatalf("CheckUncommittedWork = %v, %v; want clean", ws, err)
	}
	// Pushed but not on a target: the target view still sees the work.
	tst, err := g.BranchTargetStatus(branch, "origin", nil)
	if err != nil || tst.Preserved || tst.UnpreservedPatchCount == 0 {
		t.Fatalf("BranchTargetStatus = %+v, %v; want unsubmitted work", tst, err)
	}
}

// mergeOnRemote merges branchSHA into mainBranch through a second clone and
// pushes it, so the merge exists on the remote only; the local repo then
// fetches the objects and drops its tracking refs.
func mergeOnRemote(t *testing.T, localDir, remoteDir, mainBranch, branchSHA string, squash bool) {
	t.Helper()
	other := filepath.Join(t.TempDir(), "other")
	if out, err := exec.Command("git", "clone", remoteDir, other).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	runGit(t, other, "config", "user.email", "o@test.com")
	runGit(t, other, "config", "user.name", "Other")
	runGit(t, other, "checkout", mainBranch)
	runGit(t, localDir, "push", "--force", remoteDir, branchSHA+":refs/heads/tmp-merge-src")
	runGit(t, other, "fetch", "origin", "tmp-merge-src")
	if squash {
		runGit(t, other, "merge", "--squash", "FETCH_HEAD")
		runGit(t, other, "commit", "-m", "squash merge")
	} else {
		runGit(t, other, "merge", "--no-ff", "-m", "merge", "FETCH_HEAD")
	}
	runGit(t, other, "push", "origin", mainBranch)
	runGit(t, other, "push", "origin", "--delete", "tmp-merge-src")
	runGit(t, localDir, "fetch", "origin")
	runGit(t, localDir, "update-ref", "-d", "refs/remotes/origin/"+mainBranch)
}

func TestPreservationPassesWorkMergedOnRemoteDefault(t *testing.T) {
	for _, squash := range []bool{false, true} {
		name := "merge"
		if squash {
			name = "squash"
		}
		t.Run(name, func(t *testing.T) {
			g, localDir, remoteDir, mainBranch, branch := newUnpushedPolecat(t)
			if squash {
				if err := exec.Command("git", "-C", localDir, "merge-tree", "--write-tree", "HEAD", "HEAD").Run(); err != nil {
					t.Skipf("git merge-tree --write-tree unsupported: %v", err)
				}
			}
			head, _ := g.Rev("HEAD")
			mergeOnRemote(t, localDir, remoteDir, mainBranch, head, squash)

			for _, targets := range [][]string{nil, {mainBranch}, {"origin/" + mainBranch}} {
				st, err := g.BranchPreservationStatus(branch, "origin", targets)
				if err != nil || !st.Preserved {
					t.Fatalf("BranchPreservationStatus(%v) = %+v, %v; want preserved", targets, st, err)
				}
				tst, err := g.BranchTargetStatus(branch, "origin", targets)
				if err != nil || !tst.Preserved {
					t.Fatalf("BranchTargetStatus(%v) = %+v, %v; want preserved", targets, tst, err)
				}
			}
			ws, err := g.CheckUncommittedWork()
			if err != nil || !ws.Clean() {
				t.Fatalf("CheckUncommittedWork = %v, %v; want clean", ws, err)
			}
		})
	}
}

func TestPreservationPassesReviewOnlyLegWithoutCommits(t *testing.T) {
	localDir, _, mainBranch := initTestRepoWithRemote(t)
	g := NewGit(localDir)
	branch := "polecat/review-leg"
	if err := g.CreateBranch(branch); err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if err := g.Checkout(branch); err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	runGit(t, localDir, "update-ref", "-d", "refs/remotes/origin/"+mainBranch)

	st, err := g.BranchPreservationStatus(branch, "origin", nil)
	if err != nil || !st.Preserved {
		t.Fatalf("BranchPreservationStatus = %+v, %v; want preserved (no own commits)", st, err)
	}
	tst, err := g.BranchTargetStatus(branch, "origin", []string{mainBranch})
	if err != nil || !tst.Preserved {
		t.Fatalf("BranchTargetStatus = %+v, %v; want preserved", tst, err)
	}
	ws, err := g.CheckUncommittedWork()
	if err != nil || !ws.Clean() {
		t.Fatalf("CheckUncommittedWork = %v, %v; want clean", ws, err)
	}
}

func TestPreservationRemoteUnreachableIsNotPreserved(t *testing.T) {
	g, localDir, _, mainBranch, branch := newUnpushedPolecat(t)
	// Even with every local ref forged, an unreadable remote proves nothing.
	runGit(t, localDir, "update-ref", "refs/remotes/origin/"+mainBranch, "HEAD")
	runGit(t, localDir, "update-ref", "refs/remotes/origin/"+branch, "HEAD")
	runGit(t, localDir, "branch", "--set-upstream-to=origin/"+branch, branch)
	runGit(t, localDir, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))

	st, err := g.BranchPreservationStatus(branch, "origin", nil)
	if !errors.Is(err, ErrRemoteProofUnavailable) {
		t.Fatalf("BranchPreservationStatus err = %v, want ErrRemoteProofUnavailable", err)
	}
	if st.Preserved {
		t.Fatalf("BranchPreservationStatus = %+v, want not preserved", st)
	}
	if _, err := g.BranchTargetStatus(branch, "origin", []string{mainBranch}); !errors.Is(err, ErrRemoteProofUnavailable) {
		t.Fatalf("BranchTargetStatus err = %v, want ErrRemoteProofUnavailable", err)
	}
	if pushed, _, err := g.BranchPushedToRemote(branch, "origin"); pushed || err == nil {
		t.Fatalf("BranchPushedToRemote = (%v, %v), want not pushed with error", pushed, err)
	}
	if _, err := g.UnpushedCommits(); !errors.Is(err, ErrRemoteProofUnavailable) {
		t.Fatalf("UnpushedCommits err = %v, want ErrRemoteProofUnavailable", err)
	}
	ws, err := g.CheckUncommittedWork()
	if err != nil {
		t.Fatalf("CheckUncommittedWork: %v (an error would fail open in nuke callers)", err)
	}
	if !ws.UnpushedUnverified || ws.Clean() || ws.CleanExcludingBeads() {
		t.Fatalf("CheckUncommittedWork = %+v, want unverified and not clean", ws)
	}
	if !strings.Contains(ws.String(), "unverified") {
		t.Fatalf("String() = %q, want it to mention unverified", ws.String())
	}
}

func TestPreservationRemoteWithoutBaseIsNotPreserved(t *testing.T) {
	g, localDir, _, _, branch := newUnpushedPolecat(t)
	empty := filepath.Join(t.TempDir(), "empty.git")
	if out, err := exec.Command("git", "init", "--bare", empty).CombinedOutput(); err != nil {
		t.Fatalf("init empty: %v\n%s", err, out)
	}
	runGit(t, localDir, "remote", "set-url", "origin", empty)

	assertNotPreserved(t, g, branch, nil)
	n, err := g.UnpushedCommits()
	if err != nil || n != 2 {
		t.Fatalf("UnpushedCommits = %d, %v; want every commit (2) unpushed", n, err)
	}
}

func TestPreservationWithoutRemoteIsUnverified(t *testing.T) {
	dir := initTestRepo(t)
	g := NewGit(dir)
	if n, err := g.UnpushedCommits(); !errors.Is(err, ErrRemoteProofUnavailable) || n != 0 {
		t.Fatalf("UnpushedCommits without a remote = %d, %v; want 0, ErrRemoteProofUnavailable", n, err)
	}
	if _, err := g.BranchPreservationStatus("main", "origin", nil); !errors.Is(err, ErrRemoteProofUnavailable) {
		t.Fatalf("BranchPreservationStatus without a remote err = %v, want ErrRemoteProofUnavailable", err)
	}
}

func TestRemoteProofBranchName(t *testing.T) {
	cases := map[string]string{
		"main":                          "main",
		"origin/main":                   "main",
		"refs/heads/integration/x":      "integration/x",
		"refs/remotes/origin/feature/a": "feature/a",
		"integration/epic":              "integration/epic",
		"":                              "",
		"-x":                            "",
		"refs/remotes/upstream/main":    "",
		"upstream/main":                 "",
		"bad name":                      "",
		"glob/*":                        "",
		"refs/tags/v1":                  "",
		"HEAD":                          "",
	}
	for in, want := range cases {
		if got := remoteProofBranchName(in, "origin"); got != want {
			t.Errorf("remoteProofBranchName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPreservationUnfetchedRemoteTipIsNotPreserved(t *testing.T) {
	g, localDir, remoteDir, mainBranch, branch := newUnpushedPolecat(t)
	head, _ := g.Rev("HEAD")
	// The work lands on the remote, but this clone has not fetched it.
	mergeOnRemote(t, localDir, remoteDir, mainBranch, head, false)
	other := filepath.Join(t.TempDir(), "advancer")
	if out, err := exec.Command("git", "clone", remoteDir, other).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	runGit(t, other, "config", "user.email", "o@test.com")
	runGit(t, other, "config", "user.name", "Other")
	runGit(t, other, "checkout", mainBranch)
	runGit(t, other, "commit", "--allow-empty", "-m", "later")
	runGit(t, other, "push", "origin", mainBranch)

	_, err := g.BranchPreservationStatus(branch, "origin", nil)
	if !errors.Is(err, ErrRemoteProofUnavailable) || !strings.Contains(err.Error(), "git fetch") {
		t.Fatalf("BranchPreservationStatus err = %v, want ErrRemoteProofUnavailable asking for git fetch", err)
	}
	ws, err := g.CheckUncommittedWork()
	if err != nil || !ws.UnpushedUnverified || ws.Clean() {
		t.Fatalf("CheckUncommittedWork = %+v, %v; want unverified", ws, err)
	}

	runGit(t, localDir, "fetch", "origin")
	st, err := g.BranchPreservationStatus(branch, "origin", nil)
	if err != nil || !st.Preserved || st.ComparisonBase != "origin/"+mainBranch {
		t.Fatalf("after fetch: %+v, %v; want preserved on origin/%s", st, err, mainBranch)
	}
}

func TestPreservationCountsWorkBeyondPushedBranch(t *testing.T) {
	g, localDir, _, _, branch := newUnpushedPolecat(t)
	runGit(t, localDir, "push", "origin", branch)
	commitFile(t, g, localDir, "more.go", "package more\n", "more local work")

	st, err := g.BranchPreservationStatus(branch, "origin", nil)
	if err != nil || st.Preserved || st.UnpreservedPatchCount != 1 || st.ComparisonBase != "origin/"+branch {
		t.Fatalf("BranchPreservationStatus = %+v, %v; want 1 unpreserved patch vs origin/%s", st, err, branch)
	}
}

// H1 (security review): the remote's tip is honest, but a replace ref or a
// graft in the local repository can make it "contain" HEAD.
func TestPreservationRefusesForgedAncestry(t *testing.T) {
	for _, mechanism := range []string{"replace", "graft"} {
		t.Run(mechanism, func(t *testing.T) {
			g, localDir, _, mainBranch, branch := newUnpushedPolecat(t)
			head, _ := g.Rev("HEAD")
			mainTip, _ := g.Rev("refs/remotes/origin/" + mainBranch)
			switch mechanism {
			case "replace":
				fake, err := g.run("commit-tree", mainTip+"^{tree}", "-p", mainTip, "-p", head, "-m", "forged")
				if err != nil {
					t.Fatalf("commit-tree: %v", err)
				}
				runGit(t, localDir, "replace", mainTip, fake)
			case "graft":
				parent, _ := g.Rev(mainTip + "^")
				grafts := filepath.Join(localDir, ".git", "info", "grafts")
				if err := os.MkdirAll(filepath.Dir(grafts), 0o755); err != nil {
					t.Fatal(err)
				}
				line := mainTip + " " + head + "\n"
				if parent != "" {
					line = mainTip + " " + parent + " " + head + "\n"
				}
				if err := os.WriteFile(grafts, []byte(line), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if out, err := g.run("rev-list", mainTip); err != nil || !strings.Contains(out, head) {
				t.Fatalf("%s forgery did not take effect for plain git: %v", mechanism, err)
			}

			assertNotPreserved(t, g, branch, nil)
			assertNotPreserved(t, g, branch, []string{mainBranch})
			if st, err := g.PreservedOn(head, mainTip); err == nil && st.Preserved {
				t.Fatalf("PreservedOn accepted the %s forgery: %+v", mechanism, st)
			}
		})
	}
}

func TestProofHardeningEnv(t *testing.T) {
	for _, env := range [][]string{proofLocalEnv, proofRemoteOptions().env} {
		joined := strings.Join(env, " ")
		for _, want := range []string{"GIT_NO_REPLACE_OBJECTS=1", "GIT_GRAFT_FILE=", "core.commitGraph", "GIT_CONFIG_VALUE_0=false"} {
			if !strings.Contains(joined, want) {
				t.Errorf("proof env %q lacks %q", joined, want)
			}
		}
	}
}

// H2a (security review): removing origin must not turn unpushed commits into
// "clean".
func TestPreservationRemovedOriginIsUnverified(t *testing.T) {
	g, localDir, _, _, branch := newUnpushedPolecat(t)
	runGit(t, localDir, "remote", "remove", "origin")

	if _, err := g.BranchPreservationStatus(branch, "origin", nil); !errors.Is(err, ErrRemoteProofUnavailable) {
		t.Fatalf("BranchPreservationStatus err = %v, want ErrRemoteProofUnavailable", err)
	}
	if _, err := g.UnpushedCommits(); !errors.Is(err, ErrRemoteProofUnavailable) {
		t.Fatalf("UnpushedCommits err = %v, want ErrRemoteProofUnavailable", err)
	}
	ws, err := g.CheckUncommittedWork()
	if err != nil || !ws.UnpushedUnverified || ws.Clean() || !ws.CommitsAtRisk() {
		t.Fatalf("CheckUncommittedWork = %+v, %v; want unverified", ws, err)
	}
}

func TestPreservationUnbornHeadWithoutRemoteIsClean(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	g := NewGit(dir)
	ws, err := g.CheckUncommittedWork()
	if err != nil || !ws.Clean() {
		t.Fatalf("CheckUncommittedWork on an empty repo = %+v, %v; want clean (no commits at all)", ws, err)
	}
}

func TestIsAncestorIgnoresReplaceRefs(t *testing.T) {
	g, localDir, _, mainBranch, _ := newUnpushedPolecat(t)
	head, _ := g.Rev("HEAD")
	mainTip, _ := g.Rev("refs/remotes/origin/" + mainBranch)
	fake, err := g.run("commit-tree", mainTip+"^{tree}", "-p", mainTip, "-p", head, "-m", "forged")
	if err != nil {
		t.Fatalf("commit-tree: %v", err)
	}
	runGit(t, localDir, "replace", mainTip, fake)
	if ok, err := g.IsAncestor(head, mainTip); err != nil || ok {
		t.Fatalf("IsAncestor through a replace ref = %v, %v; want false", ok, err)
	}
	// The replace ref is still honoured by plain git (the forgery is real).
	out, err := g.run("rev-list", mainTip)
	if err != nil || !strings.Contains(out, head) {
		t.Fatalf("plain rev-list should follow the replace ref: %v", err)
	}
}
