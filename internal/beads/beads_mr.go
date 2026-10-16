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

// ListClosedMergeRequests returns terminal merge-request wisps regardless of
// close reason. The refinery LLM sometimes closes an MR with a bare `bd close`
// (close_reason "Closed"), so "merged" cannot be read from the close reason
// alone; see MRWorkLanded. Query errors are surfaced, not swallowed.
func (b *Beads) ListClosedMergeRequests() ([]*Issue, error) {
	out, err := b.run("sql", "--json",
		"SELECT w.id, w.status, w.description FROM wisps w "+
			"JOIN wisp_labels l ON w.id = l.issue_id "+
			"WHERE l.label = 'gt:merge-request' AND w.status IN ('closed', 'tombstone')")
	if err != nil {
		return nil, fmt.Errorf("querying closed merge requests: %w", err)
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return nil, nil
	}
	var rows []*Issue
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("parsing closed merge requests: %w", err)
	}
	return rows, nil
}

// MergeProver proves a commit landed on a remote target branch from git facts.
// *git.Git satisfies it; beads stays free of a git dependency.
type MergeProver interface {
	VerifyPushedCommitReachableFromPushTarget(remote, branch, commit string) error
}

// ProverResolver returns the prover for a rig (the MR's `rig` field), or nil
// when the rig's repository cannot be resolved.
type ProverResolver func(rig string) MergeProver

// MRWorkLanded reports whether an MR's work is on its target branch. It is the
// one definition of "this bead's work already landed via a merge request",
// shared by the convoy feeder, convoy launch, post-merge, witness and the
// release/unhook paths. Two signals:
//   - structured: a terminal MR with close_reason=merged / merge_commit, which
//     only closeTerminalMR writes on a merged close;
//   - git healer, for MRs closed without post-merge (bare `bd close`): the
//     commit_sha recorded at submit (not the branch's current tip) must be
//     reachable from origin/<target>, falling back to the default branch when
//     the target was deleted. Known limits: squash/rebase merges and PR mode
//     rewrite the commit, so this can only under-report (our rigs merge
//     --no-ff/ff); an MR whose head was already in the target at submit
//     (no new commits) would over-report, but gt done does not submit those.
//
// An MR without a recorded commit/target, or without a prover, is not proven.
func MRWorkLanded(mr *Issue, prover MergeProver) bool {
	if mr == nil {
		return false
	}
	fields := ParseMRFields(mr)
	if fields == nil {
		return false
	}
	return MRFieldsLanded(IssueStatus(strings.TrimSpace(mr.Status)), fields, prover)
}

// MRFieldsLanded is MRWorkLanded for already-parsed fields and a known status.
func MRFieldsLanded(status IssueStatus, fields *MRFields, prover MergeProver) bool {
	if fields == nil {
		return false
	}
	// Structured signal, written only by the merged close in closeTerminalMR.
	if status.IsTerminal() && (strings.EqualFold(strings.TrimSpace(fields.CloseReason), "merged") ||
		strings.TrimSpace(fields.MergeCommit) != "") {
		return true
	}
	target := strings.TrimSpace(fields.Target)
	commit := strings.TrimSpace(fields.CommitSHA)
	if prover == nil || target == "" || commit == "" {
		return false
	}
	// A commit already in the target when the MR was submitted carried no new
	// work; reachability would then be a false positive.
	if base := strings.TrimSpace(fields.BaseSHA); base != "" {
		if a, ok := prover.(ancestorChecker); ok {
			if isAnc, err := a.IsAncestor(commit, base); err != nil || isAnc {
				return false
			}
		}
	}
	return prover.VerifyPushedCommitReachableFromPushTarget("origin", target, commit) == nil
}

// ancestorChecker is optionally implemented by a MergeProver (*git.Git does).
type ancestorChecker interface {
	IsAncestor(ancestor, descendant string) (bool, error)
}

// MergeRequestsBySource lists pending and closed MRs of this DB grouped by
// source issue. Any query error is returned so callers can fail closed.
func (b *Beads) MergeRequestsBySource() (map[string][]*Issue, error) {
	pending, err := b.ListPendingMergeRequests()
	if err != nil {
		return nil, err
	}
	closed, err := b.ListClosedMergeRequests()
	if err != nil {
		return nil, err
	}
	bySource := make(map[string][]*Issue)
	for _, mr := range append(pending, closed...) {
		if fields := ParseMRFields(mr); fields != nil && strings.TrimSpace(fields.SourceIssue) != "" {
			src := strings.TrimSpace(fields.SourceIssue)
			bySource[src] = append(bySource[src], mr)
		}
	}
	return bySource, nil
}

// LandedMergeRequest returns the ID of the first MR in mrs whose work landed
// (MRWorkLanded), resolving each MR's prover from its rig field.
func LandedMergeRequest(mrs []*Issue, resolve ProverResolver) string {
	for _, mr := range mrs {
		var prover MergeProver
		if fields := ParseMRFields(mr); fields != nil && resolve != nil {
			prover = resolve(strings.TrimSpace(fields.Rig))
		}
		if MRWorkLanded(mr, prover) {
			return mr.ID
		}
	}
	return ""
}

// MergedMergeRequestFor returns the ID of an MR whose work landed and whose
// source issue is issueID, or "" when none exists.
func (b *Beads) MergedMergeRequestFor(issueID string, resolve ProverResolver) (string, error) {
	bySource, err := b.MergeRequestsBySource()
	if err != nil {
		return "", err
	}
	return LandedMergeRequest(bySource[strings.TrimSpace(issueID)], resolve), nil
}

// ReleaseWorkBead hands a work bead back after its worker went away (polecat
// removal, unsling, stale hook cleanup). One rule: a bead whose MR already
// landed is finished work and is closed; any other bead returns to open with
// no assignee. Reopening landed work makes the convoy feeder dispatch it
// again. If the merge queue cannot be read the bead is reopened (the worker
// must be released) and the lookup error is returned alongside.
func (b *Beads) ReleaseWorkBead(issueID string, resolve ProverResolver) (closed bool, err error) {
	mrID, lookupErr := b.MergedMergeRequestFor(issueID, resolve)
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
