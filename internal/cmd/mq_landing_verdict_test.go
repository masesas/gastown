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
		{"", "mayor", false}, // gt-pd1c.78: an agent BD_ACTOR without GT_ROLE is still an agent
	}
	for _, c := range cases {
		t.Setenv(EnvGTRole, c.role)
		t.Setenv("BD_ACTOR", c.actor)
		t.Setenv("GT_POLECAT", "")
		t.Setenv("GT_CREW", "")
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
	setCaller(t)
	t.Setenv(EnvGTRole, "gastown/polecats/nux")
	t.Setenv("BD_ACTOR", "gastown/polecats/nux")
	for _, landed := range []bool{true, false} {
		if err := runMQConfirmLanding("gastown", "gt-wisp-mr1", landed); err == nil || !strings.Contains(err.Error(), "refusing landing verdict") {
			t.Fatalf("landed=%v: err = %v, want a refusal before any lookup", landed, err)
		}
	}
}

func stubLandingTTY(t *testing.T, tty bool, input string) *strings.Builder {
	t.Helper()
	oldTTY, oldIn, oldOut := humanConfirmTTY, humanConfirmInput, humanConfirmOutput
	out := &strings.Builder{}
	humanConfirmTTY = func() bool { return tty }
	humanConfirmInput = strings.NewReader(input)
	humanConfirmOutput = out
	t.Cleanup(func() { humanConfirmTTY, humanConfirmInput, humanConfirmOutput = oldTTY, oldIn, oldOut })
	return out
}

// round 5 #3a: the verdict needs a human at a terminal who types the MR ID
// again; there is no bypass flag.
func TestConfirmLandingInteractively(t *testing.T) {
	stubLandingTTY(t, false, "gt-wisp-mr1\n")
	if err := confirmLandingInteractively("gt-wisp-mr1", "landed"); err == nil || !strings.Contains(err.Error(), "human action") {
		t.Fatalf("no TTY: err = %v, want a refusal naming the human action", err)
	}
	stubLandingTTY(t, true, "gt-wisp-mr2\n")
	if err := confirmLandingInteractively("gt-wisp-mr1", "landed"); err == nil {
		t.Fatal("a mistyped MR ID must refuse")
	}
	stubLandingTTY(t, true, "")
	if err := confirmLandingInteractively("gt-wisp-mr1", "landed"); err == nil {
		t.Fatal("no answer must refuse")
	}
	out := stubLandingTTY(t, true, "gt-wisp-mr1\n")
	if err := confirmLandingInteractively("gt-wisp-mr1", "not_landed"); err != nil {
		t.Fatalf("matching MR ID at a TTY: %v", err)
	}
	if !strings.Contains(out.String(), "Type the MR ID") || !strings.Contains(out.String(), "not_landed") {
		t.Fatalf("prompt = %q", out.String())
	}
}

// The env guard stays the first layer: an agent is refused before any prompt.
func TestRunMQConfirmLanding_EnvGuardBeforePrompt(t *testing.T) {
	out := stubLandingTTY(t, true, "gt-wisp-mr1\n")
	setCaller(t) // other tests in the package leave identity variables set
	t.Setenv(EnvGTRole, "gastown/refinery")
	t.Setenv("BD_ACTOR", "")
	if err := runMQConfirmLanding("gastown", "gt-wisp-mr1", true); err == nil || !strings.Contains(err.Error(), "refusing landing verdict") {
		t.Fatalf("err = %v, want the env refusal", err)
	}
	if out.Len() != 0 {
		t.Fatalf("an agent must be refused before the prompt, got %q", out.String())
	}
	t.Setenv(EnvGTRole, "")
	stubLandingTTY(t, false, "")
	if err := runMQConfirmLanding("gastown", "gt-wisp-mr1", false); err == nil || !strings.Contains(err.Error(), "interactive terminal") {
		t.Fatalf("human without TTY: err = %v, want the TTY refusal", err)
	}
}
