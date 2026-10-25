package cmd

import (
	"fmt"

	"github.com/steveyegge/gastown/internal/assetsync"
	"github.com/steveyegge/gastown/internal/directives"
	"github.com/steveyegge/gastown/internal/style"
)

// provisionRigDirectives installs the default rig-level role directives into
// <townRoot>/<rigName>/directives/. Existing files are kept; gt upgrade and
// gt doctor --fix refresh only files unmodified since install. Non-fatal:
// failures are printed.
func provisionRigDirectives(townRoot, rigName string) {
	set, err := directives.RigSet(townRoot, rigName)
	if err != nil {
		fmt.Printf("  %s Could not provision rig directives: %v\n", style.Warning.Render("!"), err)
		return
	}
	count, err := assetsync.Provision(set)
	if count > 0 {
		fmt.Printf("  %s Provisioned %d rig directives\n", style.Success.Render("✓"), count)
	}
	if err != nil {
		fmt.Printf("  %s Could not provision rig directives: %v\n", style.Warning.Render("!"), err)
	}
}
