package deacon

import (
	"fmt"

	"github.com/steveyegge/gastown/internal/mergeproof"
)

// mergeQueueFn reads the town-wide merge queue (MRs live in the submitting
// polecat's rig DB). Tests override it.
var mergeQueueFn = mergeproof.TownMergeQueue

// slingBeadFn dispatches a bead via gt sling. Tests override it.
var slingBeadFn = slingBead

// redispatchMergeQueueHold reports why the merge queue forbids re-dispatching
// beadID (the shared beads.MergeQueueHold predicate, gt-pd1c.59), or "" when
// it may be dispatched. An unreadable merge queue forbids it (fail closed).
func redispatchMergeQueueHold(townRoot, beadID string) string {
	hold, mrID, err := mergeQueueFn(townRoot).Hold(beadID)
	if err != nil {
		return fmt.Sprintf("cannot read merge queue, not re-dispatching: %v", err)
	}
	if !hold.Held() {
		return ""
	}
	return fmt.Sprintf("held by merge queue (MR %s %s)", mrID, hold)
}
