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

// isMachineFieldKey reports description keys written by tooling rather than
// the author (attachment and workflow metadata); such lines do not count as a
// description. It is derived from attachmentFieldKeys, the one attachment key
// list, without the weak spellings "mode" and "convoy" (ordinary words in
// prose), plus workflow_target.
func isMachineFieldKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	if k == "workflow_target" {
		return true
	}
	return canonicalAttachmentKey(k) != "" && !weakAttachmentKeys[k]
}

// HumanDescription returns desc without tooling metadata lines and without
// leading/trailing blank lines.
func HumanDescription(desc string) string {
	var kept []string
	for _, line := range strings.Split(desc, "\n") {
		trimmed := strings.TrimSpace(line)
		if i := strings.Index(trimmed, ":"); i > 0 && isMachineFieldKey(trimmed[:i]) {
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
