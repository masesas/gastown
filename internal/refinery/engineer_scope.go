package refinery

import (
	"errors"
	"fmt"

	"github.com/steveyegge/gastown/internal/mergescope"
)

// checkMergeScope is the merge-time scope gate (mergescope, gt-pd1c.68): the
// changes of head (the pinned submitted commit) since its merge base with
// base (the target tip before the merge). A violation is ScopeViolation; an
// unknown scope (unreadable settings, invalid pattern) or a failed diff is a
// plain failure. Either way nothing is merged (fail closed).
func (e *Engineer) checkMergeScope(base, head string) ProcessResult {
	allowed, err := mergescope.Load(e.rig.Path)
	if err != nil {
		return ProcessResult{Error: fmt.Sprintf("%v (refusing to merge while the merge scope is unknown)", err)}
	}
	n, err := mergescope.Check(allowed, base, mergescope.MergeBase, head, e.git.DiffNameOnly)
	var scopeErr *mergescope.Error
	switch {
	case errors.As(err, &scopeErr):
		return ProcessResult{ScopeViolation: true, Error: err.Error()}
	case err != nil:
		return ProcessResult{Error: err.Error()}
	}
	if len(allowed) > 0 {
		_, _ = fmt.Fprintf(e.output, "[Engineer] Merge scope: %d changed path(s) within allowed_paths\n", n)
	}
	return ProcessResult{Success: true}
}
