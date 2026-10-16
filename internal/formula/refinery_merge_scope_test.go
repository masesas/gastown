package formula

import (
	"strings"
	"testing"
)

// TestRefineryPatrolRunsMergeScopeGateBeforeMerge pins gt-pd1c.68: the
// refinery runs `gt mq check-scope` in the verification gate that always
// precedes merge-push (handle-failures, not run-tests, which is skipped when
// run_tests=false) and treats a failure as a branch-caused FIX_NEEDED.
func TestRefineryPatrolRunsMergeScopeGateBeforeMerge(t *testing.T) {
	f := loadRefineryPatrolFormula(t)

	gate := requireFormulaStep(t, f, "handle-failures")
	mergePush := requireFormulaStep(t, f, "merge-push")
	if !containsStepNeed(mergePush, "handle-failures") {
		t.Fatalf("merge-push needs = %v, want handle-failures", mergePush.Needs)
	}

	for _, want := range []string{
		"gt mq check-scope <rig> <mr-bead-id>",
		`even when run_tests = "false"`,
		"Failure-Type: scope",
		"never classify a\nscope failure as pre-existing",
	} {
		if !strings.Contains(gate.Description, want) {
			t.Errorf("handle-failures missing %q", want)
		}
	}
	if i, j := strings.Index(gate.Description, "gt mq check-scope"), strings.Index(gate.Description, "auto-completes"); i < 0 || j < 0 || i > j {
		t.Error("merge-scope gate must come before the auto-complete (proceed to merge) rule")
	}
}
