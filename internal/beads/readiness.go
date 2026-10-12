package beads

import "strings"

// Readiness problem codes reported by ReadinessProblems.
const (
	ReadinessMissingDescription = "missing_description"
	ReadinessMissingAcceptance  = "missing_acceptance_criteria"
)

// readinessCheckedTypes are the issue types that must carry a contract before
// dispatch. An empty type is treated as a task.
var readinessCheckedTypes = map[string]bool{"": true, "task": true, "feature": true, "bug": true}

// machineFieldKeys are description lines written by tooling rather than the
// author (attachment and workflow metadata). They do not count as a description.
var machineFieldKeys = map[string]bool{
	"attached_molecule": true, "attached-molecule": true, "attachedmolecule": true,
	"attached_formula": true, "attached-formula": true, "attachedformula": true,
	"attached_at": true, "attached-at": true, "attachedat": true,
	"attached_args": true, "attached-args": true, "attachedargs": true,
	"attached_vars": true, "attached-vars": true, "attachedvars": true,
	"dispatched_by": true, "dispatched-by": true, "dispatchedby": true,
	"no_merge": true, "no-merge": true, "nomerge": true,
	"review_only": true, "review-only": true, "reviewonly": true,
	"convoy_id": true, "convoy-id": true, "convoyid": true,
	"merge_strategy": true, "merge-strategy": true, "mergestrategy": true,
	"convoy_owned": true, "convoy-owned": true, "convoyowned": true,
	"formula_vars": true, "formula-vars": true, "formulavars": true,
	"workflow_target": true,
}

// HumanDescription returns desc without tooling metadata lines and without
// leading/trailing blank lines.
func HumanDescription(desc string) string {
	var kept []string
	for _, line := range strings.Split(desc, "\n") {
		trimmed := strings.TrimSpace(line)
		if i := strings.Index(trimmed, ":"); i > 0 && machineFieldKeys[strings.ToLower(strings.TrimSpace(trimmed[:i]))] {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Trim(strings.Join(kept, "\n"), "\n \t")
}

// CountCriteriaItems counts checklist items ("- [ ]", "- [x]", "* [ ]") in
// acceptance criteria text. Free text lines are not items.
func CountCriteriaItems(ac string) int {
	count := 0
	for _, line := range strings.Split(ac, "\n") {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) < 5 || (trimmed[0] != '-' && trimmed[0] != '*') {
			continue
		}
		box := strings.ToLower(strings.TrimSpace(trimmed[1:]))
		if strings.HasPrefix(box, "[ ]") || strings.HasPrefix(box, "[x]") {
			count++
		}
	}
	return count
}

// ReadinessProblems returns why an issue is not ready for dispatch, or nil if
// it is ready or its type is not subject to the readiness check.
func ReadinessProblems(issueType, description, acceptanceCriteria string) []string {
	if !readinessCheckedTypes[strings.ToLower(issueType)] {
		return nil
	}
	var problems []string
	if HumanDescription(description) == "" {
		problems = append(problems, ReadinessMissingDescription)
	}
	if CountCriteriaItems(acceptanceCriteria) == 0 {
		problems = append(problems, ReadinessMissingAcceptance)
	}
	return problems
}
