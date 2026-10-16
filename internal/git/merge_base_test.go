package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeBase(t *testing.T) {
	dir := initTestRepo(t)
	g := NewGit(dir)
	base, err := g.Rev("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "checkout", "-b", "side")
	if err := os.WriteFile(filepath.Join(dir, "side.txt"), []byte("s\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "side")
	got, err := g.MergeBase(base, "side")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(got) != base {
		t.Fatalf("MergeBase = %q, want %q", got, base)
	}
	if _, err := g.MergeBase(base, "no-such-ref"); err == nil {
		t.Fatal("want error for unknown ref")
	}
}

func TestListRemoteRefsWithHashesRejectsOptionLikeRemote(t *testing.T) {
	g := NewGit(initTestRepo(t))
	if _, err := g.ListRemoteRefsWithHashes("--upload-pack=touch /tmp/pwned", "refs/heads/"); err == nil {
		t.Fatal("option-looking remote must be rejected")
	}
}

func TestListLocalRefsWithHashes(t *testing.T) {
	dir := initTestRepo(t)
	g := NewGit(dir)
	head, err := g.Rev("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "update-ref", "refs/remotes/origin/integration/a", head)
	refs, err := g.ListLocalRefsWithHashes("refs/remotes/origin/integration/")
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].Name != "refs/remotes/origin/integration/a" || refs[0].Hash != head {
		t.Fatalf("got %+v", refs)
	}
	if refs, err := g.ListLocalRefsWithHashes("refs/remotes/none/"); err != nil || len(refs) != 0 {
		t.Fatalf("empty prefix: %+v %v", refs, err)
	}
}
