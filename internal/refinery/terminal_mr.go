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
	// An already-terminal MR is only treated as merged when it recorded
	// close_reason=merged, so a retry heals a source whose first close failed
	// while a rejected MR never closes its source. The work bead is resolved
	// before the agent's active_mr is cleared below.
	if mergedMRClose(opts.Reason, status, fields) {
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
	}

	if result.AgentBead != "" {
		cleared, clearErr := b.ForAgentBead().ClearAgentActiveMRIfMatches(result.AgentBead, mrID)
		result.AgentActiveMRCleared = cleared
		result.AgentActiveMRClearErr = clearErr
	}
	return result, nil
}

// mergedMRClose reports whether this close leaves the MR terminal as merged.
func mergedMRClose(reason string, prior beads.IssueStatus, fields *beads.MRFields) bool {
	if normalizedMRCloseReason(reason) != string(CloseReasonMerged) {
		return false
	}
	if prior == beads.StatusOpen {
		return true
	}
	return prior.IsTerminal() && strings.EqualFold(strings.TrimSpace(fields.CloseReason), string(CloseReasonMerged))
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
