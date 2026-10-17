package witness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/steveyegge/gastown/internal/atomicfile"
	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/lock"
	"github.com/steveyegge/gastown/internal/mail"
	"github.com/steveyegge/gastown/internal/mergeproof"
	"github.com/steveyegge/gastown/internal/polecat"
	"github.com/steveyegge/gastown/internal/session"
	"github.com/steveyegge/gastown/internal/tmux"
	"github.com/steveyegge/gastown/internal/workspace"
)

// Stranded polecat branches (gt-pd1c.78 tahap 2). A polecat branch on the
// rig's remote whose tip is on no merge target (default branch,
// integration/*) and not archived (refs/gt/archive/*) holds work that no agent
// may discard (the polecat branch deletion rule). The witness patrol reports
// such branches to the MAYOR, who decides per branch; the overseer is only
// involved when the mayor escalates (or for an unverified landing, which the
// parked-bead scan already reports). The options follow the merge queue's
// class of the tip (beads.TipDisposition):
//
//   - rejected / superseded: rework or resubmit, leave it, or abandon it
//     (gt polecat abandon: archive, verify, delete);
//   - conflict, MR closed without an official verdict (an ANOMALY when it
//     was closed by hand: git says the work did not land), no MR: rework or
//     resubmit, or leave it. A branch without an MR is reported only when no
//     polecat that may own it is alive;
//   - open MR, landed, unverified: not stranded here (the refinery, or the
//     overseer via the parked-bead scan, owns them).
//
// A branch of an MR class is not stranded when the MR's source bead is
// already terminal (the work was settled or dropped), or when the merge
// queue still holds that source through another MR (MergeQueue.Hold:
// pending = rework in progress, landed, unverified = the overseer's).
//
// A branch is reported once it has kept the same tip and class for the grace
// period (WitnessThresholds.StrandedBranchGraceD); reminders back off
// (strandedReminderBackoff: 1x, 3x, 7x UnverifiedReminderIntervalD, i.e.
// 24h, 72h, 7d by default) and stop after the last one, which says so
// ("leave it" then takes effect). One digest mail per patrol, one line per
// branch. Branches whose state says nothing is due are not re-classified.
// The report state lives in <town>/witness/stranded-branches-<rig>.json,
// written atomically under a lock (a concurrent patrol skips); an unreadable
// state is reported and counts as empty (reports repeat, never silenced). A
// failed fetch or an exhausted time budget reports nothing that patrol.
// Nothing is auto-fixed here: the only certain fix (closing parked beads
// whose MR landed by git proof) is the parked-bead scan's.

// mayorAddress is the mayor's mailbox.
const mayorAddress = "mayor/"

// branchReport is the report state of one stranded branch.
type branchReport struct {
	Tip        string    `json:"tip"`
	Class      string    `json:"class"`
	FirstSeen  time.Time `json:"first_seen"`
	NotifiedAt time.Time `json:"notified_at,omitempty"`
	// Reports is how many reports were sent for this tip and class.
	Reports int `json:"reports,omitempty"`
}

// branchReportState maps a branch to its report state.
type branchReportState map[string]branchReport

// strandedReminderBackoff are the waits before the 2nd, 3rd and 4th (last)
// report, as multiples of the reminder interval. No report follows the last.
var strandedReminderBackoff = []int{1, 3, 7}

// strandedBranchBudget bounds one reconciliation (every git command is also
// bounded by the proof limits); the rest waits for the next patrol.
const strandedBranchBudget = 2 * time.Minute

// StrandedBranchesResult is what one reconciliation did.
type StrandedBranchesResult struct {
	Checked  int      // polecat branches on the remote examined
	Reported []string // branches the mayor was mailed about this patrol
	Pending  int      // stranded branches in grace, recently reported or past the last reminder
	Busy     bool     // another patrol of this rig held the state lock; nothing was done
	Errors   []error
}

// strandedBranch is one branch due for a report.
type strandedBranch struct {
	branch, tip string
	disp        beads.TipDisposition
	firstSeen   time.Time
	reminder    bool // an earlier report went out
	last        bool // the last report: no reminder follows
}

// branchReconciler is one reconciliation of a rig's polecat branches.
type branchReconciler struct {
	ctx      context.Context
	rig      string
	tips     mergeproof.RemoteTips
	git      mergeproof.PreservationGit
	fetch    func() error // makes target and polecat tips local objects
	queue    func() *beads.MergeQueue
	alive    func(polecatName string) (bool, error)
	terminal func(beadID string) (bool, error) // the source bead is closed or gone
	grace    time.Duration
	interval time.Duration
	now      time.Time
	state    branchReportState
	send     func(*mail.Message) error
}

// ReconcileStrandedBranches reports the stranded polecat branches of rig
// rigName (rig directory rigPath) to the mayor, within strandedBranchBudget.
// It runs on every witness patrol (gt patrol scan, with the parked-bead
// scan). rigName must be beads.IsSafeID.
func ReconcileStrandedBranches(bd *BdCli, rigPath, rigName string, router *mail.Router) *StrandedBranchesResult {
	return reconcileStrandedBranches(bd, rigPath, rigName, router, passMergeQueue(rigPath))
}

// reconcileStrandedBranches is ReconcileStrandedBranches with the patrol
// pass's merge queue (shared with the parked-bead scan).
func reconcileStrandedBranches(bd *BdCli, rigPath, rigName string, router *mail.Router, queue func() *beads.MergeQueue) *StrandedBranchesResult {
	ctx, cancel := context.WithTimeout(context.Background(), strandedBranchBudget)
	defer cancel()
	r, statePath, err := newBranchReconciler(ctx, bd, rigPath, rigName, router)
	if err != nil {
		return &StrandedBranchesResult{Errors: []error{err}}
	}
	if queue != nil {
		r.queue = queue
	}
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		return &StrandedBranchesResult{Errors: []error{fmt.Errorf("stranded-branch state dir: %w", err)}}
	}
	unlock, locked, err := lock.FlockTryAcquire(statePath + ".flock")
	if err != nil {
		return &StrandedBranchesResult{Errors: []error{fmt.Errorf("locking the stranded-branch state: %w", err)}}
	}
	if !locked {
		return &StrandedBranchesResult{Busy: true}
	}
	defer unlock()
	state, loadErr := loadBranchReportState(statePath)
	r.state = state
	res := r.run()
	if loadErr != nil {
		res.Errors = append(res.Errors, fmt.Errorf("reading the stranded-branch state (reports may repeat): %w", loadErr))
	}
	if err := saveBranchReportState(statePath, r.state); err != nil {
		res.Errors = append(res.Errors, fmt.Errorf("saving the stranded-branch state: %w", err))
	}
	return res
}

// newBranchReconciler wires a reconciliation to the rig's repository (read
// through origin's push URL), the town merge queue, tmux/agent-bead
// liveness, source bead status, the operational config and the mail router,
// and returns the report state path (the caller loads it under the lock).
func newBranchReconciler(ctx context.Context, bd *BdCli, rigPath, rigName string, router *mail.Router) (*branchReconciler, string, error) {
	if !beads.IsSafeID(rigName) {
		return nil, "", fmt.Errorf("refusing rig name %q: not a valid rig name", rigName)
	}
	townRoot, err := workspace.Find(rigPath)
	if err != nil || townRoot == "" {
		return nil, "", fmt.Errorf("no Gas Town workspace above %s", rigPath)
	}
	repo, err := mergeproof.RigGit(rigPath)
	if err != nil {
		return nil, "", fmt.Errorf("rig repository: %w", err)
	}
	repo = repo.WithProofLimits()
	thresholds := config.LoadOperationalConfig(townRoot).GetWitnessConfig()
	return &branchReconciler{
		ctx:   ctx,
		rig:   rigName,
		tips:  mergeproof.RemoteTips{Defaults: []string{mergeproof.RigDefaultBranch(rigPath, repo)}, List: mergeproof.CachedLister(pushRemoteLister(repo))},
		git:   repo,
		fetch: func() error { return repo.Fetch("origin") },
		queue: func() *beads.MergeQueue { return witnessMergeQueue(rigPath) },
		alive: polecatAlive(bd, rigPath, townRoot, rigName),
		terminal: func(id string) (bool, error) {
			status, ok := getBeadStatus(bd, rigPath, id)
			if !ok {
				return false, fmt.Errorf("cannot read source bead %s", displayID(id))
			}
			return status == "" || beads.IssueStatus(status).IsTerminal(), nil
		},
		grace:    thresholds.StrandedBranchGraceD(),
		interval: thresholds.UnverifiedReminderIntervalD(),
		now:      time.Now(),
		state:    branchReportState{},
		send: func(msg *mail.Message) error {
			if router == nil {
				return errNoMailRouter
			}
			return router.Send(msg)
		},
	}, branchReportStatePath(townRoot, rigName), nil
}

func pushRemoteLister(repo *git.Git) mergeproof.RemoteLister {
	return func(prefix string) ([]git.RemoteRef, error) {
		return repo.ListPushRemoteRefsWithHashes("origin", prefix)
	}
}

// polecatAlive reports whether polecat name of the rig is alive: its session
// runs, or its agent bead still holds work (a hook bead, state not done or
// nuked).
func polecatAlive(bd *BdCli, rigPath, townRoot, rigName string) func(string) (bool, error) {
	t := tmux.NewTmux()
	return func(name string) (bool, error) {
		if !beads.IsSafeID(name) {
			return false, nil
		}
		running, err := t.HasSession(session.PolecatSessionName(session.PrefixFor(rigName), name))
		if err != nil {
			return false, fmt.Errorf("checking the session of polecat %s: %w", name, err)
		}
		if running {
			return true, nil
		}
		agentID := beads.PolecatBeadIDWithPrefix(beads.GetPrefixForRig(townRoot, rigName), rigName, name)
		snap := fetchAgentBeadSnapshot(bd, rigPath, agentID)
		if snap == nil {
			return false, nil
		}
		state := beads.AgentState(snap.AgentState)
		return strings.TrimSpace(snap.HookBead) != "" && state != beads.AgentStateDone && state != beads.AgentStateNuked, nil
	}
}

// run reconciles once and updates r.state.
func (r *branchReconciler) run() *StrandedBranchesResult {
	res := &StrandedBranchesResult{}
	refs, err := r.tips.List("refs/heads/polecat/")
	if err != nil {
		res.Errors = append(res.Errors, fmt.Errorf("listing polecat branches on the remote: %s", beads.DisplayReason(err.Error())))
		return res
	}
	onRemote := map[string]string{}
	for _, ref := range refs {
		if branch, ok := strings.CutPrefix(ref.Name, "refs/heads/"); ok && strings.HasPrefix(branch, "polecat/") {
			onRemote[branch] = strings.TrimSpace(ref.Hash)
		}
	}
	r.pruneGone(onRemote)
	if len(onRemote) == 0 {
		return res // nothing to fetch or classify
	}
	if r.fetch != nil {
		if err := r.fetch(); err != nil {
			res.Errors = append(res.Errors, fmt.Errorf("fetching origin failed, no branch reported: %s", beads.DisplayReason(err.Error())))
			return res
		}
	}
	due, ok := r.classifyAll(onRemote, res)
	if !ok {
		return res
	}
	r.report(due, res)
	return res
}

// pruneGone forgets branches that are no longer on the remote or moved.
func (r *branchReconciler) pruneGone(onRemote map[string]string) {
	for branch, st := range r.state {
		if tip, ok := onRemote[branch]; !ok || tip != st.Tip {
			delete(r.state, branch)
		}
	}
}

// classifyAll classifies every branch and returns the due ones. ok is false
// when the patrol must report nothing (remote archives or the merge queue
// unreadable, budget exhausted).
func (r *branchReconciler) classifyAll(onRemote map[string]string, res *StrandedBranchesResult) ([]strandedBranch, bool) {
	branches := make([]string, 0, len(onRemote))
	for b := range onRemote {
		branches = append(branches, b)
	}
	sort.Strings(branches)
	var q *beads.MergeQueue
	var due []strandedBranch
	for _, branch := range branches {
		if r.ctx != nil && r.ctx.Err() != nil {
			res.Errors = append(res.Errors, fmt.Errorf("time budget exhausted after %d of %d branches, no branch reported this patrol", res.Checked, len(branches)))
			return nil, false
		}
		res.Checked++
		tip := onRemote[branch]
		if st, ok := r.state[branch]; ok && !r.due(st) {
			res.Pending++ // same tip, nothing due: not re-classified
			continue
		}
		if mergeproof.CoveredByTargets(r.git, tip, r.tips) {
			delete(r.state, branch)
			continue
		}
		archived, err := r.tips.Archived(tip)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Errorf("cannot list archives, no branch reported: %s", beads.DisplayReason(err.Error())))
			return nil, false
		}
		if q == nil && !archived {
			q = r.queue()
		}
		var disp beads.TipDisposition
		if !archived {
			if disp, err = q.TipDisposition(branch, tip); err != nil {
				res.Errors = append(res.Errors, fmt.Errorf("merge queue unreadable, no branch reported: %w", err))
				return nil, false
			}
		}
		errsBefore := len(res.Errors)
		if archived || !r.strandedClass(branch, disp, q, res) {
			if len(res.Errors) == errsBefore {
				delete(r.state, branch) // settled; an unknown answer keeps the state (and its backoff)
			}
			continue
		}
		if b, ok := r.track(branch, tip, disp); ok {
			due = append(due, b)
		} else {
			res.Pending++
		}
	}
	return due, true
}

// due reports whether a branch with this state needs a report now.
func (r *branchReconciler) due(st branchReport) bool {
	if r.now.Sub(st.FirstSeen) < r.grace {
		return false
	}
	if st.Reports == 0 || st.NotifiedAt.IsZero() || st.NotifiedAt.After(r.now.Add(unverifiedMarkSkew)) {
		return st.Reports <= len(strandedReminderBackoff) // an invalid mark never silences
	}
	if st.Reports > len(strandedReminderBackoff) {
		return false // past the last reminder
	}
	return r.now.Sub(st.NotifiedAt) >= time.Duration(strandedReminderBackoff[st.Reports-1])*r.interval
}

// strandedClass reports whether a tip of this class is stranded (reported to
// the mayor). A branch without an MR counts only when none of its possible
// owners is alive; unknown liveness is not reported (it may be worked on).
func (r *branchReconciler) strandedClass(branch string, disp beads.TipDisposition, q *beads.MergeQueue, res *StrandedBranchesResult) bool {
	switch disp.Class {
	case beads.TipRejected, beads.TipSuperseded, beads.TipConflict, beads.TipNoVerdict:
		return r.sourceStillOpen(disp, q, res)
	case beads.TipNoMR:
		for _, owner := range polecat.BranchOwners(branch) {
			alive, err := r.alive(owner)
			if err != nil {
				res.Errors = append(res.Errors, err)
				return false
			}
			if alive {
				return false
			}
		}
		return true
	default: // open, landed, unverified: owned elsewhere
		return false
	}
}

// sourceStillOpen reports whether the work of a decided MR still waits for a
// decision: its source bead is not terminal and the merge queue does not
// hold it through another MR (MergeQueue.Hold: pending = rework running,
// landed, unverified = the overseer's). A source that cannot be read is not
// reported (the error is).
func (r *branchReconciler) sourceStillOpen(disp beads.TipDisposition, q *beads.MergeQueue, res *StrandedBranchesResult) bool {
	fields := beads.ParseMRFields(disp.MR)
	if fields == nil || strings.TrimSpace(fields.SourceIssue) == "" {
		return true
	}
	source := strings.TrimSpace(fields.SourceIssue)
	hold, _, err := q.Hold(source)
	if err != nil {
		res.Errors = append(res.Errors, fmt.Errorf("merge queue unreadable for %s: %w", displayID(source), err))
		return false
	}
	if hold != beads.HoldNone {
		return false
	}
	if r.terminal == nil {
		return true
	}
	done, err := r.terminal(source)
	if err != nil {
		res.Errors = append(res.Errors, err)
		return false
	}
	return !done
}

// track updates the state of a stranded branch and reports whether it is due
// (due). A new tip or class restarts the grace and the reminder count.
func (r *branchReconciler) track(branch, tip string, disp beads.TipDisposition) (strandedBranch, bool) {
	st, ok := r.state[branch]
	if !ok || st.Tip != tip || st.Class != string(disp.Class) {
		st = branchReport{Tip: tip, Class: string(disp.Class), FirstSeen: r.now}
	}
	r.state[branch] = st
	if !r.due(st) {
		return strandedBranch{}, false
	}
	return strandedBranch{branch: branch, tip: tip, disp: disp, firstSeen: st.FirstSeen,
		reminder: st.Reports > 0, last: st.Reports == len(strandedReminderBackoff)}, true
}

// report mails the due branches to the mayor in one digest and marks them;
// a failed send marks nothing (the next patrol repeats it).
func (r *branchReconciler) report(due []strandedBranch, res *StrandedBranchesResult) {
	if len(due) == 0 {
		return
	}
	if err := r.send(r.message(due)); err != nil {
		res.Errors = append(res.Errors, fmt.Errorf("mailing the mayor about %d stranded branch(es): %w", len(due), err))
		return
	}
	for _, b := range due {
		st := r.state[b.branch]
		st.NotifiedAt = r.now
		st.Reports++
		r.state[b.branch] = st
		res.Reported = append(res.Reported, b.branch)
	}
}

func (r *branchReconciler) message(due []strandedBranch) *mail.Message {
	subject := fmt.Sprintf("STRANDED_BRANCHES: %s: %d polecat branch(es) need a decision", displayID(r.rig), len(due))
	reminder := true
	lines := make([]string, 0, len(due))
	for _, b := range due {
		reminder = reminder && b.reminder
		line := strandedLine(r.rig, b) + fmt.Sprintf(" [stranded %s]", shortDuration(r.now.Sub(b.firstSeen)))
		if b.last {
			line += " [last reminder: not reported again while this tip and class stay the same]"
		}
		lines = append(lines, line)
	}
	if reminder {
		subject = "REMINDER " + subject
	}
	body := fmt.Sprintf(`Polecat branches on %s's remote whose work is on no merge target and not archived.
No agent deletes them; decide per branch (rework/resubmit = re-dispatch the source bead).

%s

Abandon (rejected/superseded only) archives the tip under refs/gt/archive/* before deleting.
Deleting unlanded branches directly and pruning archives are for a human or crew: escalate
to the overseer (gt mail send --human) when a decision needs one.
Reminders back off (%s, then 3x, then 7x) and stop after the last one; leaving a branch
alone is then a decision that holds.`,
		displayID(r.rig), strings.Join(lines, "\n"), shortDuration(r.interval))
	return &mail.Message{
		From:     fmt.Sprintf("%s/witness", r.rig),
		To:       mayorAddress,
		Subject:  subject,
		Body:     body,
		Priority: mail.PriorityNormal,
	}
}

// strandedLine is the one-line report of a branch: what it is and the exact
// options its class allows. Unsafe names are never inserted.
func strandedLine(rigName string, b strandedBranch) string {
	branch := b.branch
	if !beads.IsSafeRefName(branch) {
		branch = "<invalid branch>"
	}
	tip := b.tip
	if !beads.IsGitObjectName(tip) {
		tip = "<invalid sha>"
	} else {
		tip = tip[:12]
	}
	what := ""
	mr := displayID(b.disp.MRID)
	switch b.disp.Class {
	case beads.TipRejected, beads.TipSuperseded:
		what = fmt.Sprintf("%s (MR %s)", b.disp.Class, mr)
	case beads.TipConflict:
		what = fmt.Sprintf("conflict (MR %s)", mr)
	case beads.TipNoVerdict:
		what = fmt.Sprintf("ANOMALY: MR %s closed without a verdict, git says not landed", mr)
	default:
		what = "no MR for this tip, no live owner"
	}
	options := "rework/resubmit | leave it"
	if b.disp.Abandonable() {
		options += " | abandon: " + beads.AbandonCommand(rigName, b.branch)
	}
	return fmt.Sprintf("- %s @%s %s: %s", branch, tip, beads.DisplayReason(what), options)
}

func branchReportStatePath(townRoot, rigName string) string {
	return filepath.Join(townRoot, "witness", "stranded-branches-"+rigName+".json")
}

// loadBranchReportState reads the report state. A missing file is empty; an
// unreadable or corrupt one is empty with the error (branches are reported
// again, never silenced).
func loadBranchReportState(path string) (branchReportState, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: path from the town root and a safe rig name
	if errors.Is(err, os.ErrNotExist) {
		return branchReportState{}, nil
	}
	if err != nil {
		return branchReportState{}, err
	}
	var st branchReportState
	if err := json.Unmarshal(data, &st); err != nil {
		return branchReportState{}, fmt.Errorf("corrupt state file: %w", err)
	}
	if st == nil {
		st = branchReportState{}
	}
	return st, nil
}

// saveBranchReportState writes the state atomically (temp file + rename).
func saveBranchReportState(path string, st branchReportState) error {
	return atomicfile.EnsureDirAndWriteJSONWithPerm(path, st, 0o600)
}
