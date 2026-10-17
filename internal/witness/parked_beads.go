package witness

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/mail"
	"github.com/steveyegge/gastown/internal/workspace"
)

// overseerAddress is the human operator's mailbox (`gt mail send --human`).
const overseerAddress = "overseer"

// unverifiedMarkSkew is how far in the future an unverified_notified_at mark
// may lie (clock skew between hosts) and still count. A mark further ahead is
// invalid, so it can never silence the overseer.
const unverifiedMarkSkew = 5 * time.Minute

// errNoMailRouter: a notice that cannot be sent is an error, never a silent skip.
var errNoMailRouter = errors.New("no mail router")

// ScanParkedBeadsResult is what ScanParkedBeads did.
type ScanParkedBeadsResult struct {
	Checked  int      // parked beads (in_progress, no assignee) examined
	Closed   []string // parked beads closed because their MR landed
	Notified []string // unverified beads the overseer was mailed about
	Errors   []error
}

// ScanParkedBeads applies the merge queue's verdict to every bead parked in
// the rig DB at workDir (in_progress with no assignee, beads.ParkWorkBead):
// landed work is closed, a bead held as beads.HoldUnverified is announced to
// the overseer (once per MR, then a reminder per
// WitnessThresholds.UnverifiedReminderInterval), pending and free beads are
// left alone. It runs on every witness patrol (`gt patrol scan`); the merge
// queue is read only when a parked bead exists. rigName must be
// beads.IsSafeID.
func ScanParkedBeads(bd *BdCli, workDir, rigName string, router *mail.Router) *ScanParkedBeadsResult {
	if !beads.IsSafeID(rigName) {
		return &ScanParkedBeadsResult{Errors: []error{fmt.Errorf("refusing rig name %q: not a valid rig name", rigName)}}
	}
	return scanParkedBeads(bd, workDir, newOverseerNotice(workDir, rigName, router))
}

func scanParkedBeads(bd *BdCli, workDir string, notice overseerNotice) *ScanParkedBeadsResult {
	result := &ScanParkedBeadsResult{}
	output, err := bd.Exec(workDir, "list", "--status=in_progress", "--json", "--limit=0")
	if err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("listing in_progress beads: %w", err))
		return result
	}
	var listed []struct {
		ID       string `json:"id"`
		Assignee string `json:"assignee"`
	}
	if strings.TrimSpace(output) != "" {
		if err := json.Unmarshal([]byte(output), &listed); err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("parsing in_progress beads: %w", err))
			return result
		}
	}

	var queue *beads.MergeQueue // read once, for the first parked bead
	for _, bead := range listed {
		if strings.TrimSpace(bead.Assignee) != "" {
			continue
		}
		result.Checked++
		if queue == nil {
			queue = witnessMergeQueue(workDir)
		}
		closed, notified, err := settleParkedBead(bd, workDir, bead.ID, queue, notice)
		if closed {
			result.Closed = append(result.Closed, bead.ID)
		}
		if notified {
			result.Notified = append(result.Notified, bead.ID)
		}
		if err != nil {
			result.Errors = append(result.Errors, err)
		}
	}
	return result
}

// settleParkedBead applies the merge queue's verdict to one parked bead
// (in_progress, no assignee), computed once (beads.MergeQueue.Detail):
// beads.HoldLanded closes it (its MR landed but was closed by hand, so
// nothing else closes it), beads.HoldUnverified mails the overseer when a
// notice is due, pending and free beads are left alone. An unreadable merge
// queue is reported, never acted on.
func settleParkedBead(bd *BdCli, workDir, beadID string, queue *beads.MergeQueue, notice overseerNotice) (closed, notified bool, err error) {
	d, err := queue.Detail(beadID)
	if err != nil {
		return false, false, fmt.Errorf("merge queue unreadable, not settling %s: %w", beadID, err)
	}
	switch d.Hold {
	case beads.HoldLanded:
		reason := fmt.Sprintf("Merged in %s (settled by witness)", d.MRID)
		if err := bd.Run(workDir, "close", beadID, "--force", "-r", reason); err != nil {
			return false, false, fmt.Errorf("closing parked bead %s (MR %s landed): %w", beadID, d.MRID, err)
		}
		return true, false, nil
	case beads.HoldUnverified:
		notified, err := notice.notify(beadID, d)
		return false, notified, err
	default:
		return false, false, nil
	}
}

// overseerNotice mails the overseer about beads held as beads.HoldUnverified
// (gt-pd1c.82): only a human verdict releases them, so without a notice they
// sit parked until someone happens to run `gt doctor`.
//
// Delivery is at least once: the MR is marked only after a successful send,
// so a mark that fails (or two patrols notifying the same MR at the same
// time) repeats the notice; it is never lost.
type overseerNotice struct {
	rig      string        // witness rig: sender, and the rig shown when the MR's is unusable
	interval time.Duration // reminder interval (WitnessThresholds.UnverifiedReminderIntervalD)
	now      time.Time
	send     func(*mail.Message) error             // delivers the notice
	mark     func(mrID string, at time.Time) error // records beads.MRFields.UnverifiedNotifiedAt
}

// newOverseerNotice wires the notice to router (nil: every send fails and
// nothing is marked) and to the MR's own DB, routed from workDir.
func newOverseerNotice(workDir, rig string, router *mail.Router) overseerNotice {
	townRoot, err := workspace.Find(workDir)
	if err != nil || townRoot == "" {
		townRoot = workDir
	}
	return overseerNotice{
		rig:      rig,
		interval: config.LoadOperationalConfig(townRoot).GetWitnessConfig().UnverifiedReminderIntervalD(),
		now:      time.Now(),
		send: func(msg *mail.Message) error {
			if router == nil {
				return errNoMailRouter
			}
			return router.Send(msg)
		},
		mark: func(mrID string, at time.Time) error {
			return beads.New(workDir).MarkUnverifiedNotified(mrID, at)
		},
	}
}

// notify mails the overseer about beadID, held as unverified per d (the
// merge queue's one verdict), when its deciding MR was never notified or was
// last notified at least n.interval ago. The MR is marked only after the
// mail was sent: a failed send is retried on the next patrol. sent reports
// whether a mail went out.
func (n overseerNotice) notify(beadID string, d beads.HoldDetail) (sent bool, err error) {
	if d.Hold != beads.HoldUnverified || d.MRID == "" {
		return false, nil
	}
	last, due := n.due(d.MR)
	if !due {
		return false, nil
	}
	if err := n.send(n.message(beadID, d, last)); err != nil {
		return false, fmt.Errorf("mailing the overseer about unverified %s (MR %s): %w", beadID, d.MRID, err)
	}
	if err := n.mark(d.MRID, n.now); err != nil {
		return true, fmt.Errorf("overseer mailed about unverified %s, but MR %s was not marked (the next patrol repeats the notice): %w", beadID, d.MRID, err)
	}
	return true, nil
}

// due reports whether a notice is due for mr and when the last one went out.
// last is zero when there was none or the mark is invalid (unparsable, or
// more than unverifiedMarkSkew in the future): an invalid mark never
// silences the notice.
func (n overseerNotice) due(mr *beads.Issue) (last time.Time, due bool) {
	fields := beads.ParseMRFields(mr)
	if fields == nil || strings.TrimSpace(fields.UnverifiedNotifiedAt) == "" {
		return time.Time{}, true
	}
	last, err := time.Parse(time.RFC3339, strings.TrimSpace(fields.UnverifiedNotifiedAt))
	if err != nil || last.After(n.now.Add(unverifiedMarkSkew)) {
		return time.Time{}, true
	}
	return last, n.now.Sub(last) >= n.interval
}

func (n overseerNotice) message(beadID string, d beads.HoldDetail, last time.Time) *mail.Message {
	rig := d.Rig
	if !beads.IsSafeID(rig) {
		rig = n.rig // the witness rig, or a placeholder when that is unusable too
	}
	bead, mr := displayID(beadID), displayID(d.MRID)
	subject := fmt.Sprintf("UNVERIFIED_HOLD: %s (MR %s) needs an overseer verdict", bead, mr)
	lastLine := ""
	if !last.IsZero() {
		subject = "REMINDER " + subject
		lastLine = fmt.Sprintf("Last notice: %s\n", last.UTC().Format(time.RFC3339))
	}
	notLanded, landed := beads.UnverifiedExitCommands(rig, d.MRID)
	body := fmt.Sprintf(`Work bead %s is held by the merge queue as unverified: its MR is closed,
but git cannot prove whether the work landed. It stays parked (not in bd ready)
and nothing reopens or closes it until an overseer decides.

Bead:   %s
MR:     %s
Rig:    %s
Reason: %s
Age:    %s
%s
Decide from an interactive terminal (the MR ID must be typed again):
  Work did NOT land (reopen the bead for rework):
    %s
  Work landed (close the bead):
    %s

A reminder follows every %s while the hold lasts.`,
		bead, bead, mr, displayID(rig), d.Reason, n.holdAge(d.MR), lastLine, notLanded, landed, shortDuration(n.interval))
	return &mail.Message{
		From:     fmt.Sprintf("%s/witness", n.rig),
		To:       overseerAddress,
		Subject:  subject,
		Body:     body,
		Priority: mail.PriorityHigh,
	}
}

// displayID shows an identifier in the notice only when it is beads.IsSafeID.
func displayID(id string) string {
	if beads.IsSafeID(id) {
		return id
	}
	return "<invalid id>"
}

// holdAge is how long the MR has been closed (the hold starts when the MR
// becomes terminal).
func (n overseerNotice) holdAge(mr *beads.Issue) string {
	if mr == nil || strings.TrimSpace(mr.ClosedAt) == "" {
		return "unknown (MR has no closed_at)"
	}
	closedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(mr.ClosedAt))
	if err != nil {
		return "unknown (MR closed_at is not a valid time)"
	}
	return fmt.Sprintf("%s (MR closed %s)", shortDuration(n.now.Sub(closedAt)), closedAt.UTC().Format(time.RFC3339))
}

// shortDuration renders d as whole hours ("26h"), or minutes below an hour.
func shortDuration(d time.Duration) string {
	if d < 0 {
		d = 0 // clock skew: never a negative age
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}
