package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// rigBranchName is the shape of a configured default branch: '/'-separated
// segments of letters, digits, '.', '_' and '-' that never start with '-' or
// '.' (so never an option, "..", or an empty segment).
var rigBranchName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]*(/[A-Za-z0-9_][A-Za-z0-9._-]*)*$`)

// RigDefaultBranch is the one reader of a rig's configured default branch:
// default_branch in <rigPath>/config.json (the file rig.LoadRigConfig reads),
// used by rig.Rig.DefaultBranch, the merge-proof provers, the pre-push guard
// and the witness branch reconciler (gt-pd1c.78). It returns "" when the file
// is missing or unreadable, the field is unset, or the value is not a plain
// branch name; callers decide their own fallback.
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
	if !rigBranchName.MatchString(b) || strings.Contains(b, "..") {
		return ""
	}
	return b
}
