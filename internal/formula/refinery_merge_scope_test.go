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
		"Failure-Type: scope` and\n`$SCOPE_OUT`",
		`even when run_tests = "false"`,
		"never\nclassify a scope failure as pre-existing",
	} {
		if !strings.Contains(gate.Description, want) {
			t.Errorf("handle-failures missing %q", want)
		}
	}
	if i, j := strings.Index(gate.Description, "gt mq check-scope"), strings.Index(gate.Description, "auto-completes"); i < 0 || j < 0 || i > j {
		t.Error("merge-scope gate must come before the auto-complete (proceed to merge) rule")
	}
}

// TestRefineryPatrolMergesTheCheckedSHA pins the race fix from the PR #38
// review: merge-push lands the exact commit check-scope verified.
func TestRefineryPatrolMergesTheCheckedSHA(t *testing.T) {
	f := loadRefineryPatrolFormula(t)
	gate := requireFormulaStep(t, f, "handle-failures")
	mergePush := requireFormulaStep(t, f, "merge-push")

	if !strings.Contains(gate.Description, `sed -n 's/^Checked-SHA: //p'`) {
		t.Error("handle-failures must capture CHECKED_SHA from check-scope output")
	}
	if !strings.Contains(mergePush.Description, `git merge --no-ff -m "Merge <polecat-branch> into <merge-target>" "${CHECKED_SHA:-origin/<polecat-branch>}"`) {
		t.Error("direct merge must merge CHECKED_SHA, not the moving branch ref")
	}
	if strings.Contains(mergePush.Description, `into <merge-target>" origin/<polecat-branch>`) {
		t.Error("direct merge still merges origin/<polecat-branch> unconditionally")
	}
	for _, want := range []string{
		`[ "$(git rev-parse origin/<polecat-branch>)" = "$CHECKED_SHA" ] || echo "SCOPE_STALE"`,
		"re-run `gt mq check-scope`",
		"CHECKED_SHA=<Checked-SHA recorded in handle-failures",
	} {
		if !strings.Contains(mergePush.Description, want) {
			t.Errorf("merge-push (pr strategy) missing %q", want)
		}
	}
	if i, j := strings.Index(mergePush.Description, "SCOPE_STALE"), strings.Index(mergePush.Description, "gh pr create"); i < 0 || j < 0 || i > j {
		t.Error("PR-strategy tip verification must come before gh pr create")
	}
}
