package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/cli"
	"github.com/steveyegge/gastown/internal/events"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/workspace"
)

// gt bead clear-review-only (gt-pd1c.75) is the one explicit way to remove the
// sticky review_only marker (gt-pd1c.69) from a bead. gt sling --review-only
// sets it; nothing in the dispatch paths drops it. Clearing is a decision
// about the work, so it is audited (bead comment + review_only_cleared event)
// and refused to every agent except the mayor.

var beadClearReviewOnlyReason string

var beadClearReviewOnlyCmd = &cobra.Command{
	Use:   "clear-review-only <bead-id> --reason <why>",
	Short: "Remove the sticky review_only marker from a bead (audited)",
	Long: `Remove the review_only marker from a bead's description header.

gt sling --review-only stores review_only: true on the bead, and every later
sling, detach and burn keeps it (review-only mode is sticky). This command is
the explicit way to remove it, e.g. when a bead was marked by mistake or kept a
non-canonical value such as "review_only: off" (which counts as review-only).

Only a human (overseer), crew or the mayor may clear the marker; polecats and
other agents are refused. The clear is recorded as a bead comment (who, when,
why) before the description is changed, and as a review_only_cleared event.
A bead that is not review-only is left untouched (exit 0).

Examples:
  gt bead clear-review-only gt-abc123 --reason "marked review-only by mistake"`,
	Args: cobra.ExactArgs(1),
	RunE: runBeadClearReviewOnly,
}

func init() {
	beadClearReviewOnlyCmd.Flags().StringVar(&beadClearReviewOnlyReason, "reason", "", "Why the marker is removed (required, recorded in the audit comment)")
	beadCmd.AddCommand(beadClearReviewOnlyCmd)
}

// reviewOnlyClearActor returns who clears a review_only marker: a human, crew
// or the overseer (humanOrCrewActor), or the mayor. Polecats, conflicting
// identities and every other agent role are refused, so a polecat can never
// lift the marker of its own work. Like every gtCaller check, this guards
// against agents doing it by mistake; it is not an authentication boundary.
func reviewOnlyClearActor() (string, error) {
	c := gtCallerFrom(os.Getenv)
	if !c.exempt() && c.role == RoleMayor {
		if c.actor != "" {
			return c.actor, nil
		}
		return "mayor", nil
	}
	actor, err := humanOrCrewActor("to clear review_only")
	if err != nil {
		return "", fmt.Errorf("%w, or the mayor", err)
	}
	return actor, nil
}

// reviewOnlyClearStore is what clearing needs from beads; *beads.Beads
// implements it.
type reviewOnlyClearStore interface {
	Show(id string) (*beads.Issue, error)
	AddComment(id, comment string) error
	Update(id string, opts beads.UpdateOptions) error
}

// reviewOnlyClearStoreFor returns the beads store that owns beadID; a seam for
// tests.
var reviewOnlyClearStoreFor = func(townRoot, beadID string) reviewOnlyClearStore {
	beadsDir := beads.ResolveBeadsDirForID(filepath.Join(townRoot, ".beads"), beadID)
	return beads.NewWithBeadsDir(filepath.Dir(beadsDir), beadsDir)
}

// clearReviewOnlyMarker removes the review_only marker from beadID. It returns
// false without writing when the bead is not review-only (idempotent). The
// audit comment is written first: if it fails nothing changes, so a marker is
// never removed without a record. The description write goes through
// beads.WithoutReviewOnlyMarker, the one unset path.
func clearReviewOnlyMarker(store reviewOnlyClearStore, beadID, actor, reason string, now time.Time) (bool, error) {
	issue, err := store.Show(beadID)
	if err != nil {
		return false, fmt.Errorf("reading %s: %w; review_only was kept", beadID, err)
	}
	if !beads.IsReviewOnly(issue) {
		return false, nil
	}
	comment := fmt.Sprintf("review_only cleared by %s at %s: %s", actor, now.UTC().Format(time.RFC3339), reason)
	if err := store.AddComment(beadID, comment); err != nil {
		return false, fmt.Errorf("recording the audit comment on %s: %w; review_only was kept", beadID, err)
	}
	newDesc := beads.WithoutReviewOnlyMarker(issue.Description)
	if err := store.Update(beadID, beads.UpdateOptions{Description: &newDesc}); err != nil {
		return false, fmt.Errorf("removing review_only from %s: %w; the bead is still review-only (the audit comment was recorded), retry", beadID, err)
	}
	return true, nil
}

func runBeadClearReviewOnly(cmd *cobra.Command, args []string) error {
	beadID := strings.TrimSpace(args[0])
	if beadID == "" || strings.HasPrefix(beadID, "-") || strings.ContainsAny(beadID, " \t\r\n/") {
		return fmt.Errorf("invalid bead ID %q", beadID)
	}
	reason := strings.TrimSpace(beadClearReviewOnlyReason)
	if reason == "" {
		return fmt.Errorf("--reason is required: clearing review_only is recorded with who, when and why")
	}
	actor, err := reviewOnlyClearActor()
	if err != nil {
		return err
	}
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}
	// A sling of the same bead reads and rewrites the description: never race it.
	release, err := tryAcquireSlingBeadLock(townRoot, beadID)
	if err != nil {
		return err
	}
	defer release()

	cleared, err := clearReviewOnlyMarker(reviewOnlyClearStoreFor(townRoot, beadID), beadID, actor, reason, time.Now())
	if err != nil {
		return err
	}
	if !cleared {
		fmt.Printf("%s %s is not review-only: nothing to clear\n", style.Dim.Render("○"), beadID)
		return nil
	}
	_ = events.LogFeedAt(townRoot, events.TypeReviewOnlyCleared, actor, events.ReviewOnlyClearedPayload(beadID, actor, reason))
	fmt.Printf("%s Cleared review_only on %s (by %s: %s)\n", style.Bold.Render("✓"), beadID, actor, reason)
	return nil
}

// reviewOnlyMarkerWarnings explains a review_only field that does not mean
// what it looks like: a non-canonical value (counts as review-only, kept as
// written) or a marker line outside the description header (ignored).
func reviewOnlyMarkerWarnings(beadID string, info *beadInfo) []string {
	if info == nil {
		return nil
	}
	status := beads.ReviewOnlyStatusOf(&beads.Issue{Description: info.Description})
	var warnings []string
	if status.NonCanonical != "" {
		warnings = append(warnings, fmt.Sprintf(
			"%s has review_only: %q, which is not true/false: it is treated as review-only (fail closed) and kept as written; set it to true, or run %s bead clear-review-only %s --reason \"<why>\"",
			beadID, status.NonCanonical, cli.Name(), beadID))
	}
	if !status.Marked && status.IgnoredLines > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"%s has a review_only line outside the description header: it is ignored and the bead is NOT review-only; use %s sling --review-only to mark it",
			beadID, cli.Name()))
	}
	return warnings
}

func printReviewOnlyMarkerWarnings(beadID string, info *beadInfo) {
	for _, warning := range reviewOnlyMarkerWarnings(beadID, info) {
		style.PrintWarning("%s", warning)
	}
}
