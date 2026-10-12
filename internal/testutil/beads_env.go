package testutil

import "os"

// beadsDoltAutoStartEnv controls whether beads (the in-process SDK and the bd
// CLI) auto-starts a local dolt sql-server for a .beads directory that has no
// reachable server.
const beadsDoltAutoStartEnv = "BEADS_DOLT_AUTO_START"

// beadsAutoStartPreset records whether the caller set the variable explicitly.
var beadsAutoStartPreset bool

// Tests run against temp .beads directories that never need a local server;
// tests that need Dolt use the shared test container. Beads only ties an
// auto-started server to its parent on Linux, so on macOS every auto-start
// leaked a dolt sql-server once the test's temp dir was removed. Importing
// testutil therefore disables auto-start unless the caller chose otherwise.
// `make test` sets the same variable for packages that do not import testutil.
func init() {
	if _, ok := os.LookupEnv(beadsDoltAutoStartEnv); ok {
		beadsAutoStartPreset = true
		return
	}
	_ = os.Setenv(beadsDoltAutoStartEnv, "0")
}
