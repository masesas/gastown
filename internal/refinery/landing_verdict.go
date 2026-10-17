package refinery

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/mergeproof"
)

// LandingVerdictResult is what ConfirmLanding did.
type LandingVerdictResult struct {
	MR            *MergeRequest
	Verdict       string // beads.LandingLanded or beads.LandingNotLanded
	SourceIssue   string
	SourceClosed  bool
	SourceRelease beads.ReleaseOutcome
	SourceErr     error
}

// rigProverFor builds the rig's git prover; tests override it.
var rigProverFor = func(rigPath string) beads.MergeProver { return mergeproof.ForRigPath(rigPath) }

// ConfirmLanding records a human verdict on a terminal MR whose landing git
// cannot decide (beads.HoldUnverified, gt-pd1c.59) and then lets the existing
// rule act on it: landed closes the source bead (merged close in
// closeTerminalMR), not_landed releases it (ReleaseWorkBead: reopen, or keep
// parked if another MR holds it). It is the one manual exit for an unverified
// hold and refuses whenever the predicate can already decide: an open MR, a
// structured merged signal, an earlier verdict, or a definite git answer.
// Callers authorize the actor (humans only, see gt mq); actor records the
// identity source (e.g. "human:alice via tty"), via the command used. The
// verdict is audited on the MR (landing_by) and as a comment on the source.
func (m *Manager) ConfirmLanding(idOrBranch string, landed bool, actor, via string) (*LandingVerdictResult, error) {
	b := beads.New(m.rig.BeadsPath())
	mr, err := m.findMRForTerminalCleanup(idOrBranch, b)
	if err != nil {
		return nil, err
	}
	issue, err := b.Show(mr.ID)
	if err != nil {
		return nil, fmt.Errorf("reading MR %s: %w", mr.ID, err)
	}
	if !beads.IssueStatus(strings.TrimSpace(issue.Status)).IsTerminal() {
		return nil, fmt.Errorf("MR %s is %s: a landing verdict is only for closed MRs; the refinery owns open ones", mr.ID, issue.Status)
	}
	fields := beads.ParseMRFields(issue)
	if fields == nil {
		fields = &beads.MRFields{}
	}
	prover := rigProverFor(m.rig.Path)
	if reason := beads.UnverifiedReason(issue, prover); reason == "" {
		return nil, fmt.Errorf("MR %s needs no verdict: its landing is already decided (merge record, earlier verdict, or git)", mr.ID)
	}
	verdict := beads.LandingNotLanded
	if landed {
		verdict = beads.LandingLanded
	}
	fields.Landing = verdict
	fields.LandingBy = strings.TrimSpace(actor)
	desc := beads.SetMRFields(issue, fields)
	if err := b.Update(mr.ID, beads.UpdateOptions{Description: &desc}); err != nil {
		return nil, fmt.Errorf("recording landing verdict on MR %s: %w", mr.ID, err)
	}

	audit := fmt.Sprintf("landing verdict %s on MR %s by %s via %s", verdict, mr.ID, fields.LandingBy, strings.TrimSpace(via))
	source := strings.TrimSpace(fields.SourceIssue)
	if source != "" {
		if err := b.AddComment(source, audit); err != nil {
			_, _ = fmt.Fprintf(m.output, "Warning: could not record the verdict on source %s: %v\n", source, err)
		}
	}

	reason := "rejected: not landed (verdict by " + fields.LandingBy + ")"
	if landed {
		reason = string(CloseReasonMerged)
	}
	closeResult, err := closeTerminalMR(b, mr.ID, terminalMRCloseOptions{
		Reason:        reason,
		AgentBeadHint: mr.AgentBead,
		Target:        mr.TargetBranch,
		Out:           m.output,
		Prover:        prover,
		TownRoot:      filepath.Dir(m.rig.Path),
	})
	if err != nil {
		retry := "gt mq reject <rig> " + mr.ID + " --reason \"retry not-landed release\""
		if landed {
			retry = "gt mq post-merge <rig> " + mr.ID
		}
		return nil, fmt.Errorf("verdict %s is recorded on MR %s, but applying it failed: %w\nRecover: rerun `%s` (the recorded verdict is now proof), or let the witness orphan scan / `gt doctor --fix` settle the source", verdict, mr.ID, err, retry)
	}
	reportSourceRelease(m.output, "", closeResult)
	res := &LandingVerdictResult{
		MR:            mr,
		Verdict:       verdict,
		SourceIssue:   closeResult.SourceIssue,
		SourceClosed:  closeResult.SourceClose.Closed,
		SourceRelease: closeResult.SourceRelease,
		SourceErr:     closeResult.SourceReleaseErr,
	}
	return res, nil
}
