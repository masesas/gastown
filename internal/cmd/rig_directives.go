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
// failures are printed. Returns the number of files written.
func provisionRigDirectives(townRoot, rigName string) int {
	set, err := directives.RigSet(townRoot, rigName)
	if err == nil {
		var count int
		count, err = assetsync.Provision(set)
		if err == nil {
			if count > 0 {
				fmt.Printf("  %s Provisioned %d rig directives\n", style.Success.Render("✓"), count)
			}
			return count
		}
	}
	fmt.Printf("  %s Could not provision rig directives: %v\n", style.Warning.Render("!"), err)
	return 0
}
