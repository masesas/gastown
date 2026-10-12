package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/steveyegge/gastown/internal/beads"
)

const (
	// defaultContractSectionLines caps each contract section shown by gt prime.
	defaultContractSectionLines = 200
	// defaultContractTotalLines caps the whole contract shown by gt prime.
	defaultContractTotalLines = 400
	// contractMaxLinesEnv overrides defaultContractTotalLines (positive integer).
	contractMaxLinesEnv = "GT_PRIME_CONTRACT_MAX_LINES"
)

// contractLimits bounds how many body lines renderTaskContract emits.
type contractLimits struct {
	PerSectionLines int
	TotalLines      int
}

// defaultContractLimits returns the built-in limits, with the total overridden
// by GT_PRIME_CONTRACT_MAX_LINES when it holds a positive integer.
func defaultContractLimits() contractLimits {
	limits := contractLimits{
		PerSectionLines: defaultContractSectionLines,
		TotalLines:      defaultContractTotalLines,
	}
	if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv(contractMaxLinesEnv))); err == nil && n > 0 {
		limits.TotalLines = n
	}
	return limits
}

// renderTaskContract renders the task contract of issue: Description (without
// tooling metadata lines), Acceptance Criteria, Design and Notes, in that
// order. Empty sections are skipped. Sections over the per-section limit, or
// beyond the total limit, are cut with an explicit marker naming the omitted
// line count and the command to read the rest.
func renderTaskContract(issue *beads.Issue, limits contractLimits) string {
	sections := []struct{ title, body string }{
		{"Description", beads.HumanDescription(issue.Description)},
		{"Acceptance Criteria", issue.AcceptanceCriteria},
		{"Design", issue.Design},
		{"Notes", issue.Notes},
	}

	var sb strings.Builder
	budget := limits.TotalLines
	for _, s := range sections {
		body := strings.Trim(s.body, "\n \t")
		if body == "" {
			continue
		}
		lines := strings.Split(body, "\n")
		shown := min(min(len(lines), limits.PerSectionLines), budget)
		budget -= shown

		fmt.Fprintf(&sb, "%s:\n", s.title)
		for _, line := range lines[:shown] {
			fmt.Fprintf(&sb, "  %s\n", line)
		}
		if omitted := len(lines) - shown; omitted > 0 {
			fmt.Fprintf(&sb, "  [truncated: %d more lines — run: bd show %s]\n", omitted, issue.ID)
		}
	}
	return sb.String()
}
