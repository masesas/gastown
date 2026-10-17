package beads

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/steveyegge/gastown/internal/util"
)

// MergeQueueHold is the merge queue's verdict on a work (source) bead. It is
// the one shared predicate for "may this bead be reset, reopened or dispatched
// again?", used by gt done, the refinery, the witness, the deacon, the convoy
// feeder and launch, polecat removal, unsling and gt doctor (gt-pd1c.59).
//
// Ownership: a source bead whose work was submitted as an MR is closed only by
// the refinery, when the MR merges (closeTerminalMR). Until then it is held.
type MergeQueueHold int

const (
	// HoldNone: no MR, or only MRs closed without landing (rejected,
	// superseded, conflict). The bead needs (re)work and may be dispatched.
	HoldNone MergeQueueHold = iota
	// HoldPending: an MR is open. The refinery owns the work until the MR
	// merges (the refinery then closes the bead) or is closed without landing
	// (the refinery then releases it).
	HoldPending
	// HoldLanded: an MR's work is on its target branch (MRWorkLanded). The
	// bead is finished work; it is closed, never reopened.
	HoldLanded
	// HoldUnverified: every MR is terminal, none provably landed, but at least
	// one could not be proven NOT landed either (no prover, no recorded
	// commit/target, git error). The bead is held (fail closed): parked,
	// neither closed nor reopened, until git can decide.
	HoldUnverified
)

// Held reports whether the bead must not be reset, reopened or dispatched.
func (h MergeQueueHold) Held() bool { return h != HoldNone }

func (h MergeQueueHold) String() string {
	switch h {
	case HoldPending:
		return "pending"
	case HoldLanded:
		return "landed"
	case HoldUnverified:
		return "unverified"
	default:
		return "none"
	}
}

// MergeQueueHoldOf classifies a bead from its merge requests (any status) and
// returns the deciding MR. Pending wins over landed: while an MR is open the
// refinery owns the bead (it closes it on merge, including the already-merged
// recheck), and the classification needs no git call. For terminal MRs: one
// that landed (close_reason/merge_commit, or git reachability for MRs closed
// by hand) makes it HoldLanded; otherwise any MR whose landing git could not
// decide makes it HoldUnverified. HoldNone needs every MR definitely not
// landed: only then may the bead be dispatched again.
func MergeQueueHoldOf(mrs []*Issue, resolve ProverResolver) (MergeQueueHold, string) {
	d := mergeQueueHoldDetail(mrs, resolve)
	return d.Hold, d.MRID
}

// HoldDetail is the merge queue's verdict on a bead with its deciding MR,
// computed once (MergeQueueHoldOf). Rig and Reason explain a HoldUnverified
// hold: the MR's rig and why git cannot decide whether its work landed
// (one line, at most maxHoldReasonLen bytes, URL credentials masked).
type HoldDetail struct {
	Hold   MergeQueueHold
	MRID   string // deciding MR ("" for HoldNone)
	MR     *Issue // deciding MR (nil for HoldNone)
	Rig    string // HoldUnverified only
	Reason string // HoldUnverified only
	// Evidence is what decided a HoldLanded verdict (recorded bead signal,
	// git, or a human verdict); empty for other holds (gt-pd1c.78).
	Evidence LandingEvidence
}

// mergeQueueHoldDetail is the one implementation of MergeQueueHoldOf.
func mergeQueueHoldDetail(mrs []*Issue, resolve ProverResolver) HoldDetail {
	for _, mr := range mrs {
		if mr != nil && !IssueStatus(strings.TrimSpace(mr.Status)).IsTerminal() {
			return HoldDetail{Hold: HoldPending, MRID: mr.ID, MR: mr}
		}
	}
	var unverified *HoldDetail
	for _, mr := range mrs {
		if mr == nil {
			continue
		}
		fields := ParseMRFields(mr)
		var prover MergeProver
		rig := ""
		if fields != nil {
			rig = strings.TrimSpace(fields.Rig)
			if resolve != nil {
				prover = resolve(rig)
			}
		}
		verdict, reason, evidence := mrLandingVerdictReason(IssueStatus(strings.TrimSpace(mr.Status)), fields, prover)
		switch verdict {
		case verdictLanded:
			return HoldDetail{Hold: HoldLanded, MRID: mr.ID, MR: mr, Evidence: evidence}
		case verdictUnknown:
			if unverified == nil {
				unverified = &HoldDetail{Hold: HoldUnverified, MRID: mr.ID, MR: mr, Rig: rig, Reason: holdReason(reason)}
			}
		}
	}
	if unverified != nil {
		return *unverified
	}
	return HoldDetail{Hold: HoldNone}
}

// maxHoldReasonLen bounds HoldDetail.Reason (it may carry git error text).
const maxHoldReasonLen = 300

// holdReason makes a landing reason safe to show to a human: one line, URL
// credentials masked, bounded length.
func holdReason(reason string) string {
	reason = strings.Join(strings.Fields(util.RedactURLCredentials(reason)), " ")
	if len(reason) > maxHoldReasonLen {
		cut := maxHoldReasonLen
		for cut > 0 && !utf8.RuneStart(reason[cut]) {
			cut--
		}
		reason = reason[:cut] + "…"
	}
	return reason
}

// MergeQueueIndex holds the merge requests of one or more beads DBs grouped
// by source issue (MRs live in the submitting polecat's rig DB, which need not
// be the source bead's DB).
type MergeQueueIndex map[string][]*Issue

// Hold classifies issueID (see MergeQueueHoldOf).
func (idx MergeQueueIndex) Hold(issueID string, resolve ProverResolver) (MergeQueueHold, string) {
	return MergeQueueHoldOf(idx[strings.TrimSpace(issueID)], resolve)
}

// TownMergeQueueIndex reads the merge requests of every beads DB in the town
// (TownBeadsSearchDirs), each DB once. Any unreadable DB is an error: callers
// hold work rather than risk dispatching it twice.
func TownMergeQueueIndex(townRoot string) (MergeQueueIndex, error) {
	dirs, err := TownBeadsSearchDirs(townRoot)
	if err != nil {
		return nil, err
	}
	idx := make(MergeQueueIndex)
	seen := make(map[string]bool)
	for _, dir := range dirs {
		beadsDir := ResolveBeadsDir(dir)
		if seen[beadsDir] {
			continue
		}
		seen[beadsDir] = true
		found, err := NewWithBeadsDir(dir, beadsDir).MergeRequestsBySource()
		if err != nil {
			return nil, fmt.Errorf("listing merge requests in %s: %w", beadsDir, err)
		}
		for source, mrs := range found {
			idx[source] = append(idx[source], mrs...)
		}
	}
	return idx, nil
}

// MergeQueue is the town's merge queue read once: every beads DB in the town
// (TownMergeQueueIndex), because an MR lives in the submitting polecat's rig
// DB while its source bead may live in another DB (hq-, another rig). It is
// the one scope in which beads are classified (gt-pd1c.59): every caller that
// asks "is this bead held?" holds a MergeQueue. A read error is kept and
// returned by every lookup, so callers fail closed.
type MergeQueue struct {
	index   MergeQueueIndex
	err     error
	resolve ProverResolver
}

// ReadMergeQueue reads the town's merge queue (mergeproof.TownMergeQueue adds
// the town's git provers).
func ReadMergeQueue(townRoot string, resolve ProverResolver) *MergeQueue {
	idx, err := TownMergeQueueIndex(townRoot)
	return NewMergeQueue(idx, err, resolve)
}

// NewMergeQueue wraps an already-read index (or the error reading it).
func NewMergeQueue(idx MergeQueueIndex, err error, resolve ProverResolver) *MergeQueue {
	return &MergeQueue{index: idx, err: err, resolve: resolve}
}

// Err is the error reading the queue, if any.
func (q *MergeQueue) Err() error {
	if q == nil {
		return errors.New("merge queue not read")
	}
	return q.err
}

// Hold classifies issueID (MergeQueueHoldOf) and returns the deciding MR. An
// unread queue is an error.
func (q *MergeQueue) Hold(issueID string) (MergeQueueHold, string, error) {
	d, err := q.Detail(issueID)
	return d.Hold, d.MRID, err
}

// Detail classifies issueID like Hold and also returns the deciding MR and,
// for HoldUnverified, its rig and why git cannot decide (one computation:
// callers that act on the hold and explain it use Detail once). An unread
// queue is an error.
func (q *MergeQueue) Detail(issueID string) (HoldDetail, error) {
	if err := q.Err(); err != nil {
		return HoldDetail{Hold: HoldNone}, err
	}
	return mergeQueueHoldDetail(q.index[strings.TrimSpace(issueID)], q.resolve), nil
}

// UnverifiedExitCommands are the only two ways out of a HoldUnverified hold,
// for the MR mrID in rig: the overseer's
// verdict that the work did not land (reopen for rework) or did land (close).
// Shared by every place that tells a human how to decide (gt-pd1c.82).
// Values that are not IsSafeID are never inserted: rig becomes "<rig>" and
// mrID "<mr-id>", so the copy-paste commands cannot carry injected text.
func UnverifiedExitCommands(rig, mrID string) (notLanded, landed string) {
	rig = strings.TrimSpace(rig)
	if !IsSafeID(rig) {
		rig = "<rig>"
	}
	mrID = strings.TrimSpace(mrID)
	if !IsSafeID(mrID) {
		mrID = "<mr-id>"
	}
	return fmt.Sprintf("gt mq reject %s %s --confirm-not-landed", rig, mrID),
		fmt.Sprintf("gt mq post-merge %s %s --confirm-landed", rig, mrID)
}

// MarkUnverifiedNotified records on MR mrID (routed to its own DB) that the
// overseer was told about its unverified hold at time at
// (MRFields.UnverifiedNotifiedAt, RFC3339 UTC). The MR is re-read first so a
// concurrent field change is not overwritten with a stale snapshot. Callers
// mark only after the notice was delivered (fail closed: an unsent notice is
// retried, never recorded).
func (b *Beads) MarkUnverifiedNotified(mrID string, at time.Time) error {
	issue, err := b.Show(mrID)
	if err != nil {
		return fmt.Errorf("reading MR %s: %w", mrID, err)
	}
	if issue == nil {
		return fmt.Errorf("reading MR %s: %w", mrID, ErrNotFound)
	}
	fields := ParseMRFields(issue)
	if fields == nil {
		fields = &MRFields{}
	}
	fields.UnverifiedNotifiedAt = at.UTC().Format(time.RFC3339)
	desc := SetMRFields(issue, fields)
	if err := b.Update(mrID, UpdateOptions{Description: &desc}); err != nil {
		return fmt.Errorf("recording unverified notice on MR %s: %w", mrID, err)
	}
	return nil
}

// MergeRequests returns the MRs (any status) whose source is issueID.
func (q *MergeQueue) MergeRequests(issueID string) []*Issue {
	if q == nil {
		return nil
	}
	return q.index[strings.TrimSpace(issueID)]
}

// CheckSubmission refuses a new MR for issueID while the merge queue holds it
// through an MR from a different branch: another worker (or a stale branch)
// must not submit work the refinery already owns. MRs from the same branch
// are the owner's own (idempotent retry, resubmit after FIX_NEEDED) and are
// allowed. Shared by gt done and gt mq submit; an unread queue refuses (fail
// closed).
func (q *MergeQueue) CheckSubmission(issueID, branch string) error {
	hold, mrID, err := q.Hold(issueID)
	if err != nil {
		return fmt.Errorf("cannot read the merge queue to check %s: %w", issueID, err)
	}
	if !hold.Held() {
		return nil
	}
	branch = strings.TrimSpace(branch)
	for _, mr := range q.MergeRequests(issueID) {
		pending := !IssueStatus(strings.TrimSpace(mr.Status)).IsTerminal()
		if !pending && mr.ID != mrID {
			continue
		}
		mrBranch := ""
		if fields := ParseMRFields(mr); fields != nil {
			mrBranch = strings.TrimSpace(fields.Branch)
		}
		if mrBranch != branch {
			if hold == HoldUnverified {
				notLanded, landed := UnverifiedExitCommands("", mr.ID)
				return fmt.Errorf("%s is held by the merge queue: MR %s from branch %q is closed but git cannot tell whether its work landed; an overseer must decide with `%s` or `%s`", issueID, mr.ID, mrBranch, notLanded, landed)
			}
			return fmt.Errorf("%s is held by the merge queue: MR %s (%s) from branch %q; the refinery owns it until the MR merges or is rejected", issueID, mr.ID, hold, mrBranch)
		}
	}
	return nil
}

// TownBeadsSearchDirs returns the town root plus every rig directory (and rig
// mayor clone) that has a .beads directory.
func TownBeadsSearchDirs(townRoot string) ([]string, error) {
	dirs := []string{townRoot}
	seen := map[string]bool{townRoot: true}
	entries, err := os.ReadDir(townRoot)
	if err != nil {
		return nil, fmt.Errorf("discovering beads search dirs: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || e.Name() == "mayor" || e.Name() == "settings" {
			continue
		}
		rigDir := filepath.Join(townRoot, e.Name())
		for _, dir := range []string{rigDir, filepath.Join(rigDir, "mayor", "rig")} {
			if _, err := os.Stat(filepath.Join(dir, ".beads")); err == nil && !seen[dir] {
				dirs = append(dirs, dir)
				seen[dir] = true
			}
		}
	}
	return dirs, nil
}

// ParkWorkBead marks a source bead as held by the merge queue: status
// in_progress (not in `bd ready`) with no assignee (a reused polecat name
// never inherits it). The refinery closes it when the MR merges or releases it
// when the MR is closed without landing. A bead that is already terminal (the
// refinery may have just closed it) is left alone and parked is false; bd has
// no conditional update, so the re-read narrows but cannot close that window.
func (b *Beads) ParkWorkBead(issueID string) (parked bool, err error) {
	if terminal, err := b.workBeadTerminal(issueID); err != nil || terminal {
		return false, err
	}
	status := string(StatusInProgress)
	empty := ""
	if err := b.Update(issueID, UpdateOptions{Status: &status, Assignee: &empty}); err != nil {
		return false, fmt.Errorf("parking work bead %s for the merge queue: %w", issueID, err)
	}
	return true, nil
}

// workBeadTerminal re-reads issueID (routed to its own DB) just before a
// release writes it.
func (b *Beads) workBeadTerminal(issueID string) (bool, error) {
	issue, err := b.Show(issueID)
	if err != nil {
		return false, fmt.Errorf("reading work bead %s: %w", issueID, err)
	}
	if issue == nil {
		return false, fmt.Errorf("reading work bead %s: %w", issueID, ErrNotFound)
	}
	return IssueStatus(strings.TrimSpace(issue.Status)).IsTerminal(), nil
}

// ReleaseOutcome is what ReleaseWorkBead / SettleHeldWorkBead did.
type ReleaseOutcome string

const (
	// ReleaseClosed: the bead's MR landed; the bead was closed.
	ReleaseClosed ReleaseOutcome = "closed"
	// ReleaseParked: the bead's MR is pending (or the queue was unreadable);
	// the bead was parked (ParkWorkBead).
	ReleaseParked ReleaseOutcome = "parked"
	// ReleaseReopened: no MR holds the bead; it is open with no assignee.
	ReleaseReopened ReleaseOutcome = "reopened"
	// ReleaseUnchanged: the bead was already terminal and was not touched.
	ReleaseUnchanged ReleaseOutcome = "unchanged"
)

// ErrLandingUnverified is returned (as a warning, with ReleaseParked) when no
// MR of a released bead is pending or landed but git could not prove that its
// work did not land: the bead is parked instead of reopened.
var ErrLandingUnverified = errors.New("cannot verify that the MR's work did not land")

// errNotHeld is returned by SettleHeldWorkBead for HoldNone.
var errNotHeld = errors.New("work bead is not held by the merge queue")

// SettleHeldWorkBead applies the merge queue's verdict to a held bead whose
// worker is gone: HoldLanded closes it ("Merged in <mr>", --force because an
// attached molecule's open steps would block a plain close) unless it is
// already terminal, HoldPending and HoldUnverified park it. HoldNone is an error and touches nothing: the caller decides how to
// release work that is not held.
func (b *Beads) SettleHeldWorkBead(issueID string, hold MergeQueueHold, mrID string) (ReleaseOutcome, error) {
	switch hold {
	case HoldLanded:
		if terminal, err := b.workBeadTerminal(issueID); err != nil || terminal {
			if err != nil {
				return "", err
			}
			return ReleaseUnchanged, nil
		}
		if err := b.ForceCloseWithReason("Merged in "+mrID, issueID); err != nil {
			return "", fmt.Errorf("closing merged work bead %s: %w", issueID, err)
		}
		return ReleaseClosed, nil
	case HoldPending, HoldUnverified:
		parked, err := b.ParkWorkBead(issueID)
		if err != nil {
			return "", err
		}
		if !parked {
			return ReleaseUnchanged, nil
		}
		return ReleaseParked, nil
	default:
		return "", fmt.Errorf("%s: %w", issueID, errNotHeld)
	}
}

// ReleaseWorkBead hands a work bead back after its worker went away (polecat
// removal, unsling, stale hook cleanup, an MR closed without landing). One
// rule, from the town's merge queue (q): landed work is closed, work with a
// pending MR is parked, anything else returns to open with no assignee. If the
// queue could not be read the bead is parked (fail closed: the worker is
// released but the bead is not made dispatchable) and the read error is
// returned alongside. HoldUnverified also parks, with ErrLandingUnverified as
// a warning. A bead that is already terminal is left alone.
func (b *Beads) ReleaseWorkBead(issueID string, q *MergeQueue) (ReleaseOutcome, error) {
	hold, mrID, lookupErr := q.Hold(issueID)
	if lookupErr != nil {
		parked, err := b.ParkWorkBead(issueID)
		if err != nil {
			return "", errors.Join(lookupErr, err)
		}
		if !parked {
			return ReleaseUnchanged, lookupErr
		}
		return ReleaseParked, lookupErr
	}
	if hold.Held() {
		outcome, err := b.SettleHeldWorkBead(issueID, hold, mrID)
		if err == nil && hold == HoldUnverified {
			err = fmt.Errorf("%s: %w (MR %s): parked, not reopened", issueID, ErrLandingUnverified, mrID)
		}
		return outcome, err
	}
	if terminal, err := b.workBeadTerminal(issueID); err != nil || terminal {
		if err != nil {
			return "", err
		}
		return ReleaseUnchanged, nil
	}
	openStatus := string(StatusOpen)
	empty := ""
	if err := b.Update(issueID, UpdateOptions{Status: &openStatus, Assignee: &empty}); err != nil {
		return "", fmt.Errorf("reopening work bead %s: %w", issueID, err)
	}
	return ReleaseReopened, nil
}
