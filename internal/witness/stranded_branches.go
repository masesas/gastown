package witness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/mail"
	"github.com/steveyegge/gastown/internal/mergeproof"
	"github.com/steveyegge/gastown/internal/polecat"
	"github.com/steveyegge/gastown/internal/rig"
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
// A branch is reported once it has kept the same tip and class for the grace
// period (WitnessThresholds.StrandedBranchGraceD), then again every reminder
// interval (UnverifiedReminderIntervalD) while it stays stranded. One digest
// mail per patrol, one line per branch. The report state lives in
// <town>/witness/stranded-branches-<rig>.json; a lost or corrupt file only
// repeats reports (at least once, never silenced). Nothing is auto-fixed
// here: the only certain fix (closing parked beads whose MR landed by git
// proof) is the parked-bead scan's.

// mayorAddress is the mayor's mailbox.
const mayorAddress = "mayor/"

// branchReport is the report state of one stranded branch.
type branchReport struct {
	Tip        string    `json:"tip"`
	Class      string    `json:"class"`
	FirstSeen  time.Time `json:"first_seen"`
	NotifiedAt time.Time `json:"notified_at,omitempty"`
}

// branchReportState maps a branch to its report state.
type branchReportState map[string]branchReport

// StrandedBranchesResult is what one reconciliation did.
type StrandedBranchesResult struct {
	Checked  int      // polecat branches on the remote examined
	Reported []string // branches the mayor was mailed about this patrol
	Pending  int      // stranded branches in grace or already reported recently
	Errors   []error
}

// strandedBranch is one branch due for a report.
type strandedBranch struct {
	branch, tip string
	disp        beads.TipDisposition
	firstSeen   time.Time
	reminder    bool
}

// branchReconciler is one reconciliation of a rig's polecat branches.
type branchReconciler struct {
	rig      string
	tips     mergeproof.RemoteTips
	git      mergeproof.PreservationGit
	queue    func() *beads.MergeQueue
	alive    func(polecatName string) (bool, error)
	grace    time.Duration
	interval time.Duration
	now      time.Time
	state    branchReportState
	send     func(*mail.Message) error
}

// ReconcileStrandedBranches reports the stranded polecat branches of rig
// rigName (rig directory rigPath) to the mayor. It runs on every witness
// patrol (gt patrol scan, with the parked-bead scan). rigName must be
// beads.IsSafeID.
func ReconcileStrandedBranches(bd *BdCli, rigPath, rigName string, router *mail.Router) *StrandedBranchesResult {
	r, statePath, err := newBranchReconciler(bd, rigPath, rigName, router)
	if err != nil {
		return &StrandedBranchesResult{Errors: []error{err}}
	}
	res := r.run()
	if err := saveBranchReportState(statePath, r.state); err != nil {
		res.Errors = append(res.Errors, fmt.Errorf("saving the stranded-branch report state: %w", err))
	}
	return res
}

// newBranchReconciler wires a reconciliation to the rig's repository (origin
// fetched best effort, read through its push URL), the town merge queue,
// tmux/agent-bead liveness, the operational config, the report state file
// (returned) and the mail router.
func newBranchReconciler(bd *BdCli, rigPath, rigName string, router *mail.Router) (*branchReconciler, string, error) {
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
	_ = repo.Fetch("origin") // best effort: target and polecat tips become local objects
	thresholds := config.LoadOperationalConfig(townRoot).GetWitnessConfig()
	statePath := branchReportStatePath(townRoot, rigName)
	return &branchReconciler{
		rig:      rigName,
		tips:     mergeproof.RemoteTips{Defaults: []string{rigDefaultBranch(rigPath, repo)}, List: mergeproof.CachedLister(pushRemoteLister(repo))},
		git:      repo,
		queue:    func() *beads.MergeQueue { return witnessMergeQueue(rigPath) },
		alive:    polecatAlive(bd, rigPath, townRoot, rigName),
		grace:    thresholds.StrandedBranchGraceD(),
		interval: thresholds.UnverifiedReminderIntervalD(),
		now:      time.Now(),
		state:    loadBranchReportState(statePath),
		send: func(msg *mail.Message) error {
			if router == nil {
				return errNoMailRouter
			}
			return router.Send(msg)
		},
	}, statePath, nil
}

func pushRemoteLister(repo *git.Git) mergeproof.RemoteLister {
	return func(prefix string) ([]git.RemoteRef, error) {
		return repo.ListPushRemoteRefsWithHashes("origin", prefix)
	}
}

// rigDefaultBranch is the rig's configured default branch, else origin/HEAD.
func rigDefaultBranch(rigPath string, repo *git.Git) string {
	if cfg, err := rig.LoadRigConfig(rigPath); err == nil && strings.TrimSpace(cfg.DefaultBranch) != "" {
		return strings.TrimSpace(cfg.DefaultBranch)
	}
	return repo.RemoteDefaultBranch()
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
	sort.Slice(refs, func(i, j int) bool { return refs[i].Name < refs[j].Name })
	var q *beads.MergeQueue
	stranded := map[string]bool{}
	var due []strandedBranch
	for _, ref := range refs {
		branch, ok := strings.CutPrefix(ref.Name, "refs/heads/")
		if !ok || !strings.HasPrefix(branch, "polecat/") {
			continue
		}
		res.Checked++
		tip := strings.TrimSpace(ref.Hash)
		if mergeproof.CoveredByTargets(r.git, tip, r.tips) {
			continue
		}
		archived, err := r.tips.Archived(tip)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Errorf("cannot list archives, no branch reported: %s", beads.DisplayReason(err.Error())))
			return res
		}
		if archived {
			continue
		}
		if q == nil {
			q = r.queue()
		}
		disp, err := q.TipDisposition(branch, tip)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Errorf("merge queue unreadable, no branch reported: %w", err))
			return res
		}
		if !r.strandedClass(branch, disp, res) {
			continue
		}
		stranded[branch] = true
		if b, ok := r.track(branch, tip, disp); ok {
			due = append(due, b)
		} else {
			res.Pending++
		}
	}
	for branch := range r.state {
		if !stranded[branch] {
			delete(r.state, branch)
		}
	}
	r.report(due, res)
	return res
}

// strandedClass reports whether a tip of this class is stranded (reported to
// the mayor). A branch without an MR counts only when none of its possible
// owners is alive; unknown liveness is not reported (it may be worked on).
func (r *branchReconciler) strandedClass(branch string, disp beads.TipDisposition, res *StrandedBranchesResult) bool {
	switch disp.Class {
	case beads.TipRejected, beads.TipSuperseded, beads.TipConflict, beads.TipNoVerdict:
		return true
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

// track updates the state of a stranded branch and reports whether it is due:
// same tip and class for the grace period, never reported or last reported an
// interval ago. A new tip or class restarts the grace.
func (r *branchReconciler) track(branch, tip string, disp beads.TipDisposition) (strandedBranch, bool) {
	st, ok := r.state[branch]
	if !ok || st.Tip != tip || st.Class != string(disp.Class) {
		st = branchReport{Tip: tip, Class: string(disp.Class), FirstSeen: r.now}
	}
	r.state[branch] = st
	if r.now.Sub(st.FirstSeen) < r.grace {
		return strandedBranch{}, false
	}
	notified := !st.NotifiedAt.IsZero() && !st.NotifiedAt.After(r.now.Add(unverifiedMarkSkew))
	if notified && r.now.Sub(st.NotifiedAt) < r.interval {
		return strandedBranch{}, false
	}
	return strandedBranch{branch: branch, tip: tip, disp: disp, firstSeen: st.FirstSeen, reminder: notified}, true
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
		lines = append(lines, strandedLine(r.rig, b)+fmt.Sprintf(" [stranded %s]", shortDuration(r.now.Sub(b.firstSeen))))
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
Reminders follow every %s while a branch stays stranded.`,
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

// loadBranchReportState reads the report state; a missing or corrupt file is
// empty (branches are reported again, never silenced).
func loadBranchReportState(path string) branchReportState {
	data, err := os.ReadFile(path) //nolint:gosec // G304: path from the town root and a safe rig name
	if err != nil {
		return branchReportState{}
	}
	var st branchReportState
	if json.Unmarshal(data, &st) != nil || st == nil {
		return branchReportState{}
	}
	return st
}

func saveBranchReportState(path string, st branchReportState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
