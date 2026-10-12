package cmd

import (
	"fmt"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/events"
)

// verifyDoneOwnership refuses gt done when the issue is assigned to another
// agent or its ownership cannot be read. It runs before any done-intent
// label, push, or merge request so a stale session (e.g. after a force
// re-sling) cannot submit work for a bead it no longer owns.
func verifyDoneOwnership(cwd, issueID, sender string) error {
	bd, _, _ := routedIssueBeads(cwd, issueID)
	issue, err := bd.Show(issueID)
	if ownErr := checkDoneOwnership(issueID, issue, err, sender); ownErr != nil {
		_ = events.LogFeed(events.TypeStateUnknown, sender,
			events.StateUnknownPayload(issueID, "done_ownership", ownErr.Error(), "gt done"))
		return ownErr
	}
	return nil
}

// checkDoneOwnership decides whether sender may complete issueID.
// An unassigned issue is allowed (no other owner is known); an issue assigned
// to someone else, or one that cannot be read, is refused.
func checkDoneOwnership(issueID string, issue *beads.Issue, lookupErr error, sender string) error {
	if lookupErr != nil {
		return fmt.Errorf("cannot verify ownership of %s: %w\nNothing was pushed and no merge request was created; retry gt done when beads is reachable", issueID, lookupErr)
	}
	if issue == nil {
		return fmt.Errorf("cannot verify ownership of %s: issue not found\nNothing was pushed and no merge request was created", issueID)
	}
	if issue.Assignee != "" && issue.Assignee != sender {
		return fmt.Errorf("%s is assigned to %s, not %s (re-slung to another agent?)\nNothing was pushed and no merge request was created", issueID, issue.Assignee, sender)
	}
	return nil
}

// mergeAssignedLookups combines the hooked and in_progress assignment
// queries. Work found by either query wins; otherwise a failed query makes
// the result an error rather than an empty "no assignment".
func mergeAssignedLookups(hooked []*beads.Issue, hookedErr error, inProgress []*beads.Issue, inProgressErr error) ([]*beads.Issue, error) {
	if hookedErr == nil && len(hooked) > 0 {
		return hooked, nil
	}
	if inProgressErr == nil && len(inProgress) > 0 {
		return inProgress, nil
	}
	if hookedErr != nil {
		return nil, fmt.Errorf("listing hooked assignments: %w", hookedErr)
	}
	if inProgressErr != nil {
		return nil, fmt.Errorf("listing in_progress assignments: %w", inProgressErr)
	}
	return inProgress, nil
}
