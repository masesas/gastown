package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return strings.TrimSpace(gitIn(t, dir, args...))
}

func shapeCommitFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", name)
	gitIn(t, dir, "commit", "-q", "-m", "change "+name)
	return gitOut(t, dir, "rev-parse", "HEAD")
}

func TestCommitParentsAndMergeTreeWrite(t *testing.T) {
	dir := initTestRepo(t)
	g := NewGit(dir)
	root := gitOut(t, dir, "rev-parse", "HEAD")
	base := gitOut(t, dir, "rev-parse", "--abbrev-ref", "HEAD")

	if parents, err := g.CommitParents(root); err != nil || len(parents) != 0 {
		t.Fatalf("root parents = %v, %v; want none", parents, err)
	}
	gitIn(t, dir, "checkout", "-q", "-b", "feature")
	feature := shapeCommitFile(t, dir, "feature.txt", "feature\n")
	gitIn(t, dir, "checkout", "-q", base)
	mainTip := shapeCommitFile(t, dir, "main.txt", "main\n")
	gitIn(t, dir, "merge", "-q", "--no-ff", "-m", "merge feature", "feature")
	merge := gitOut(t, dir, "rev-parse", "HEAD")

	parents, err := g.CommitParents(merge)
	if err != nil || len(parents) != 2 || parents[0] != mainTip || parents[1] != feature {
		t.Fatalf("merge parents = %v, %v; want [%s %s]", parents, err, mainTip, feature)
	}
	tree, err := g.MergeTreeWrite(mainTip, feature)
	if err != nil {
		t.Fatal(err)
	}
	if want := gitOut(t, dir, "rev-parse", merge+"^{tree}"); tree != want {
		t.Fatalf("merge-tree = %s, want the merge commit's tree %s", tree, want)
	}

	if _, err := g.CommitParents("1234567890123456789012345678901234567890"); err == nil {
		t.Fatal("unknown commit: want an error, not an empty parent list")
	}
	if _, err := g.CommitParents("--all"); err == nil {
		t.Fatal("option-looking commit must be refused")
	}
}

func TestMergeTreeWriteConflictIsAnError(t *testing.T) {
	dir := initTestRepo(t)
	g := NewGit(dir)
	base := gitOut(t, dir, "rev-parse", "--abbrev-ref", "HEAD")
	gitIn(t, dir, "checkout", "-q", "-b", "a")
	a := shapeCommitFile(t, dir, "README.md", "a\n")
	gitIn(t, dir, "checkout", "-q", base)
	b := shapeCommitFile(t, dir, "README.md", "b\n")
	if tree, err := g.MergeTreeWrite(a, b); err == nil {
		t.Fatalf("conflicting merge-tree returned tree %s, want an error", tree)
	}
}

// F12 / gt-pd1c.64: a remote that is a local repository (file:// URL or a
// path, e.g. the pilot mirror) cannot host pull requests, so no PR lookup
// runs and nothing is "protected" by an unknowable PR state.
func TestHasOpenPullRequestLocalOriginHasNoPRs(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "gh-called")
	installFakeGH(t, "#!/bin/sh\ntouch "+marker+"\nexit 1\n")
	bare := filepath.Join(t.TempDir(), "mirror.git")
	gitIn(t, filepath.Dir(bare), "init", "-q", "--bare", bare)

	for _, url := range []string{"file://" + bare, bare} {
		dir := initTestRepo(t)
		g := NewGit(dir)
		gitIn(t, dir, "remote", "add", "origin", url)
		if g.HasOpenPullRequest(PullRequestRef{Branch: "polecat/nux/gt-1", HeadSHA: "abc"}) {
			t.Fatalf("origin %s: an open PR was assumed on a local repository", url)
		}
		if _, err := os.Stat(marker); err == nil {
			t.Fatalf("origin %s: gh was consulted for a local repository", url)
		}
	}
}

// Security review L1: deletion and the landing proofs use the push target, so
// a local fetch URL with a hosted push URL is not "local"; and an MR that
// records a PR is always looked up.
func TestHasOpenPullRequestLocalFetchURLWithHostedPushURLIsLookedUp(t *testing.T) {
	installFakeGH(t, "#!/bin/sh\necho 'gh: network down' >&2\nexit 1\n")
	bare := filepath.Join(t.TempDir(), "mirror.git")
	gitIn(t, filepath.Dir(bare), "init", "-q", "--bare", bare)

	dir := initTestRepo(t)
	g := NewGit(dir)
	gitIn(t, dir, "remote", "add", "origin", "file://"+bare)
	gitIn(t, dir, "remote", "set-url", "--push", "origin", "https://github.com/fork/repo.git")
	if !g.HasOpenPullRequest(PullRequestRef{Branch: "polecat/nux/gt-1"}) {
		t.Fatal("local fetch URL with a GitHub push URL was treated as local")
	}

	local := initTestRepo(t)
	gitIn(t, local, "remote", "add", "origin", "file://"+bare)
	if !NewGit(local).HasOpenPullRequest(PullRequestRef{URL: "https://github.com/o/r/pull/7", Branch: "polecat/nux/gt-1"}) {
		t.Fatal("an MR that records a PR must be looked up even on a local origin")
	}
}

func TestCommitTreeIgnoresReplaceRefs(t *testing.T) {
	dir := initTestRepo(t)
	g := NewGit(dir)
	a := gitOut(t, dir, "rev-parse", "HEAD")
	b := shapeCommitFile(t, dir, "b.txt", "b\n")
	treeB := gitOut(t, dir, "rev-parse", b+"^{tree}")
	gitIn(t, dir, "replace", b, a)
	got, err := g.CommitTree(b)
	if err != nil || got != treeB {
		t.Fatalf("CommitTree through a replace ref = %q, %v; want the recorded tree %s", got, err, treeB)
	}
	if _, err := g.CommitTree("--all"); err == nil {
		t.Fatal("option-looking commit must be refused")
	}
}

func TestHasOpenPullRequestHostedOriginStillFailsClosed(t *testing.T) {
	installFakeGH(t, "#!/bin/sh\necho 'gh: network down' >&2\nexit 1\n")
	for _, url := range []string{"https://github.com/fork/repo.git", "git@gitlab.example.com:team/repo.git"} {
		dir := initTestRepo(t)
		g := NewGit(dir)
		gitIn(t, dir, "remote", "add", "origin", url)
		if !g.HasOpenPullRequest(PullRequestRef{Branch: "polecat/nux/gt-1"}) {
			t.Fatalf("origin %s: an unknown PR state must count as open (gas-fk4)", url)
		}
	}
}

func TestIsLocalRemoteURL(t *testing.T) {
	for url, want := range map[string]bool{
		"file:///srv/mirror.git":            true,
		"/srv/mirror.git":                   true,
		"./mirror.git":                      true,
		"../mirror.git":                     true,
		`C:\repos\mirror.git`:               true,
		"https://github.com/o/r.git":        false,
		"git@github.com:o/r.git":            false,
		"ssh://git@host/o/r.git":            false,
		"host:path/r.git":                   false,
		"":                                  false,
		"x@github.com/example/repo.git":     false,
		"http://127.0.0.1:8080/proxy/r.git": false,
	} {
		if got := isLocalRemoteURL(url); got != want {
			t.Errorf("isLocalRemoteURL(%q) = %v, want %v", url, got, want)
		}
	}
}
