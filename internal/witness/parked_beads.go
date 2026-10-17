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

// errNoMailRouter: a notice that cannot be sent is an error, never a silent skip.
var errNoMailRouter = errors.New("no mail router")

// sendOverseerMail delivers a notice to the overseer. Tests override it.
var sendOverseerMail = func(router *mail.Router, msg *mail.Message) error {
	if router == nil {
		return errNoMailRouter
	}
	return router.Send(msg)
}

// markUnverifiedNotified records on the MR that the overseer was notified
// (beads.MRFields.UnverifiedNotifiedAt). Tests override it.
var markUnverifiedNotified = func(workDir, mrID string, at time.Time) error {
	return beads.New(workDir).MarkUnverifiedNotified(mrID, at)
}

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
// queue is read only when a parked bead exists.
func ScanParkedBeads(bd *BdCli, workDir, rigName string, router *mail.Router) *ScanParkedBeadsResult {
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

	notice := newOverseerNotice(workDir, rigName, router)
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
// (in_progress, no assignee): beads.HoldLanded closes it (its MR landed but
// was closed by hand, so nothing else closes it), beads.HoldUnverified mails
// the overseer when a notice is due, pending and free beads are left alone.
// An unreadable merge queue is reported, never acted on.
func settleParkedBead(bd *BdCli, workDir, beadID string, queue *beads.MergeQueue, notice overseerNotice) (closed, notified bool, err error) {
	hold, mrID, err := queue.Hold(beadID)
	if err != nil {
		return false, false, fmt.Errorf("merge queue unreadable, not settling %s: %w", beadID, err)
	}
	switch hold {
	case beads.HoldLanded:
		reason := fmt.Sprintf("Merged in %s (settled by witness)", mrID)
		if err := bd.Run(workDir, "close", beadID, "--force", "-r", reason); err != nil {
			return false, false, fmt.Errorf("closing parked bead %s (MR %s landed): %w", beadID, mrID, err)
		}
		return true, false, nil
	case beads.HoldUnverified:
		notified, err := notice.notify(queue, beadID)
		return false, notified, err
	default:
		return false, false, nil
	}
}

// overseerNotice mails the overseer about beads held as beads.HoldUnverified
// (gt-pd1c.82): only a human verdict releases them, so without a notice they
// sit parked until someone happens to run `gt doctor`.
type overseerNotice struct {
	workDir  string        // rig work dir (routes the MR mark to the MR's DB)
	rig      string        // witness rig: sender, and the rig shown when the MR records none
	router   *mail.Router  // nil: every notice fails (and nothing is marked)
	interval time.Duration // reminder interval (WitnessThresholds.UnverifiedReminderIntervalD)
	now      time.Time
}

func newOverseerNotice(workDir, rig string, router *mail.Router) overseerNotice {
	townRoot, err := workspace.Find(workDir)
	if err != nil || townRoot == "" {
		townRoot = workDir
	}
	return overseerNotice{
		workDir:  workDir,
		rig:      rig,
		router:   router,
		interval: config.LoadOperationalConfig(townRoot).GetWitnessConfig().UnverifiedReminderIntervalD(),
		now:      time.Now(),
	}
}

// notify mails the overseer about beadID when the merge queue holds it as
// unverified (beads.MergeQueue.Unverified, the one detection) and its
// deciding MR was never notified, or was last notified at least n.interval
// ago. The MR is marked only after the mail was sent: a failed send is
// retried on the next patrol. sent reports whether a mail went out.
func (n overseerNotice) notify(queue *beads.MergeQueue, beadID string) (sent bool, err error) {
	mrID, mrRig, reason := queue.Unverified(beadID)
	if mrID == "" {
		return false, nil
	}
	mr := mergeRequestByID(queue.MergeRequests(beadID), mrID)
	last, due := n.due(mr)
	if !due {
		return false, nil
	}
	if strings.TrimSpace(mrRig) == "" {
		mrRig = n.rig
	}
	msg := n.message(beadID, mrID, mrRig, reason, mr, last)
	if err := sendOverseerMail(n.router, msg); err != nil {
		return false, fmt.Errorf("mailing the overseer about unverified %s (MR %s): %w", beadID, mrID, err)
	}
	if err := markUnverifiedNotified(n.workDir, mrID, n.now); err != nil {
		return true, fmt.Errorf("overseer mailed about unverified %s, but MR %s was not marked (the next patrol repeats the notice): %w", beadID, mrID, err)
	}
	return true, nil
}

// due reports whether a notice is due for mr and when the last one went out
// (zero: never, or the mark is unreadable, which never silences the notice).
func (n overseerNotice) due(mr *beads.Issue) (last time.Time, due bool) {
	fields := beads.ParseMRFields(mr)
	if fields == nil || strings.TrimSpace(fields.UnverifiedNotifiedAt) == "" {
		return time.Time{}, true
	}
	last, err := time.Parse(time.RFC3339, strings.TrimSpace(fields.UnverifiedNotifiedAt))
	if err != nil {
		return time.Time{}, true
	}
	return last, n.now.Sub(last) >= n.interval
}

func (n overseerNotice) message(beadID, mrID, rig, reason string, mr *beads.Issue, last time.Time) *mail.Message {
	subject := fmt.Sprintf("UNVERIFIED_HOLD: %s (MR %s) needs an overseer verdict", beadID, mrID)
	lastLine := ""
	if !last.IsZero() {
		subject = "REMINDER " + subject
		lastLine = fmt.Sprintf("Last notice: %s\n", last.UTC().Format(time.RFC3339))
	}
	notLanded, landed := beads.UnverifiedExitCommands(rig, mrID)
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
		beadID, beadID, mrID, rig, reason, n.holdAge(mr), lastLine, notLanded, landed, shortDuration(n.interval))
	return &mail.Message{
		From:     fmt.Sprintf("%s/witness", n.rig),
		To:       overseerAddress,
		Subject:  subject,
		Body:     body,
		Priority: mail.PriorityHigh,
	}
}

// holdAge is how long the MR has been closed (the hold starts when the MR
// becomes terminal).
func (n overseerNotice) holdAge(mr *beads.Issue) string {
	if mr == nil || strings.TrimSpace(mr.ClosedAt) == "" {
		return "unknown (MR has no closed_at)"
	}
	closedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(mr.ClosedAt))
	if err != nil {
		return fmt.Sprintf("unknown (MR closed_at %q)", mr.ClosedAt)
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

func mergeRequestByID(mrs []*beads.Issue, id string) *beads.Issue {
	for _, mr := range mrs {
		if mr != nil && mr.ID == id {
			return mr
		}
	}
	return nil
}
