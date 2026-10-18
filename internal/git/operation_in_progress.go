package git

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// inProgressMarkers are the git-dir entries of an unfinished operation, in
// the order they are reported.
var inProgressMarkers = []struct{ op, path string }{
	{"merge", "MERGE_HEAD"},
	{"rebase", "rebase-merge"},
	{"rebase", "rebase-apply"},
	{"cherry-pick", "CHERRY_PICK_HEAD"},
	{"revert", "REVERT_HEAD"},
}

// OperationInProgress names an unfinished merge, rebase, cherry-pick or
// revert in this work tree ("merge", "rebase", "cherry-pick", "revert"), or
// "" when there is none. It reads the work tree's own git dir (git rev-parse
// --git-path), so a linked worktree is judged by its own state. An error
// means the state could not be read; callers that mutate the tree should
// treat it as "in progress" (fail closed).
func (g *Git) OperationInProgress() (string, error) {
	for _, m := range inProgressMarkers {
		out, err := g.run("rev-parse", "--git-path", m.path)
		if err != nil {
			return "", fmt.Errorf("read git state: %w", err)
		}
		p := strings.TrimSpace(out)
		if !filepath.IsAbs(p) {
			p = filepath.Join(g.workDir, p)
		}
		if _, err := os.Lstat(p); err == nil {
			return m.op, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("read git state %s: %w", m.path, err)
		}
	}
	return "", nil
}
