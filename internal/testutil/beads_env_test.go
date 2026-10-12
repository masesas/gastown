package testutil

import (
	"os"
	"testing"
)

func TestImportDisablesBeadsDoltAutoStart(t *testing.T) {
	if beadsAutoStartPreset {
		t.Skip("BEADS_DOLT_AUTO_START was set explicitly by the caller")
	}
	if got := os.Getenv(beadsDoltAutoStartEnv); got != "0" {
		t.Fatalf("%s = %q after importing testutil, want \"0\"", beadsDoltAutoStartEnv, got)
	}
}
