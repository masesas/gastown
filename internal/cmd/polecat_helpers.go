package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/polecat"
	"github.com/steveyegge/gastown/internal/rig"
	"github.com/steveyegge/gastown/internal/style"
)

// polecatTarget represents a polecat to operate on.
type polecatTarget struct {
	rigName     string
	polecatName string
	mgr         *polecat.Manager
	r           *rig.Rig
}

// resolvePolecatTargets builds a list of polecats from command args.
// If useAll is true, the first arg is treated as a rig name and all polecats in it are returned.
// Otherwise, args are parsed as rig/polecat addresses.
func resolvePolecatTargets(args []string, useAll bool) ([]polecatTarget, error) {
	var targets []polecatTarget

	if useAll {
		// --all flag: first arg is just the rig name
		rigName := args[0]
		// Check if it looks like rig/polecat format
		if _, _, err := parseAddress(rigName); err == nil {
			return nil, fmt.Errorf("with --all, provide just the rig name (e.g., 'gt polecat <cmd> %s --all')", strings.Split(rigName, "/")[0])
		}

		mgr, r, err := getPolecatManager(rigName)
		if err != nil {
			return nil, err
		}

		polecats, err := mgr.List()
		if err != nil {
			return nil, fmt.Errorf("listing polecats: %w", err)
		}

		for _, p := range polecats {
			targets = append(targets, polecatTarget{
				rigName:     rigName,
				polecatName: p.Name,
				mgr:         mgr,
				r:           r,
			})
		}
	} else {
		// Multiple rig/polecat arguments - require explicit rig/polecat format
		for _, arg := range args {
			// Validate format: must contain "/" to avoid misinterpreting rig names as polecat names
			if !strings.Contains(arg, "/") {
				return nil, fmt.Errorf("invalid address '%s': must be in 'rig/polecat' format (e.g., 'gastown/Toast')", arg)
			}

			rigName, polecatName, err := parseAddress(arg)
			if err != nil {
				return nil, fmt.Errorf("invalid address '%s': %w", arg, err)
			}

			mgr, r, err := getPolecatManager(rigName)
			if err != nil {
				return nil, err
			}

			targets = append(targets, polecatTarget{
				rigName:     rigName,
				polecatName: polecatName,
				mgr:         mgr,
				r:           r,
			})
		}
	}

	return targets, nil
}

// SafetyCheckResult holds the result of safety checks for a polecat.
type SafetyCheckResult struct {
	Polecat       string
	Blocked       bool
	Reasons       []string
	CleanupStatus polecat.CleanupStatus
	HookBead      string
	HookStale     bool // true if hooked bead is closed
	ActiveMR      string
	OpenMR        string
	AssignedIssue string // open work bead still assigned to the polecat
	GitState      *GitState
	// AgentBeadUnreadable: the agent bead could not be read, so the hook and
	// cleanup_status checks did not run (gt-pd1c.78: nuke --force by agents).
	AgentBeadUnreadable bool
}

// checkPolecatSafety performs safety checks before destructive operations.
// Returns nil if the polecat is safe to operate on, or a SafetyCheckResult with reasons if blocked.
func checkPolecatSafety(target polecatTarget) *SafetyCheckResult {
	result := &SafetyCheckResult{
		Polecat: fmt.Sprintf("%s/%s", target.rigName, target.polecatName),
	}

	// Get polecat info for branch name
	polecatInfo, infoErr := target.mgr.Get(target.polecatName)

	// Check 1: work at risk in the worktree, from the git state gt computes
	// now. The agent bead's cleanup_status never replaces it (gt-pd1c.96):
	// below it can only add a blocker.
	if !errors.Is(infoErr, polecat.ErrPolecatNotFound) {
		clonePath := target.mgr.ClonePath(target.polecatName)
		if infoErr == nil && polecatInfo != nil {
			clonePath = polecatInfo.ClonePath
		}
		gitState, gitErr := getGitState(clonePath)
		result.GitState = gitState
		result.Reasons = append(result.Reasons, gitStateSafetyReasons(gitState, gitErr)...)
	}

	bd := beads.New(target.r.Path)
	agentBeadID := polecatBeadIDForRig(target.r, target.rigName, target.polecatName)
	agentIssue, fields, err := bd.GetAgentBead(agentBeadID)
	if blocker := agentBeadLookupBlocker(err); blocker != "" {
		result.Reasons = append(result.Reasons, blocker)
		result.AgentBeadUnreadable = true
	}

	if err == nil && fields != nil {
		currentIssue := ""
		if infoErr == nil && polecatInfo != nil {
			currentIssue = polecatInfo.Issue
		}
		sourceHint := agentSourceIssueHint(currentIssue, fields)
		hookBead := agentHookBead(agentIssue, fields)
		activeMRAssessment := polecat.ActiveMRAssessment{}
		if fields.ActiveMR != "" {
			gitSafe := false
			if polecatInfo != nil {
				gitSafe = activeMRGitSafeForWorktree(polecatInfo.ClonePath)
			}
			activeMRAssessment = polecat.AssessActiveMR(bd, polecat.ActiveMRInput{ActiveMR: fields.ActiveMR, SourceIssueHint: sourceHint, RequireGitSafe: true, GitSafe: gitSafe})
		}
		beadTerminal := isAssignedBeadTerminal(bd, sourceHint)
		if activeMRAssessment.SourceTerminal {
			beadTerminal = true
		}

		// cleanup_status from the agent bead: a hint. "clean" adds nothing
		// (Check 1 already decided from git); a dirty value blocks unless it
		// is provably stale.
		result.CleanupStatus = polecat.CleanupStatus(fields.CleanupStatus)
		switch result.CleanupStatus {
		case polecat.CleanupClean:
			// No extra blocker.
		default:
			gitSafe := false
			if polecatInfo != nil {
				gitSafe = activeMRGitSafeForWorktree(polecatInfo.ClonePath)
			}
			hookSafe, hookTerminal, _ := hookBeadSafeForCleanup(bd, hookBead)
			activeMRSafe := !activeMRAssessment.Pending
			if polecat.CanIgnoreStaleCleanupStatus(result.CleanupStatus, beadTerminal || hookTerminal, hookSafe, activeMRSafe, gitSafe) {
				// OK: stale self-report after terminal source and direct clean git.
			} else {
				result.Reasons = append(result.Reasons, cleanupStatusBlocker(result.CleanupStatus))
			}
		}

		// Check 3: Work on hook
		if hookBead != "" {
			result.HookBead = hookBead
			// Check if hooked bead is still active (not closed)
			hookedIssue, err := bd.Show(hookBead)
			if err == nil && hookedIssue != nil {
				if hookedIssue.Status != "closed" {
					result.Reasons = append(result.Reasons, fmt.Sprintf("has work on hook (%s)", hookBead))
				} else {
					result.HookStale = true
				}
			} else {
				result.Reasons = append(result.Reasons, fmt.Sprintf("has work on hook (%s, unverified)", hookBead))
			}
		}

		if fields.ActiveMR != "" {
			result.ActiveMR = fields.ActiveMR
			if blocker := activeMRAssessment.Reason; activeMRAssessment.Pending && blocker != "" {
				result.Reasons = append(result.Reasons, blocker)
			}
		}
	}

	// Check 4: Open work still assigned to this polecat (hooked/in_progress/open).
	assignee := fmt.Sprintf("%s/polecats/%s", target.rigName, target.polecatName)
	assignedIssue, assignedErr := bd.GetAssignedIssue(assignee)
	if blocker := assignedWorkBlocker(assignedIssue, assignedErr); blocker != "" {
		if assignedIssue != nil {
			result.AssignedIssue = assignedIssue.ID
		}
		result.Reasons = append(result.Reasons, blocker)
	}

	// Check 2: Open MR beads for this branch
	if infoErr == nil && polecatInfo != nil && polecatInfo.Branch != "" {
		mr, mrErr := bd.FindMRForBranch(polecatInfo.Branch)
		if mrErr != nil {
			result.Reasons = append(result.Reasons, fmt.Sprintf("open_mr_lookup_error: %v", mrErr))
		} else if mr != nil {
			result.OpenMR = mr.ID
			result.Reasons = append(result.Reasons, fmt.Sprintf("has open MR (%s)", mr.ID))
		}
	}

	result.Blocked = len(result.Reasons) > 0
	return result
}

// gitStateSafetyReasons turns the worktree git state into nuke blockers. An
// unreadable state blocks (fail closed, gt-pd1c.95).
func gitStateSafetyReasons(state *GitState, err error) []string {
	if err != nil {
		return []string{fmt.Sprintf("cannot check git state: %v", err)}
	}
	if state == nil || state.Clean {
		return nil
	}
	switch {
	case state.UnpushedCommits > 0:
		return []string{fmt.Sprintf("has %d unpushed commit(s)", state.UnpushedCommits)}
	case len(state.UncommittedFiles) > 0:
		return []string{fmt.Sprintf("has %d uncommitted file(s)", len(state.UncommittedFiles))}
	case state.StashCount > 0:
		return []string{fmt.Sprintf("has %d stash(es)", state.StashCount)}
	}
	return []string{"git state is not clean"}
}

func rigPrefix(r *rig.Rig) string {
	townRoot := filepath.Dir(r.Path)
	return beads.GetPrefixForRig(townRoot, r.Name)
}

func polecatBeadIDForRig(r *rig.Rig, rigName, polecatName string) string {
	return beads.PolecatBeadIDWithPrefix(rigPrefix(r), rigName, polecatName)
}

// displaySafetyCheckBlocked prints blocked polecats and guidance.
func displaySafetyCheckBlocked(blocked []*SafetyCheckResult) {
	displaySafetyCheckBlockedTo(os.Stderr, blocked)
}

func displaySafetyCheckBlockedTo(w io.Writer, blocked []*SafetyCheckResult) {
	fmt.Fprintf(w, "%s Cannot nuke the following polecats:\n\n", style.Error.Render("Error:"))
	var polecatList []string
	for _, b := range blocked {
		fmt.Fprintf(w, "  %s:\n", style.Bold.Render(b.Polecat))
		for _, r := range b.Reasons {
			fmt.Fprintf(w, "    - %s\n", r)
		}
		polecatList = append(polecatList, b.Polecat)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Safety checks failed. Resolve issues before nuking, or use --force.")
	fmt.Fprintln(w, "Options:")
	fmt.Fprintln(w, "  1. Complete work: gt done (from polecat session)")
	fmt.Fprintln(w, "  2. Push changes: git push (from polecat worktree)")
	fmt.Fprintln(w, "  3. Escalate: gt mail send mayor/ -s \"RECOVERY_NEEDED\" -m \"...\"")
	fmt.Fprintf(w, "  4. Force nuke (LOSES WORK): gt polecat nuke --force %s\n", strings.Join(polecatList, " "))
	fmt.Fprintln(w, "     (agents only when the branch is provably safe to delete; otherwise a human or crew at a terminal)")
	fmt.Fprintln(w)
}

func formatSafetyCheckBlockers(blocked []*SafetyCheckResult) string {
	parts := make([]string, 0, len(blocked))
	for _, b := range blocked {
		parts = append(parts, fmt.Sprintf("%s: %s", b.Polecat, strings.Join(b.Reasons, "; ")))
	}
	return strings.Join(parts, " | ")
}

// displayDryRunSafetyCheck shows safety check status for dry-run mode. It returns true when a normal nuke would refuse.
func displayDryRunSafetyCheck(target polecatTarget) bool {
	fmt.Printf("\n  Safety checks:\n")
	result := checkPolecatSafety(target)
	polecatInfo, infoErr := target.mgr.Get(target.polecatName)
	bd := beads.New(target.r.Path)
	agentBeadID := polecatBeadIDForRig(target.r, target.rigName, target.polecatName)
	agentIssue, fields, err := bd.GetAgentBead(agentBeadID)

	// Check 1: the git state decides (gt-pd1c.96); the bead's cleanup_status
	// below is only the agent-reported hint.
	if infoErr == nil && polecatInfo != nil {
		gitState, gitErr := getGitState(polecatInfo.ClonePath)
		if gitErr != nil {
			fmt.Printf("    - Git state: %s\n", style.Warning.Render("cannot check"))
		} else if gitState.Clean {
			fmt.Printf("    - Git state: %s\n", style.Success.Render("clean"))
		} else {
			fmt.Printf("    - Git state: %s\n", style.Error.Render("dirty"))
		}
	} else {
		fmt.Printf("    - Git state: %s\n", style.Dim.Render("unknown (no polecat info)"))
	}
	if err != nil || fields == nil {
		fmt.Printf("    - Hook: %s\n", style.Dim.Render("unknown (no agent bead)"))
	} else {
		cleanupStatus := polecat.CleanupStatus(fields.CleanupStatus)
		if cleanupStatus.IsSafe() {
			fmt.Printf("    - Cleanup status (hint): %s\n", style.Success.Render(string(cleanupStatus)))
		} else if cleanupStatus.RequiresRecovery() {
			fmt.Printf("    - Cleanup status (hint): %s\n", style.Error.Render(string(cleanupStatus)))
		} else {
			statusText := string(cleanupStatus)
			if statusText == "" {
				statusText = "<missing>"
			}
			fmt.Printf("    - Cleanup status (hint): %s\n", style.Warning.Render(statusText))
		}

		hookBead := agentIssue.HookBead
		if hookBead == "" {
			hookBead = fields.HookBead
		}
		if hookBead != "" {
			hookedIssue, err := bd.Show(hookBead)
			if err == nil && hookedIssue != nil && hookedIssue.Status == "closed" {
				fmt.Printf("    - Hook: %s (%s, closed - stale)\n", style.Warning.Render("stale"), hookBead)
			} else {
				fmt.Printf("    - Hook: %s (%s)\n", style.Error.Render("has work"), hookBead)
			}
		} else {
			fmt.Printf("    - Hook: %s\n", style.Success.Render("empty"))
		}

		if fields.ActiveMR != "" {
			sourceHint := agentSourceIssueHint("", fields)
			gitSafe := false
			if infoErr == nil && polecatInfo != nil {
				gitSafe = activeMRGitSafeForWorktree(polecatInfo.ClonePath)
			}
			if blocker := activeMRBlocker(bd, fields.ActiveMR, sourceHint, true, gitSafe); blocker != "" {
				fmt.Printf("    - Active MR: %s (%s)\n", style.Error.Render("blocked"), blocker)
			} else {
				fmt.Printf("    - Active MR: %s (%s)\n", style.Success.Render("terminal"), fields.ActiveMR)
			}
		}
	}

	// Check 2: Open MR
	if infoErr == nil && polecatInfo != nil && polecatInfo.Branch != "" {
		mr, mrErr := bd.FindMRForBranch(polecatInfo.Branch)
		if mrErr == nil && mr != nil {
			fmt.Printf("    - Open MR: %s (%s)\n", style.Error.Render("yes"), mr.ID)
		} else {
			fmt.Printf("    - Open MR: %s\n", style.Success.Render("none"))
		}
	} else {
		fmt.Printf("    - Open MR: %s\n", style.Dim.Render("unknown (no branch info)"))
	}

	// Check 4: Assigned work (from the safety result, which failed closed on errors)
	assignedBlocker := ""
	for _, reason := range result.Reasons {
		if strings.HasPrefix(reason, "has assigned work") || strings.HasPrefix(reason, "assigned work unknown") {
			assignedBlocker = reason
		}
	}
	if assignedBlocker != "" {
		fmt.Printf("    - Assigned work: %s (%s)\n", style.Error.Render("blocked"), assignedBlocker)
	} else {
		fmt.Printf("    - Assigned work: %s\n", style.Success.Render("none"))
	}

	return result.Blocked
}

// assignedWorkBlocker returns a nuke blocker when the polecat still owns open
// work, or when that cannot be determined (fail closed). The agent bead's
// hook_bead is no longer maintained, so the assigned work bead is the
// authoritative signal.
func assignedWorkBlocker(issue *beads.Issue, lookupErr error) string {
	if lookupErr != nil {
		return fmt.Sprintf("assigned work unknown: %v", lookupErr)
	}
	if issue != nil {
		return fmt.Sprintf("has assigned work (%s, %s)", issue.ID, issue.Status)
	}
	return ""
}

// agentBeadLookupBlocker returns a nuke blocker when reading the agent bead
// failed. A missing agent bead is not an error (GetAgentBead returns nil).
func agentBeadLookupBlocker(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("agent bead lookup failed: %v", err)
}
