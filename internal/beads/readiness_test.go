package beads

import (
	"reflect"
	"testing"
)

func TestCountCriteriaItems(t *testing.T) {
	tests := []struct {
		name string
		ac   string
		want int
	}{
		{name: "empty", ac: "", want: 0},
		{name: "free text only", ac: "The page loads fast.\nErrors are shown.", want: 0},
		{name: "unchecked", ac: "- [ ] one\n- [ ] two", want: 2},
		{name: "checked lower and upper", ac: "- [x] one\n- [X] two", want: 2},
		{name: "mixed with text and indentation", ac: "Intro\n  - [ ] one\n- [x] two\n* [ ] star bullet", want: 3},
		{name: "bracket without bullet is not an item", ac: "[ ] not a bullet", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CountCriteriaItems(tt.ac); got != tt.want {
				t.Errorf("CountCriteriaItems() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestHumanDescription_StripsMachineFields(t *testing.T) {
	desc := "attached_formula: mol-polecat-work\ndispatched_by: mayor\nworkflow_target: rig\n\nImplement the thing.\nnote: keep this line"
	want := "Implement the thing.\nnote: keep this line"
	if got := HumanDescription(desc); got != want {
		t.Errorf("HumanDescription() = %q, want %q", got, want)
	}
}

func TestReadinessProblems(t *testing.T) {
	const ac = "- [ ] works"
	tests := []struct {
		name      string
		issueType string
		desc      string
		ac        string
		want      []string
	}{
		{name: "ready task", issueType: "task", desc: "Do X", ac: ac, want: nil},
		{name: "missing description", issueType: "feature", desc: "", ac: ac, want: []string{ReadinessMissingDescription}},
		{name: "only machine fields", issueType: "bug", desc: "attached_formula: mol-polecat-work", ac: ac, want: []string{ReadinessMissingDescription}},
		{name: "missing acceptance criteria", issueType: "task", desc: "Do X", ac: "", want: []string{ReadinessMissingAcceptance}},
		{name: "free-text acceptance criteria", issueType: "task", desc: "Do X", ac: "It works", want: []string{ReadinessMissingAcceptance}},
		{name: "both missing", issueType: "task", want: []string{ReadinessMissingDescription, ReadinessMissingAcceptance}},
		{name: "empty type defaults to task", issueType: "", desc: "", ac: "", want: []string{ReadinessMissingDescription, ReadinessMissingAcceptance}},
		{name: "epic not checked", issueType: "epic"},
		{name: "gate not checked", issueType: "gate"},
		{name: "chore not checked", issueType: "chore"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ReadinessProblems(tt.issueType, tt.desc, tt.ac)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ReadinessProblems() = %v, want %v", got, tt.want)
			}
		})
	}
}
