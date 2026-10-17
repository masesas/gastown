package git

import (
	"os"
	"path/filepath"
	"testing"
)

// gt-pd1c.78: PreservedOn is the preservation rule of gt polecat prune
// (ancestor, merge-tree no-op, patch-equivalent) for two commits.
func TestPreservedOn(t *testing.T) {
	dir := initTestRepo(t)
	g := NewGit(dir)
	commit := func(file, content, msg string) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		runGit(t, dir, "add", "-A")
		runGit(t, dir, "commit", "-q", "-m", msg)
		sha, err := g.Rev("HEAD")
		if err != nil {
			t.Fatal(err)
		}
		return sha
	}
	base, _ := g.Rev("HEAD")
	runGit(t, dir, "checkout", "-q", "-b", "work")
	work := commit("a.txt", "a\n", "work")
	runGit(t, dir, "checkout", "-q", "-b", "target", base)
	other := commit("b.txt", "b\n", "other")
	runGit(t, dir, "merge", "-q", "--no-ff", "-m", "merge work", work)
	merged, _ := g.Rev("HEAD")
	runGit(t, dir, "checkout", "-q", "-b", "squashed", other)
	runGit(t, dir, "cherry-pick", work)
	picked, _ := g.Rev("HEAD")
	runGit(t, dir, "checkout", "-q", "-b", "extra", work)
	extra := commit("c.txt", "c\n", "extra")

	cases := []struct {
		name       string
		head, base string
		want       bool
	}{
		{"ancestor of merge", work, merged, true},
		{"cherry-picked", work, picked, true},
		{"not on target", work, other, false},
		{"extra commit after merge", extra, merged, false},
	}
	for _, c := range cases {
		st, err := g.PreservedOn(c.head, c.base)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if st.Preserved != c.want {
			t.Errorf("%s: Preserved = %v (%s), want %v", c.name, st.Preserved, st.Evidence, c.want)
		}
	}
	if _, err := g.PreservedOn("", merged); err == nil {
		t.Error("an empty commit must be an error")
	}
	if _, err := g.PreservedOn("-x", merged); err == nil {
		t.Error("an option-looking commit must be an error")
	}
}
