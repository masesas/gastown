package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/mergescope"
	"github.com/steveyegge/gastown/internal/refinery"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/workspace"
)

// gt mq check-scope: the manual form of the merge-scope gate
// (internal/mergescope, gt-pd1c.68). gt done / gt mq submit (submit time),
// the refinery merge engine behind gt mq merge (merge time) and the managed
// pre-push hook (push time) enforce the same mergescope.Check.

// mqCheckScopeSHAPrefix starts the machine-readable line of a passing
// `gt mq check-scope`: the commit that was checked.
const mqCheckScopeSHAPrefix = "Checked-SHA: "

// check-scope --range flags (layer 2, pre-push).
var (
	mqCheckScopeRange bool
	mqCheckScopeRig   string
)

var mqCheckScopeCmd = &cobra.Command{
	Use:   "check-scope <rig> <mr-id-or-branch> | --range <old-sha> <new-sha> [--rig <rig>]",
	Short: "Verify an MR only changes paths allowed by merge_queue.allowed_paths",
	Long: `Check that a merge request changes only paths allowed by the rig's
merge_queue.allowed_paths (settings/config.json).

The command fetches origin, resolves the tip of origin/<branch> once and diffs
origin/<target>...<tip>, so a branch re-pushed after submit is checked again.
On success the last line is "Checked-SHA: <tip>", the commit that was checked.
Exit status is non-zero when any changed path is outside allowed_paths (the
offending paths are listed), or when the check cannot be completed (fail
closed). A rig without allowed_paths is unrestricted and always passes.

A read-only diagnostic: it changes nothing. The gates that enforce the scope
are gt done / gt mq submit (submit time), 'gt mq merge' (merge time: the same
check in code on the pinned commit_sha right before it merges; the refinery
runs no separate check-scope step) and the managed pre-push hook (push time).

With --range, no MR or fetch is involved: the command checks the two-dot diff
<old-sha>..<new-sha> in the current repository against the rig's allowed_paths
(rig from --rig, else from the current directory). Intended for a pre-push hook.
Both refs are required; a zero SHA (new branch) fails - pass a base ref instead.

Examples:
  gt mq check-scope gastown gt-mr-abc123
  gt mq check-scope gastown polecat/nux/gt-xyz
  gt mq check-scope --range 1a2b3c 4d5e6f --rig gastown`,
	Args: cobra.ExactArgs(2),
	RunE: runMQCheckScope,
}

func init() {
	mqCheckScopeCmd.Flags().BoolVar(&mqCheckScopeRange, "range", false, "Check the two-dot diff <old-sha>..<new-sha> in the current repo (pre-push)")
	mqCheckScopeCmd.Flags().StringVar(&mqCheckScopeRig, "rig", "", "Rig whose allowed_paths apply (with --range; default: current rig)")
	mqCmd.AddCommand(mqCheckScopeCmd)
}

// mqCheckScopeManager resolves the MR to check.
type mqCheckScopeManager interface {
	FindMRAnyStatus(idOrBranch string) (*refinery.MergeRequest, error)
}

// mqCheckScopeGit is the git surface check-scope needs.
type mqCheckScopeGit interface {
	DiffNameOnly(base, head string) ([]string, error)
	Fetch(remote string) error
	Rev(ref string) (string, error)
}

func runMQCheckScope(_ *cobra.Command, args []string) error {
	if mqCheckScopeRange {
		return runMQCheckScopeRangeCmd(args[0], args[1])
	}
	if mqCheckScopeRig != "" {
		return fmt.Errorf("--rig is only valid with --range")
	}
	mgr, r, _, err := getRefineryManager(args[0])
	if err != nil {
		return err
	}
	rigGit, err := getRigGit(r.Path)
	if err != nil {
		return fmt.Errorf("merge scope: %w", err)
	}
	out, err := runMQCheckScopeFor(mgr, r.Path, rigGit, args[1])
	if err != nil {
		return err
	}
	fmt.Println(out)
	return nil
}

// runMQCheckScopeFor checks the MR identified by idOrBranch against the rig
// scope and returns a one-line success summary.
func runMQCheckScopeFor(mgr mqCheckScopeManager, rigPath string, g mqCheckScopeGit, idOrBranch string) (string, error) {
	allowed, err := mergescope.Load(rigPath)
	if err != nil {
		return "", err
	}
	if len(allowed) == 0 {
		// Unrestricted: no Checked-SHA; the refinery merges origin/<branch> as before.
		return fmt.Sprintf("%s merge scope unrestricted (no merge_queue.allowed_paths)", style.Dim.Render("○")), nil
	}
	mr, err := mgr.FindMRAnyStatus(idOrBranch)
	if err != nil {
		return "", fmt.Errorf("merge scope: resolve MR %s: %w", idOrBranch, err)
	}
	if mr == nil {
		return "", fmt.Errorf("merge scope: resolve MR %s: %w", idOrBranch, refinery.ErrMRNotFound)
	}
	branch := strings.TrimSpace(mr.Branch)
	target := strings.TrimSpace(mr.TargetBranch)
	if branch == "" || target == "" {
		return "", fmt.Errorf("merge scope: MR %s is missing its branch or target", mr.ID)
	}
	// Fetch so origin/<branch> is the tip the refinery will merge, not a
	// stale tracking ref from before a re-push.
	if err := g.Fetch("origin"); err != nil {
		return "", fmt.Errorf("merge scope: fetch origin: %w", err)
	}
	// Pin the tip once: the diff and the reported Checked-SHA are the same
	// commit, and the refinery merges exactly that commit (merge-push), so a
	// push landing after this check cannot be merged unchecked.
	base := "origin/" + target
	sha, err := g.Rev("refs/remotes/origin/" + branch + "^{commit}")
	if err != nil {
		return "", fmt.Errorf("merge scope: resolve origin/%s: %w", branch, err)
	}
	sha = strings.TrimSpace(sha)
	n, err := mergescope.Check(allowed, base, mergescope.MergeBase, sha, g.DiffNameOnly)
	if err != nil {
		return "", fmt.Errorf("MR %s: %w", mr.ID, err)
	}
	return fmt.Sprintf("%s MR %s: %d changed path(s) within allowed_paths (%s...origin/%s)\n%s%s",
		style.Bold.Render("✓"), mr.ID, n, base, branch, mqCheckScopeSHAPrefix, sha), nil
}

func runMQCheckScopeRangeCmd(oldRev, newRev string) error {
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}
	rigName := mqCheckScopeRig
	if rigName == "" {
		if rigName, _, err = findCurrentRig(townRoot); err != nil {
			return err
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting current directory: %w", err)
	}
	out, err := runMQCheckScopeRange(filepath.Join(townRoot, rigName), git.NewGit(cwd), oldRev, newRev)
	if err != nil {
		return err
	}
	fmt.Println(out)
	return nil
}

// mqScopeRangeDiffer is the git surface --range needs.
type mqScopeRangeDiffer interface {
	DiffNameOnlyRange(from, to string) ([]string, error)
}

// runMQCheckScopeRange checks the two-dot diff oldRev..newRev against the
// rig scope. Empty or option-looking revisions fail closed; unknown revisions
// (including the all-zero SHA of a new branch) fail in git and are returned.
func runMQCheckScopeRange(rigPath string, g mqScopeRangeDiffer, oldRev, newRev string) (string, error) {
	allowed, err := mergescope.Load(rigPath)
	if err != nil {
		return "", err
	}
	if len(allowed) == 0 {
		return fmt.Sprintf("%s merge scope unrestricted (no merge_queue.allowed_paths)", style.Dim.Render("○")), nil
	}
	for _, rev := range []string{oldRev, newRev} {
		if strings.TrimSpace(rev) == "" || strings.HasPrefix(rev, "-") {
			return "", fmt.Errorf("merge scope: invalid revision %q for --range", rev)
		}
	}
	n, err := mergescope.Check(allowed, oldRev, mergescope.TwoDot, newRev, g.DiffNameOnlyRange)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s %d changed path(s) within allowed_paths (%s..%s)",
		style.Bold.Render("✓"), n, oldRev, newRev), nil
}
