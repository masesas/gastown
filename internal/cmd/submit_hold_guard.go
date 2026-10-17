package cmd

import (
	"fmt"

	"github.com/steveyegge/gastown/internal/mergeproof"
)

// submitMergeQueue reads the town-wide merge queue; tests override it.
var submitMergeQueue = mergeproof.TownMergeQueue

// refuseHeldSubmission is the one submit guard of gt done and gt mq submit
// (gt-pd1c.59): an issue held by the merge queue through an MR from another
// branch belongs to the refinery, so no second MR (or no-MR close) is made for
// it. The owner's own branch may resubmit. An unreadable queue refuses.
func refuseHeldSubmission(townRoot, issueID, branch string) error {
	queue := submitMergeQueue(townRoot)
	if err := queue.CheckSubmission(issueID, branch); err != nil {
		if queue.Err() != nil {
			return fmt.Errorf("%w\nNothing was pushed and no merge request was created. Your commits are safe on the local branch %s; rerun the command once beads is reachable again", err, branch)
		}
		return fmt.Errorf("%w\nNothing was pushed and no merge request was created", err)
	}
	return nil
}
