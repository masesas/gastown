package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func runDiffTestGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestDiffNameOnlyIsExactAndRenameSafe pins the contract the merge-scope gate
// (gt-pd1c.68) relies on: every touched path is listed byte-exact (no
// whitespace trimming, no C-quoting) and a rename lists both the removed and
// the added path, so a move out of a forbidden directory cannot hide.
func TestDiffNameOnlyIsExactAndRenameSafe(t *testing.T) {
	dir := initTestRepo(t)
	if err := os.MkdirAll(filepath.Join(dir, "secret"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secret", "key.txt"), []byte("k\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runDiffTestGit(t, dir, "add", "-A")
	runDiffTestGit(t, dir, "commit", "-m", "base")
	runDiffTestGit(t, dir, "branch", "base")

	if err := os.MkdirAll(filepath.Join(dir, "apps"), 0o755); err != nil {
		t.Fatal(err)
	}
	runDiffTestGit(t, dir, "mv", "secret/key.txt", "apps/key.txt")
	if err := os.MkdirAll(filepath.Join(dir, " apps"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, " apps", "ünï.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runDiffTestGit(t, dir, "add", "-A")
	runDiffTestGit(t, dir, "commit", "-m", "move")

	got, err := NewGit(dir).DiffNameOnly("base", "HEAD")
	if err != nil {
		t.Fatalf("DiffNameOnly: %v", err)
	}
	sort.Strings(got)
	want := []string{" apps/ünï.txt", "apps/key.txt", "secret/key.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DiffNameOnly() = %q, want %q", got, want)
	}

	empty, err := NewGit(dir).DiffNameOnly("HEAD", "HEAD")
	if err != nil || empty != nil {
		t.Fatalf("DiffNameOnly(no changes) = %q, %v; want nil, nil", empty, err)
	}
}
