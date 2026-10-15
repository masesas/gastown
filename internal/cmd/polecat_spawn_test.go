package cmd

import (
	"errors"
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

// (a) A failed start (here: no workspace) raises the infra counter, not the respawn counter.
func TestStartSession_FailureCountsAsInfraNotRespawn(t *testing.T) {
	townRoot := t.TempDir()
	t.Chdir(t.TempDir()) // not a Gas Town workspace: StartSession fails early
	info := &SpawnedPolecatInfo{RigName: "rig", PolecatName: "p", trackBead: "gt-x", townRoot: townRoot}
	if _, err := info.StartSession(); err == nil {
		t.Fatal("expected StartSession to fail outside a workspace")
	}
	if blocked, n := witness.ShouldBlockInfraFailures(townRoot, "gt-x"); blocked || n != 1 {
		t.Fatalf("infra failures = %d blocked=%v, want 1 not blocked", n, blocked)
	}
	if got := witness.BeadRespawnCount(townRoot, "gt-x"); got != 0 {
		t.Fatalf("respawn count = %d, want 0 after infra failure", got)
	}
}

// (b) Consecutive infra failures block dispatch, with a message naming respawn-reset.
func TestCheckBeadRespawnLimit_BlocksAfterInfraFailures(t *testing.T) {
	townRoot := t.TempDir()
	for i := 0; i < witness.MaxBeadInfraFailures; i++ {
		if err := checkBeadRespawnLimit(townRoot, "rig", "gt-infra"); err != nil {
			t.Fatalf("attempt %d blocked early: %v", i, err)
		}
		recordSpawnOutcome(townRoot, "gt-infra", errors.New("tmux: no space"))
	}
	err := checkBeadRespawnLimit(townRoot, "rig", "gt-infra")
	if err == nil || !strings.Contains(err.Error(), "respawn-reset") {
		t.Fatalf("expected infra block mentioning respawn-reset, got %v", err)
	}
	if got := witness.BeadRespawnCount(townRoot, "gt-infra"); got != 0 {
		t.Fatalf("respawn count = %d, want 0", got)
	}
}

// (c) A started session counts as one respawn and clears the infra streak; the
// respawn limit then blocks (loop guard for work failures).
func TestRecordSpawnOutcome_SuccessCountsRespawnAndResetsInfra(t *testing.T) {
	townRoot := t.TempDir()
	recordSpawnOutcome(townRoot, "gt-ok", errors.New("boom"))
	recordSpawnOutcome(townRoot, "gt-ok", nil)
	if _, n := witness.ShouldBlockInfraFailures(townRoot, "gt-ok"); n != 0 {
		t.Fatalf("infra failures = %d, want 0 after success", n)
	}
	if got := witness.BeadRespawnCount(townRoot, "gt-ok"); got != 1 {
		t.Fatalf("respawn count = %d, want 1", got)
	}
	max := config.LoadOperationalConfig(townRoot).GetWitnessConfig().MaxBeadRespawnsV()
	for i := 1; i < max; i++ {
		recordSpawnOutcome(townRoot, "gt-ok", nil)
	}
	err := checkBeadRespawnLimit(townRoot, "rig", "gt-ok")
	if err == nil || !strings.Contains(err.Error(), "respawn limit reached") {
		t.Fatalf("expected respawn limit error, got %v", err)
	}
}

// (d) respawn-reset clears both counters.
func TestResetBeadRespawnCount_ClearsBothCounters(t *testing.T) {
	townRoot := t.TempDir()
	recordSpawnOutcome(townRoot, "gt-r", nil)
	recordSpawnOutcome(townRoot, "gt-r", errors.New("x"))
	if err := witness.ResetBeadRespawnCount(townRoot, "gt-r"); err != nil {
		t.Fatal(err)
	}
	if witness.BeadRespawnCount(townRoot, "gt-r") != 0 {
		t.Fatal("respawn count not reset")
	}
	if _, n := witness.ShouldBlockInfraFailures(townRoot, "gt-r"); n != 0 {
		t.Fatal("infra count not reset")
	}
}

// Auto-force (Force without ExplicitForce) must still pass the gate and record.
func TestRespawnGateApplies_OnlyExplicitForceBypasses(t *testing.T) {
	cases := []struct {
		name string
		opts SlingSpawnOptions
		want bool
	}{
		{"plain", SlingSpawnOptions{HookBead: "b"}, true},
		{"auto-force", SlingSpawnOptions{HookBead: "b", Force: true}, true},
		{"explicit force", SlingSpawnOptions{HookBead: "b", Force: true, ExplicitForce: true}, false},
		{"no bead", SlingSpawnOptions{}, false},
	}
	for _, c := range cases {
		if got := respawnGateApplies(c.opts); got != c.want {
			t.Errorf("%s: respawnGateApplies = %v, want %v", c.name, got, c.want)
		}
	}
}

// finishDispatch updates the counters once: a post-spawn error then a later
// outcome must not double count; untracked infos are no-ops.
func TestFinishDispatch_IdempotentAndUntrackedNoop(t *testing.T) {
	townRoot := t.TempDir()
	info := &SpawnedPolecatInfo{trackBead: "gt-f", townRoot: townRoot}
	info.finishDispatch(errors.New("hook failed"))
	info.finishDispatch(errors.New("again"))
	info.finishDispatch(nil)
	if _, n := witness.ShouldBlockInfraFailures(townRoot, "gt-f"); n != 1 {
		t.Fatalf("infra failures = %d, want 1", n)
	}
	if got := witness.BeadRespawnCount(townRoot, "gt-f"); got != 0 {
		t.Fatalf("respawn count = %d, want 0", got)
	}
	(&SpawnedPolecatInfo{}).finishDispatch(errors.New("x")) // must not panic or write
	var nilInfo *SpawnedPolecatInfo
	nilInfo.finishDispatch(errors.New("x"))
}
