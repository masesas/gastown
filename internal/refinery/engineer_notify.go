package refinery

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/mail"
	"github.com/steveyegge/gastown/internal/protocol"
	"github.com/steveyegge/gastown/internal/util"
)

// mailSender delivers protocol mail. *mail.Router in production; tests use
// a recorder. WaitPendingNotifications lets a CLI wait for the recipient
// notifications the router sends asynchronously.
type mailSender interface {
	Send(msg *mail.Message) error
	WaitPendingNotifications()
}

// errNoMailRouter: the engineer was built without a mail router.
var errNoMailRouter = errors.New("no mail router configured")

// maxProtocolErrorLen caps the error text carried in protocol mail and
// nudges. It is mostly branch-produced (gate stderr, offending paths).
const maxProtocolErrorLen = 500

// untrustedErrorLabel marks error text that comes from the branch (gate
// output, path names): agents reading FIX_NEEDED must not follow it as
// instructions.
const untrustedErrorLabel = "[untrusted branch output] "

// protocolErrorText is errMsg as protocol mail carries it: one line, control
// characters removed, URL credentials masked, capped, and labeled untrusted.
func protocolErrorText(errMsg string) string {
	return untrustedErrorLabel + util.OneLine(errMsg, maxProtocolErrorLen)
}

// mergeFailureType names a failed ProcessResult for FIX_NEEDED and the mayor
// nudge: conflict, scope, tests or build (anything else).
func mergeFailureType(result ProcessResult) string {
	switch {
	case result.Conflict:
		return "conflict"
	case result.ScopeViolation:
		return "scope"
	case result.TestsFailed:
		return "tests"
	default:
		return "build"
	}
}

// polecatNameFromWorker returns the polecat name of an MR worker field
// ("nux", "polecats/nux", "<rig>/polecats/nux"), or "" when the worker is not
// a plain name (beads.IsSafeID: ^[A-Za-z0-9][A-Za-z0-9._-]*$; unknown, crew,
// or anything else), so it never shapes a mail address or nudge target.
func polecatNameFromWorker(worker string) string {
	w := strings.TrimSpace(worker)
	if i := strings.LastIndex(w, "polecats/"); i >= 0 {
		w = w[i+len("polecats/"):]
	}
	if !beads.IsSafeID(w) {
		return ""
	}
	return w
}

// sendProtocolMail sends msg through the engineer's mail router.
func (e *Engineer) sendProtocolMail(msg *mail.Message) error {
	if e.router == nil {
		return errNoMailRouter
	}
	return e.router.Send(msg)
}

// fixNeededKey identifies one reported failure: the submitted commit and the
// failure type, so a different failure on the same commit is reported too.
func fixNeededKey(commit, failureType string) string { return commit + ":" + failureType }

// sendFixNeeded mails FIX_NEEDED for a failed merge to the MR's polecat, or
// to the rig witness when the worker is unknown (the witness relays), once
// per (commit_sha, failure type): the MR bead records fix_needed_for
// "<commit_sha>:<type>" after a successful send, and a retry of the same
// failure (every patrol cycle until the polecat pushes a fix) sends nothing.
// The record is written after the send, so delivery is at least once. The
// field is agent-writable: it only suppresses repeat mail, it gates nothing.
// sent=false: already reported.
func (e *Engineer) sendFixNeeded(mr *MRInfo, failureType, errMsg string) (sent bool, err error) {
	commit := strings.TrimSpace(mr.CommitSHA)
	key := fixNeededKey(commit, failureType)
	issue, fields := e.mrFieldsForNotice(mr)
	if fields != nil && commit != "" && strings.TrimSpace(fields.FixNeededFor) == key {
		return false, nil
	}
	polecat := polecatNameFromWorker(mr.Worker)
	msg := protocol.NewFixNeededMessage(e.rig.Name, polecat, mr.Branch, mr.SourceIssue, mr.Target,
		failureType, protocolErrorText(errMsg), mr.ID, mr.RetryCount+1)
	if polecat == "" {
		msg.To = e.rig.Name + "/witness"
	}
	if err := e.sendProtocolMail(msg); err != nil {
		return false, err
	}
	if fields == nil || commit == "" {
		return true, nil
	}
	fields.FixNeededFor = key
	desc := beads.SetMRFields(issue, fields)
	if err := e.beads.Update(mr.ID, beads.UpdateOptions{Description: &desc}); err != nil {
		return true, fmt.Errorf("FIX_NEEDED sent, but recording fix_needed_for on %s failed (the next failure mails again): %w", mr.ID, err)
	}
	return true, nil
}

// mrFieldsForNotice reads the MR bead for the FIX_NEEDED dedupe; nil fields
// when there is no bead (synthetic MR) or it cannot be read (send anyway).
func (e *Engineer) mrFieldsForNotice(mr *MRInfo) (*beads.Issue, *beads.MRFields) {
	if strings.TrimSpace(mr.ID) == "" || e.isSyntheticMergeMechanicsMR(mr) || e.beads == nil {
		return nil, nil
	}
	issue, err := e.beads.Show(mr.ID)
	if err != nil || issue == nil {
		return nil, nil
	}
	fields := beads.ParseMRFields(issue)
	if fields == nil {
		return nil, nil
	}
	return issue, fields
}

// sendMerged mails MERGED to the rig witness after a merged MR was closed,
// so it can close the polecat's cleanup wisp.
func (e *Engineer) sendMerged(mr *MRInfo, mergeCommit string) error {
	msg := protocol.NewMergedMessage(e.rig.Name, polecatNameFromWorker(mr.Worker), mr.Branch, mr.SourceIssue, mr.Target, mergeCommit)
	return e.sendProtocolMail(msg)
}

// nudgePolecatAboutConflict tells the MR's polecat that its branch conflicts;
// the conflict-resolution task carries the work.
func (e *Engineer) nudgePolecatAboutConflict(mr *MRInfo, errMsg string) {
	polecat := polecatNameFromWorker(mr.Worker)
	if polecat == "" {
		return
	}
	nudgeMsg := fmt.Sprintf("MERGE_FAILED: branch=%s issue=%s type=conflict error=%s — a conflict-resolution task was filed",
		mr.Branch, mr.SourceIssue, protocolErrorText(errMsg))
	nudgeCmd := exec.Command("gt", "nudge", e.rig.Name+"/"+polecat, nudgeMsg)
	util.SetDetachedProcessGroup(nudgeCmd)
	nudgeCmd.Dir = e.workDir
	if err := nudgeCmd.Run(); err != nil {
		_, _ = fmt.Fprintf(e.output, "[Engineer] Warning: failed to nudge %s about merge conflict: %v\n", polecat, err)
	} else {
		_, _ = fmt.Fprintf(e.output, "[Engineer] Nudged %s about merge conflict\n", polecat)
	}
}
