package polecat

import (
	"errors"
	"fmt"

	"github.com/steveyegge/gastown/internal/git"
)

// Removal proof (gt-pd1c.96). Discarding a polecat worktree (remove, repair,
// broken-sandbox reclaim) is decided from the git state gt computes at
// decision time: git.CheckUncommittedWork, whose unpushed count is the remote
// preservation proof of gt-pd1c.86. The agent bead's cleanup_status is never
// that proof. gt done records it from the same check, but the agent can
// rewrite its own bead (bd update), and the value is stale as soon as the
// agent touches the tree again. It may add a blocker or a report, never make
// a dirty or unproven tree count as clean.

// ErrWorkUnproven marks a refusal because gt could not compute the worktree's
// git state (status or stash check failed, worktree missing or broken), so it
// cannot prove that nothing would be lost.
var ErrWorkUnproven = errors.New("polecat work not proven safe to discard")

// UnprovenWorkError reports that the worktree's git state could not be read.
// It matches ErrWorkUnproven, ErrHasUncommittedWork (work may be at risk) and
// the underlying git error.
type UnprovenWorkError struct {
	PolecatName string
	Err         error
}

func (e *UnprovenWorkError) Error() string {
	return fmt.Sprintf("cannot prove polecat %s has no work at risk: %v (use --force to discard it anyway; risks data loss)", e.PolecatName, e.Err)
}

func (e *UnprovenWorkError) Unwrap() []error {
	return []error{ErrWorkUnproven, ErrHasUncommittedWork, e.Err}
}

// worktreeDiscardBlocker returns why the worktree at clonePath must not be
// discarded, or nil when gt proves nothing is at risk. clonePath must be the
// root of its own checkout (git.CheckWorktreeWork): an enclosing repository
// never answers for it. force accepts
// uncommitted changes, commits not proven on the remote and an unreadable git
// state; stashes still block (intentional work in progress).
func worktreeDiscardBlocker(name, clonePath string, force bool) error {
	status, err := git.NewGit(clonePath).CheckWorktreeWork()
	if err != nil {
		if force {
			return nil
		}
		return &UnprovenWorkError{PolecatName: name, Err: err}
	}
	if status.Clean() || (force && status.StashCount == 0) {
		return nil
	}
	return &UncommittedWorkError{PolecatName: name, Status: status}
}
