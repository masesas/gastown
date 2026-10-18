package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/refinery"
	"github.com/steveyegge/gastown/internal/rig"
	"github.com/steveyegge/gastown/internal/style"
)

// mqMergeJSON: print the outcome as JSON (gt mq merge --json).
var mqMergeJSON bool

var mqMergeCmd = &cobra.Command{
	Use:   "merge <rig> <mr-id>",
	Short: "Merge one MR end to end (merge, push, close, clean up, notify)",
	Long: `Land one merge request with the refinery's Go merge engine, the single
supported way for the refinery to merge (gt-pd1c.78):

  1. Check the MR: an open gt:merge-request of this rig, not blocked, not
     claimed by someone else, with a plain branch name, a full commit_sha and
     a trusted target (the rig default branch or integration/*).
  2. Claim it and check the refinery worktree: no merge/rebase in progress,
     a clean tree, the local target with no commits missing on origin (they
     are never pushed). A local branch missing but on origin at the
     submitted commit_sha is created there.
  3. Recheck eligibility, run the merge-scope gate (merge_queue.allowed_paths)
     on the pinned commit_sha, merge it with a merge commit, run every gate
     on the merged tree, and push. A failed gate always blocks.
  4. Prove from git that the target contains commit_sha, close the MR as
     merged and the source issue "Merged in <mr>" (this unblocks merge-blocks
     dependents), delete the branch, and mail MERGED to the witness.

On failure the MR stays open and the claim is released: a conflict files a
conflict-resolution task, a failed gate or scope violation mails FIX_NEEDED to
the polecat. Nothing is pushed unless every check passed. merge_queue.auto_push
false is refused.

Gates come from <rig>/settings/config.json (setup, typecheck, lint, build,
test commands) unless <rig>/config.json has an explicit merge_queue.gates map.
Settings in settings/config.json win over config.json. Gates run without
agent identity or credentials in their environment, and the merge commit
they ran on is verified (parents, tree) and pushed by SHA. A merge with no
gate is refused unless run_tests=false, the explicit opt-out that disables
every gate, explicit ones included (reported as a warning).

Only the refinery or crew of <rig>, or a human, may run it. The check reads
the caller's environment (GT_ROLE, GT_RIG, BD_ACTOR): it stops agents taking
the wrong exit by mistake and is not an authentication boundary.

Exit status is 0 only when the MR was merged and fully finalized. With --json
the outcome is printed as one JSON object on stdout in every case (engine
progress goes to stderr); "kind" is one of merged, conflict, gates_failed,
scope_violation, not_eligible, needs_approval, slot_timeout, branch_missing,
cleanup_failed, precondition, error. cleanup_failed means the merge is on
origin but finalizing failed: fix the cause and run 'gt mq post-merge'.

Examples:
  gt mq merge gastown gt-mr-abc123
  gt mq merge gastown gt-mr-abc123 --json`,
	Args: cobra.ExactArgs(2),
	RunE: runMQMerge,
}

// mqMergeEngine is the merge engine surface gt mq merge uses
// (refinery.Engineer).
type mqMergeEngine interface {
	MergeOne(ctx context.Context, mrID string) refinery.MergeOutcome
	SetOutput(w io.Writer)
}

func runMQMerge(cmd *cobra.Command, args []string) error {
	rigName, mrID := args[0], args[1]
	if err := mqMergeCallerAllowed(gtCallerFrom(os.Getenv), os.Getenv, rigName); err != nil {
		return reportMQMergeRefusal(os.Stdout, mrID, mqMergeJSON, err)
	}
	_, r, err := getRig(rigName)
	if err != nil {
		return reportMQMergeRefusal(os.Stdout, mrID, mqMergeJSON, err)
	}
	parent := cmd.Context()
	if parent == nil {
		parent = context.Background()
	}
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runMQMergeWith(ctx, refinery.NewEngineer(r), rigName, mrID, mqMergeJSON, os.Stdout, os.Stderr)
}

// mqMergeCallerAllowed lets the refinery, crew and humans (gtCaller.exempt)
// merge; every other agent role, and conflicting identities, are refused. A
// refinery or crew caller must belong to rigName: every rig its identity
// variables name (GT_RIG, the rig of GT_ROLE and of BD_ACTOR) must be rigName,
// and at least one must be set. A human shell and the overseer name no rig.
// Like the other coordinator guards this reads the environment, so it stops
// agents taking the wrong exit by mistake; it is not an authentication
// boundary (stage 4 binds the process through the pre-receive hook).
func mqMergeCallerAllowed(c gtCaller, getenv func(string) string, rigName string) error {
	if c.human() {
		return nil
	}
	if !c.exempt() && c.role != RoleRefinery {
		return fmt.Errorf("refusing gt mq merge from %s: only the refinery, crew or a human may merge", c.describe())
	}
	rigs := mqMergeCallerRigs(getenv)
	if len(rigs) == 0 {
		if c.role == roleOverseer {
			return nil
		}
		return fmt.Errorf("refusing gt mq merge on rig %s from %s: the caller names no rig (GT_RIG or GT_ROLE <rig>/...)", rigName, c.describe())
	}
	for _, r := range rigs {
		if !rig.SameName(r, rigName) {
			return fmt.Errorf("refusing gt mq merge on rig %s from %s of rig %s: only the refinery or crew of %s may merge it", rigName, c.describe(), r, rigName)
		}
	}
	return nil
}

// mqMergeCallerRigs lists the rigs the caller's identity variables name.
func mqMergeCallerRigs(getenv func(string) string) []string {
	var rigs []string
	if r := strings.TrimSpace(getenv("GT_RIG")); r != "" {
		rigs = append(rigs, r)
	}
	for _, key := range []string{EnvGTRole, "BD_ACTOR"} {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			if _, r, _ := parseRoleString(v); r != "" {
				rigs = append(rigs, r)
			}
		}
	}
	return rigs
}

// reportMQMergeRefusal reports a refusal before the engine ran: as a
// precondition outcome with --json (exit 1), else as the plain error.
func reportMQMergeRefusal(stdout io.Writer, mrID string, asJSON bool, err error) error {
	if !asJSON {
		return err
	}
	if jerr := writeMQMergeJSON(stdout, refinery.MergeOutcome{Kind: refinery.OutcomePrecondition, MRID: mrID, Message: err.Error()}); jerr != nil {
		return jerr
	}
	return NewSilentExit(1)
}

// runMQMergeWith runs the engine and reports its outcome. The engine's
// progress goes to stdout, or to stderr with --json so stdout is exactly one
// JSON object. Exit 1 (silent: the outcome is already printed) unless merged.
func runMQMergeWith(ctx context.Context, eng mqMergeEngine, rigName, mrID string, asJSON bool, stdout, stderr io.Writer) error {
	if asJSON {
		eng.SetOutput(stderr)
	} else {
		eng.SetOutput(stdout)
	}
	out := eng.MergeOne(ctx, mrID)
	if asJSON {
		if err := writeMQMergeJSON(stdout, out); err != nil {
			return err
		}
	} else {
		printMQMergeOutcome(stdout, rigName, out)
	}
	if out.Merged() {
		return nil
	}
	return NewSilentExit(1)
}

func writeMQMergeJSON(w io.Writer, out refinery.MergeOutcome) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("encode merge outcome: %w", err)
	}
	return nil
}

func printMQMergeOutcome(w io.Writer, rigName string, out refinery.MergeOutcome) {
	if !out.Merged() {
		_, _ = fmt.Fprintf(w, "%s %s: MR %s not merged\n", style.Bold.Render("✗"), out.Kind, out.MRID)
		if out.Message != "" {
			_, _ = fmt.Fprintf(w, "  %s\n", out.Message)
		}
		if out.Kind == refinery.OutcomeCleanupFailed {
			_, _ = fmt.Fprintf(w, "  The merge is on origin. After fixing the cause run: gt mq post-merge %s %s\n", rigName, out.MRID)
		}
		printMQMergeWarnings(w, out)
		return
	}
	_, _ = fmt.Fprintf(w, "%s Merged %s into %s: %s\n", style.Bold.Render("✓"), out.MRID, out.Target, shortMQMergeSHA(out.MergeCommit))
	if out.SourceIssue != "" {
		_, _ = fmt.Fprintf(w, "  Source %s closed (Merged in %s)\n", out.SourceIssue, out.MRID)
	}
	_, _ = fmt.Fprintf(w, "  Gates: %d ran\n", out.GatesRan)
	if c := out.BranchCleanup; c != nil {
		switch {
		case c.RemoteDeleted && c.LocalDeleted:
			_, _ = fmt.Fprintf(w, "  Branch %s deleted (remote and local)\n", c.Branch)
		case c.RemoteDeleted:
			_, _ = fmt.Fprintf(w, "  Branch %s deleted on origin\n", c.Branch)
		case c.OpenPR:
			_, _ = fmt.Fprintf(w, "  Branch %s kept on origin (open PR, or PR state unknown)\n", c.Branch)
		case c.Disabled:
			_, _ = fmt.Fprintf(w, "  Branch %s kept (delete_merged_branches=false)\n", c.Branch)
		}
	}
	printMQMergeWarnings(w, out)
}

func printMQMergeWarnings(w io.Writer, out refinery.MergeOutcome) {
	for _, warning := range out.Warnings {
		_, _ = fmt.Fprintf(w, "  %s %s\n", style.Warning.Render("!"), warning)
	}
}

func shortMQMergeSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
