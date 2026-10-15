package cmd

import (
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/witness"
)

func TestEffectivePolecatDirCap(t *testing.T) {
	tests := []struct {
		name       string
		configured int
		want       int
	}{
		{"negative uses floor", -1, minPolecatDirsPerRig},
		{"zero uses floor", 0, minPolecatDirsPerRig},
		{"default below floor uses floor", 10, minPolecatDirsPerRig},
		{"one below floor uses floor", minPolecatDirsPerRig - 1, minPolecatDirsPerRig},
		{"floor remains floor", minPolecatDirsPerRig, minPolecatDirsPerRig},
		{"above floor is honored", 45, 45},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := effectivePolecatDirCap(tt.configured); got != tt.want {
				t.Errorf("effectivePolecatDirCap(%d) = %d, want %d", tt.configured, got, tt.want)
			}
		})
	}
}

// Dispatching (and failing to spawn/start) must never raise the per-bead
// respawn counter: only a polecat that ran and died does (witness). gt-pd1c.53.
func TestCheckBeadRespawnLimit_DoesNotRecordAttempts(t *testing.T) {
	townRoot := t.TempDir()
	for i := 0; i < 10; i++ {
		if err := checkBeadRespawnLimit(townRoot, "rig", "gt-infra"); err != nil {
			t.Fatalf("attempt %d: unexpected block: %v", i, err)
		}
	}
	if witness.ShouldBlockRespawn(townRoot, "gt-infra") {
		t.Fatal("dispatch attempts alone must not trip the respawn limit")
	}
	// RecordBeadRespawn returns the new count: 1 proves nothing was recorded before.
	if got := witness.RecordBeadRespawn(townRoot, "gt-infra"); got != 1 {
		t.Fatalf("first record returned %d, want 1 (nothing recorded by dispatch checks)", got)
	}
}

// The loop guard stays: agent-work failures recorded by the witness block re-dispatch.
func TestCheckBeadRespawnLimit_BlocksAfterWorkFailures(t *testing.T) {
	townRoot := t.TempDir()
	max := config.LoadOperationalConfig(townRoot).GetWitnessConfig().MaxBeadRespawnsV()
	for i := 0; i < max; i++ {
		witness.RecordBeadRespawn(townRoot, "gt-bad")
	}
	err := checkBeadRespawnLimit(townRoot, "rig", "gt-bad")
	if err == nil || !strings.Contains(err.Error(), "respawn limit reached") {
		t.Fatalf("expected respawn limit error, got %v", err)
	}
}
