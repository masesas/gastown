package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/style"
)

// doneTargetOrigin records which rule picked the landing target branch.
type doneTargetOrigin int

const (
	doneTargetFromDefault doneTargetOrigin = iota
	doneTargetFromExplicit
	doneTargetFromFormulaVars
	doneTargetFromIntegration
)

// resolveDoneTarget is the single target-branch resolver for completed work:
// the merge-queue path (gt done MR, gt mq submit) and the direct-push path all
// land on the branch it returns. Priority:
//
//  1. explicit target (gt done --target, gt mq submit --epic)
//  2. base_branch in the source bead's formula_vars (gt sling --base-branch)
//  3. integration branch of the bead's parent epic (detectIntegration; nil = disabled)
//  4. the rig's default branch
func resolveDoneTarget(defaultBranch, explicitTarget string, source *beads.Issue, detectIntegration func() string) (string, doneTargetOrigin) {
	if explicitTarget != "" {
		return explicitTarget, doneTargetFromExplicit
	}
	if af := beads.ParseAttachmentFields(source); af != nil {
		// Same view of the vars the polecat was primed with (attached_vars over formula_vars).
		if bb := extractFormulaVar(strings.Join(attachmentFormulaVars(af), "\n"), "base_branch"); bb != "" && bb != defaultBranch {
			return bb, doneTargetFromFormulaVars
		}
	}
	if detectIntegration != nil {
		if auto := detectIntegration(); auto != "" {
			return auto, doneTargetFromIntegration
		}
	}
	return defaultBranch, doneTargetFromDefault
}

// rigIntegrationDetector returns the integration-branch auto-detection step of
// resolveDoneTarget for a rig, or nil when the rig disables it
// (merge_queue.integration_branch_refinery_enabled=false). onErr, when set,
// receives detection errors; detection failures fall back to the next rule.
func rigIntegrationDetector(townRoot, rigName string, bd *beads.Beads, g *git.Git, issueID string, onErr func(error)) func() string {
	if bd == nil || issueID == "" {
		// DetectIntegrationBranch dereferences bd; without a source there is no epic to walk.
		return nil
	}
	if settings, err := config.LoadRigSettings(config.RigSettingsPath(filepath.Join(townRoot, rigName))); err == nil && settings.MergeQueue != nil &&
		!settings.MergeQueue.IsRefineryIntegrationEnabled() {
		return nil
	}
	return func() string {
		target, err := beads.DetectIntegrationBranch(bd, g, issueID)
		if err != nil {
			if onErr != nil {
				onErr(err)
			}
			return ""
		}
		return target
	}
}

// printDoneTarget reports which rule picked a non-default target branch.
func printDoneTarget(target string, origin doneTargetOrigin, explicitLabel string) {
	switch origin {
	case doneTargetFromExplicit:
		fmt.Printf("  Target branch: %s (from %s)\n", target, explicitLabel)
	case doneTargetFromFormulaVars:
		fmt.Printf("  Target branch override: %s (from formula_vars)\n", target)
	case doneTargetFromIntegration:
		fmt.Printf("  Target branch: %s (integration branch of parent epic)\n", target)
	case doneTargetFromDefault:
		// Default target needs no announcement.
	}
}

// noteIntegrationDetectError prints a non-fatal integration detection error.
func noteIntegrationDetectError(err error) {
	fmt.Printf("  %s\n", style.Dim.Render(fmt.Sprintf("(note: %v)", err)))
}
