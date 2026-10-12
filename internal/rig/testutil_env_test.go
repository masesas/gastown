package rig

// Importing testutil disables beads dolt auto-start for this package's tests
// (see testutil/beads_env.go); without it, tests leaked dolt sql-servers.
import _ "github.com/steveyegge/gastown/internal/testutil"
