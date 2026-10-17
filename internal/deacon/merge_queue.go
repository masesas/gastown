package deacon

import (
	"fmt"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/mergeproof"
)

// mergeQueueIndexFn reads every rig's merge requests (MRs live in the
// submitting polecat's rig DB). Tests override it.
var mergeQueueIndexFn = beads.TownMergeQueueIndex

// slingBeadFn dispatches a bead via gt sling. Tests override it.
var slingBeadFn = slingBead

// redispatchMergeQueueHold reports why the merge queue forbids re-dispatching
// beadID (the shared beads.MergeQueueHold predicate, gt-pd1c.59), or "" when
// it may be dispatched. An unreadable merge queue forbids it (fail closed).
func redispatchMergeQueueHold(townRoot, beadID string) string {
	idx, err := mergeQueueIndexFn(townRoot)
	if err != nil {
		return fmt.Sprintf("cannot read merge queue, not re-dispatching: %v", err)
	}
	hold, mrID := idx.Hold(beadID, mergeproof.Resolver(townRoot))
	if !hold.Held() {
		return ""
	}
	return fmt.Sprintf("held by merge queue (MR %s %s)", mrID, hold)
}
