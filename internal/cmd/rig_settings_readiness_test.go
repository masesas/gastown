package cmd

import (
	"path/filepath"
	"testing"

	"github.com/steveyegge/gastown/internal/config"
)

func TestRigSettingsSet_Readiness(t *testing.T) {
	townRoot, rigName := setupTestRigForSettings(t)
	rigPath := filepath.Join(townRoot, rigName)

	if err := runRigSettingsSet(rigSettingsSetCmd, []string{rigName, "readiness", "enforce"}); err != nil {
		t.Fatalf("runRigSettingsSet: %v", err)
	}
	if got := config.GetReadinessMode(rigPath); got != config.ReadinessEnforce {
		t.Errorf("readiness = %q, want enforce", got)
	}

	if err := runRigSettingsSet(rigSettingsSetCmd, []string{rigName, "readiness", "strict"}); err == nil {
		t.Error("invalid readiness value should be rejected")
	}
	if got := config.GetReadinessMode(rigPath); got != config.ReadinessEnforce {
		t.Errorf("invalid value must not overwrite the setting, got %q", got)
	}
}
