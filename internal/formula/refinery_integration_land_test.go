package formula

import (
	"strings"
	"testing"
)

// TestRefineryPatrolIntegrationLandRequiresAutoLand guards gt-pd1c.71: with
// integration_branch_auto_land not "true" the refinery must not run
// `gt mq integration land` at all (the old wording told it a human runs that
// command, which the agent read as "the sanctioned way to land"). It must only
// report readiness to the mayor so a human can land.
func TestRefineryPatrolIntegrationLandRequiresAutoLand(t *testing.T) {
	f := loadRefineryPatrolFormula(t)
	step := requireFormulaStep(t, f, "check-integration-branches")
	d := step.Description

	required := []string{
		`If integration_branch_auto_land is anything other than "true"`,
		"you MUST NOT run `gt mq integration land <epic-id>`",
		"gt mail send mayor/",
		"READY_TO_LAND",
		"a human lands it",
		"auto-land disabled for rig",
	}
	for _, want := range required {
		if !strings.Contains(d, want) {
			t.Errorf("check-integration-branches missing %q", want)
		}
	}

	if strings.Contains(d, "requires a human\n  to run `gt mq integration land` manually") ||
		strings.Contains(d, `If integration_branch_auto_land = "false": Say "Auto-land disabled, nothing to do."`) {
		t.Error("check-integration-branches still carries the ambiguous auto_land=false wording")
	}

	// The land command may only appear after the BOTH-true gate.
	gate := strings.Index(d, `If BOTH are "true"`)
	landRun := strings.Index(d, "run `gt mq integration land <epic-id>` and capture")
	if gate < 0 || landRun < 0 || landRun < gate {
		t.Errorf("land instruction must come after the BOTH-true gate (gate=%d land=%d)", gate, landRun)
	}
}
