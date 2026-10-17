package cmd

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ExternalBlocker is an execution blocker of a staged slingable bead that is
// not itself part of the staged set: a bead outside the epic tree or task
// list, or a child that `bd list --parent` does not return (bd 1.3.0 hides
// gate-type beads even with --all). It is never staged, tracked or dispatched
// by the convoy. It only gates its consumers until it reaches a terminal
// status (gt-pd1c.43).
type ExternalBlocker struct {
	ID     string
	Title  string
	Type   string // issue type reported by bd show; "" when unresolved
	Status string // status reported by bd show; "" when unresolved
	Err    error  // non-nil when the status could not be read (fail-closed: gates)
}

// externalBlockerLookupFn resolves a blocker outside the staged set. It is the
// prefix-routed `bd show --json` lookup used for every other staged bead;
// tests override it.
var externalBlockerLookupFn = bdShow

// isConvoyExecutionDep reports whether a dependency type orders execution in a
// convoy. Matches the feeder's blockingDepTypes (internal/convoy).
func isConvoyExecutionDep(depType string) bool {
	switch depType {
	case "blocks", "conditional-blocks", "waits-for", "merge-blocks":
		return true
	}
	return false
}

// isTerminalBlockerStatus reports whether a blocker in this status no longer
// blocks its dependents. Same rule as the feeder's isIssueBlocked.
func isTerminalBlockerStatus(status string) bool {
	return status == "closed" || status == "tombstone"
}

// lookupExternalBlocker resolves one blocker outside the staged set. A failed
// or empty lookup yields an unresolved blocker, which gates (fail-closed).
func lookupExternalBlocker(id string) *ExternalBlocker {
	res, err := externalBlockerLookupFn(id)
	if err == nil && res == nil {
		err = errors.New("no result")
	}
	if err != nil {
		return &ExternalBlocker{ID: id, Err: err}
	}
	return &ExternalBlocker{ID: id, Title: res.Title, Type: res.IssueType, Status: res.Status}
}

// addExternalBlocker records an execution edge from a blocker outside the
// staged set to an in-set dependent. Each blocker is looked up once per DAG.
func (dag *ConvoyDAG) addExternalBlocker(to *ConvoyDAGNode, blockerID string) {
	if _, seen := dag.External[blockerID]; !seen {
		dag.External[blockerID] = lookupExternalBlocker(blockerID)
	}
	for _, existing := range to.BlockedBy {
		if existing == blockerID {
			return
		}
	}
	to.BlockedBy = append(to.BlockedBy, blockerID)
}

// isOpenBlocker reports whether blockerID still blocks its dependents, for a
// staged node or a blocker outside the staged set alike. Unresolved external
// blockers count as open. IDs the DAG knows nothing about are not blockers;
// buildConvoyDAG records every execution edge of a slingable bead, so that
// only happens for hand-built DAGs.
func (dag *ConvoyDAG) isOpenBlocker(blockerID string) bool {
	if node, ok := dag.Nodes[blockerID]; ok {
		return !isTerminalBlockerStatus(node.Status)
	}
	if ext, ok := dag.External[blockerID]; ok {
		return ext.Err != nil || !isTerminalBlockerStatus(ext.Status)
	}
	return false
}

// knowsBlocker reports whether blockerID is a staged node or a recorded
// external blocker.
func (dag *ConvoyDAG) knowsBlocker(blockerID string) bool {
	if _, ok := dag.Nodes[blockerID]; ok {
		return true
	}
	_, ok := dag.External[blockerID]
	return ok
}

// describeBlocker renders a blocker ID for output, marking blockers outside
// the staged set with their type and status.
func (dag *ConvoyDAG) describeBlocker(blockerID string) string {
	ext, ok := dag.External[blockerID]
	if !ok {
		return blockerID
	}
	if ext.Err != nil {
		return fmt.Sprintf("%s (outside staged set: status unreadable)", blockerID)
	}
	typ := ext.Type
	if typ == "" {
		typ = "task"
	}
	return fmt.Sprintf("%s (outside staged set: %s, %s)", blockerID, typ, ext.Status)
}

func (dag *ConvoyDAG) describeBlockers(ids []string) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, dag.describeBlocker(id))
	}
	return strings.Join(parts, ", ")
}

// gatedFindings turns gated tasks into staging warnings. Shared by the stage
// (text and JSON) and mountain paths.
func gatedFindings(gated []GatedTask, dag *ConvoyDAG) []StagingFinding {
	findings := make([]StagingFinding, 0, len(gated))
	for _, g := range gated {
		if len(g.GatedBy) == 0 {
			findings = append(findings, StagingFinding{
				Severity:     "warning",
				Category:     "gated",
				BeadIDs:      []string{g.TaskID},
				Message:      fmt.Sprintf("task %s is transitively gated: one of its blockers is itself gated", g.TaskID),
				SuggestedFix: fmt.Sprintf("resolve the gates of the tasks blocking %s", g.TaskID),
			})
			continue
		}
		ids := strings.Join(g.GatedBy, ", ")
		findings = append(findings, StagingFinding{
			Severity:     "warning",
			Category:     "gated",
			BeadIDs:      []string{g.TaskID},
			Message:      fmt.Sprintf("task %s is gated by open blocker(s) this convoy does not dispatch: %s", g.TaskID, dag.describeBlockers(g.GatedBy)),
			SuggestedFix: fmt.Sprintf("close or tombstone %s to include %s in waves", ids, g.TaskID),
		})
	}
	return findings
}

// detectUnresolvedBlockers warns once per external blocker whose status could
// not be read. Its consumers stay gated (fail-closed). The lookup error is not
// echoed: it can carry raw bd output.
func detectUnresolvedBlockers(dag *ConvoyDAG) []StagingFinding {
	var findings []StagingFinding
	for _, id := range sortedExternalIDs(dag) {
		if dag.External[id].Err == nil {
			continue
		}
		consumers := externalBlockerConsumers(dag, id)
		findings = append(findings, StagingFinding{
			Severity:     "warning",
			Category:     "unresolved-blocker",
			BeadIDs:      append([]string{id}, consumers...),
			Message:      fmt.Sprintf("status of blocker %s (outside staged set) could not be read; holding %s (fail-closed)", id, strings.Join(consumers, ", ")),
			SuggestedFix: fmt.Sprintf("check that %s exists and its prefix is routed (bd show %s), then re-stage", id, id),
		})
	}
	return findings
}

func sortedExternalIDs(dag *ConvoyDAG) []string {
	ids := make([]string, 0, len(dag.External))
	for id := range dag.External {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// externalBlockerConsumers returns the sorted staged beads blocked by id.
func externalBlockerConsumers(dag *ConvoyDAG, id string) []string {
	var consumers []string
	for nodeID, node := range dag.Nodes {
		for _, b := range node.BlockedBy {
			if b == id {
				consumers = append(consumers, nodeID)
				break
			}
		}
	}
	sort.Strings(consumers)
	return consumers
}

// buildExternalBlockersJSON details the gating IDs that are outside the
// staged set. Returns nil when there are none.
func buildExternalBlockersJSON(gatedBy []string, dag *ConvoyDAG) []ExternalBlockerJSON {
	var out []ExternalBlockerJSON
	for _, id := range gatedBy {
		ext, ok := dag.External[id]
		if !ok {
			continue
		}
		out = append(out, ExternalBlockerJSON{ID: id, Type: ext.Type, Status: ext.Status, Resolved: ext.Err == nil})
	}
	return out
}
