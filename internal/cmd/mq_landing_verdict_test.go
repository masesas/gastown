package cmd

import (
	"strings"
	"testing"
)

// gt-pd1c.59 round 4 (B1): the manual landing verdict is a human decision.
func TestLandingVerdictActor(t *testing.T) {
	cases := []struct {
		role, actor string
		ok          bool
	}{
		{"", "", true},
		{"", "overseer", true},
		{"gastown/crew/max", "gastown/crew/max", true},
		{"gastown/polecats/nux", "gastown/polecats/nux", false},
		{"", "gastown/polecats/nux", false},
		{"gastown/refinery", "", false},
		{"gastown/witness", "", false},
		{"deacon", "", false},
		{"mayor", "", false},
	}
	for _, c := range cases {
		t.Setenv(EnvGTRole, c.role)
		t.Setenv("BD_ACTOR", c.actor)
		actor, err := landingVerdictActor()
		if (err == nil) != c.ok {
			t.Errorf("role=%q actor=%q: err=%v, want ok=%v", c.role, c.actor, err, c.ok)
		}
		if err != nil && !strings.Contains(err.Error(), "only a human") {
			t.Errorf("refusal must name who may record it: %v", err)
		}
		if err == nil && actor == "" {
			t.Errorf("role=%q actor=%q: empty actor", c.role, c.actor)
		}
	}
}

// A polecat cannot take the exit through either command.
func TestMQConfirmLanding_RefusesPolecat(t *testing.T) {
	t.Setenv(EnvGTRole, "gastown/polecats/nux")
	t.Setenv("BD_ACTOR", "gastown/polecats/nux")
	for _, landed := range []bool{true, false} {
		if err := runMQConfirmLanding("gastown", "gt-wisp-mr1", landed); err == nil || !strings.Contains(err.Error(), "refusing landing verdict") {
			t.Fatalf("landed=%v: err = %v, want a refusal before any lookup", landed, err)
		}
	}
}
