package daemon

import "sync"

// WorkAssignment is the answer to "does this agent have open assigned work?".
// The zero value is WorkUnknown so an unset answer never permits a kill.
type WorkAssignment int

const (
	// WorkUnknown means the lookup failed; destructive actions must be skipped.
	WorkUnknown WorkAssignment = iota
	// WorkNo means every lookup succeeded and found no open assigned work.
	WorkNo
	// WorkYes means at least one open work bead is assigned.
	WorkYes
)

func (w WorkAssignment) String() string {
	switch w {
	case WorkNo:
		return "no"
	case WorkYes:
		return "yes"
	default:
		return "unknown"
	}
}

// unknownEscalationCycles is how many consecutive reaper cycles may report
// unknown assigned work for a session before the witness is notified.
const unknownEscalationCycles = 3

// unknownWorkTracker counts consecutive unknown lookups per session and
// remembers whether the current episode was already escalated.
type unknownWorkTracker struct {
	mu        sync.Mutex
	counts    map[string]int
	escalated map[string]bool
}

// record notes one more unknown cycle for session and reports whether this
// cycle should escalate (exactly once per episode).
func (u *unknownWorkTracker) record(session string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.counts == nil {
		u.counts = make(map[string]int)
		u.escalated = make(map[string]bool)
	}
	u.counts[session]++
	if u.counts[session] >= unknownEscalationCycles && !u.escalated[session] {
		u.escalated[session] = true
		return true
	}
	return false
}

// forget ends the unknown episode for session (definite answer or session gone).
func (u *unknownWorkTracker) forget(session string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	delete(u.counts, session)
	delete(u.escalated, session)
}
