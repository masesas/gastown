package doctor

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/steveyegge/gastown/internal/assetsync"
	"github.com/steveyegge/gastown/internal/directives"
)

// DirectiveCheck verifies that the default role directives shipped with the
// binary are installed and current, at town level and in every registered rig.
// It uses the same rules as FormulaCheck: outdated, missing and new files are
// fixable; files edited by the user, or written without gt, are reported and
// never overwritten.
type DirectiveCheck struct {
	FixableCheck
}

// NewDirectiveCheck creates a new directive check.
func NewDirectiveCheck() *DirectiveCheck {
	return &DirectiveCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "directives",
				CheckDescription: "Check default role directives are up-to-date",
				CheckCategory:    CategoryConfig,
			},
		},
	}
}

// scopedSet is one directive set and the label used in details ("town" or the rig name).
type scopedSet struct {
	scope string
	set   assetsync.Set
}

// directiveSets returns the town set and one set per registered rig, rigs sorted by name.
func directiveSets(townRoot string) ([]scopedSet, error) {
	sets := []scopedSet{{scope: "town", set: directives.TownSet(townRoot)}}
	rigs, err := discoverRigs(townRoot)
	if err != nil {
		return nil, fmt.Errorf("discovering rigs: %w", err)
	}
	sort.Strings(rigs)
	for _, r := range rigs {
		s, err := directives.RigSet(townRoot, r)
		if err != nil {
			return nil, err
		}
		sets = append(sets, scopedSet{scope: r, set: s})
	}
	return sets, nil
}

// Run checks if directives need installing or updating.
func (c *DirectiveCheck) Run(ctx *CheckContext) *CheckResult {
	sets, err := directiveSets(ctx.TownRoot)
	if err != nil {
		return c.warn(err)
	}

	var total assetsync.Report
	var details []string
	needsFix := false
	for _, ss := range sets {
		r, err := assetsync.Check(ss.set)
		if err != nil {
			return c.warn(fmt.Errorf("%s: %w", ss.scope, err))
		}
		total.OK += r.OK
		total.Outdated += r.Outdated
		total.Modified += r.Modified
		total.Missing += r.Missing
		total.New += r.New
		total.Error += r.Error
		for _, f := range r.Files {
			line, fixable := directiveDetail(f.Status)
			if line == "" {
				continue
			}
			details = append(details, fmt.Sprintf("  %s/%s: %s", ss.scope, f.Name, line))
			needsFix = needsFix || fixable
		}
	}

	if len(details) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: fmt.Sprintf("%d directives up-to-date", total.OK),
		}
	}

	result := &CheckResult{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: "Directives: " + directiveCounts(total),
		Details: details,
	}
	if needsFix {
		result.Status = StatusWarning
		result.FixHint = "Run 'gt doctor --fix' (or 'gt upgrade') to install or update directives"
	}
	return result
}

func (c *DirectiveCheck) warn(err error) *CheckResult {
	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusWarning,
		Message: fmt.Sprintf("Could not check directives: %v", err),
	}
}

// directiveDetail returns the detail text for a file status and whether Fix acts on it.
func directiveDetail(status string) (string, bool) {
	switch status {
	case assetsync.StatusOutdated:
		return "update available", true
	case assetsync.StatusMissing:
		return "missing (will reinstall)", true
	case assetsync.StatusNew:
		return "new directive available", true
	case assetsync.StatusModified:
		return "locally modified (skipping)", false
	case assetsync.StatusError:
		return "unreadable (skipping)", false
	}
	return "", false
}

func directiveCounts(r assetsync.Report) string {
	var parts []string
	for _, p := range []struct {
		n    int
		name string
	}{
		{r.Outdated, "outdated"},
		{r.Missing, "missing"},
		{r.New, "new"},
		{r.Modified, "modified"},
		{r.Error, "unreadable"},
	} {
		if p.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", p.n, p.name))
		}
	}
	return strings.Join(parts, ", ")
}

// Fix installs new and missing directives and refreshes unmodified ones in
// every set. A failing set does not stop the others; all errors are returned.
func (c *DirectiveCheck) Fix(ctx *CheckContext) error {
	sets, err := directiveSets(ctx.TownRoot)
	if err != nil {
		return err
	}
	var errs []error
	for _, ss := range sets {
		if _, _, _, err := assetsync.Update(ss.set); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", ss.scope, err))
		}
	}
	return errors.Join(errs...)
}
