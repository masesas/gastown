package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/steveyegge/gastown/internal/refname"
)

// RigDefaultBranch is the one reader of a rig's configured default branch:
// default_branch in <rigPath>/config.json (the file rig.LoadRigConfig reads),
// used by rig.Rig.DefaultBranch, the merge-proof provers, the pre-push guard
// and the witness branch reconciler (gt-pd1c.78). It returns "" when the file
// is missing or unreadable, the field is unset, or the value is not a plain
// branch name (refname.Valid, the one ref-name rule shared with
// beads.IsSafeRefName); callers decide their own fallback.
func RigDefaultBranch(rigPath string) string {
	data, err := os.ReadFile(filepath.Join(rigPath, "config.json")) //nolint:gosec // G304: rig path from the town layout
	if err != nil {
		return ""
	}
	var cfg struct {
		DefaultBranch string `json:"default_branch"`
	}
	if json.Unmarshal(data, &cfg) != nil {
		return ""
	}
	b := strings.TrimSpace(cfg.DefaultBranch)
	if b == "" {
		return ""
	}
	if !refname.Valid(b) {
		warnRejectedDefaultBranch(rigPath, b)
		return ""
	}
	return b
}

// rejectedDefaultBranchWarned dedups the warning per rig and value.
var rejectedDefaultBranchWarned sync.Map

// warnRejectedDefaultBranch tells the operator once that a configured
// default_branch was ignored (it is not refname.Valid: e.g. it starts with a
// character other than a letter or digit, or contains characters outside
// letters, digits, '.', '_', '+', '@', '-', '/'): the rig then falls back to
// the remote's HEAD or the guard's main/master rules.
func warnRejectedDefaultBranch(rigPath, value string) {
	key := rigPath + "\x00" + value
	if _, seen := rejectedDefaultBranchWarned.LoadOrStore(key, true); seen {
		return
	}
	fmt.Fprintf(os.Stderr, "warning: %s: default_branch %q is not accepted (segments start with a letter or digit; letters, digits, '.', '_', '+', '@', '-', '/' only); ignoring it\n",
		filepath.Join(rigPath, "config.json"), value)
}
