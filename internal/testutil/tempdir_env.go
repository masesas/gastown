package testutil

import (
	"os"
	"path/filepath"
)

// On macOS the default TMPDIR lives under /var, a symlink to /private/var.
// Code under test resolves symlinks (beads dirs, routes, worktrees) while
// tests build expectations from t.TempDir(), so every path comparison
// differed only by the /private prefix and those tests failed unless run with
// a non-symlinked TMPDIR. Importing testutil points TMPDIR at the resolved
// directory so t.TempDir() already returns canonical paths.
func init() {
	dir := os.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil || resolved == filepath.Clean(dir) {
		return
	}
	_ = os.Setenv("TMPDIR", resolved)
}
