// Package beads provides merge request and gate utilities.
package beads

import (
	"bytes"
	"encoding/json"
	"errors"
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
// release/unhook paths (decision order: mrLandingVerdictReason, git first).
// Two signals:
//   - structured: a terminal MR with close_reason=merged / merge_commit, which
//     only closeTerminalMR writes on a merged close; trusted only when git
//     cannot decide (a record git disproves is unverified, gt-pd1c.78 F7);
//   - git, also for MRs closed without post-merge (bare `bd close`): the
//     commit_sha recorded at submit (not the branch's current tip) must be
//     reachable from origin/<target>, falling back to the default branch when
//     the target was deleted, or its work must be on the target as an
//     equivalent patch (rebase/squash merge, PatchProver; gt-pd1c.81). An MR
//     whose head was already in the target at submit (no new commits) would
//     over-report, which the base_sha check guards against.
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
	return mrLandingVerdict(status, fields, prover) == verdictLanded
}

// Human landing verdicts (MRFields.Landing).
const (
	LandingLanded    = "landed"
	LandingNotLanded = "not_landed"
)

// UnverifiedReason explains why the landing of a terminal MR cannot be
// decided, or "" when it can (structured signal, definite git answer, or a
// human verdict). prover may be nil.
func UnverifiedReason(mr *Issue, prover MergeProver) string {
	if mr == nil {
		return "missing MR"
	}
	verdict, reason, _ := mrLandingVerdictReason(IssueStatus(strings.TrimSpace(mr.Status)), ParseMRFields(mr), prover)
	if verdict != verdictUnknown {
		return ""
	}
	return reason
}

// landingVerdict is the three-valued answer to "did this MR's work land?".
// Only a definite verdictNotLanded lets released work be dispatched again.
type landingVerdict int

const (
	verdictUnknown landingVerdict = iota
	verdictLanded
	verdictNotLanded
)

// notOnTargetError is implemented by prover errors that are a definite "the
// commit is not reachable from the target" (git.CommitNotOnBranchError), as
// opposed to a failure to find out.
type notOnTargetError interface{ CommitNotOnTarget() bool }

// IsCommitNotOnTarget reports whether a MergeProver error is a definite "not
// reachable" answer.
func IsCommitNotOnTarget(err error) bool {
	var n notOnTargetError
	return errors.As(err, &n) && n.CommitNotOnTarget()
}

func mrLandingVerdict(status IssueStatus, fields *MRFields, prover MergeProver) landingVerdict {
	verdict, _, _ := mrLandingVerdictReason(status, fields, prover)
	return verdict
}

// LandingEvidence says what decided an MR's landing verdict (gt-pd1c.78):
// callers that discard work on a "landed" verdict trust only git.
type LandingEvidence string

const (
	// EvidenceNone: no verdict (unknown landing).
	EvidenceNone LandingEvidence = ""
	// EvidenceRecorded: the structured merged signal on the bead
	// (close_reason=merged or merge_commit) while git could not decide (no
	// prover, no commit/target recorded, git error). Bead fields can be
	// edited with bd update, so this is not git proof.
	EvidenceRecorded LandingEvidence = "recorded"
	// EvidenceGit: a definite git answer from the MR's prover.
	EvidenceGit LandingEvidence = "git"
	// EvidenceHumanVerdict: a recorded human landing verdict (MRFields.Landing).
	EvidenceHumanVerdict LandingEvidence = "human_verdict"
)

// recordedMergedContradiction is the unverified reason when the bead says
// merged and git proves the commit is not on the target (gt-pd1c.78 F7).
const recordedMergedContradiction = "the MR is recorded as merged (close_reason/merge_commit), " +
	"but git shows its commit is not on the target"

// mrLandingVerdictReason decides whether an MR's work landed. Git decides
// whenever it can (reachability, then patch equivalence); bead fields can be
// edited with bd update, so they never override a definite git answer
// (gt-pd1c.78 F7):
//
//   - git landed: landed (EvidenceGit);
//   - git not landed: not landed (EvidenceGit), unless the terminal MR records
//     a merge (close_reason=merged or merge_commit, written by
//     closeTerminalMR): bead and git then contradict each other and only a
//     human verdict (MRFields.Landing) decides; without one the landing is
//     unknown (held as unverified, the overseer decides);
//   - git cannot decide: the recorded merge (EvidenceRecorded), else the human
//     verdict (terminal MRs only), else unknown.
//
// A false `landing:` written by hand therefore cannot reopen work git proves
// landed, nor close work git proves did not land. For verdictUnknown the
// reason says why; the evidence says which source decided.
func mrLandingVerdictReason(status IssueStatus, fields *MRFields, prover MergeProver) (landingVerdict, string, LandingEvidence) {
	if fields == nil {
		return verdictUnknown, "MR has no merge-request fields", EvidenceNone
	}
	recorded := status.IsTerminal() && (strings.EqualFold(strings.TrimSpace(fields.CloseReason), CloseReasonMerged) ||
		strings.TrimSpace(fields.MergeCommit) != "")
	gitVerdict, reason := gitLandingVerdict(fields, prover, recorded)
	switch {
	case gitVerdict == verdictLanded || (gitVerdict == verdictNotLanded && !recorded):
		return gitVerdict, "", EvidenceGit
	case gitVerdict == verdictNotLanded:
		reason = recordedMergedContradiction
	case recorded:
		return verdictLanded, "", EvidenceRecorded
	}
	if status.IsTerminal() {
		switch strings.TrimSpace(fields.Landing) {
		case LandingLanded:
			return verdictLanded, "", EvidenceHumanVerdict
		case LandingNotLanded:
			return verdictNotLanded, "", EvidenceHumanVerdict
		}
	}
	return verdictUnknown, reason, EvidenceNone
}

// gitLandingVerdict is the git part of the landing decision. recorded: the MR
// records a merge; its commit already being in base_sha (no new work) then
// is no contradiction, so reachability decides (F7, round 1).
func gitLandingVerdict(fields *MRFields, prover MergeProver, recorded bool) (landingVerdict, string) {
	target := strings.TrimSpace(fields.Target)
	commit := strings.TrimSpace(fields.CommitSHA)
	switch {
	case target == "" || commit == "":
		return verdictUnknown, "no commit_sha or target recorded on the MR"
	case prover == nil:
		return verdictUnknown, "no git prover for rig " + strings.TrimSpace(fields.Rig)
	case !IsGitObjectName(commit):
		return verdictUnknown, "commit_sha is not a hex git object name"
	case !IsSafeRefName(target):
		return verdictUnknown, "target is not a plain branch name"
	}
	base := strings.TrimSpace(fields.BaseSHA)
	if base != "" && !IsGitObjectName(base) {
		return verdictUnknown, "base_sha is not a hex git object name"
	}
	// A commit already in the target when the MR was submitted carried no new
	// work; reachability would then be a false positive. For an MR that
	// records a merge, reachability is what confirms the record.
	if base != "" && !recorded {
		if a, ok := prover.(ancestorChecker); ok {
			isAnc, err := a.IsAncestor(commit, base)
			if err != nil {
				return verdictUnknown, "git could not compare the commit with base_sha: " + err.Error()
			}
			if isAnc {
				return verdictNotLanded, ""
			}
		}
	}
	err := prover.VerifyPushedCommitReachableFromPushTarget("origin", target, commit)
	switch {
	case err == nil:
		return verdictLanded, ""
	case IsCommitNotOnTarget(err):
		return patchLandingVerdict(target, commit, base, prover)
	default:
		return verdictUnknown, "git could not verify the commit against the target: " + err.Error()
	}
}

// patchLandingVerdict is the second git proof, run only after a definite "the
// commit is not reachable from the target": a rebase or squash merge rewrites
// the commit, so "not landed" also needs "no equivalent patch on the target"
// (gt-pd1c.81). A prover without the check cannot rule a rewrite out.
func patchLandingVerdict(target, commit, base string, prover MergeProver) (landingVerdict, string) {
	pp, ok := prover.(PatchProver)
	if !ok {
		return verdictUnknown, "commit is not on the target and the prover cannot check for a rebase/squash merge"
	}
	equivalent, err := pp.PatchEquivalentOnPushTarget("origin", target, commit, base)
	switch {
	case err != nil:
		return verdictUnknown, "commit is not on the target and git could not rule out a rebase/squash merge: " + err.Error()
	case equivalent:
		return verdictLanded, ""
	default:
		return verdictNotLanded, ""
	}
}

// PatchProver is implemented by MergeProvers that can recognise work landed
// by a rebase or squash merge (*git.Git and the mergeproof provers do).
// PatchEquivalentOnPushTarget returns (true, nil) when an equivalent patch of
// commit is on remote/branch, (false, nil) when there definitely is none, and
// an error when git cannot tell; landing decisions treat the error as unknown.
// base is the MR's base_sha or "": when set, only target commits after it may
// be the equivalent (an older commit with the same patch proves nothing).
type PatchProver interface {
	PatchEquivalentOnPushTarget(remote, branch, commit, base string) (bool, error)
}

// ancestorChecker is optionally implemented by a MergeProver (*git.Git does).
type ancestorChecker interface {
	IsAncestor(ancestor, descendant string) (bool, error)
}

// sourcelessKey groups MRs without a source_issue in the merge queue index.
// No bead ID trims to it (it starts with a NUL byte), so no bead's hold sees
// these MRs; the per-branch lookups do.
const sourcelessKey = "\x00sourceless"

// MergeRequestsBySource lists pending and closed MRs of this DB grouped by
// source issue (MRs without one under sourcelessKey). Any query error is
// returned so callers can fail closed.
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
		fields := ParseMRFields(mr)
		if fields == nil {
			continue
		}
		src := strings.TrimSpace(fields.SourceIssue)
		if src == "" {
			// No source: never a bead's hold, but still an MR of its branch
			// (MergeQueue.TipDisposition, gt-pd1c.78 N2).
			src = sourcelessKey
		}
		bySource[src] = append(bySource[src], mr)
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
