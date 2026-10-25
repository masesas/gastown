// Package directives ships the default role directives with the gt binary.
//
// Source of truth: internal/directives/defaults/
//
//	town/<role>.md -> <town>/directives/<role>.md         (gt install)
//	rig/<role>.md  -> <town>/<rig>/directives/<role>.md   (gt rig add)
//
// These are the paths config.LoadRoleDirective reads. Provisioning and
// refreshing use internal/assetsync, the same mechanism as formulas: a file
// is written only when absent, its hash is recorded in
// directives/.installed.json, and gt upgrade / gt doctor --fix refresh only
// files unmodified since install. A directive file gt did not install is
// never overwritten (assetsync.Set.PreserveUntracked).
package directives

import (
	"embed"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/steveyegge/gastown/internal/assetsync"
)

//go:embed defaults/town/*.md defaults/rig/*.md
var defaultsFS embed.FS

// DirName is the directives directory name at town and rig level.
const DirName = "directives"

// RecordKey is the key of the hash record in directives/.installed.json.
const RecordKey = "directives"

func isMarkdown(name string) bool { return strings.HasSuffix(name, ".md") }

// TownSet returns the town-level default directives, installed to <townRoot>/directives/.
func TownSet(townRoot string) assetsync.Set {
	return assetsync.Set{
		FS:                defaultsFS,
		SrcDir:            "defaults/town",
		DestDir:           filepath.Join(townRoot, DirName),
		RecordKey:         RecordKey,
		Match:             isMarkdown,
		PreserveUntracked: true,
	}
}

// RigSet returns the rig-level default directives, installed to <townRoot>/<rigName>/directives/.
// rigName must be a single path element; anything else is rejected so the
// destination cannot escape the town.
func RigSet(townRoot, rigName string) (assetsync.Set, error) {
	if rigName == "" || rigName == "." || rigName == ".." || strings.ContainsAny(rigName, `/\`) {
		return assetsync.Set{}, fmt.Errorf("invalid rig name %q", rigName)
	}
	return assetsync.Set{
		FS:                defaultsFS,
		SrcDir:            "defaults/rig",
		DestDir:           filepath.Join(townRoot, rigName, DirName),
		RecordKey:         RecordKey,
		Match:             isMarkdown,
		PreserveUntracked: true,
	}, nil
}
