package testutil

import (
	"path/filepath"
	"testing"
)

func TestImportResolvesTempDirSymlinks(t *testing.T) {
	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", dir, err)
	}
	if resolved != dir {
		t.Fatalf("t.TempDir() = %q, want symlink-free path %q", dir, resolved)
	}
}
