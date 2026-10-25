package doctor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

// directiveSets returns the town set and one set per registered rig, rigs
// sorted by name. It never aborts: a rig with an invalid name or an
// unreadable rigs.json is reported in errs and the remaining sets are still
// returned. Rigs whose directory does not exist are skipped silently, so a
// fix never creates a phantom <town>/<rig>/directives/.
func directiveSets(townRoot string) (sets []scopedSet, errs []error) {
	sets = []scopedSet{{scope: "town", set: directives.TownSet(townRoot)}}
	rigs, err := discoverRigs(townRoot)
	if err != nil {
		return sets, []error{fmt.Errorf("discovering rigs: %w", err)}
	}
	sort.Strings(rigs)
	for _, r := range rigs {
		s, err := directives.RigSet(townRoot, r)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if fi, err := os.Stat(filepath.Join(townRoot, r)); err != nil || !fi.IsDir() {
			continue
		}
		sets = append(sets, scopedSet{scope: r, set: s})
	}
	return sets, errs
}

// Run checks if directives need installing or updating. Errors in one set
// do not hide the status of the others.
func (c *DirectiveCheck) Run(ctx *CheckContext) *CheckResult {
	sets, errs := directiveSets(ctx.TownRoot)

	var total assetsync.Report
	var details []string
	needsFix := false
	for _, ss := range sets {
		r, err := assetsync.Check(ss.set)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", ss.scope, err))
			continue
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

	for _, err := range errs {
		details = append(details, fmt.Sprintf("  error: %v", err))
	}

	if len(details) == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: fmt.Sprintf("%d directives up-to-date", total.OK),
		}
	}

	parts := []string{}
	if counts := directiveCounts(total); counts != "" {
		parts = append(parts, counts)
	}
	if len(errs) > 0 {
		parts = append(parts, fmt.Sprintf("%d error(s)", len(errs)))
	}
	message := "Directives: " + strings.Join(parts, ", ")
	result := &CheckResult{
		Name:    c.Name(),
		Status:  StatusOK,
		Message: message,
		Details: details,
	}
	if needsFix || len(errs) > 0 {
		result.Status = StatusWarning
	}
	if needsFix {
		result.FixHint = "Run 'gt doctor --fix' (or 'gt upgrade') to install or update directives"
	}
	return result
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
		return "unreadable or symlink (skipping)", false
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
	sets, errs := directiveSets(ctx.TownRoot)
	for _, ss := range sets {
		if _, _, _, err := assetsync.Update(ss.set); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", ss.scope, err))
		}
	}
	return errors.Join(errs...)
}
