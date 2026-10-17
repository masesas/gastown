package beads

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
)

// Held reports whether the bead must not be reset, reopened or dispatched.
func (h MergeQueueHold) Held() bool { return h != HoldNone }

func (h MergeQueueHold) String() string {
	switch h {
	case HoldPending:
		return "pending"
	case HoldLanded:
		return "landed"
	default:
		return "none"
	}
}

// MergeQueueHoldOf classifies a bead from its merge requests (any status) and
// returns the deciding MR. Pending wins over landed: while an MR is open the
// refinery owns the bead (it closes it on merge, including the already-merged
// recheck), and the classification needs no git call. Landing is proven only
// for beads whose MRs are all terminal (MRWorkLanded: close_reason/merge_commit
// or git reachability, for MRs closed by hand).
func MergeQueueHoldOf(mrs []*Issue, resolve ProverResolver) (MergeQueueHold, string) {
	for _, mr := range mrs {
		if mr != nil && !IssueStatus(strings.TrimSpace(mr.Status)).IsTerminal() {
			return HoldPending, mr.ID
		}
	}
	if mrID := LandedMergeRequest(mrs, resolve); mrID != "" {
		return HoldLanded, mrID
	}
	return HoldNone, ""
}

// MergeQueueHoldFor classifies issueID from the merge requests of this beads
// DB. A query error is returned so callers can fail closed.
func (b *Beads) MergeQueueHoldFor(issueID string, resolve ProverResolver) (MergeQueueHold, string, error) {
	bySource, err := b.MergeRequestsBySource()
	if err != nil {
		return HoldNone, "", err
	}
	hold, mrID := MergeQueueHoldOf(bySource[strings.TrimSpace(issueID)], resolve)
	return hold, mrID, nil
}

// MergeQueueIndex holds the merge requests of one or more beads DBs grouped
// by source issue (MRs live in the submitting polecat's rig DB, which need not
// be the source bead's DB).
type MergeQueueIndex map[string][]*Issue

// Hold classifies issueID (see MergeQueueHoldOf).
func (idx MergeQueueIndex) Hold(issueID string, resolve ProverResolver) (MergeQueueHold, string) {
	return MergeQueueHoldOf(idx[strings.TrimSpace(issueID)], resolve)
}

// Held reports whether issueID is held.
func (idx MergeQueueIndex) Held(issueID string, resolve ProverResolver) bool {
	hold, _ := idx.Hold(issueID, resolve)
	return hold.Held()
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
// when the MR is closed without landing.
func (b *Beads) ParkWorkBead(issueID string) error {
	status := string(StatusInProgress)
	empty := ""
	if err := b.Update(issueID, UpdateOptions{Status: &status, Assignee: &empty}); err != nil {
		return fmt.Errorf("parking work bead %s for the merge queue: %w", issueID, err)
	}
	return nil
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
)

// errNotHeld is returned by SettleHeldWorkBead for HoldNone.
var errNotHeld = errors.New("work bead is not held by the merge queue")

// SettleHeldWorkBead applies the merge queue's verdict to a held bead whose
// worker is gone: HoldLanded closes it ("Merged in <mr>", --force because an
// attached molecule's open steps would block a plain close), HoldPending parks
// it. HoldNone is an error and touches nothing: the caller decides how to
// release work that is not held.
func (b *Beads) SettleHeldWorkBead(issueID string, hold MergeQueueHold, mrID string) (ReleaseOutcome, error) {
	switch hold {
	case HoldLanded:
		if err := b.ForceCloseWithReason("Merged in "+mrID, issueID); err != nil {
			return "", fmt.Errorf("closing merged work bead %s: %w", issueID, err)
		}
		return ReleaseClosed, nil
	case HoldPending:
		if err := b.ParkWorkBead(issueID); err != nil {
			return "", err
		}
		return ReleaseParked, nil
	default:
		return "", fmt.Errorf("%s: %w", issueID, errNotHeld)
	}
}

// ReleaseWorkBead hands a work bead back after its worker went away (polecat
// removal, unsling, stale hook cleanup, an MR closed without landing). One
// rule, from this DB's merge queue (MergeQueueHoldFor): landed work is closed,
// work with a pending MR is parked, anything else returns to open with no
// assignee. If the merge queue cannot be read the bead is parked (fail closed:
// the worker is released but the bead is not made dispatchable) and the
// lookup error is returned alongside; gt doctor (null-assignee-steps) reopens
// it later if no MR holds it.
func (b *Beads) ReleaseWorkBead(issueID string, resolve ProverResolver) (ReleaseOutcome, error) {
	hold, mrID, lookupErr := b.MergeQueueHoldFor(issueID, resolve)
	if lookupErr != nil {
		if err := b.ParkWorkBead(issueID); err != nil {
			return "", errors.Join(lookupErr, err)
		}
		return ReleaseParked, lookupErr
	}
	if hold.Held() {
		return b.SettleHeldWorkBead(issueID, hold, mrID)
	}
	openStatus := string(StatusOpen)
	empty := ""
	if err := b.Update(issueID, UpdateOptions{Status: &openStatus, Assignee: &empty}); err != nil {
		return "", err
	}
	return ReleaseReopened, nil
}
