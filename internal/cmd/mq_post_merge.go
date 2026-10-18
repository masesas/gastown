package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/refinery"
	"github.com/steveyegge/gastown/internal/style"
)

// gt mq post-merge is a thin wrapper over the merge engine's one finalize
// (refinery.Engineer.PostMerge, gt-pd1c.78 PR-C): proof bound to the MR,
// source close "Merged in <mr>", branch cleanup and MERGED live there only.

// mqPostMergeEngine is the engine surface gt mq post-merge uses
// (refinery.Engineer).
type mqPostMergeEngine interface {
	PostMerge(mrID string, opts refinery.PostMergeOptions) (*refinery.MRInfo, refinery.FinalizeReport, error)
	SetOutput(w io.Writer)
}

func runMQPostMerge(_ *cobra.Command, args []string) error {
	rigName, mrID := args[0], args[1]
	if mqPostMergeConfirmLanded {
		return runMQConfirmLanding(stdioHumanPrompt(), rigName, mrID, true)
	}
	if err := mqRefineryCallerAllowed("gt mq post-merge", gtCallerFrom(os.Getenv), os.Getenv, rigName); err != nil {
		return err
	}
	_, r, err := getRig(rigName)
	if err != nil {
		return err
	}
	opts := refinery.PostMergeOptions{SkipBranchDelete: mqPostMergeSkipBranchDelete}
	return runMQPostMergeWith(refinery.NewEngineer(r), mrID, opts, os.Stdout)
}

// runMQPostMergeWith runs the engine's post-merge and reports it. Exit
// non-zero on a refusal (nothing changed) and when any finalize step failed.
func runMQPostMergeWith(eng mqPostMergeEngine, mrID string, opts refinery.PostMergeOptions, stdout io.Writer) error {
	eng.SetOutput(stdout)
	mr, report, err := eng.PostMerge(mrID, opts)
	if err != nil {
		return fmt.Errorf("post-merge %s: %w", mrID, err)
	}
	printMQPostMerge(stdout, mr, report)
	if err := report.Err(); err != nil {
		return fmt.Errorf("post-merge %s: %w", mr.ID, err)
	}
	return nil
}

func printMQPostMerge(w io.Writer, mr *refinery.MRInfo, report refinery.FinalizeReport) {
	mark := style.Bold.Render("✓")
	if !report.OK() {
		mark = style.Bold.Render("✗")
	}
	_, _ = fmt.Fprintf(w, "%s Post-merge: %s (%s into %s)\n", mark, mr.ID, mr.Branch, mr.Target)
	switch {
	case report.SourceErr != nil:
		_, _ = fmt.Fprintf(w, "  %s Source %s not closed: %v\n", style.Warning.Render("!"), mr.SourceIssue, report.SourceErr)
	case report.SourceSkip != "":
		_, _ = fmt.Fprintf(w, "  %s Source not closed by design (%s)\n", style.Dim.Render("○"), report.SourceSkip)
	case mr.SourceIssue != "":
		_, _ = fmt.Fprintf(w, "  %s Source %s closed (Merged in %s)\n", style.Success.Render("✓"), mr.SourceIssue, mr.ID)
	}
	printMQPostMergeBranch(w, report.BranchCleanup)
	if report.MergedMailErr != nil {
		_, _ = fmt.Fprintf(w, "  %s MERGED mail not sent: %v\n", style.Warning.Render("!"), report.MergedMailErr)
	}
}

func printMQPostMergeBranch(w io.Writer, c refinery.BranchCleanup) {
	switch {
	case c.Err != nil:
		_, _ = fmt.Fprintf(w, "  %s Branch %s: %v\n", style.Warning.Render("!"), c.Branch, c.Err)
	case c.NoBranch:
		_, _ = fmt.Fprintf(w, "  %s No branch name in MR\n", style.Dim.Render("○"))
	case c.Skipped:
		_, _ = fmt.Fprintf(w, "  %s Branch %s kept (--skip-branch-delete)\n", style.Dim.Render("○"), c.Branch)
	case c.Protected:
		_, _ = fmt.Fprintf(w, "  %s Branch %s kept (default branch or integration/*)\n", style.Dim.Render("○"), c.Branch)
	case c.Disabled:
		_, _ = fmt.Fprintf(w, "  %s Branch %s kept (delete_merged_branches=false)\n", style.Dim.Render("○"), c.Branch)
	case c.OpenPR:
		_, _ = fmt.Fprintf(w, "  %s Branch %s kept on origin (open PR, or PR state unknown)\n", style.Dim.Render("○"), c.Branch)
	case c.RemoteDeleted && c.LocalDeleted:
		_, _ = fmt.Fprintf(w, "  %s Branch %s deleted (remote and local)\n", style.Success.Render("✓"), c.Branch)
	case c.RemoteDeleted:
		_, _ = fmt.Fprintf(w, "  %s Branch %s deleted on origin\n", style.Success.Render("✓"), c.Branch)
	case c.RemoteAbsent:
		_, _ = fmt.Fprintf(w, "  %s Branch %s already gone on origin\n", style.Dim.Render("○"), c.Branch)
	case c.LocalDeleted:
		_, _ = fmt.Fprintf(w, "  %s Branch %s deleted locally\n", style.Success.Render("✓"), c.Branch)
	}
}
