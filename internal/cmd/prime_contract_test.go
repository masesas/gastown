package cmd

import (
	"fmt"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
)

func numberedLines(prefix string, n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("%s %d", prefix, i+1)
	}
	return strings.Join(lines, "\n")
}

func indentedLines(prefix string, from, to int) string {
	var sb strings.Builder
	for i := from; i <= to; i++ {
		fmt.Fprintf(&sb, "  %s %d\n", prefix, i)
	}
	return sb.String()
}

func TestRenderTaskContract_Full(t *testing.T) {
	issue := &beads.Issue{
		ID:                 "gt-abc",
		Description:        numberedLines("desc", 30),
		AcceptanceCriteria: "- [ ] one\n- [ ] two\n- [x] three\n- [ ] four",
		Design:             "use the adapter pattern",
		Notes:              "watch the migration order",
	}

	got := renderTaskContract(issue, contractLimits{PerSectionLines: 200, TotalLines: 400})

	want := "Description:\n" + indentedLines("desc", 1, 30) +
		"Acceptance Criteria:\n  - [ ] one\n  - [ ] two\n  - [x] three\n  - [ ] four\n" +
		"Design:\n  use the adapter pattern\n" +
		"Notes:\n  watch the migration order\n"
	if got != want {
		t.Fatalf("contract mismatch\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderTaskContract_TruncatesWithMarker(t *testing.T) {
	t.Run("per section", func(t *testing.T) {
		issue := &beads.Issue{ID: "gt-abc", Description: numberedLines("desc", 10), Notes: "short"}
		got := renderTaskContract(issue, contractLimits{PerSectionLines: 4, TotalLines: 100})
		want := "Description:\n" + indentedLines("desc", 1, 4) +
			"  [truncated: 6 more lines — run: bd show gt-abc]\n" +
			"Notes:\n  short\n"
		if got != want {
			t.Fatalf("got:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("total", func(t *testing.T) {
		issue := &beads.Issue{ID: "gt-abc", Description: numberedLines("desc", 3), Design: numberedLines("des", 4)}
		got := renderTaskContract(issue, contractLimits{PerSectionLines: 100, TotalLines: 5})
		want := "Description:\n" + indentedLines("desc", 1, 3) +
			"Design:\n" + indentedLines("des", 1, 2) +
			"  [truncated: 2 more lines — run: bd show gt-abc]\n"
		if got != want {
			t.Fatalf("got:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("total exhausted before later section", func(t *testing.T) {
		issue := &beads.Issue{ID: "gt-abc", Description: numberedLines("desc", 3), Notes: "n1\nn2"}
		got := renderTaskContract(issue, contractLimits{PerSectionLines: 100, TotalLines: 3})
		want := "Description:\n" + indentedLines("desc", 1, 3) +
			"Notes:\n  [truncated: 2 more lines — run: bd show gt-abc]\n"
		if got != want {
			t.Fatalf("got:\n%s\nwant:\n%s", got, want)
		}
	})
}

func TestRenderTaskContract_SkipsEmptyAndMachineLines(t *testing.T) {
	issue := &beads.Issue{
		ID:          "gt-abc",
		Description: "attached_formula: mol-x\nreal text\nattached_vars: [\"a=b\"]",
		Design:      "  \n",
		Notes:       "a note",
	}
	got := renderTaskContract(issue, contractLimits{PerSectionLines: 200, TotalLines: 400})
	want := "Description:\n  real text\nNotes:\n  a note\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}

	empty := renderTaskContract(&beads.Issue{ID: "gt-e", Description: "attached_formula: mol-x"}, defaultContractLimits())
	if empty != "" {
		t.Fatalf("expected empty contract, got %q", empty)
	}
}

func TestContractLimitsFromEnv(t *testing.T) {
	tests := []struct {
		name      string
		env       string
		wantTotal int
	}{
		{"unset", "", defaultContractTotalLines},
		{"valid", "50", 50},
		{"not a number", "abc", defaultContractTotalLines},
		{"zero", "0", defaultContractTotalLines},
		{"negative", "-3", defaultContractTotalLines},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GT_PRIME_CONTRACT_MAX_LINES", tt.env)
			got := defaultContractLimits()
			if got.TotalLines != tt.wantTotal {
				t.Errorf("TotalLines = %d, want %d", got.TotalLines, tt.wantTotal)
			}
			if got.PerSectionLines != defaultContractSectionLines {
				t.Errorf("PerSectionLines = %d, want %d", got.PerSectionLines, defaultContractSectionLines)
			}
		})
	}
}
