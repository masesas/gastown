package formula

import (
	"strings"
	"testing"
)

// gt-pd1c.78 tahap 3 (PR-B): the refinery runs exactly ONE command per MR,
// `gt mq merge <rig> <mr-id> --json`, and branches on its kind. The pilot
// followed formula/template text that told it to merge, push and `bd close`
// by hand; none of that may remain.
var refineryOutcomeKinds = []string{
	"merged", "conflict", "gates_failed", "scope_violation", "not_eligible", "needs_approval",
	"slot_timeout", "branch_missing", "cleanup_failed", "precondition", "error",
}

func TestRefineryPatrolMergesOnlyWithGtMqMerge(t *testing.T) {
	f := loadRefineryPatrolFormula(t)
	d := requireFormulaStep(t, f, "process-branch").Description
	if !strings.Contains(d, "gt mq merge <rig> <mr-id> --json") {
		t.Fatal("process-branch must run `gt mq merge <rig> <mr-id> --json`")
	}
	for _, kind := range refineryOutcomeKinds {
		if !strings.Contains(d, "`"+kind+"`") {
			t.Errorf("process-branch does not say what to do for kind %q", kind)
		}
	}
	if !strings.Contains(d, `"next"`) {
		t.Error("process-branch must follow the `next` hint for cleanup_failed")
	}
	// The steps gt mq merge replaced are gone (D5, D10).
	for _, gone := range []string{"run-tests", "quality-review", "handle-failures", "merge-push"} {
		for _, s := range f.Steps {
			if s.ID == gone {
				t.Errorf("step %q still exists: gt mq merge runs it in Go", gone)
			}
		}
	}
	if !containsStepNeed(requireFormulaStep(t, f, "loop-check"), "process-branch") {
		t.Error("loop-check must follow process-branch")
	}
}

func TestRefineryPatrolHasNoManualMergePath(t *testing.T) {
	content := refineryFormulaContent(t)
	for _, bad := range []string{
		"git merge --ff-only", "git merge --no-ff -m", "git push origin <merge-target>", "git push origin {{target_branch}}",
		"gt mq check-scope", "Checked-SHA", "gh pr merge \"$PR_URL\"", "close MR with reason",
		"git checkout -b temp", "git branch -D temp", "{{test_command}}", "{{setup_command}}",
	} {
		if strings.Contains(content, bad) {
			t.Errorf("refinery formula still contains manual merge text %q", bad)
		}
	}
	// D10: gates come from the rig config inside gt mq merge; the formula
	// carries no gate or merge-mechanics variables.
	f := loadRefineryPatrolFormula(t)
	for _, v := range []string{"run_tests", "setup_command", "typecheck_command", "lint_command", "test_command",
		"build_command", "delete_merged_branches", "require_review"} {
		if _, ok := f.Vars[v]; ok {
			t.Errorf("formula still declares var %q", v)
		}
	}
}

func TestRefineryPatrolForbiddenList(t *testing.T) {
	content := refineryFormulaContent(t)
	for _, want := range []string{
		"FORBIDDEN: `git merge`, `git rebase`, `git cherry-pick` or `git push` to the default branch or `integration/*`",
		"FORBIDDEN: `bd close` or `bd update --status` on an MR bead or its source issue",
		"FORBIDDEN: deleting polecat branches",
		"FORBIDDEN: `--force` on `git push`",
		"This list is the rule.",
		"is not refused (binding the push to `gt mq merge` is stage 4)",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("FORBIDDEN list missing %q", want)
		}
	}
}

func refineryFormulaContent(t *testing.T) string {
	t.Helper()
	b, err := formulasFS.ReadFile("formulas/mol-refinery-patrol.formula.toml")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
