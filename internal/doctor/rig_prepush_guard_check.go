package doctor

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/steveyegge/gastown/internal/pushguard"
)

// RigPrePushGuardCheck verifies that every rig repository agents push from
// runs the Gas Town managed pre-push guard (gt-pd1c.72): the shared
// .repo.git (refinery and polecat worktrees), mayor/rig and crew clones.
//
// Missing, outdated and unchained-project hooks are fixable. A conflict (a
// project-owned core.hooksPath whose pre-push does not call
// `gt hooks pre-push`, or an occupied chain slot) is reported but never
// overwritten: a human resolves it.
type RigPrePushGuardCheck struct {
	FixableCheck
}

// NewRigPrePushGuardCheck creates the rig pre-push guard check.
func NewRigPrePushGuardCheck() *RigPrePushGuardCheck {
	return &RigPrePushGuardCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "rig-prepush-guard",
				CheckDescription: "Check rig repos run the Gas Town pre-push guard (land + allowed_paths)",
				CheckCategory:    CategoryRig,
			},
		},
	}
}

func (c *RigPrePushGuardCheck) rigs(ctx *CheckContext) []string {
	all := findAllRigs(ctx.TownRoot)
	if ctx.RigName == "" {
		return all
	}
	var out []string
	for _, r := range all {
		if filepath.Base(r) == ctx.RigName {
			out = append(out, r)
		}
	}
	return out
}

// Run inspects every rig repository; it changes nothing.
func (c *RigPrePushGuardCheck) Run(ctx *CheckContext) *CheckResult {
	var (
		details []string
		total   int
		bad     int
	)
	for _, rigPath := range c.rigs(ctx) {
		sts, err := pushguard.InspectRig(rigPath)
		for _, st := range sts {
			total++
			if st.OK() {
				continue
			}
			bad++
			details = append(details, describePrePushStatus(ctx.TownRoot, st))
		}
		if err != nil {
			bad++
			details = append(details, fmt.Sprintf("%s: %v", relToTown(ctx.TownRoot, rigPath), err))
		}
	}
	if bad == 0 {
		return &CheckResult{
			Name:    c.Name(),
			Status:  StatusOK,
			Message: fmt.Sprintf("%d rig repo hook dir(s) guarded", total),
		}
	}
	return &CheckResult{
		Name:    c.Name(),
		Status:  StatusWarning,
		Message: fmt.Sprintf("%d rig repo(s) without the Gas Town pre-push guard (agents can push lands / out-of-scope paths)", bad),
		Details: details,
		FixHint: "Run 'gt doctor --fix' to install the guard; conflicts need a manual fix (see details)",
	}
}

// Fix installs the guard wherever it is fixable. Conflicts are left for a
// human and do not fail the fix; Run keeps reporting them.
func (c *RigPrePushGuardCheck) Fix(ctx *CheckContext) error {
	var errs []error
	for _, rigPath := range c.rigs(ctx) {
		if _, err := pushguard.EnsureRig(rigPath); err != nil {
			errs = append(errs, withoutConflicts(err))
		}
	}
	return errors.Join(errs...)
}

// withoutConflicts drops ErrConflict members from a joined error.
func withoutConflicts(err error) error {
	var multi interface{ Unwrap() []error }
	if !errors.As(err, &multi) {
		if errors.Is(err, pushguard.ErrConflict) {
			return nil
		}
		return err
	}
	var keep []error
	for _, e := range multi.Unwrap() {
		if !errors.Is(e, pushguard.ErrConflict) {
			keep = append(keep, e)
		}
	}
	return errors.Join(keep...)
}

func describePrePushStatus(townRoot string, st pushguard.Status) string {
	s := fmt.Sprintf("%s: %s", relToTown(townRoot, st.RepoPath), st.State)
	if st.Detail != "" {
		s += " - " + st.Detail
	}
	return s
}

func relToTown(townRoot, p string) string {
	if rel, err := filepath.Rel(townRoot, p); err == nil {
		return rel
	}
	return p
}
