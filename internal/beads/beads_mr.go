// Package beads provides merge request and gate utilities.
package beads

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// FindMRForBranch searches for an open merge-request bead for the given branch.
// Returns the MR bead if found, nil if not found.
// This enables idempotent `gt done` - if an MR already exists, we skip creation.
func (b *Beads) FindMRForBranch(branch string) (*Issue, error) {
	return b.findMRForBranch(branch, true)
}

// FindMRForBranchAny searches for a merge-request bead for the given branch
// across all statuses (open and closed). Used by recovery checks to determine
// if work was ever submitted to the merge queue. See #1035.
func (b *Beads) FindMRForBranchAny(branch string) (*Issue, error) {
	return b.findMRForBranch(branch, false)
}

// FindMRForBranchAndSHA searches for an open merge-request bead matching both
// the branch name AND the commit SHA. This is the correct dedup key: two MRs
// from the same branch but with different commit SHAs are distinct submissions
// (e.g., polecat fixed a gate failure and re-pushed). See GH#3032.
//
// Returns nil if no MR matches both branch and SHA. Callers should create a
// new MR in that case and supersede old MRs for the same source issue.
func (b *Beads) FindMRForBranchAndSHA(branch, commitSHA string) (*Issue, error) {
	issues, err := b.ListMergeRequests(ListOptions{
		Status: "all",
		Label:  "gt:merge-request",
	})
	if err != nil {
		return nil, err
	}

	branchPrefix := "branch: " + branch + "\n"
	for _, issue := range issues {
		if issue.Status == "closed" {
			continue
		}
		if !strings.HasPrefix(issue.Description, branchPrefix) {
			continue
		}
		// Branch matches — check commit SHA.
		// If the MR has no commit_sha field (legacy), fall back to branch-only
		// match for backward compatibility.
		fields := ParseMRFields(issue)
		if fields != nil && fields.CommitSHA != "" && commitSHA != "" {
			if fields.CommitSHA != commitSHA {
				// Same branch but different SHA — this is a stale MR.
				// Don't return it; caller will create a new MR and supersede.
				continue
			}
		}
		return issue, nil
	}

	return nil, nil
}

// findMRForBranch searches the wisps table (Dolt) for a merge-request
// bead matching the given branch.
// Uses status=all which includes all issue statuses with full descriptions.
// Ephemeral=true routes to the wisps table where MR beads live (GH#2446).
// When skipClosed is true, closed beads are excluded (for open-MR checks).
func (b *Beads) findMRForBranch(branch string, skipClosed bool) (*Issue, error) {
	branchPrefix := "branch: " + branch + "\n"

	issues, err := b.ListMergeRequests(ListOptions{
		Status: "all",
		Label:  "gt:merge-request",
	})
	if err != nil {
		return nil, err
	}
	for _, issue := range issues {
		if skipClosed && issue.Status == "closed" {
			continue
		}
		if strings.HasPrefix(issue.Description, branchPrefix) {
			return issue, nil
		}
	}

	return nil, nil
}

// ListPendingMergeRequests returns merge-request wisps that are not terminal:
// submitted by gt done and not yet merged or rejected by the refinery. It uses
// the same wisps-table SQL as ListMergeRequests, but neither hydrates details
// nor swallows query errors, so callers deciding whether work may be
// dispatched again can fail closed when the queue cannot be read.
func (b *Beads) ListPendingMergeRequests() ([]*Issue, error) {
	out, err := b.run("sql", "--json",
		"SELECT w.id, w.status, w.description FROM wisps w "+
			"JOIN wisp_labels l ON w.id = l.issue_id "+
			"WHERE l.label = 'gt:merge-request' AND w.status NOT IN ('closed', 'tombstone')")
	if err != nil {
		return nil, fmt.Errorf("querying pending merge requests: %w", err)
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return nil, nil
	}
	var rows []*Issue
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("parsing pending merge requests: %w", err)
	}
	var pending []*Issue
	for _, row := range rows {
		if !IssueStatus(row.Status).IsTerminal() {
			pending = append(pending, row)
		}
	}
	return pending, nil
}

// ListMergedMergeRequests returns merge-request wisps the refinery closed as
// merged (close_reason: merged). Like ListPendingMergeRequests it surfaces query
// errors instead of swallowing them, so callers deciding whether work may be
// dispatched or reopened can fail closed when the queue cannot be read.
func (b *Beads) ListMergedMergeRequests() ([]*Issue, error) {
	out, err := b.run("sql", "--json",
		"SELECT w.id, w.status, w.description FROM wisps w "+
			"JOIN wisp_labels l ON w.id = l.issue_id "+
			"WHERE l.label = 'gt:merge-request' AND w.status = 'closed' "+
			"AND w.description LIKE '%merged%'")
	if err != nil {
		return nil, fmt.Errorf("querying merged merge requests: %w", err)
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return nil, nil
	}
	var rows []*Issue
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("parsing merged merge requests: %w", err)
	}
	var merged []*Issue
	for _, row := range rows {
		if IsMergedMergeRequest(row) {
			merged = append(merged, row)
		}
	}
	return merged, nil
}

// IsMergedMergeRequest reports whether an MR bead is terminal with
// close_reason=merged, i.e. its work landed on the target branch.
func IsMergedMergeRequest(mr *Issue) bool {
	if mr == nil || !IssueStatus(strings.TrimSpace(mr.Status)).IsTerminal() {
		return false
	}
	fields := ParseMRFields(mr)
	return fields != nil && strings.EqualFold(strings.TrimSpace(fields.CloseReason), "merged")
}

// MergedSourceIssues maps source issue ID -> merged MR ID for the given MR
// beads. It is the one definition of "this bead's work already landed via a
// merge request", shared by the convoy feeder, convoy launch and the
// release/unhook paths.
func MergedSourceIssues(mrs []*Issue) map[string]string {
	merged := make(map[string]string)
	for _, mr := range mrs {
		if !IsMergedMergeRequest(mr) {
			continue
		}
		if fields := ParseMRFields(mr); fields != nil && fields.SourceIssue != "" {
			merged[fields.SourceIssue] = mr.ID
		}
	}
	return merged
}

// MergedMergeRequestFor returns the ID of a merged MR whose source issue is
// issueID, or "" when none exists.
func (b *Beads) MergedMergeRequestFor(issueID string) (string, error) {
	mrs, err := b.ListMergedMergeRequests()
	if err != nil {
		return "", err
	}
	return MergedSourceIssues(mrs)[strings.TrimSpace(issueID)], nil
}

// ReleaseWorkBead hands a work bead back after its worker went away (polecat
// removal, unsling, stale hook cleanup). One rule: a bead whose MR already
// merged is finished work and is closed; any other bead returns to open with
// no assignee. Reopening merged work makes the convoy feeder dispatch it
// again. If the merge queue cannot be read the bead is reopened (the worker
// must be released) and the lookup error is returned alongside.
func (b *Beads) ReleaseWorkBead(issueID string) (closed bool, err error) {
	mrID, lookupErr := b.MergedMergeRequestFor(issueID)
	if mrID != "" {
		if err := b.ForceCloseWithReason("Merged in "+mrID, issueID); err != nil {
			return false, fmt.Errorf("closing merged work bead %s: %w", issueID, err)
		}
		return true, nil
	}
	openStatus := "open"
	empty := ""
	if err := b.Update(issueID, UpdateOptions{Status: &openStatus, Assignee: &empty}); err != nil {
		return false, err
	}
	return false, lookupErr
}

// FindOpenMRsForIssue returns all open merge-request beads whose source_issue
// matches the given issue ID. Used to find prior attempts when re-dispatching
// an issue and to supersede old MRs when a new one is created.
func (b *Beads) FindOpenMRsForIssue(issueID string) ([]*Issue, error) {
	issues, err := b.ListMergeRequests(ListOptions{
		Status: "open",
		Label:  "gt:merge-request",
	})
	if err != nil {
		return nil, err
	}

	var matches []*Issue
	for _, issue := range issues {
		if MatchesMRSourceIssue(issue.Description, issueID) {
			matches = append(matches, issue)
		}
	}
	return matches, nil
}

// MatchesMRSourceIssue returns true if the MR description contains a
// source_issue field matching the given issue ID exactly. The trailing
// newline in the needle prevents partial ID matches (e.g., "gt-abc"
// must not match "gt-abcdef").
func MatchesMRSourceIssue(description, issueID string) bool {
	needle := "source_issue: " + issueID + "\n"
	return strings.Contains(description, needle)
}
