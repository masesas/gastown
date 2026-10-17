package doctor

import (
	"encoding/csv"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/doltserver"
	"github.com/steveyegge/gastown/internal/mergeproof"
)

// NullAssigneeCheck detects in_progress beads with a NULL or empty assignee.
//
// These records arise from partial writes (crash mid-transaction or race
// condition during bd update). They are invisible to bd show / bd list because
// bd's deserialization fails silently on a NULL assignee, so the steps remain
// stuck in_progress indefinitely and block molecule progress.
//
// Detection uses bd sql --csv (raw SQL passthrough, not affected by bd's ORM).
// Fix resets status to open + clears assignee so the step can be re-dispatched,
// then issues a Dolt commit to record the repair in history. Beads held by the
// merge queue (gt done parks a submitted MR's source as in_progress with no
// assignee, gt-pd1c.59) are excluded; with the merge queue unreadable nothing
// is fixed.
type NullAssigneeCheck struct {
	FixableCheck
	affected []nullAssigneeRow
}

type nullAssigneeRow struct {
	ID        string
	Title     string
	UpdatedAt string
	RigDB     string // Dolt database name (= rig name)
}

// NewNullAssigneeCheck creates a new null-assignee steps check.
func NewNullAssigneeCheck() *NullAssigneeCheck {
	return &NullAssigneeCheck{
		FixableCheck: FixableCheck{
			BaseCheck: BaseCheck{
				CheckName:        "null-assignee-steps",
				CheckDescription: "Check for in_progress beads with NULL assignee (invisible to bd, blocking indefinitely)",
				CheckCategory:    CategoryCleanup,
			},
		},
	}
}

const nullAssigneeSelectQuery = `SELECT id, title, updated_at FROM issues WHERE status = 'in_progress' AND (assignee IS NULL OR assignee = '') ORDER BY updated_at ASC`

// nullAssigneeMergeQueueFn reads the town's merge queue; tests override it.
var nullAssigneeMergeQueueFn = beads.TownMergeQueueIndex

// Run queries each rig database for in_progress beads with NULL/empty assignee.
func (c *NullAssigneeCheck) Run(ctx *CheckContext) *CheckResult {
	c.affected = nil

	databases, err := doltserver.ListDatabases(ctx.TownRoot)
	if err != nil || len(databases) == 0 {
		return &CheckResult{
			Name:     c.Name(),
			Status:   StatusOK,
			Message:  "No rig databases found (skipping)",
			Category: c.Category(),
		}
	}

	var rows []nullAssigneeRow
	for _, db := range databases {
		rigDir := filepath.Join(ctx.TownRoot, db)
		found, err := queryNullAssigneeBeads(rigDir)
		if err != nil {
			// Non-fatal: Dolt might not be running or rig may not be bd-managed.
			continue
		}
		for _, row := range found {
			row.RigDB = db
			rows = append(rows, row)
		}
	}
	return c.classify(ctx, rows, nullAssigneeMergeQueueFn)
}

// classify drops beads held by the merge queue (gt-pd1c.59: gt done parks the
// source of a submitted MR as in_progress with no assignee until the refinery
// closes or releases it) and reports the rest as fixable. An unreadable merge
// queue makes nothing fixable (fail closed: resetting a parked bead to open
// would dispatch work that is waiting for its merge).
func (c *NullAssigneeCheck) classify(ctx *CheckContext, rows []nullAssigneeRow, readQueue func(string) (beads.MergeQueueIndex, error)) *CheckResult {
	c.affected = nil
	if len(rows) == 0 {
		return c.okResult()
	}
	idx, err := readQueue(ctx.TownRoot)
	if err != nil {
		return &CheckResult{
			Name:     c.Name(),
			Status:   StatusWarning,
			Message:  fmt.Sprintf("%d in_progress bead(s) with NULL assignee, but the merge queue is unreadable: cannot tell broken beads from beads awaiting merge", len(rows)),
			Details:  []string{err.Error()},
			FixHint:  "Restore the beads/Dolt server, then rerun 'gt doctor'",
			Category: c.Category(),
		}
	}
	kept, held := splitMergeQueueHeld(rows, idx, mergeproof.Resolver(ctx.TownRoot))
	c.affected = kept
	if len(kept) == 0 {
		return c.okResult()
	}

	details := make([]string, 0, len(kept)+1)
	for _, row := range kept {
		details = append(details, fmt.Sprintf("[%s] %s — %s (updated: %s)",
			row.RigDB, row.ID, shortenTitle(row.Title, 60), row.UpdatedAt))
	}
	if len(held) > 0 {
		details = append(details, fmt.Sprintf("%d more held by the merge queue (awaiting merge) — not touched", len(held)))
	}

	return &CheckResult{
		Name:   c.Name(),
		Status: StatusWarning,
		Message: fmt.Sprintf(
			"%d in_progress bead(s) with NULL assignee — invisible to bd, blocking molecule progress",
			len(kept),
		),
		Details:  details,
		FixHint:  "Run 'gt doctor --fix' to reset to open for re-dispatch",
		Category: c.Category(),
	}
}

func (c *NullAssigneeCheck) okResult() *CheckResult {
	return &CheckResult{
		Name:     c.Name(),
		Status:   StatusOK,
		Message:  "No in_progress beads with NULL assignee found",
		Category: c.Category(),
	}
}

// splitMergeQueueHeld separates rows the merge queue holds (beads.MergeQueueHold)
// from the rest.
func splitMergeQueueHeld(rows []nullAssigneeRow, idx beads.MergeQueueIndex, resolve beads.ProverResolver) (kept, held []nullAssigneeRow) {
	for _, row := range rows {
		if idx.Held(row.ID, resolve) {
			held = append(held, row)
			continue
		}
		kept = append(kept, row)
	}
	return kept, held
}

// nullAssigneeFixQueryFor resets exactly the given beads (still in_progress
// with no assignee) to open.
func nullAssigneeFixQueryFor(ids []string) string {
	quoted := make([]string, 0, len(ids))
	for _, id := range ids {
		quoted = append(quoted, "'"+strings.ReplaceAll(id, "'", "''")+"'")
	}
	return "UPDATE issues SET status = 'open', assignee = '' WHERE id IN (" + strings.Join(quoted, ", ") +
		") AND status = 'in_progress' AND (assignee IS NULL OR assignee = '')"
}

// Fix resets the affected beads (not held by the merge queue) to open (clears
// assignee) via direct SQL, then commits the repair to Dolt history.
func (c *NullAssigneeCheck) Fix(ctx *CheckContext) error {
	if len(c.affected) == 0 {
		return nil
	}

	// Group affected bead IDs by database.
	affected := make(map[string][]string)
	for _, row := range c.affected {
		affected[row.RigDB] = append(affected[row.RigDB], row.ID)
	}

	var errs []string
	for db, ids := range affected {
		rigDir := filepath.Join(ctx.TownRoot, db)

		// Reset beads via direct SQL (bypasses bd ORM which fails on NULL assignee).
		if err := execBdSQLWrite(rigDir, nullAssigneeFixQueryFor(ids)); err != nil {
			errs = append(errs, fmt.Sprintf("%s: update failed: %v", db, err))
			continue
		}

		// Commit the repair to Dolt history (non-fatal: repair is effective even
		// without a version commit, but the commit gives audit visibility).
		commitMsg := "fix: reset in_progress beads with null assignee (gt doctor)"
		if err := doltserver.CommitServerWorkingSet(ctx.TownRoot, db, commitMsg); err != nil {
			// Non-fatal: data is already fixed, commit is best-effort.
			_ = err
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("partial fix: %s", strings.Join(errs, "; "))
	}
	return nil
}

// queryNullAssigneeBeads returns in_progress beads with NULL/empty assignee for a rig.
// Uses bd sql --csv (raw SQL passthrough, not affected by bd ORM deserialization).
func queryNullAssigneeBeads(rigDir string) ([]nullAssigneeRow, error) {
	cmd := exec.Command("bd", "sql", "--csv", nullAssigneeSelectQuery) //nolint:gosec // G204: args are constants
	cmd.Dir = rigDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("bd sql: %w", err)
	}

	r := csv.NewReader(strings.NewReader(string(output)))
	records, err := r.ReadAll()
	if err != nil || len(records) < 2 {
		return nil, nil // No results or empty table
	}

	rows := make([]nullAssigneeRow, 0, len(records)-1)
	for _, rec := range records[1:] { // Skip CSV header
		if len(rec) < 3 {
			continue
		}
		rows = append(rows, nullAssigneeRow{
			ID:        strings.TrimSpace(rec[0]),
			Title:     strings.TrimSpace(rec[1]),
			UpdatedAt: strings.TrimSpace(rec[2]),
		})
	}
	return rows, nil
}

// execBdSQLWrite executes a SQL write statement via bd sql.
func execBdSQLWrite(rigDir, query string) error {
	cmd := exec.Command("bd", "sql", query) //nolint:gosec // G204: query is a constant
	cmd.Dir = rigDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

// shortenTitle truncates a title with ellipsis if it exceeds n runes.
func shortenTitle(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-3]) + "..."
}
