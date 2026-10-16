package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/refinery"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/workspace"
)

// Merge-scope gate (gt-pd1c.68).
//
// merge_queue.allowed_paths in <rig>/settings/config.json limits which repo
// paths an MR may change. Enforcement layers:
//   - submit time (hard, in code): gt done and gt mq submit refuse an
//     out-of-scope branch before any MR bead exists (enforceMergeScope);
//   - merge time (formula instruction only): the refinery patrol is told to
//     run `gt mq check-scope` right before merge-push and to merge exactly the
//     Checked-SHA. This catches a branch re-pushed after submit only when the
//     refinery follows the formula; an agent that pushes directly (e.g.
//     `git push origin temp:<target>`) bypasses it;
//   - push time (gt-pd1c.72): the Gas Town managed pre-push hook runs
//     `gt hooks pre-push`, which refuses an agent push to the default branch
//     or integration/* whose remote..local range leaves the scope
//     (hooks_prepush.go). `gt mq check-scope --range` remains for manual use.
// All share checkMergeScopeWith (byte-exact paths, renames on both sides).

// mqCheckScopeSHAPrefix starts the machine-readable line of a passing
// `gt mq check-scope`; the refinery merges exactly this commit.
const mqCheckScopeSHAPrefix = "Checked-SHA: "

// mergeScopeMaxListed caps how many offending paths an error lists.
const mergeScopeMaxListed = 20

// MergeScopeError reports MR paths that fall outside merge_queue.allowed_paths.
type MergeScopeError struct {
	Paths []string // offending paths, in diff order
	Base  string   // base ref of the diff
	Head  string   // head ref/commit of the diff
	Range string   // "..." (merge-base diff, default) or ".." (two-dot)
}

func (e *MergeScopeError) Error() string {
	var b strings.Builder
	sep := e.Range
	if sep == "" {
		sep = "..."
	}
	fmt.Fprintf(&b, "merge scope violation: %d path(s) changed in %s%s%s are outside merge_queue.allowed_paths:",
		len(e.Paths), e.Base, sep, e.Head)
	for i, p := range e.Paths {
		if i == mergeScopeMaxListed {
			fmt.Fprintf(&b, "\n  ... and %d more", len(e.Paths)-mergeScopeMaxListed)
			break
		}
		fmt.Fprintf(&b, "\n  %q", p)
	}
	b.WriteString("\nRemove these changes from the branch (or ask the rig operator to widen allowed_paths in settings/config.json).")
	return b.String()
}

// mergeScopeDiffer is the git surface the scope check needs.
type mergeScopeDiffer interface {
	DiffNameOnly(base, head string) ([]string, error)
}

// loadMergeScope returns the rig's merge_queue.allowed_paths (nil =
// unrestricted). Only that key is read, so unrelated invalid settings never
// block submission; an unparseable file or invalid pattern fails closed. See
// config.LoadMergeQueueAllowedPaths.
func loadMergeScope(rigPath string) ([]string, error) {
	allowed, err := config.LoadMergeQueueAllowedPaths(config.RigSettingsPath(rigPath))
	if err != nil {
		return nil, fmt.Errorf("merge scope: %w", err)
	}
	return allowed, nil
}

// checkMergeScope diffs base...head and returns the number of changed paths
// checked, or a *MergeScopeError listing the paths outside allowed. An empty
// allowed list is unrestricted and does not run git. A diff failure is
// returned as an error (fail closed).
func checkMergeScope(g mergeScopeDiffer, allowed []string, base, head string) (int, error) {
	return checkMergeScopeWith(allowed, base, "...", head, g.DiffNameOnly)
}

// checkMergeScopeWith is the single scope check: list changed paths with diff
// (base<sep>head) and report those outside allowed. Empty allowed = no git.
func checkMergeScopeWith(allowed []string, base, sep, head string, diff func(base, head string) ([]string, error)) (int, error) {
	if len(allowed) == 0 {
		return 0, nil
	}
	changed, err := diff(base, head)
	if err != nil {
		return 0, fmt.Errorf("merge scope: diff %s%s%s: %w", base, sep, head, err)
	}
	if outside := config.PathsOutsideAllowed(allowed, changed); len(outside) > 0 {
		return len(changed), &MergeScopeError{Paths: outside, Base: base, Head: head, Range: sep}
	}
	return len(changed), nil
}

// enforceMergeScope is the submit-time gate used by gt done and gt mq submit:
// it loads the rig scope and checks base...head against it.
func enforceMergeScope(rigPath string, g mergeScopeDiffer, base, head string) error {
	allowed, err := loadMergeScope(rigPath)
	if err != nil {
		return err
	}
	_, err = checkMergeScope(g, allowed, base, head)
	return err
}

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
On success the last line is "Checked-SHA: <tip>"; the refinery formula merges
exactly that commit, so a later push is not merged unchecked as long as the
refinery follows the formula. Exit status is non-zero when any changed path
is outside allowed_paths (the offending paths are listed), or when the check
cannot be completed (fail closed). A rig without allowed_paths is unrestricted
and always passes.

Designed for the refinery patrol: run it before merge-push and treat a failure
like a failed gate (FIX_NEEDED, do not merge). This merge-time check is a formula
instruction; the hard gate is gt done / gt mq submit.

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
	FindMRForPostMerge(idOrBranch string) (*refinery.MergeRequest, error)
}

// mqCheckScopeGit is the git surface check-scope needs.
type mqCheckScopeGit interface {
	mergeScopeDiffer
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
	allowed, err := loadMergeScope(rigPath)
	if err != nil {
		return "", err
	}
	if len(allowed) == 0 {
		// Unrestricted: no Checked-SHA; the refinery merges origin/<branch> as before.
		return fmt.Sprintf("%s merge scope unrestricted (no merge_queue.allowed_paths)", style.Dim.Render("○")), nil
	}
	mr, err := mgr.FindMRForPostMerge(idOrBranch)
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
	n, err := checkMergeScope(g, allowed, base, sha)
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
	allowed, err := loadMergeScope(rigPath)
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
	n, err := checkMergeScopeWith(allowed, oldRev, "..", newRev, g.DiffNameOnlyRange)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s %d changed path(s) within allowed_paths (%s..%s)",
		style.Bold.Render("✓"), n, oldRev, newRev), nil
}
