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
