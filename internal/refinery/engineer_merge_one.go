package refinery

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/gofrs/flock"
	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/mergeproof"
	"github.com/steveyegge/gastown/internal/rig"
	"github.com/steveyegge/gastown/internal/util"
)

// MergeOutcomeKind classifies what MergeOne did. It is part of the JSON of
// `gt mq merge --json`; values are stable.
type MergeOutcomeKind string

const (
	// OutcomeMerged: merged, pushed, proven, MR closed as merged, source
	// closed "Merged in <mr>", branch cleaned up, MERGED mailed. With
	// already_landed the MR's commit_sha was already proven on its target, so
	// nothing was merged or pushed again: only the finalize steps ran.
	OutcomeMerged MergeOutcomeKind = "merged"
	// OutcomeConflict: the MR conflicts with its target. A conflict-resolution
	// task blocks the MR; the claim is released.
	OutcomeConflict MergeOutcomeKind = "conflict"
	// OutcomeGatesFailed: a quality gate failed on the merged tree. Nothing
	// pushed; FIX_NEEDED sent. A failed gate always blocks (no baseline re-run).
	OutcomeGatesFailed MergeOutcomeKind = "gates_failed"
	// OutcomeScopeViolation: the MR changes paths outside
	// merge_queue.allowed_paths. Nothing merged or pushed; FIX_NEEDED sent.
	OutcomeScopeViolation MergeOutcomeKind = "scope_violation"
	// OutcomeNotEligible: the MR may not be merged now (not an open MR,
	// blocked, claimed by someone else, bad fields, or rejected by the
	// pre-merge recheck, which closes it).
	OutcomeNotEligible MergeOutcomeKind = "not_eligible"
	// OutcomeNeedsApproval: merge_strategy=pr and the PR lacks an approval.
	OutcomeNeedsApproval MergeOutcomeKind = "needs_approval"
	// OutcomeSlotTimeout: the default-branch merge slot stayed busy. Retry.
	OutcomeSlotTimeout MergeOutcomeKind = "slot_timeout"
	// OutcomeBranchMissing: the MR branch is not on origin (work may be
	// lost); the mayor is told.
	OutcomeBranchMissing MergeOutcomeKind = "branch_missing"
	// OutcomeCleanupFailed: the merge landed on origin but finalizing it
	// (proof, closing the MR or its source, or deleting the branch) failed.
	// Fix the cause and run the command in next: `gt mq merge <rig> <mr>` while
	// the MR is still open (it finalizes the landed merge without merging
	// again), `gt mq post-merge <rig> <mr>` once it is closed.
	OutcomeCleanupFailed MergeOutcomeKind = "cleanup_failed"
	// OutcomePrecondition: the refinery worktree or the configuration is not
	// fit for a merge (dirty tree, unfinished merge/rebase, stray local
	// target commits, auto_push=false, unreadable config). Nothing merged.
	OutcomePrecondition MergeOutcomeKind = "precondition"
	// OutcomeError: anything else (beads or git failure, cancellation).
	OutcomeError MergeOutcomeKind = "error"
)

// MergeOutcome is the result of MergeOne.
type MergeOutcome struct {
	Kind          MergeOutcomeKind `json:"kind"`
	MRID          string           `json:"mr_id"`
	Branch        string           `json:"branch,omitempty"`
	Target        string           `json:"target,omitempty"`
	SourceIssue   string           `json:"source_issue,omitempty"`
	CommitSHA     string           `json:"commit_sha,omitempty"`
	MergeCommit   string           `json:"merge_commit,omitempty"`
	ConflictTask  string           `json:"conflict_task,omitempty"`
	GatesRan      int              `json:"gates_ran"`
	Message       string           `json:"message,omitempty"`
	BranchCleanup *BranchCleanup   `json:"branch_cleanup,omitempty"`
	// AlreadyLanded: commit_sha was already proven on the target when this
	// run started, so it only finalized the MR (no merge, gates or push).
	AlreadyLanded bool `json:"already_landed,omitempty"`
	// Next is the command that finishes the MR after a cleanup_failed.
	Next     string   `json:"next,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// Merged reports whether the MR was merged and fully finalized.
func (o MergeOutcome) Merged() bool { return o.Kind == OutcomeMerged }

// Errors returned by LoadMRForMerge and CheckWorktreeReady; MergeOne maps
// them to not_eligible, precondition and branch_missing.
var (
	// ErrMRNotEligible: the MR may not be merged now.
	ErrMRNotEligible = errors.New("MR is not eligible for merge")
	// ErrWorktreeNotReady: the refinery worktree is not fit for a merge.
	ErrWorktreeNotReady = errors.New("refinery worktree is not ready")
	// ErrSourceBranchMissing: the MR branch is not on origin.
	ErrSourceBranchMissing = errors.New("MR branch is missing on origin")
)

// mergeLockName is the per-rig lock file (under <rig>/.runtime/locks) that
// serializes MergeOne runs on the refinery worktree.
const mergeLockName = "refinery-merge.lock"

// mergeClaimSeq makes per-run claimants unique within a process.
var mergeClaimSeq uint64

// refineryIdentity is this rig's refinery; claims by it, or by one of its
// MergeOne runs ("<rig>/refinery/<run>") that is no longer running, are ours
// while we hold the merge lock.
func (e *Engineer) refineryIdentity() string { return e.rig.Name + "/refinery" }

// runClaimantPattern is the shape of a MergeOne run claimant after
// "<rig>/refinery/" (newClaimant); the first number is the run's pid.
var runClaimantPattern = regexp.MustCompile(`^merge-([0-9]+)-[0-9]+$`)

// ownClaim reports whether an MR assignee is ours to take: empty, exactly
// this rig's refinery, or a stale MergeOne run claimant of it
// (claimRefusal).
func (e *Engineer) ownClaim(assignee string) bool { return e.claimRefusal(assignee, "<mr>") == "" }

// claimRefusal says why assignee is not ours to take, or "" when it is. A run
// claimant "<rig>/refinery/merge-<pid>-<n>" is stale, and so ours, when its
// pid is this process (MergeOne holds the merge lock, so no other run of this
// process is in flight) or no process with that pid exists on this host. A
// live pid, or one that cannot be parsed, is refused: claims are never taken
// from a run that may still be working.
func (e *Engineer) claimRefusal(assignee, mrID string) string {
	assignee = strings.TrimSpace(assignee)
	id := e.refineryIdentity()
	if assignee == "" || assignee == id {
		return ""
	}
	release := "release it first: gt refinery release " + mrID
	run, ok := strings.CutPrefix(assignee, id+"/")
	assignee = util.OneLine(assignee, 120) // bead data: one line in messages
	m := runClaimantPattern.FindStringSubmatch(run)
	if !ok || m == nil {
		return fmt.Sprintf("is claimed by %s (%s)", assignee, release)
	}
	pid, err := strconv.Atoi(m[1])
	if err != nil || pid <= 0 {
		return fmt.Sprintf("is claimed by %s, a merge run with an invalid pid (%s)", assignee, release)
	}
	if pid == os.Getpid() || !claimantProcessAlive(pid) {
		return ""
	}
	return fmt.Sprintf("is claimed by %s, a merge run whose process %d is still running "+
		"(let it finish; if it is stuck, stop it, or %s)", assignee, pid, release)
}

// newClaimant is a claimant unique to this MergeOne run, so the
// compare-and-set claim can tell this run from any other writer.
func (e *Engineer) newClaimant() string {
	return fmt.Sprintf("%s/merge-%d-%d", e.refineryIdentity(), os.Getpid(), atomic.AddUint64(&mergeClaimSeq, 1))
}

// lockMergeWorktree takes the rig's exclusive merge lock without waiting.
// Every engine entry point that drives the refinery worktree (checkout,
// merge, reset, push) holds it: MergeOne and ProcessBatch (which covers
// processSingleMR and the batch stack). The lock file is created 0600. The
// returned func releases it.
func (e *Engineer) lockMergeWorktree() (func(), error) {
	dir := filepath.Join(e.rig.Path, ".runtime", "locks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create lock dir: %w", err)
	}
	fl := flock.New(filepath.Join(dir, mergeLockName), flock.SetPermissions(0o600))
	locked, err := fl.TryLock()
	if err != nil {
		return nil, fmt.Errorf("take the merge lock %s: %w", fl.Path(), err)
	}
	if !locked {
		return nil, fmt.Errorf("another merge is running on rig %s (lock %s); retry when it finishes", e.rig.Name, fl.Path())
	}
	return func() { _ = fl.Unlock() }, nil
}

// MergeOne lands one MR end to end, the single Go path for a refinery merge
// (gt-pd1c.78 tahap 3): the rig merge lock; LoadConfig (gates required unless
// run_tests=false); LoadMRForMerge; a compare-and-set claim by a per-run
// claimant, as which every bead write of the run is made (asClaimant: bd only
// lets the assignee close a bead, gt-pd1c.93); CheckWorktreeReady; then, when
// git already proves commit_sha on the target, only the finalize steps
// (already_landed: a re-run after cleanup_failed never merges again);
// otherwise ProcessMRInfo (eligibility recheck, scope gate, merge, gates,
// verified push); HandleMRInfoSuccess (proof, close "merged" + source
// "Merged in <mr>", branch cleanup, MERGED mail) or HandleMRInfoFailure. The
// claim is released on every outcome that leaves the MR open. auto_push=false
// is refused: a merge that is not pushed cannot be finalized. ctx cancels
// gates and the merge-slot wait; a canceled run never pushes.
func (e *Engineer) MergeOne(ctx context.Context, mrID string) (out MergeOutcome) {
	out.MRID = strings.TrimSpace(mrID)
	defer e.waitPendingMail()
	unlock, err := e.lockMergeWorktree()
	if err != nil {
		return out.with(OutcomePrecondition, err.Error())
	}
	defer unlock()

	mr, out, ok := e.prepareMergeOne(out)
	if !ok {
		return out
	}
	claimant := e.newClaimant()
	run, err := e.asClaimant(claimant)
	if err != nil {
		return out.with(OutcomeError, fmt.Sprintf("act as claimant %s: %v", claimant, err))
	}
	fields, out, ok := run.claimForMerge(out, mr, claimant)
	if !ok {
		return out
	}
	terminal := false
	defer func() {
		if terminal {
			return
		}
		if err := run.ReleaseMR(mr.ID); err != nil {
			out.Warnings = append(out.Warnings, fmt.Sprintf("release claim on %s: %v (run gt refinery release %s)", mr.ID, err, mr.ID))
		}
	}()
	out, terminal = run.runMergeOne(ctx, out, mr, fields)
	return out
}

// asClaimant returns a copy of e whose bead operations all run as claimant
// (beads.WithActor): the run owns its claim in bd's eyes, so bd's ownership
// guards let it close the MR it claimed (gt-pd1c.93). The caller's identity
// (a human's git user, or <rig>/refinery) never equals a run claimant.
func (e *Engineer) asClaimant(claimant string) (*Engineer, error) {
	scoped, err := e.beads.WithActor(claimant)
	if err != nil {
		return nil, err
	}
	run := *e
	run.beads = scoped
	return &run, nil
}

// prepareMergeOne loads the configuration and the MR; ok=false means out is
// the final (refusal) outcome.
func (e *Engineer) prepareMergeOne(out MergeOutcome) (*MRInfo, MergeOutcome, bool) {
	if err := e.LoadConfig(); err != nil {
		return nil, out.with(OutcomePrecondition, fmt.Sprintf("load merge queue config: %v", err)), false
	}
	if !e.config.AutoPush {
		return nil, out.with(OutcomePrecondition, "merge_queue.auto_push is false: gt mq merge only lands work it can push and prove; "+
			"enable auto_push or merge by hand and run gt mq post-merge"), false
	}
	if e.configuredGateCount() == 0 {
		if e.config.RunTests {
			return nil, out.with(OutcomePrecondition, "no merge gates configured; set run_tests=false to merge without gates "+
				"(or set setup/typecheck/lint/build/test commands in settings/config.json)"), false
		}
		out.Warnings = append(out.Warnings, "run_tests=false: merging without any gate")
	}
	mr, err := e.LoadMRForMerge(out.MRID)
	if err != nil {
		if errors.Is(err, ErrMRNotEligible) {
			return nil, out.with(OutcomeNotEligible, err.Error()), false
		}
		return nil, out.with(OutcomeError, err.Error()), false
	}
	out.Branch, out.Target, out.SourceIssue, out.CommitSHA = mr.Branch, mr.Target, mr.SourceIssue, mr.CommitSHA
	return mr, out, true
}

// claimForMerge claims mr for this run and reads the claim back
// (compare-and-set): when another writer won, the MR is not ours and its
// claim is left alone. It returns the MR fields as read back with the claim.
func (e *Engineer) claimForMerge(out MergeOutcome, mr *MRInfo, claimant string) (*beads.MRFields, MergeOutcome, bool) {
	if err := e.ClaimMR(mr.ID, claimant); err != nil {
		return nil, out.with(OutcomeError, fmt.Sprintf("claim MR %s: %v", mr.ID, err)), false
	}
	issue, err := e.beads.Show(mr.ID)
	if err != nil || issue == nil {
		return nil, out.with(OutcomeError, fmt.Sprintf("read back the claim on %s: %v (claim state unknown: check it, then gt refinery release %s)", mr.ID, err, mr.ID)), false
	}
	if got := strings.TrimSpace(issue.Assignee); got != claimant {
		return nil, out.with(OutcomeNotEligible, fmt.Sprintf("%v: MR %s was claimed by %s while this run claimed it", ErrMRNotEligible, mr.ID, got)), false
	}
	fields := beads.ParseMRFields(issue)
	if fields == nil {
		fields = &beads.MRFields{}
	}
	return fields, out, true
}

// runMergeOne checks the worktree, then finalizes an MR whose commit_sha is
// already on its target (a run that pushed but failed to finalize, or a merge
// by hand) or merges it, and finalizes or reports the failure. fields are the
// MR fields read back with the claim. terminal: the MR is no longer open
// (keep the claim).
func (e *Engineer) runMergeOne(ctx context.Context, out MergeOutcome, mr *MRInfo, fields *beads.MRFields) (MergeOutcome, bool) {
	if err := e.checkWorktreeBase(mr); err != nil {
		return e.worktreeRefusal(out, mr, err), false
	}
	if err := ctx.Err(); err != nil {
		return out.with(OutcomeError, fmt.Sprintf("canceled before merge: %v", err)), false
	}
	var step mergeStep
	step, out = e.landingStep(out, mr, fields)
	switch step {
	case stepFinalize:
		return e.finalizeLanded(out, mr)
	case stepRefuse:
		return out, false
	}
	if err := e.ensureLocalSourceBranch(mr); err != nil {
		return e.worktreeRefusal(out, mr, err), false
	}

	result := e.ProcessMRInfo(ctx, mr)
	out.CommitSHA = mr.CommitSHA
	out.GatesRan = result.GatesRan
	if result.Success {
		out = e.recordMergeCommit(out, mr, result.MergeCommit)
		return e.finalizeMergeOne(out, mr, result)
	}
	kind := failureOutcomeKind(result)
	if kind == OutcomeError {
		// Infrastructure failure: nothing for the polecat to fix, no FIX_NEEDED.
		return out.with(OutcomeError, result.Error), false
	}
	e.HandleMRInfoFailure(mr, result)
	out.ConflictTask = mr.ConflictTaskID
	// A NoMerge MR was closed by the recheck or the failure handler, unless
	// that close failed: then it is still open and the claim is released.
	terminal := result.NoMerge && !e.mrIsOpen(mr.ID)
	return out.with(kind, result.Error), terminal
}

// recordMergeCommit writes the pushed merge commit to the MR before any
// finalize step that can fail, so a failed finalize always leaves the
// evidence a re-run needs to finalize without merging again (landingStep).
// A failed write is a warning: the finalize below records it again.
func (e *Engineer) recordMergeCommit(out MergeOutcome, mr *MRInfo, merge string) MergeOutcome {
	merge = strings.TrimSpace(merge)
	if merge == "" || mr == nil || strings.TrimSpace(mr.ID) == "" || e.isSyntheticMergeMechanicsMR(mr) {
		return out
	}
	err := func() error {
		issue, err := e.beads.Show(mr.ID)
		if err != nil || issue == nil {
			return fmt.Errorf("read MR: %v", err)
		}
		fields := beads.ParseMRFields(issue)
		if fields == nil {
			return errors.New("MR has no merge-request fields")
		}
		fields.MergeCommit = merge
		desc := beads.SetMRFields(issue, fields)
		return e.beads.Update(mr.ID, beads.UpdateOptions{Description: &desc})
	}()
	if err != nil {
		out.Warnings = append(out.Warnings, fmt.Sprintf("record merge_commit %s on %s: %v (a failed finalize then needs gt mq post-merge)",
			shortSHA(merge), mr.ID, err))
	}
	return out
}

// worktreeRefusal maps a CheckWorktreeReady error to the outcome.
func (e *Engineer) worktreeRefusal(out MergeOutcome, mr *MRInfo, err error) MergeOutcome {
	if errors.Is(err, ErrSourceBranchMissing) {
		e.HandleMRInfoFailure(mr, ProcessResult{BranchNotFound: true, Error: err.Error()})
		return out.with(OutcomeBranchMissing, err.Error())
	}
	if errors.Is(err, ErrWorktreeNotReady) {
		return out.with(OutcomePrecondition, err.Error())
	}
	return out.with(OutcomeError, err.Error())
}

// mrIsOpen reports whether MR id is still open (true when it cannot be read,
// so the claim is released rather than kept).
func (e *Engineer) mrIsOpen(id string) bool {
	issue, err := e.beads.Show(id)
	if err != nil || issue == nil {
		return true
	}
	return beads.IssueStatus(strings.TrimSpace(issue.Status)) == beads.StatusOpen
}

// finalizeMergeOne runs HandleMRInfoSuccess after a pushed merge and reports
// whether the MR is now terminal (closed as merged).
func (e *Engineer) finalizeMergeOne(out MergeOutcome, mr *MRInfo, result ProcessResult) (MergeOutcome, bool) {
	out.MergeCommit = result.MergeCommit
	report := e.HandleMRInfoSuccess(mr, result)
	cleanup := report.BranchCleanup
	out.BranchCleanup = &cleanup
	if report.MergedMailErr != nil {
		out.Warnings = append(out.Warnings, fmt.Sprintf("MERGED mail to %s/witness not sent: %v", e.rig.Name, report.MergedMailErr))
	}
	if report.OK() {
		return out.with(OutcomeMerged, ""), true
	}
	// While the MR is open, re-running the engine finalizes it (the landed
	// commit is detected, nothing is merged again); once it is closed, only
	// the post-merge cleanup is left.
	out.Next = fmt.Sprintf("gt mq merge %s %s", e.rig.Name, mr.ID)
	if report.MRClosed() {
		out.Next = fmt.Sprintf("gt mq post-merge %s %s", e.rig.Name, mr.ID)
	}
	landed := "merge " + shortSHA(result.MergeCommit)
	if strings.TrimSpace(result.MergeCommit) == "" {
		landed = "commit_sha " + shortSHA(mr.CommitSHA)
	}
	msg := fmt.Sprintf("%s landed on origin/%s but %v; fix the cause, then run: %s", landed, mr.Target, report.Err(), out.Next)
	return out.with(OutcomeCleanupFailed, msg), report.MRClosed()
}

// failureOutcomeKind maps a failed ProcessResult to an outcome kind.
func failureOutcomeKind(result ProcessResult) MergeOutcomeKind {
	switch {
	case result.Conflict:
		return OutcomeConflict
	case result.ScopeViolation:
		return OutcomeScopeViolation
	case result.TestsFailed:
		return OutcomeGatesFailed
	case result.SlotTimeout:
		return OutcomeSlotTimeout
	case result.BranchNotFound:
		return OutcomeBranchMissing
	case result.NoMerge:
		return OutcomeNotEligible
	case result.NeedsApproval:
		return OutcomeNeedsApproval
	default:
		return OutcomeError
	}
}

func (o MergeOutcome) with(kind MergeOutcomeKind, msg string) MergeOutcome {
	o.Kind = kind
	o.Message = msg
	return o
}

func (e *Engineer) waitPendingMail() {
	if e.router != nil {
		e.router.WaitPendingNotifications()
	}
}

// LoadMRForMerge reads MR mrID and returns it when it may be merged (or, when
// it already landed, finalized) now: a gt:merge-request bead of this rig,
// open, not owned-direct, no close_reason other than merged, with a plain
// branch name, a trusted target (the rig default branch or integration/*,
// mergeproof.UntrustedMergeTarget), a full commit_sha, no unresolved
// blocker, and unassigned, claimed by this refinery, or claimed by one of its
// runs that is no longer running (claimRefusal). Refusals wrap
// ErrMRNotEligible; read failures do not.
func (e *Engineer) LoadMRForMerge(mrID string) (*MRInfo, error) {
	if !beads.IsSafeID(mrID) {
		return nil, fmt.Errorf("%w: %q is not a bead id", ErrMRNotEligible, mrID)
	}
	issue, err := e.beads.Show(mrID)
	if err != nil {
		if errors.Is(err, beads.ErrNotFound) {
			return nil, fmt.Errorf("%w: MR %s not found", ErrMRNotEligible, mrID)
		}
		return nil, fmt.Errorf("read MR %s: %w", mrID, err)
	}
	if issue == nil {
		return nil, fmt.Errorf("%w: MR %s not found", ErrMRNotEligible, mrID)
	}
	fields, reason := e.mergeIneligibility(issue)
	if reason != "" {
		return nil, fmt.Errorf("%w: MR %s %s", ErrMRNotEligible, mrID, reason)
	}
	return issueToMRInfo(issue, fields), nil
}

// mergeIneligibility returns the MR fields, or why the MR may not be merged.
func (e *Engineer) mergeIneligibility(issue *beads.Issue) (*beads.MRFields, string) {
	if !beads.HasLabel(issue, "gt:merge-request") {
		return nil, "is not a merge request (no gt:merge-request label)"
	}
	if status := strings.TrimSpace(issue.Status); beads.IssueStatus(status) != beads.StatusOpen {
		return nil, fmt.Sprintf("is %s, not open", status)
	}
	if beads.HasLabel(issue, "gt:owned-direct") {
		return nil, "is owned-direct (gt:owned-direct)"
	}
	fields := beads.ParseMRFields(issue)
	if fields == nil {
		return nil, "has no merge-request fields"
	}
	if fields.Rig != "" && !rig.SameName(fields.Rig, e.rig.Name) {
		return nil, fmt.Sprintf("belongs to rig %s", fields.Rig)
	}
	// close_reason merged is left to MergeOne: it finalizes the MR when git
	// proves commit_sha landed and refuses it otherwise. Any other verdict
	// (rejected, superseded, ...) is final.
	if reason := strings.TrimSpace(fields.CloseReason); reason != "" && !strings.EqualFold(reason, string(CloseReasonMerged)) {
		return nil, fmt.Sprintf("records close_reason %q; an MR with a close verdict is not merged again", util.OneLine(reason, 80))
	}
	if src := strings.TrimSpace(fields.SourceIssue); src != "" && !beads.IsSafeID(src) {
		return nil, fmt.Sprintf("source_issue %q is not a plain bead id", util.OneLine(src, 80))
	}
	if !beads.IsSafeRefName(fields.Branch) {
		return nil, fmt.Sprintf("branch %q is not a plain branch name", fields.Branch)
	}
	if err := mergeproof.UntrustedMergeTarget(fields.Target, e.defaultBranch()); err != nil {
		return nil, err.Error()
	}
	if fields.Branch == fields.Target {
		return nil, fmt.Sprintf("branch %s is its own target", fields.Branch)
	}
	if !beads.IsGitObjectName(strings.TrimSpace(fields.CommitSHA)) {
		return nil, "has no full commit_sha (resubmit with gt done / gt mq submit)"
	}
	if blocker := beads.FirstUnresolvedBlockerID(issue); blocker != "" || beads.HasUnresolvedBlockers(issue) {
		return nil, fmt.Sprintf("is blocked by %s", firstNonEmpty(blocker, "an open dependency"))
	}
	if refusal := e.claimRefusal(issue.Assignee, issue.ID); refusal != "" {
		return nil, refusal
	}
	return fields, ""
}

// CheckWorktreeReady makes sure the refinery worktree can merge mr without
// touching anything else: no unfinished merge/rebase/cherry-pick/revert; a
// clean tree (untracked files included: they could change gate results);
// origin fetched; the local target, if any, equal to or an ancestor of
// origin/<target>, so no stray local commit is ever pushed. When the MR
// branch is missing locally but origin/<branch> is exactly the submitted
// commit_sha, a local branch is created there (D6). Refusals wrap
// ErrWorktreeNotReady or ErrSourceBranchMissing.
func (e *Engineer) CheckWorktreeReady(mr *MRInfo) error {
	if err := e.checkWorktreeBase(mr); err != nil {
		return err
	}
	return e.ensureLocalSourceBranch(mr)
}

// checkWorktreeBase is CheckWorktreeReady without the source branch step:
// no unfinished operation, a clean tree, origin fetched, a sound local
// target. MergeOne runs it before deciding whether the MR already landed.
func (e *Engineer) checkWorktreeBase(mr *MRInfo) error {
	if op, err := e.git.OperationInProgress(); err != nil {
		return fmt.Errorf("%w: %v", ErrWorktreeNotReady, err)
	} else if op != "" {
		return fmt.Errorf("%w: a %s is in progress in %s; finish or abort it first", ErrWorktreeNotReady, op, e.workDir)
	}
	status, err := e.git.Status()
	if err != nil {
		return fmt.Errorf("%w: git status: %v", ErrWorktreeNotReady, err)
	}
	if !status.Clean {
		n := len(status.Modified) + len(status.Added) + len(status.Deleted) + len(status.Untracked) + len(status.Unmerged)
		return fmt.Errorf("%w: %d uncommitted or untracked path(s) in %s; clean the tree first", ErrWorktreeNotReady, n, e.workDir)
	}
	if err := e.git.Fetch("origin"); err != nil {
		return fmt.Errorf("%w: fetch origin: %v", ErrWorktreeNotReady, err)
	}
	return e.checkLocalTarget(mr.Target)
}

// checkLocalTarget refuses a local target branch with commits that are not
// on origin/<target>: doMerge pushes the whole local target.
func (e *Engineer) checkLocalTarget(target string) error {
	remoteRef := "refs/remotes/origin/" + target
	if ok, err := e.git.RefExists(remoteRef); err != nil {
		return fmt.Errorf("%w: read origin/%s: %v", ErrWorktreeNotReady, target, err)
	} else if !ok {
		return fmt.Errorf("%w: origin/%s does not exist", ErrWorktreeNotReady, target)
	}
	ok, err := e.git.BranchExists(target)
	if err != nil {
		return fmt.Errorf("%w: read local %s: %v", ErrWorktreeNotReady, target, err)
	}
	if !ok {
		return nil // doMerge checks it out from origin/<target>
	}
	contained, err := e.git.IsAncestor("refs/heads/"+target, remoteRef)
	if err != nil {
		return fmt.Errorf("%w: compare %s with origin/%s: %v", ErrWorktreeNotReady, target, target, err)
	}
	if !contained {
		return fmt.Errorf("%w: local %s has commits that are not on origin/%s; refusing to push them "+
			"(inspect with: git log origin/%s..%s)", ErrWorktreeNotReady, target, target, target, target)
	}
	return e.fastForwardLocalTarget(target, remoteRef)
}

// fastForwardLocalTarget moves a local target that is behind origin/<target>
// up to it, so the merge is built on the origin tip even when doMerge's
// `git pull` fails (it only warns). The tree is clean and the move is a
// fast-forward (checkLocalTarget verified ancestry).
func (e *Engineer) fastForwardLocalTarget(target, remoteRef string) error {
	local, err := e.git.Rev("refs/heads/" + target)
	if err != nil {
		return fmt.Errorf("%w: resolve local %s: %v", ErrWorktreeNotReady, target, err)
	}
	remote, err := e.git.Rev(remoteRef)
	if err != nil {
		return fmt.Errorf("%w: resolve origin/%s: %v", ErrWorktreeNotReady, target, err)
	}
	if strings.TrimSpace(local) == strings.TrimSpace(remote) {
		return nil
	}
	current, err := e.git.CurrentBranch()
	if err != nil {
		return fmt.Errorf("%w: read current branch: %v", ErrWorktreeNotReady, err)
	}
	if current == target {
		err = e.git.MergeFFOnly(remoteRef)
	} else {
		err = e.git.ResetBranch(target, remoteRef)
	}
	if err != nil {
		return fmt.Errorf("%w: fast-forward %s to origin/%s: %v", ErrWorktreeNotReady, target, target, err)
	}
	_, _ = fmt.Fprintf(e.output, "[Engineer] Fast-forwarded %s to origin/%s (%s)\n", target, target, shortSHA(remote))
	return nil
}

// ensureLocalSourceBranch creates the local MR branch at commit_sha when it
// is missing locally and origin/<branch> is exactly commit_sha (D6). An
// existing local branch is left alone: doMerge refuses it unless it is at
// commit_sha.
func (e *Engineer) ensureLocalSourceBranch(mr *MRInfo) error {
	ok, err := e.git.BranchExists(mr.Branch)
	if err != nil {
		return fmt.Errorf("%w: read local %s: %v", ErrWorktreeNotReady, mr.Branch, err)
	}
	if ok {
		return nil
	}
	remoteRef := "refs/remotes/origin/" + mr.Branch
	exists, err := e.git.RefExists(remoteRef)
	if err != nil {
		return fmt.Errorf("%w: read origin/%s: %v", ErrWorktreeNotReady, mr.Branch, err)
	}
	if !exists {
		return fmt.Errorf("%w: %s is neither local nor on origin", ErrSourceBranchMissing, mr.Branch)
	}
	tip, err := e.git.Rev(remoteRef + "^{commit}")
	if err != nil {
		return fmt.Errorf("%w: resolve origin/%s: %v", ErrWorktreeNotReady, mr.Branch, err)
	}
	commit := strings.TrimSpace(mr.CommitSHA)
	if strings.TrimSpace(tip) != commit {
		return fmt.Errorf("%w: origin/%s is at %s, not the submitted commit_sha %s; resubmit the MR",
			ErrWorktreeNotReady, mr.Branch, shortSHA(tip), shortSHA(commit))
	}
	if err := e.git.CreateBranchFrom(mr.Branch, commit); err != nil {
		return fmt.Errorf("%w: create local %s at %s: %v", ErrWorktreeNotReady, mr.Branch, shortSHA(commit), err)
	}
	_, _ = fmt.Fprintf(e.output, "[Engineer] Created local branch %s at submitted commit %s (from origin)\n", mr.Branch, shortSHA(commit))
	return nil
}
