package git

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrNotWorktreeRoot means a directory is not the root of its own git
// checkout: it has no .git entry, or git resolves it to an enclosing
// repository's top level.
var ErrNotWorktreeRoot = errors.New("not the root of a git worktree")

// VerifyWorktreeRoot proves that g's working directory is the root of its own
// checkout: it has its own .git entry and git resolves its top level to that
// same directory (symlinks resolved on both sides). Without this, git run in
// a checkout whose .git was deleted walks up to an enclosing repository (e.g.
// a git-initialized town root) and answers for that repository instead
// (gt-pd1c.96 security review M1).
func (g *Git) VerifyWorktreeRoot() error {
	if g.gitDir != "" || g.workDir == "" {
		return fmt.Errorf("%w: no working directory to verify", ErrNotWorktreeRoot)
	}
	if _, err := os.Lstat(filepath.Join(g.workDir, ".git")); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s has no .git", ErrNotWorktreeRoot, g.workDir)
		}
		return fmt.Errorf("checking %s: %w", filepath.Join(g.workDir, ".git"), err)
	}
	top, err := g.run("rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("resolving the top level of %s: %w", g.workDir, err)
	}
	want, err := filepath.EvalSymlinks(g.workDir)
	if err != nil {
		return fmt.Errorf("resolving %s: %w", g.workDir, err)
	}
	got, err := filepath.EvalSymlinks(top)
	if err != nil {
		return fmt.Errorf("resolving %s: %w", top, err)
	}
	if filepath.Clean(want) != filepath.Clean(got) {
		return fmt.Errorf("%w: %s resolves to %s", ErrNotWorktreeRoot, g.workDir, top)
	}
	return nil
}

// CheckWorktreeWork is CheckUncommittedWork for the root of a checkout that a
// caller is about to discard, reset or report as safe: it first proves the
// directory is that root (VerifyWorktreeRoot), so an enclosing repository can
// never answer for it.
func (g *Git) CheckWorktreeWork() (*UncommittedWorkStatus, error) {
	if err := g.VerifyWorktreeRoot(); err != nil {
		return nil, err
	}
	return g.CheckUncommittedWork()
}
