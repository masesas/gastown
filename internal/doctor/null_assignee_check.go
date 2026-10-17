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
	affected   []nullAssigneeRow // reopened by Fix
	landed     []nullAssigneeRow // closed by Fix: parked beads whose MR landed
	unverified []nullAssigneeRow // never fixed: a human verdict is needed
}

type nullAssigneeRow struct {
	ID        string
	Title     string
	UpdatedAt string
	RigDB     string // Dolt database name (= rig name)
	MRID      string // deciding MR (landed: closed by Fix; unverified: needs a verdict)
	MRRig     string // rig of the deciding MR (for the exit command)
	Reason    string // why landing is unverified
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

// nullAssigneeMergeQueueFn reads the town-wide merge queue; tests override it.
var nullAssigneeMergeQueueFn = mergeproof.TownMergeQueue

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

// classify sorts the rows with the merge queue (gt-pd1c.59: gt done parks the
// source of a submitted MR as in_progress with no assignee until the refinery
// closes or releases it): a pending one is waiting for its merge and is left
// alone, a landed one (MR closed by hand, so nothing closed its source) is
// closed by Fix, the rest is reopened by Fix. An unreadable merge queue makes
// nothing fixable (fail closed).
func (c *NullAssigneeCheck) classify(ctx *CheckContext, rows []nullAssigneeRow, readQueue func(string) *beads.MergeQueue) *CheckResult {
	c.affected, c.landed, c.unverified = nil, nil, nil
	if len(rows) == 0 {
		return c.okResult()
	}
	queue := readQueue(ctx.TownRoot)
	if err := queue.Err(); err != nil {
		return &CheckResult{
			Name:     c.Name(),
			Status:   StatusWarning,
			Message:  fmt.Sprintf("%d in_progress bead(s) with NULL assignee, but the merge queue is unreadable: cannot tell broken beads from beads awaiting merge", len(rows)),
			Details:  []string{err.Error()},
			FixHint:  "Restore the beads/Dolt server, then rerun 'gt doctor'",
			Category: c.Category(),
		}
	}
	reopen, landed, pending, unverified := splitMergeQueueHeld(rows, queue)
	c.affected, c.landed, c.unverified = reopen, landed, unverified
	if len(reopen) == 0 && len(landed) == 0 && len(unverified) == 0 {
		return c.okResult()
	}

	details := make([]string, 0, len(reopen)+len(landed)+len(unverified)+1)
	for _, row := range reopen {
		details = append(details, fmt.Sprintf("[%s] %s — %s (updated: %s)",
			row.RigDB, row.ID, shortenTitle(row.Title, 60), row.UpdatedAt))
	}
	for _, row := range landed {
		details = append(details, fmt.Sprintf("[%s] %s — MR %s landed, bead still open: will be closed",
			row.RigDB, row.ID, row.MRID))
	}
	for _, row := range unverified {
		rig := row.MRRig
		if rig == "" {
			rig = row.RigDB
		}
		notLanded, landed := beads.UnverifiedExitCommands(rig, row.MRID)
		details = append(details, fmt.Sprintf("[%s] %s — UNVERIFIED: MR %s is closed but its landing cannot be decided (%s; updated: %s). Not fixed automatically; an overseer decides: `%s` (reopen for rework) or `%s` (close)",
			row.RigDB, row.ID, row.MRID, row.Reason, row.UpdatedAt, notLanded, landed))
	}
	if len(pending) > 0 {
		details = append(details, fmt.Sprintf("%d more held by a pending MR (awaiting merge) — not touched", len(pending)))
	}

	return &CheckResult{
		Name:   c.Name(),
		Status: StatusWarning,
		Message: fmt.Sprintf(
			"%d in_progress bead(s) with NULL assignee to reopen, %d whose MR landed to close, %d unverified (need an overseer verdict)",
			len(reopen), len(landed), len(unverified),
		),
		Details:  details,
		FixHint:  "Run 'gt doctor --fix' to reopen broken beads and close landed work; unverified beads need the commands listed",
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

// splitMergeQueueHeld sorts rows by their merge-queue hold
// (beads.MergeQueueHold): none (reopen), landed (close), pending (leave: the
// MR is still in the queue), unverified (leave, but report: the MR is closed
// and only a human verdict can decide).
func splitMergeQueueHeld(rows []nullAssigneeRow, queue *beads.MergeQueue) (reopen, landed, pending, unverified []nullAssigneeRow) {
	for _, row := range rows {
		d, _ := queue.Detail(row.ID)
		switch d.Hold {
		case beads.HoldLanded:
			row.MRID = d.MRID
			landed = append(landed, row)
		case beads.HoldPending:
			pending = append(pending, row)
		case beads.HoldUnverified:
			row.MRID, row.MRRig, row.Reason = d.MRID, d.Rig, d.Reason
			unverified = append(unverified, row)
		default:
			reopen = append(reopen, row)
		}
	}
	return reopen, landed, pending, unverified
}

// nullAssigneeFixQueryFor resets exactly the given beads (still in_progress
// with no assignee) to open. Every ID must be beads.IsSafeID.
func nullAssigneeFixQueryFor(ids []string) (string, error) {
	quoted := make([]string, 0, len(ids))
	for _, id := range ids {
		if !beads.IsSafeID(id) {
			return "", fmt.Errorf("refusing bead ID %q: not a valid bead ID", id)
		}
		quoted = append(quoted, "'"+id+"'")
	}
	return "UPDATE issues SET status = 'open', assignee = '' WHERE id IN (" + strings.Join(quoted, ", ") +
		") AND status = 'in_progress' AND (assignee IS NULL OR assignee = '')", nil
}

// Fix resets the affected beads (not held by the merge queue) to open (clears
// assignee) via direct SQL, then commits the repair to Dolt history.
func (c *NullAssigneeCheck) Fix(ctx *CheckContext) error {
	if len(c.affected) == 0 && len(c.landed) == 0 {
		return nil
	}

	var errs []string
	// Landed work: close through the shared settle (gt-pd1c.59).
	for _, row := range c.landed {
		if !beads.IsSafeID(row.ID) {
			errs = append(errs, fmt.Sprintf("%s: refusing bead ID %q: not a valid bead ID", row.RigDB, row.ID))
			continue
		}
		rigDir := filepath.Join(ctx.TownRoot, row.RigDB)
		if _, err := beads.New(rigDir).SettleHeldWorkBead(row.ID, beads.HoldLanded, row.MRID); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", row.RigDB, err))
		}
	}

	// Group affected bead IDs by database.
	affected := make(map[string][]string)
	for _, row := range c.affected {
		affected[row.RigDB] = append(affected[row.RigDB], row.ID)
	}

	for db, ids := range affected {
		rigDir := filepath.Join(ctx.TownRoot, db)

		// Reset beads via direct SQL (bypasses bd ORM which fails on NULL assignee).
		query, err := nullAssigneeFixQueryFor(ids)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", db, err))
			continue
		}
		if err := execBdSQLWrite(rigDir, query); err != nil {
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
