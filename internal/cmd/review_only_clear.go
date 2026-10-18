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
	"github.com/steveyegge/gastown/internal/util"
	"github.com/steveyegge/gastown/internal/workspace"
)

// gt bead clear-review-only (gt-pd1c.75) is the one explicit way to remove the
// sticky review_only marker (gt-pd1c.69) from a bead. gt sling --review-only
// sets it; nothing in the dispatch paths drops it. Clearing reopens the way to
// commits and merges for the work, so it is a human action: a human (overseer)
// or crew at an interactive terminal who types the bead ID again. Every agent
// is refused, the mayor included (it escalates by mail instead). The clear is
// audited with a request comment before the write, a confirmation comment
// after a verified write, and a review_only_cleared event.

var beadClearReviewOnlyReason string

// maxClearReasonLen bounds the --reason recorded in the audit comments.
const maxClearReasonLen = 500

var beadClearReviewOnlyCmd = &cobra.Command{
	Use:   "clear-review-only <bead-id> --reason <why>",
	Short: "Remove the sticky review_only marker from a bead (human only, audited)",
	Long: `Remove the review_only marker from a bead's description header.

gt sling --review-only stores review_only: true on the bead, and every later
sling, detach and burn keeps it (review-only mode is sticky). This command is
the explicit way to remove it, e.g. when a bead was marked by mistake or kept a
non-canonical value such as "review_only: off" (which counts as review-only).

Only a human (overseer) or crew may clear the marker, from an interactive
terminal, typing the bead ID again to confirm. Every agent is refused, the
mayor included: an agent that thinks the marker is wrong escalates by mail.
The clear is recorded as a bead comment ("review_only clear requested by ...")
before the description is changed, a second comment ("review_only cleared by
...") once the change is verified, and a review_only_cleared event. A bead that
is not review-only is left untouched (exit 0, no prompt).

--reason is one line (line breaks are folded), at most 500 bytes, without
control characters.

Examples:
  gt bead clear-review-only gt-abc123 --reason "marked review-only by mistake"`,
	Args: cobra.ExactArgs(1),
	RunE: runBeadClearReviewOnly,
}

func init() {
	beadClearReviewOnlyCmd.Flags().StringVar(&beadClearReviewOnlyReason, "reason", "", "Why the marker is removed (required, recorded in the audit comments)")
	beadCmd.AddCommand(beadClearReviewOnlyCmd)
}

// reviewOnlyClearActor returns who clears a review_only marker: a human, crew
// or the overseer (humanOrCrewActor). Every agent role (polecat, mayor,
// witness, refinery, deacon, ...) and conflicting identities are refused. Like
// every gtCaller check, this guards against agents doing it by mistake; it is
// not an authentication boundary. The actor is recorded in the audit
// comments, so it must be a single line without control characters.
func reviewOnlyClearActor() (string, error) {
	actor, err := humanOrCrewActor("to clear review_only")
	if err != nil {
		return "", err
	}
	if util.HasControlChars(actor) || util.OneLine(actor, 0) != actor {
		return "", fmt.Errorf("refusing to clear review_only: the caller identity %q is not a single line of printable text; only a human (overseer) or crew with a printable identity may do this", actor)
	}
	return actor, nil
}

// normalizeClearReason makes --reason one line (whitespace folded, URL
// credentials masked, util.OneLine) and refuses an empty reason, control
// characters and more than maxClearReasonLen bytes.
func normalizeClearReason(raw string) (string, error) {
	// Refuse (not strip) control characters other than line breaks and tabs:
	// a recorded human reason is not silently altered. util.OneLine would
	// drop them, so check the whitespace-folded input first.
	if util.HasControlChars(strings.Join(strings.Fields(raw), " ")) {
		return "", fmt.Errorf("--reason must not contain control characters")
	}
	reason := util.OneLine(raw, 0)
	switch {
	case reason == "":
		return "", fmt.Errorf("--reason is required: clearing review_only is recorded with who, when and why")
	case len(reason) > maxClearReasonLen:
		return "", fmt.Errorf("--reason is %d bytes; at most %d are allowed", len(reason), maxClearReasonLen)
	}
	return reason, nil
}

// reviewOnlyClearStore is what clearing needs from beads; *beads.Beads
// implements it.
type reviewOnlyClearStore interface {
	Show(id string) (*beads.Issue, error)
	AddComment(id, comment string) error
	Update(id string, opts beads.UpdateOptions) error
}

// townReviewOnlyClearStore returns the beads store that owns beadID.
func townReviewOnlyClearStore(townRoot, beadID string) reviewOnlyClearStore {
	beadsDir := beads.ResolveBeadsDirForID(filepath.Join(townRoot, ".beads"), beadID)
	return beads.NewWithBeadsDir(filepath.Dir(beadsDir), beadsDir)
}

// clearReviewOnlyDeps are the outside effects of gt bead clear-review-only,
// injected so tests (and a later switch of the human prompt implementation)
// replace one value instead of package variables.
type clearReviewOnlyDeps struct {
	// confirm asks the human to type expected again (TTY required).
	confirm  func(action, prompt, expected string) error
	storeFor func(townRoot, beadID string) reviewOnlyClearStore
	now      func() time.Time
}

func defaultClearReviewOnlyDeps() clearReviewOnlyDeps {
	return clearReviewOnlyDepsWith(stdioHumanPrompt())
}

// clearReviewOnlyDepsWith are the production deps with the human asked at
// prompt (the shared humanPrompt seam).
func clearReviewOnlyDepsWith(prompt humanPrompt) clearReviewOnlyDeps {
	return clearReviewOnlyDeps{confirm: prompt.confirm, storeFor: townReviewOnlyClearStore, now: time.Now}
}

// reviewOnlyClearOutcome is the result of clearReviewOnlyMarker. ConfirmErr
// is set when the marker was removed but the confirmation comment could not
// be written (the request comment is on the bead).
type reviewOnlyClearOutcome struct {
	Cleared    bool
	ConfirmErr error
}

// clearReviewOnlyMarker removes the review_only marker from beadID. A bead
// that is not review-only is left alone (idempotent). Order:
//  1. request comment "review_only clear requested by <who> at <ts>: <why>";
//     if it fails nothing changes, so the marker is never removed unrecorded;
//  2. the description write, through beads.WithoutReviewOnlyMarker (the one
//     unset path);
//  3. a re-read that must show the bead no longer review-only;
//  4. confirmation comment "review_only cleared by <who> at <ts>: <why>".
func clearReviewOnlyMarker(store reviewOnlyClearStore, beadID, actor, reason string, now time.Time) (reviewOnlyClearOutcome, error) {
	issue, err := store.Show(beadID)
	if err != nil {
		return reviewOnlyClearOutcome{}, fmt.Errorf("reading %s: %w; review_only was kept", beadID, err)
	}
	if !beads.IsReviewOnly(issue) {
		return reviewOnlyClearOutcome{}, nil
	}
	stamp := fmt.Sprintf("%s at %s: %s", actor, now.UTC().Format(time.RFC3339), reason)
	if err := store.AddComment(beadID, "review_only clear requested by "+stamp); err != nil {
		return reviewOnlyClearOutcome{}, fmt.Errorf("recording the audit comment on %s: %w; review_only was kept", beadID, err)
	}
	newDesc := beads.WithoutReviewOnlyMarker(issue.Description)
	if err := store.Update(beadID, beads.UpdateOptions{Description: &newDesc}); err != nil {
		return reviewOnlyClearOutcome{}, fmt.Errorf("removing review_only from %s: %w; the bead is still review-only (the request comment was recorded), retry", beadID, err)
	}
	after, err := store.Show(beadID)
	if err != nil {
		return reviewOnlyClearOutcome{}, fmt.Errorf("verifying %s after the clear: %w; check it with bd show and retry", beadID, err)
	}
	if beads.IsReviewOnly(after) {
		return reviewOnlyClearOutcome{}, fmt.Errorf("%s is still review-only after the update (concurrent write?); retry", beadID)
	}
	outcome := reviewOnlyClearOutcome{Cleared: true}
	if err := store.AddComment(beadID, "review_only cleared by "+stamp); err != nil {
		outcome.ConfirmErr = err
	}
	return outcome, nil
}

func runBeadClearReviewOnly(cmd *cobra.Command, args []string) error {
	return clearReviewOnlyCommand(args[0], beadClearReviewOnlyReason, defaultClearReviewOnlyDeps())
}

// clearReviewOnlyCommand is gt bead clear-review-only with its dependencies.
func clearReviewOnlyCommand(rawBeadID, rawReason string, deps clearReviewOnlyDeps) error {
	beadID := strings.TrimSpace(rawBeadID)
	if !beads.IsSafeID(beadID) {
		return fmt.Errorf("invalid bead ID %q", beadID)
	}
	reason, err := normalizeClearReason(rawReason)
	if err != nil {
		return err
	}
	actor, err := reviewOnlyClearActor()
	if err != nil {
		return err
	}
	townRoot, err := workspace.FindFromCwdOrError()
	if err != nil {
		return fmt.Errorf("not in a Gas Town workspace: %w", err)
	}
	store := deps.storeFor(townRoot, beadID)

	// Nothing to clear: no prompt, no comment.
	issue, err := store.Show(beadID)
	if err != nil {
		return fmt.Errorf("reading %s: %w", beadID, err)
	}
	if !beads.IsReviewOnly(issue) {
		fmt.Printf("%s %s is not review-only: nothing to clear\n", style.Dim.Render("○"), beadID)
		return nil
	}
	prompt := fmt.Sprintf("Clear review_only on %s? Its work may then be committed, pushed and merged.\nReason: %s\nType the bead ID to confirm: ", beadID, reason)
	if err := deps.confirm("clearing review_only on "+beadID, prompt, beadID); err != nil {
		return err
	}

	// A sling of the same bead reads and rewrites the description: never race it.
	release, err := tryAcquireSlingBeadLock(townRoot, beadID)
	if err != nil {
		return err
	}
	defer release()

	outcome, err := clearReviewOnlyMarker(store, beadID, actor, reason, deps.now())
	if err != nil {
		return err
	}
	if !outcome.Cleared {
		fmt.Printf("%s %s is not review-only: nothing to clear\n", style.Dim.Render("○"), beadID)
		return nil
	}
	if outcome.ConfirmErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: review_only was cleared on %s, but the confirmation comment could not be written: %v (the request comment is recorded)\n", beadID, outcome.ConfirmErr)
	}
	// The event feed is best-effort; the bead comments are the audit record.
	if err := events.LogFeedAt(townRoot, events.TypeReviewOnlyCleared, actor, events.ReviewOnlyClearedPayload(beadID, actor, reason)); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not record the review_only_cleared event for %s: %v\n", beadID, err)
	}
	fmt.Printf("%s Cleared review_only on %s (by %s: %s)\n", style.Bold.Render("✓"), beadID, actor, reason)
	return nil
}

// reviewOnlyMarkerWarnings explains a review_only field that does not mean
// what it looks like: a non-canonical value (counts as review-only, kept as
// written) or a marker line outside the description header (ignored). gt
// sling, the scheduler dispatch, gt done and gt mq submit all print these
// (printReviewOnlyMarkerWarnings).
func reviewOnlyMarkerWarnings(beadID string, info *beadInfo) []string {
	if info == nil {
		return nil
	}
	return reviewOnlyDescriptionWarnings(beadID, info.Description)
}

func reviewOnlyDescriptionWarnings(beadID, description string) []string {
	status := beads.ReviewOnlyStatusOf(&beads.Issue{Description: description})
	var warnings []string
	if status.NonCanonical {
		warnings = append(warnings, fmt.Sprintf(
			"%s has review_only: %q, which is not true/false: it is treated as review-only (fail closed) and kept as written; set it to true, or have a human run %s bead clear-review-only %s --reason \"<why>\"",
			beadID, status.Value, cli.Name(), beadID))
	}
	if !status.Marked && status.IgnoredLines > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"%s has a review_only line outside the description header: it is ignored and the bead is NOT review-only; use %s sling --review-only to mark it",
			beadID, cli.Name()))
	}
	return warnings
}

func printReviewOnlyMarkerWarnings(beadID string, info *beadInfo) {
	if info == nil {
		return
	}
	printReviewOnlyDescriptionWarnings(beadID, info.Description)
}

func printReviewOnlyDescriptionWarnings(beadID, description string) {
	for _, warning := range reviewOnlyDescriptionWarnings(beadID, description) {
		style.PrintWarning("%s", warning)
	}
}
