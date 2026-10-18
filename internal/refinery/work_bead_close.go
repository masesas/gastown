package refinery

import (
	"fmt"
	"io"
	"strings"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/util"
)

type mergedWorkBeadCloseRequest struct {
	MRID        string
	Branch      string
	Target      string
	SourceIssue string
	AgentBead   string
	MergeCommit string
}

type mergedWorkBeadCloseResult struct {
	WorkBeadID string
	Closed     bool
	NotFound   bool
	// CloseErr is set when the source was not left closed "Merged in <mr>":
	// it could not be read or closed, it is not a concrete work bead, the
	// reason failed validateMergedCloseReason, or it was already closed with
	// another reason (merge-blocks dependents would stay blocked).
	CloseErr error
	// Skip names a by-design reason the source is not closed by this merge:
	// there is no source bead (a bead-less/synthetic MR, or no source_issue
	// and no agent fallback), or the source opts out of merge closes
	// (no_merge, review_only, merge_strategy=local). Not a failure.
	Skip string
}

// mergedErr is nil when the merged source is closed "Merged in ..." or is
// skipped by design (Skip); otherwise why it is not.
func (r mergedWorkBeadCloseResult) mergedErr() error {
	switch {
	case r.CloseErr != nil:
		return r.CloseErr
	case r.Skip != "" || r.Closed:
		return nil
	default:
		return fmt.Errorf("source %s was not closed", r.WorkBeadID)
	}
}

// alreadyMergedClosed checks a terminal source: closed "Merged in ..." (by
// this or another MR of the same work) is fine, anything else is reported.
func alreadyMergedClosed(issue *beads.Issue) error {
	if strings.HasPrefix(issue.CloseReason, mergedCloseReasonPrefix) {
		return nil
	}
	return fmt.Errorf("source %s is already %s with close reason %q, not %q...: merge-blocks dependents stay blocked; close it by hand with the merged reason",
		issue.ID, issue.Status, util.OneLine(issue.CloseReason, 120), mergedCloseReasonPrefix)
}

// mergedCloseReasonPrefix starts the one close reason a merged source bead
// gets on the refinery path. beads.IsBlockerResolved resolves a merge-blocks
// dependency only for a close reason with this prefix (#1893).
const mergedCloseReasonPrefix = "Merged in "

// validateMergedCloseReason checks the close reason of a merged source bead:
// its first line must be exactly "Merged in <mrID>" (further lines carry
// target_branch/commit_sha), and mrID must be a plain bead ID. Anything else
// (free text such as "landed on main as <sha> via MR <id>") would leave
// merge-blocks dependents blocked forever without an error, so it is refused
// before the close (gt-pd1c.78 tahap 3).
func validateMergedCloseReason(reason, mrID string) error {
	if !beads.IsSafeID(mrID) {
		return fmt.Errorf("merged close refused: MR id %q is not a plain bead id", util.OneLine(mrID, 80))
	}
	firstLine, _, _ := strings.Cut(reason, "\n")
	if want := mergedCloseReasonPrefix + mrID; firstLine != want {
		return fmt.Errorf("merged close refused: close reason %q does not start with the line %q", util.OneLine(firstLine, 120), want)
	}
	return nil
}

type workBeadCloser interface {
	Show(id string) (*beads.Issue, error)
	ForceCloseWithReason(reason string, ids ...string) error
}

type issueReader interface {
	Show(id string) (*beads.Issue, error)
}

func closeMergedWorkBead(work workBeadCloser, agent issueReader, out io.Writer, req mergedWorkBeadCloseRequest) mergedWorkBeadCloseResult {
	logf := func(format string, args ...interface{}) {
		if out != nil {
			_, _ = fmt.Fprintf(out, format, args...)
		}
	}

	workBeadID := resolveMergedWorkBead(agent, req)
	result := mergedWorkBeadCloseResult{WorkBeadID: workBeadID}
	if workBeadID == "" {
		logf("[Refinery] Note: merged MR %s has no resolvable work bead to close\n", req.MRID)
		result.NotFound = true
		result.Skip = "no source bead"
		return result
	}
	if work == nil {
		logf("[Refinery] Warning: no beads client available to close work bead %s\n", workBeadID)
		result.NotFound = true
		result.CloseErr = fmt.Errorf("no beads client to close source %s", workBeadID)
		return result
	}

	issue, err := work.Show(workBeadID)
	if err != nil || issue == nil {
		logf("[Refinery] Warning: failed to fetch work bead %s: %v\n", workBeadID, err)
		result.NotFound = true
		result.CloseErr = fmt.Errorf("read source %s: %v", workBeadID, err)
		return result
	}
	if reason := beads.ConcreteWorkIssueRejectReason(issue); reason != "" {
		logf("[Refinery] Warning: refusing to close non-concrete work bead %s (%s)\n", workBeadID, reason)
		result.NotFound = true
		result.CloseErr = fmt.Errorf("source %s is not a concrete work bead (%s)", workBeadID, reason)
		return result
	}
	if beads.IssueStatus(strings.TrimSpace(issue.Status)).IsTerminal() {
		logf("[Refinery] Work bead already closed: %s\n", workBeadID)
		result.Closed = true
		result.CloseErr = alreadyMergedClosed(issue)
		return result
	}
	if reason := refineryMergedWorkBeadCloseBlockReason(issue); reason != "" {
		logf("[Refinery] Note: work bead %s is not closed by a merge (%s)\n", workBeadID, reason)
		result.NotFound = true
		result.Skip = "source has " + reason
		return result
	}

	closeReason := mergedCloseReasonPrefix + req.MRID
	if req.MergeCommit != "" {
		closeReason = fmt.Sprintf("%s\ntarget_branch: %s\ncommit_sha: %s", closeReason, req.Target, req.MergeCommit)
	}
	if err := validateMergedCloseReason(closeReason, req.MRID); err != nil {
		logf("[Refinery] Warning: refusing to close work bead %s: %v\n", workBeadID, err)
		result.CloseErr = err
		return result
	}

	if err := work.ForceCloseWithReason(closeReason, workBeadID); err != nil {
		if issue, showErr := work.Show(workBeadID); showErr == nil && issue != nil &&
			beads.ConcreteWorkIssueRejectReason(issue) == "" &&
			beads.IssueStatus(strings.TrimSpace(issue.Status)).IsTerminal() {
			logf("[Refinery] Work bead already closed: %s\n", workBeadID)
			result.Closed = true
			result.CloseErr = alreadyMergedClosed(issue)
			return result
		}
		logf("[Refinery] Warning: failed to close work bead %s: %v\n", workBeadID, err)
		result.NotFound = true
		result.CloseErr = fmt.Errorf("close source %s: %w", workBeadID, err)
		return result
	}

	logf("[Refinery] Closed work bead: %s\n", workBeadID)
	result.Closed = true
	return result
}

func resolveMergedWorkBead(agent issueReader, req mergedWorkBeadCloseRequest) string {
	if sourceIssue := cleanWorkBeadID(req.SourceIssue); sourceIssue != "" {
		return sourceIssue
	}
	if agent == nil || cleanWorkBeadID(req.AgentBead) == "" || cleanWorkBeadID(req.MRID) == "" {
		return ""
	}

	agentIssue, err := agent.Show(req.AgentBead)
	if err != nil || !beads.IsAgentBead(agentIssue) {
		return ""
	}
	fields := beads.ParseAgentFields(agentIssue.Description)
	if fields == nil {
		return ""
	}
	if fields.ActiveMR != req.MRID && fields.MRID != req.MRID {
		return ""
	}
	agentBranch := strings.TrimSpace(fields.Branch)
	requestBranch := strings.TrimSpace(req.Branch)
	if agentBranch == "" || strings.EqualFold(agentBranch, "null") {
		return ""
	}
	if requestBranch == "" || strings.EqualFold(requestBranch, "null") || agentBranch != requestBranch {
		return ""
	}
	return cleanWorkBeadID(fields.LastSourceIssue)
}

func cleanWorkBeadID(id string) string {
	id = strings.TrimSpace(id)
	if strings.EqualFold(id, "null") {
		return ""
	}
	return id
}

func refineryMergedWorkBeadCloseBlockReason(issue *beads.Issue) string {
	if fields := beads.ParseAttachmentFields(issue); fields != nil {
		switch {
		case fields.NoMerge:
			return "no_merge"
		case fields.ReviewOnly:
			return "review_only"
		case strings.EqualFold(strings.TrimSpace(fields.MergeStrategy), "local"):
			return "merge_strategy:local"
		}
	}
	return ""
}
