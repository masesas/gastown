package cmd

import (
	"bytes"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/beads"
	convoyops "github.com/steveyegge/gastown/internal/convoy"
	"github.com/steveyegge/gastown/internal/workspace"
)

// convoyLaunchForce controls whether to launch a convoy with warnings.
var convoyLaunchForce bool

// DispatchResult records the outcome of dispatching a single task.
type DispatchResult struct {
	BeadID  string
	Rig     string
	Success bool
	// Skipped marks a dispatch that failed because another actor took the bead
	// first (it is no longer open/unassigned). Not an error.
	Skipped bool
	Error   error
}

// mergeQueueHeldFn lists beads that must not be dispatched because their MR is
// pending or merged. Tests override it.
var mergeQueueHeldFn = mergeQueueHeldIssues

// dispatchTaskDirect dispatches a single task to its rig.
// In production, this delegates to gt sling. Tests override this variable
// with a stub to avoid spawning real processes.
var dispatchTaskDirect = func(townRoot, beadID, rig, baseBranch string) error {
	cmd := exec.Command("gt", convoyops.SlingArgs(beadID, rig, baseBranch)...)
	cmd.Dir = townRoot
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gt sling %s %s: %w\nstderr: %s", beadID, rig, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

var convoyLaunchCmd = &cobra.Command{
	Use:   "launch <convoy-id | epic-id | task-id...>",
	Short: "Launch a staged convoy: transition to open and dispatch Wave 1",
	Long: `Launch a staged convoy by transitioning its status from staged to open
and dispatching Wave 1 tasks.

For staged convoy-id input: transitions directly and dispatches.
For epic/task input: runs stage + launch in one step.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runConvoyLaunch,
}

func init() {
	convoyLaunchCmd.Flags().BoolVar(&convoyLaunchForce, "force", false, "Launch even with warnings")
}

// transitionConvoyToOpen transitions a staged convoy to open status.
// If the convoy is staged_ready, it transitions unconditionally.
// If the convoy is staged_warnings and force is true, it transitions.
// If the convoy is staged_warnings and force is false, it returns an error.
// If the convoy is already open it is a no-op: launch is idempotent so it can
// be re-run to re-dispatch beads stranded by an earlier failed spawn.
// If the convoy is closed, it returns an error.
func transitionConvoyToOpen(convoyID string, force bool) error {
	result, err := bdShow(convoyID)
	if err != nil {
		return fmt.Errorf("cannot resolve convoy %s: %w", convoyID, err)
	}

	status := normalizeConvoyStatus(result.Status)

	switch status {
	case convoyStatusStagedReady:
		// Transition directly to open.
		return bdUpdateStatus(convoyID, convoyStatusOpen)

	case convoyStatusStagedWarnings:
		if !force {
			return fmt.Errorf("convoy %s has warnings, use --force to launch", convoyID)
		}
		return bdUpdateStatus(convoyID, convoyStatusOpen)

	case convoyStatusOpen:
		return nil

	case convoyStatusClosed:
		return fmt.Errorf("convoy %s is closed", convoyID)

	default:
		return fmt.Errorf("convoy %s has unexpected status %q", convoyID, result.Status)
	}
}

// bdUpdateStatus runs `bd update <id> --status=<status>` against the town beads
// database, since convoys live at the HQ level.
func bdUpdateStatus(beadID, status string) error {
	townBeads, err := getTownBeadsDir()
	if err != nil {
		return err
	}
	cmd := exec.Command("bd", "update", beadID, "--status="+status)
	cmd.Dir = townBeads
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("bd update %s --status=%s: %w\noutput: %s", beadID, status, err, out)
	}
	return nil
}

// collectBlockedRigsInDAG returns a map of parked/docked rig names to the
// bead IDs that target them. Only considers slingable nodes. (gt-4owfd.1)
func collectBlockedRigsInDAG(dag *ConvoyDAG, townRoot string) map[string][]string {
	blockedRigBeads := make(map[string][]string)
	for _, node := range dag.Nodes {
		if !isSlingableType(node.Type) {
			continue
		}
		if node.Rig == "" {
			continue
		}
		if blocked, _ := IsRigParkedOrDocked(townRoot, node.Rig); blocked {
			blockedRigBeads[node.Rig] = append(blockedRigBeads[node.Rig], node.ID)
		}
	}
	return blockedRigBeads
}

// checkBlockedRigsForLaunch checks if any target rigs are parked or docked.
// Returns an error listing all blocked rigs if any are found and force is false.
// (gt-4owfd.1)
func checkBlockedRigsForLaunch(dag *ConvoyDAG, townRoot string, force bool) error {
	blockedRigBeads := collectBlockedRigsInDAG(dag, townRoot)
	if len(blockedRigBeads) == 0 {
		return nil
	}

	// Build sorted list of blocked rigs for deterministic output
	var rigs []string
	for rig := range blockedRigBeads {
		rigs = append(rigs, rig)
	}
	sort.Strings(rigs)

	if force {
		// Warn but proceed
		fmt.Printf("Warning: %d non-operational rig(s) in convoy: %s\n", len(rigs), strings.Join(rigs, ", "))
		fmt.Printf("  Proceeding with --force (tasks may fail)\n")
		return nil
	}

	// Build detailed error message
	var details []string
	for _, rig := range rigs {
		beadIDs := blockedRigBeads[rig]
		sort.Strings(beadIDs)
		details = append(details, fmt.Sprintf("  %s: %s", rig, strings.Join(beadIDs, ", ")))
	}

	return fmt.Errorf("cannot launch: %d target rig(s) are parked or docked:\n%s\n\nUse 'gt rig unpark' or 'gt rig undock' to restore, or --force to proceed anyway",
		len(rigs), strings.Join(details, "\n"))
}

// dispatchWave1 dispatches all tasks in Wave 1 of the computed waves.
// Individual task failures do not abort remaining dispatches (I-14).
// Returns a result for every Wave 1 task and a non-nil error only if waves
// are empty or contain no Wave 1.
func dispatchWave1(convoyID string, dag *ConvoyDAG, waves []Wave, townRoot, baseBranch string) ([]DispatchResult, error) {
	if len(waves) == 0 {
		return nil, fmt.Errorf("convoy %s: no waves to dispatch", convoyID)
	}

	wave1 := waves[0]
	if wave1.Number != 1 {
		return nil, fmt.Errorf("convoy %s: first wave has unexpected number %d", convoyID, wave1.Number)
	}

	var results []DispatchResult
	for _, taskID := range wave1.Tasks {
		node := dag.Nodes[taskID]
		rig := ""
		if node != nil {
			rig = node.Rig
		}

		err := dispatchTaskDirect(townRoot, taskID, rig, baseBranch)
		results = append(results, DispatchResult{
			BeadID:  taskID,
			Rig:     rig,
			Success: err == nil,
			Error:   err,
		})
	}

	return results, nil
}

// readyUndispatchedTasks returns the slingable tasks of the DAG that still need
// a worker: status open, no assignee, and every execution blocker closed, staged
// or not (an unreadable blocker outside the staged set holds the bead). Beads
// that are hooked, in progress, assigned, or finished are never returned, which
// keeps re-launching an open convoy idempotent. held are beads whose work is
// in or through a merge request (mergeQueueHeldIssues, the same predicate the
// convoy feeder uses): a bead whose MR merged is finished even if its status
// was never closed. Result is sorted by ID.
func readyUndispatchedTasks(dag *ConvoyDAG, held mergeQueueHeld) []string {
	var ready []string
	for id, node := range dag.Nodes {
		if !isSlingableType(node.Type) || node.Status != "open" || node.Assignee != "" || held(id) {
			continue
		}
		blocked := false
		for _, blockerID := range node.BlockedBy {
			if dag.isOpenBlocker(node, blockerID) {
				blocked = true
				break
			}
		}
		if !blocked {
			ready = append(ready, id)
		}
	}
	sort.Strings(ready)
	return ready
}

// dispatchReadyTasks dispatches every ready, undispatched task of an already
// open convoy. Individual failures do not abort the remaining dispatches.
func dispatchReadyTasks(dag *ConvoyDAG, townRoot, baseBranch string) ([]DispatchResult, error) {
	held, err := mergeQueueHeldFn(townRoot)
	if err != nil {
		// Fail closed, as the feeder does: dispatching without knowing which
		// beads already merged risks re-running finished work. Surfaced as an
		// error so the launch does not look like a successful no-op.
		return nil, fmt.Errorf("holding relaunch dispatch, cannot read merge queues: %w", err)
	}
	var results []DispatchResult
	for _, taskID := range readyUndispatchedTasks(dag, held) {
		rig := dag.Nodes[taskID].Rig
		err := dispatchTaskDirect(townRoot, taskID, rig, baseBranch)
		skipped := err != nil && beadTakenByOther(taskID)
		results = append(results, DispatchResult{BeadID: taskID, Rig: rig, Success: err == nil, Skipped: skipped, Error: err})
	}
	return results, nil
}

// wakeRigFn boots a rig and nudges its witness. Tests override it.
var wakeRigFn = wakeRigAgents

// wakeDispatchedRigs wakes each rig that received at least one successful
// dispatch, once per rig. Every sling runs with --no-boot (avoids lock
// contention, same as the daemon feed), so the boot and witness nudge that
// `gt sling` would do per dispatch happen here, after the loop.
func wakeDispatchedRigs(results []DispatchResult) {
	seen := make(map[string]bool)
	var rigs []string
	for _, r := range results {
		if r.Success && r.Rig != "" && !seen[r.Rig] {
			seen[r.Rig] = true
			rigs = append(rigs, r.Rig)
		}
	}
	sort.Strings(rigs)
	for _, rig := range rigs {
		wakeRigFn(rig)
	}
}

// beadTakenByOther reports whether a bead is no longer open and unassigned,
// i.e. a concurrent sling (daemon feed, another launch) won the race. State
// based on purpose: the sling lock error text is not a stable contract.
func beadTakenByOther(beadID string) bool {
	r, err := bdShow(beadID)
	return err == nil && (r.Status != "open" || r.Assignee != "")
}

// relaunchFailures returns the dispatch results that are real failures.
func relaunchFailures(results []DispatchResult) []DispatchResult {
	var failed []DispatchResult
	for _, r := range results {
		if !r.Success && !r.Skipped {
			failed = append(failed, r)
		}
	}
	return failed
}

// baseBranchOfConvoy reads the convoy's target branch from its description
// fields, the same source the daemon feed uses.
func baseBranchOfConvoy(convoy *bdShowResult) string {
	if cf := beads.ParseConvoyFields(&beads.Issue{Description: convoy.Description}); cf != nil {
		return cf.BaseBranch
	}
	return ""
}

// renderRelaunchOutput formats the output of re-launching an open convoy.
func renderRelaunchOutput(convoyID string, results []DispatchResult, dag *ConvoyDAG) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Convoy already open: %s\n", convoyID)
	if len(results) == 0 {
		b.WriteString("  Nothing to dispatch: no ready bead without an active worker.\n")
		return b.String()
	}
	b.WriteString("Re-dispatched stranded beads:\n")
	for _, r := range results {
		marker := "✓"
		errInfo := ""
		if r.Skipped {
			marker = "○"
			errInfo = "    skipped: already taken by another dispatcher"
		} else if !r.Success {
			marker = "✗"
			errInfo = fmt.Sprintf("    error: %v", r.Error)
		}
		fmt.Fprintf(&b, "  %s %s  %s%s\n", marker, r.BeadID, dag.Nodes[r.BeadID].Title, errInfo)
	}
	return b.String()
}

// renderLaunchOutput formats the post-launch console output showing convoy ID,
// wave summary, dispatched tasks with status, and helpful hints.
func renderLaunchOutput(convoyID string, waves []Wave, results []DispatchResult, dag *ConvoyDAG) string {
	var b strings.Builder

	// Section 1: Convoy ID with status.
	fmt.Fprintf(&b, "Convoy launched: %s (status: open)\n", convoyID)
	b.WriteString("\n")

	// Section 2: Monitor command hint.
	fmt.Fprintf(&b, "  Monitor: gt convoy status %s\n", convoyID)
	b.WriteString("\n")

	// Section 3: Wave summary.
	totalTasks := 0
	for _, w := range waves {
		totalTasks += len(w.Tasks)
	}
	b.WriteString("Wave summary:\n")
	fmt.Fprintf(&b, "  %d waves, %d tasks total\n", len(waves), totalTasks)
	for _, w := range waves {
		status := "pending"
		if w.Number == 1 {
			status = "dispatched"
		}
		fmt.Fprintf(&b, "  Wave %d: %d tasks (%s)\n", w.Number, len(w.Tasks), status)
	}
	b.WriteString("\n")

	// Section 4: Dispatched tasks (Wave 1).
	b.WriteString("Dispatched (Wave 1):\n")

	// Sort results by BeadID for deterministic output.
	sorted := make([]DispatchResult, len(results))
	copy(sorted, results)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].BeadID < sorted[j].BeadID
	})

	for _, r := range sorted {
		marker := "✓"
		if !r.Success {
			marker = "✗"
		}

		title := ""
		if node := dag.Nodes[r.BeadID]; node != nil {
			title = node.Title
		}

		rigInfo := ""
		if r.Rig != "" {
			rigInfo = fmt.Sprintf("  (rig: %s)", r.Rig)
		}

		errInfo := ""
		if r.Error != nil {
			errInfo = fmt.Sprintf("    error: %v", r.Error)
		}

		fmt.Fprintf(&b, "  %s %s  %s%s%s\n", marker, r.BeadID, title, rigInfo, errInfo)
	}
	b.WriteString("\n")

	// Section 5: TUI hint.
	b.WriteString("  Hint: gt convoy -i for interactive monitoring\n")
	b.WriteString("\n")

	// Section 6: Daemon explanation.
	b.WriteString("Subsequent waves will be dispatched automatically by the daemon as tasks complete.\n")

	return b.String()
}

// isLaunchableStatus reports whether `gt convoy launch <convoy-id>` acts on a
// convoy in this status: staged (first launch) or open (idempotent re-launch).
func isLaunchableStatus(status string) bool {
	return isStagedStatus(status) || status == convoyStatusOpen
}

// runConvoyLaunch is the handler for `gt convoy launch`.
func runConvoyLaunch(cmd *cobra.Command, args []string) error {
	// Step 1: Validate args.
	if err := validateStageArgs(args); err != nil {
		return err
	}

	// Step 2: Resolve bead types via bd show for each arg.
	beadTypes := make(map[string]*bdShowResult)
	for _, arg := range args {
		result, err := bdShow(arg)
		if err != nil {
			return fmt.Errorf("cannot resolve bead %s: %w", arg, err)
		}
		beadTypes[arg] = result
	}

	// Step 3: If single arg is a convoy with staged status, transition to open
	// and dispatch Wave 1.
	if len(args) == 1 {
		result := beadTypes[args[0]]
		if isConvoyIssue(result.IssueType, result.Labels) && isLaunchableStatus(normalizeConvoyStatus(result.Status)) {
			convoyID := args[0]
			baseBranch := baseBranchOfConvoy(result)
			alreadyOpen := normalizeConvoyStatus(result.Status) == convoyStatusOpen

			if err := transitionConvoyToOpen(convoyID, convoyLaunchForce); err != nil {
				return err
			}

			// Rebuild DAG from tracked beads and dispatch Wave 1.
			beads, deps, err := collectConvoyBeads(convoyID)
			if err != nil {
				return fmt.Errorf("collect beads for dispatch: %w", err)
			}

			dag := buildConvoyDAG(beads, deps)
			waves, _, err := computeWaves(dag)
			if err != nil {
				return fmt.Errorf("compute waves for dispatch: %w", err)
			}

			townRoot, err := workspace.FindFromCwdOrError()
			if err != nil {
				return fmt.Errorf("resolve town root for dispatch: %w", err)
			}

			// Check for parked/docked rigs before dispatch (gt-4owfd.1, #2120)
			if err := checkBlockedRigsForLaunch(dag, townRoot, convoyLaunchForce); err != nil {
				return err
			}

			if alreadyOpen {
				// Recovery: convoy was launched before; only (re)dispatch ready
				// beads that have no worker yet.
				results, err := dispatchReadyTasks(dag, townRoot, baseBranch)
				if err != nil {
					return fmt.Errorf("relaunch %s: %w", convoyID, err)
				}
				wakeDispatchedRigs(results)
				fmt.Print(renderRelaunchOutput(convoyID, results, dag))
				if failed := relaunchFailures(results); len(failed) > 0 {
					return fmt.Errorf("relaunch %s: %d of %d dispatches failed", convoyID, len(failed), len(results))
				}
				return nil
			}

			results, err := dispatchWave1(convoyID, dag, waves, townRoot, baseBranch)
			if err != nil {
				return fmt.Errorf("dispatch wave 1: %w", err)
			}

			wakeDispatchedRigs(results)

			// Report results.
			fmt.Print(renderLaunchOutput(convoyID, waves, results, dag))
			return nil
		}
	}

	// Step 4: For non-convoy or non-staged input, delegate to stage+launch flow.
	// Set the --launch flag on convoyStageCmd and delegate to runConvoyStage.
	convoyStageLaunch = true
	defer func() { convoyStageLaunch = false }()
	return runConvoyStage(cmd, args)
}
