package refinery

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/steveyegge/gastown/internal/beads"
)

type terminalMRCloseOptions struct {
	Reason        string
	MergeCommit   string
	AgentBeadHint string
	MissingOK     bool
	ExpectedMR    *MergeRequest
	// Target and Out only matter for merged closes: Target is recorded in the
	// source bead's close reason, Out receives its progress/warning lines.
	Target string
	Out    io.Writer
	// Prover proves from git that the MR's commit is on its target branch. It
	// lets an MR closed by hand (bare `bd close`, close_reason not "merged")
	// still close its source bead. Optional.
	Prover beads.MergeProver
}

type terminalMRCloseResult struct {
	MRID                  string
	SourceIssue           string
	AgentBead             string
	Closed                bool
	AlreadyTerminal       bool
	AgentActiveMRCleared  bool
	AgentActiveMRClearErr error

	// SourceClose is the outcome of closing the source (work) bead; it is only
	// meaningful when SourceCloseAttempted (the MR is merged-terminal).
	SourceClose          mergedWorkBeadCloseResult
	SourceCloseAttempted bool

	// SourceRelease is set when this call closed the MR without landing and
	// handed its parked source bead back (beads.ReleaseWorkBead); empty when
	// the source was merged, owned by a live worker, terminal or unknown.
	SourceRelease    beads.ReleaseOutcome
	SourceReleaseErr error
}

func closeTerminalMR(b *beads.Beads, mrID string, opts terminalMRCloseOptions) (*terminalMRCloseResult, error) {
	mrID = strings.TrimSpace(mrID)
	result := &terminalMRCloseResult{MRID: mrID}
	if b == nil || mrID == "" {
		return result, nil
	}

	issue, err := b.Show(mrID)
	if err != nil {
		if errors.Is(err, beads.ErrNotFound) && opts.MissingOK {
			return result, nil
		}
		return result, fmt.Errorf("fetch MR for close: %w", err)
	}
	if issue == nil {
		return result, nil
	}

	fields := beads.ParseMRFields(issue)
	if fields == nil {
		fields = &beads.MRFields{}
	}
	result.SourceIssue = strings.TrimSpace(fields.SourceIssue)
	result.AgentBead = firstNonEmpty(opts.AgentBeadHint, fields.AgentBead)
	if err := validateTerminalMRCloseSnapshot(mrID, fields, opts.ExpectedMR); err != nil {
		return result, err
	}

	status := beads.IssueStatus(strings.TrimSpace(issue.Status))
	switch {
	case status == beads.StatusOpen:
		if opts.MergeCommit != "" {
			fields.MergeCommit = opts.MergeCommit
		}
		if closeReason := normalizedMRCloseReason(opts.Reason); closeReason != "" {
			fields.CloseReason = closeReason
		}
		if result.AgentBead != "" && strings.TrimSpace(fields.AgentBead) == "" {
			fields.AgentBead = result.AgentBead
		}

		newDesc := beads.SetMRFields(issue, fields)
		if err := b.Update(mrID, beads.UpdateOptions{Description: &newDesc}); err != nil {
			return result, fmt.Errorf("record MR close metadata: %w", err)
		}
		if err := b.CloseWithReason(opts.Reason, mrID); err != nil {
			return result, fmt.Errorf("close MR: %w", err)
		}
		result.Closed = true
	case status.IsTerminal():
		result.AlreadyTerminal = true
	default:
		return result, nil
	}

	// One rule for merged MRs: the source bead is closed exactly when its MR is
	// closed as merged, in this function, so no merge path (post-merge, engineer
	// success, already-merged recheck, post-merge retry) can leave it behind.
	// An already-terminal MR counts as merged when it recorded
	// close_reason/merge_commit merged, or when git proves its commit is on the
	// target (beads.MRFieldsLanded; covers an MR closed by hand without
	// post-merge). A retry thus heals a source whose first close failed, while a
	// rejected MR that never landed never closes its source. The work bead is
	// resolved before the agent's active_mr is cleared below.
	if mergedMRClose(opts.Reason, status, fields, opts.Prover) {
		result.SourceCloseAttempted = true
		result.SourceClose = closeMergedWorkBead(b, nil, opts.Out, mergedWorkBeadCloseRequest{
			MRID:   mrID,
			Target: firstNonEmpty(opts.Target, fields.Target),
			SourceIssue: resolveMergedWorkBead(b.ForAgentBead(), mergedWorkBeadCloseRequest{
				MRID:        mrID,
				Branch:      fields.Branch,
				SourceIssue: fields.SourceIssue,
				AgentBead:   result.AgentBead,
			}),
			MergeCommit: firstNonEmpty(opts.MergeCommit, fields.MergeCommit),
		})
	} else if result.Closed {
		// The other half of the ownership rule (gt-pd1c.59): gt done parks the
		// source of a submitted MR, so the close that ends an MR without
		// landing (rejected, superseded, conflict) hands it back for rework.
		result.SourceRelease, result.SourceReleaseErr = releaseUnmergedSource(b, result.SourceIssue, opts.Prover)
	}

	if result.AgentBead != "" {
		cleared, clearErr := b.ForAgentBead().ClearAgentActiveMRIfMatches(result.AgentBead, mrID)
		result.AgentActiveMRCleared = cleared
		result.AgentActiveMRClearErr = clearErr
	}
	return result, nil
}

// releaseUnmergedSource gives the source bead of an MR closed without landing
// back to the merge-queue rule (beads.ReleaseWorkBead): reopened for rework,
// parked if another MR of it is still pending, closed if one already landed.
// Only an unowned, non-terminal source is released: one that a live worker has
// claimed again, or that is already closed, is left alone. Errors are
// reported, not returned: the MR close itself succeeded.
func releaseUnmergedSource(b *beads.Beads, sourceIssue string, prover beads.MergeProver) (beads.ReleaseOutcome, error) {
	if sourceIssue == "" {
		return "", nil
	}
	issue, err := b.Show(sourceIssue)
	if err != nil {
		if errors.Is(err, beads.ErrNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("read source %s for release: %w", sourceIssue, err)
	}
	if issue == nil || beads.IssueStatus(strings.TrimSpace(issue.Status)).IsTerminal() || strings.TrimSpace(issue.Assignee) != "" {
		return "", nil
	}
	return b.ReleaseWorkBead(sourceIssue, func(string) beads.MergeProver { return prover })
}

// reportSourceRelease prints what releaseUnmergedSource did with the source
// bead of an MR closed without landing.
func reportSourceRelease(out io.Writer, prefix string, result *terminalMRCloseResult) {
	if out == nil || result == nil {
		return
	}
	if result.SourceReleaseErr != nil {
		_, _ = fmt.Fprintf(out, "%sWarning: source %s of MR %s not released: %v\n", prefix, result.SourceIssue, result.MRID, result.SourceReleaseErr)
		return
	}
	if result.SourceRelease != "" {
		_, _ = fmt.Fprintf(out, "%sSource %s of MR %s %s\n", prefix, result.SourceIssue, result.MRID, result.SourceRelease)
	}
}

// mergedMRClose reports whether this close leaves the MR terminal as merged,
// or the MR is already terminal and its work provably landed on the target
// (git is the proof, not the close reason: the refinery agent may have closed
// it with a bare `bd close`).
func mergedMRClose(reason string, prior beads.IssueStatus, fields *beads.MRFields, prover beads.MergeProver) bool {
	if normalizedMRCloseReason(reason) == string(CloseReasonMerged) && prior == beads.StatusOpen {
		return true
	}
	if !prior.IsTerminal() {
		return false
	}
	return beads.MRFieldsLanded(prior, fields, prover)
}

func validateTerminalMRCloseSnapshot(mrID string, fields *beads.MRFields, expected *MergeRequest) error {
	if expected == nil || fields == nil {
		return nil
	}
	checks := []struct {
		name string
		got  string
		want string
	}{
		{name: "branch", got: fields.Branch, want: expected.Branch},
		{name: "source_issue", got: fields.SourceIssue, want: expected.IssueID},
		{name: "commit_sha", got: fields.CommitSHA, want: expected.CommitSHA},
	}
	if strings.TrimSpace(expected.TargetBranch) != "" {
		checks = append(checks, struct {
			name string
			got  string
			want string
		}{name: "target", got: fields.Target, want: expected.TargetBranch})
	}
	for _, check := range checks {
		got := strings.TrimSpace(check.got)
		want := strings.TrimSpace(check.want)
		if want != "" && got != want {
			return fmt.Errorf("MR %s changed after merge proof: %s=%q, verified %q", mrID, check.name, got, want)
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
