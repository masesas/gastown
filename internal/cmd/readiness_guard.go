package cmd

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/events"
	"github.com/steveyegge/gastown/internal/style"
)

// Readiness decision outcomes. The empty outcome means "ready or not checked":
// nothing to report.
const (
	readinessWarned     = "warned"
	readinessRejected   = "rejected"
	readinessOverridden = "overridden"

	// readinessReadError is the problem code used when the bead could not be read.
	readinessReadError = "read_error"

	// readinessRejectedErrMsg is SlingResult.ErrMsg for a readiness rejection.
	readinessRejectedErrMsg = "not_ready"
)

// readinessDecision is the verdict of checkReadiness.
type readinessDecision struct {
	Allow    bool
	Outcome  string // "", "warned", "rejected", "overridden"
	Problems []string
}

// checkReadiness decides whether a bead may be dispatched. It performs no I/O;
// callers emit events and print. force/reason come from --force-unready/--reason.
func checkReadiness(mode config.ReadinessMode, info *beadInfo, readErr error, force bool, reason string) (readinessDecision, error) {
	if force && reason == "" {
		return readinessDecision{}, errors.New("--force-unready requires --reason")
	}
	if mode == config.ReadinessOff {
		return readinessDecision{Allow: true}, nil
	}

	var problems []string
	switch {
	case readErr != nil || info == nil:
		problems = []string{readinessReadError}
	default:
		problems = beads.ReadinessProblems(info.IssueType, info.Description, info.AcceptanceCriteria)
	}
	if len(problems) == 0 {
		return readinessDecision{Allow: true}, nil
	}

	if mode == config.ReadinessEnforce {
		if force {
			return readinessDecision{Allow: true, Outcome: readinessOverridden, Problems: problems}, nil
		}
		return readinessDecision{Outcome: readinessRejected, Problems: problems}, nil
	}
	return readinessDecision{Allow: true, Outcome: readinessWarned, Problems: problems}, nil
}

// readinessModeForRig returns the readiness mode of rigName, or the default
// when the bead does not belong to a rig.
func readinessModeForRig(townRoot, rigName string) config.ReadinessMode {
	if rigName == "" {
		return config.DefaultReadinessMode
	}
	return config.GetReadinessMode(filepath.Join(townRoot, rigName))
}

// enforceReadiness runs the readiness check for beadID against the readiness
// mode of rigName, prints warnings, records an override comment, and emits a
// task_readiness event (best-effort). It returns an error when dispatch must
// be refused. info/readErr are the result of reading the bead.
func enforceReadiness(townRoot, rigName, beadID string, info *beadInfo, readErr error, force bool, reason string) error {
	mode := readinessModeForRig(townRoot, rigName)
	decision, err := checkReadiness(mode, info, readErr, force, reason)
	if err != nil {
		return err
	}
	if decision.Outcome == "" {
		return nil
	}

	actor := detectActor()
	problems := strings.Join(decision.Problems, ", ")
	payloadReason := ""
	switch decision.Outcome {
	case readinessWarned:
		fmt.Printf("%s Bead %s is not ready for dispatch: %s\n", style.Warning.Render("⚠"), beadID, problems)
	case readinessOverridden:
		payloadReason = reason
		fmt.Printf("%s Overriding readiness check for %s (%s): %s\n", style.Warning.Render("⚠"), beadID, problems, reason)
		recordReadinessOverride(townRoot, beadID, actor, reason)
	}
	_ = events.LogFeedAt(townRoot, events.TypeTaskReadiness, actor,
		events.TaskReadinessPayload(beadID, rigName, string(mode), decision.Outcome, decision.Problems, actor, payloadReason))

	if decision.Allow {
		return nil
	}
	return fmt.Errorf("bead %s is not ready for dispatch: %s\nAdd a description and an acceptance criteria checklist (- [ ] item), or override with --force-unready --reason \"<why>\"", beadID, problems)
}

// recordReadinessOverride appends the override as a bead comment. Failure is
// reported but does not block the override: the event still records it.
func recordReadinessOverride(townRoot, beadID, actor, reason string) {
	beadsDir := beads.ResolveBeadsDirForID(filepath.Join(townRoot, ".beads"), beadID)
	b := beads.NewWithBeadsDir(filepath.Dir(beadsDir), beadsDir)
	if err := b.AddComment(beadID, fmt.Sprintf("readiness override by %s: %s", actor, reason)); err != nil {
		fmt.Printf("%s Could not record readiness override comment on %s: %v\n", style.Dim.Render("Warning:"), beadID, err)
	}
}
