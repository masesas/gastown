package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/refinery"
	"github.com/steveyegge/gastown/internal/style"
)

// Merge-scope gate (gt-pd1c.68).
//
// merge_queue.allowed_paths in <rig>/settings/config.json limits which repo
// paths an MR may change. The gate runs at both convergence points:
//   - submit time: gt done and gt mq submit refuse an out-of-scope branch
//     before any MR bead exists (enforceMergeScope);
//   - merge time: the refinery patrol runs `gt mq check-scope` on the freshly
//     fetched branch right before merge-push, so a branch re-pushed after
//     submit cannot slip through.
// Both use checkMergeScope, which diffs merge-base(base)..head with
// git.DiffNameOnly (byte-exact, renames counted on both sides).

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
}

func (e *MergeScopeError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "merge scope violation: %d path(s) changed in %s...%s are outside merge_queue.allowed_paths:",
		len(e.Paths), e.Base, e.Head)
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
	if len(allowed) == 0 {
		return 0, nil
	}
	changed, err := g.DiffNameOnly(base, head)
	if err != nil {
		return 0, fmt.Errorf("merge scope: diff %s...%s: %w", base, head, err)
	}
	if outside := config.PathsOutsideAllowed(allowed, changed); len(outside) > 0 {
		return len(changed), &MergeScopeError{Paths: outside, Base: base, Head: head}
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

var mqCheckScopeCmd = &cobra.Command{
	Use:   "check-scope <rig> <mr-id-or-branch>",
	Short: "Verify an MR only changes paths allowed by merge_queue.allowed_paths",
	Long: `Check that a merge request changes only paths allowed by the rig's
merge_queue.allowed_paths (settings/config.json).

The command fetches origin, resolves the tip of origin/<branch> once and diffs
origin/<target>...<tip>, so a branch re-pushed after submit is checked again.
On success the last line is "Checked-SHA: <tip>": the refinery merges exactly
that commit, so a push landing after the check is never merged unchecked. Exit status is non-zero when any changed path
is outside allowed_paths (the offending paths are listed), or when the check
cannot be completed (fail closed). A rig without allowed_paths is unrestricted
and always passes.

Designed for the refinery patrol: run it before merge-push and treat a failure
like a failed gate (FIX_NEEDED, do not merge).

Examples:
  gt mq check-scope gastown gt-mr-abc123
  gt mq check-scope gastown polecat/nux/gt-xyz`,
	Args: cobra.ExactArgs(2),
	RunE: runMQCheckScope,
}

func init() {
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
