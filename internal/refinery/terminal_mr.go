package refinery

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/mergeproof"
)

// readTownMergeQueue reads the town-wide merge queue; tests override it.
var readTownMergeQueue = mergeproof.TownMergeQueue

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
	// TownRoot scopes the release of an unmerged MR's source bead: the
	// town-wide merge queue decides it (an MR may live in another DB than its
	// source). Without it the source is not released.
	TownRoot string
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
	} else if result.Closed || result.AlreadyTerminal {
		// The other half of the ownership rule (gt-pd1c.59): gt done parks the
		// source of a submitted MR, so an MR that ended without landing
		// (rejected, superseded, conflict, closed by hand) hands it back for
		// rework. Repeating the close (a second `gt mq reject`) repeats the
		// release, so a release that failed once can be retried.
		result.SourceRelease, result.SourceReleaseErr = releaseUnmergedSource(b, result.SourceIssue, opts.TownRoot)
	}

	if result.AgentBead != "" {
		cleared, clearErr := b.ForAgentBead().ClearAgentActiveMRIfMatches(result.AgentBead, mrID)
		result.AgentActiveMRCleared = cleared
		result.AgentActiveMRClearErr = clearErr
	}
	return result, nil
}

// releaseUnmergedSource gives the source bead of an MR that ended without
// landing back to the merge-queue rule (beads.ReleaseWorkBead over the
// town-wide queue): reopened for rework, parked if another MR of it is still
// pending (in any DB), closed if one already landed. Only an unowned,
// non-terminal source is released: one that a live worker has claimed again,
// or that is already closed, is left alone. The source is read through the
// beads routing (its own DB). Problems are returned for reporting, never
// fatal: the MR close itself succeeded.
func releaseUnmergedSource(b *beads.Beads, sourceIssue, townRoot string) (beads.ReleaseOutcome, error) {
	if sourceIssue == "" {
		return "", nil
	}
	issue, err := b.Show(sourceIssue)
	if err != nil {
		if errors.Is(err, beads.ErrNotFound) {
			return "", fmt.Errorf("source %s not found, nothing to release", sourceIssue)
		}
		return "", fmt.Errorf("read source %s for release: %w", sourceIssue, err)
	}
	if issue == nil {
		return "", fmt.Errorf("source %s not found, nothing to release", sourceIssue)
	}
	if beads.IssueStatus(strings.TrimSpace(issue.Status)).IsTerminal() || strings.TrimSpace(issue.Assignee) != "" {
		return "", nil
	}
	if strings.TrimSpace(townRoot) == "" {
		return "", fmt.Errorf("town root unknown, source %s not released", sourceIssue)
	}
	return b.ReleaseWorkBead(sourceIssue, readTownMergeQueue(townRoot))
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
