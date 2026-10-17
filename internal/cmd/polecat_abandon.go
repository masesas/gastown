package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/mergeproof"
	"github.com/steveyegge/gastown/internal/style"
)

// gt polecat abandon (gt-pd1c.78 tahap 2) is the one way an agent discards a
// polecat branch whose work is not on a merge target: only work whose MR was
// closed with the official verdict rejected or superseded
// (beads.TipDisposition.Abandonable), and only after the tip is preserved:
//
//  1. archive: push the tip to refs/gt/archive/<branch>/<unix>-<sha>
//     (mergeproof.ArchiveRef) on origin;
//  2. verify: the archive ref on origin points at the tip;
//  3. delete: the branch on origin, only if it still points at the tip;
//  4. record: a note (reason, archive ref, actor) on the MR and source beads.
//
// A failure before 3 deletes nothing. The pre-push guard enforces the same
// rule (an agent may create an archive only for abandonable work, and delete
// a polecat branch only when its tip is archived or on a target), so a raw
// git push cannot do more than this command. Pruning archives is for humans
// and crew.

var polecatAbandonReason string

var polecatAbandonCmd = &cobra.Command{
	Use:   "abandon <rig>/<polecat> | <rig>/polecat/<branch>",
	Short: "Archive a rejected or superseded polecat branch, then delete it",
	Long: `Discard a polecat branch whose MR was closed as rejected or superseded,
without losing its work: the branch tip is first pushed to an archive ref on
the remote (refs/gt/archive/<branch>/<unix>-<sha>), the archive is verified,
then the branch is deleted and the abandon is recorded on the MR and source
beads.

Only rejected or superseded work may be abandoned. Refused, with the way out:
  - an open MR (the refinery owns the branch),
  - an MR closed for conflicts, or closed without an official verdict
    (rework and resubmit),
  - a tip no MR recorded (resubmit or rework),
  - an MR whose landing git cannot decide (an overseer verdict first).

The address is a polecat (its current branch) or a branch:
  gt polecat abandon gastown/nux --reason "superseded by gt-wisp-9"
  gt polecat abandon gastown/polecat/nux/gt-1 --reason "rejected: wrong approach"

Archives are pruned by a human or crew (git push origin --delete
refs/gt/archive/...); agents may not delete them.`,
	Args:         cobra.ExactArgs(1),
	SilenceUsage: true,
	RunE:         runPolecatAbandon,
}

func init() {
	polecatAbandonCmd.Flags().StringVar(&polecatAbandonReason, "reason", "", "Why the work is abandoned (required; recorded on the beads)")
	polecatCmd.AddCommand(polecatAbandonCmd)
}

// abandonRequest is one abandon, validated by abandonPolecatBranch.
type abandonRequest struct {
	rig, branch, reason, actor string
	defaultBranch              string
	now                        time.Time
}

// abandonGit is the git surface of an abandon (a rig repository, remote
// "origin").
type abandonGit interface {
	mergeproof.PreservationGit
	ListPushRemoteRefsWithHashes(remote, prefix string) ([]git.RemoteRef, error)
	FetchBranch(remote, branch string) error
	Push(remote, refspec string, force bool) error
	DeleteRemoteBranchIfAt(remote, branch, expectedHash string) error
}

// abandonResult is what an abandon did. RecordErr: the branch was abandoned
// but the note could not be recorded on every bead.
type abandonResult struct {
	Tip, ArchiveRef, MRID, Source string
	RecordErr                     error
}

// abandonPolecatBranch runs the abandon (see the file comment). record adds
// a note to a bead.
func abandonPolecatBranch(req abandonRequest, g abandonGit, queue func() *beads.MergeQueue,
	record func(id, note string) error) (*abandonResult, error) {
	reason := beads.DisplayReason(req.reason)
	switch {
	case !beads.IsSafeID(strings.TrimSpace(req.rig)):
		return nil, fmt.Errorf("invalid rig name %q", req.rig)
	case !strings.HasPrefix(req.branch, "polecat/") || !beads.IsSafeRefName(req.branch):
		return nil, fmt.Errorf("refusing to abandon %q: not a polecat branch name", req.branch)
	case reason == "":
		return nil, errors.New("--reason is required: say why the work is abandoned (it is recorded on the beads)")
	case queue == nil:
		return nil, errors.New("the merge queue is not available (fail closed)")
	}
	tips := mergeproof.RemoteTips{
		Defaults: []string{req.defaultBranch},
		List: func(prefix string) ([]git.RemoteRef, error) {
			return g.ListPushRemoteRefsWithHashes("origin", prefix)
		},
	}
	tip, err := tips.BranchTip(req.branch)
	if err != nil {
		return nil, fmt.Errorf("cannot read origin: %s", gitErrLine(err))
	}
	if tip == "" {
		return nil, fmt.Errorf("%s is not on the remote: nothing to abandon", req.branch)
	}
	if err := g.FetchBranch("origin", "refs/heads/"+req.branch); err != nil {
		return nil, fmt.Errorf("cannot fetch %s to archive it: %s", req.branch, gitErrLine(err))
	}
	if mergeproof.CoveredByTargets(g, tip, tips) {
		return nil, fmt.Errorf("%s is already on a merge target: nothing to abandon; `gt polecat prune --remote` deletes it", req.branch)
	}
	disp, err := queue().TipDisposition(req.branch, tip)
	if err != nil {
		return nil, fmt.Errorf("cannot read the merge queue: %s (fail closed)", firstLine(err.Error()))
	}
	if !disp.Abandonable() {
		return nil, fmt.Errorf("refusing to abandon %s: %s", req.branch, tipRefusal(req.rig, req.branch, disp))
	}
	return archiveAndDelete(req, reason, tip, disp, g, tips, record)
}

// archiveAndDelete is steps 1-4 of an abandon the merge queue allowed.
func archiveAndDelete(req abandonRequest, reason, tip string, disp beads.TipDisposition, g abandonGit,
	tips mergeproof.RemoteTips, record func(id, note string) error) (*abandonResult, error) {
	ref, err := mergeproof.ArchiveRef(req.branch, req.now, tip)
	if err != nil {
		return nil, err
	}
	if err := g.Push("origin", tip+":"+ref, false); err != nil {
		return nil, fmt.Errorf("archiving %s at %s failed, nothing was deleted: %s", req.branch, ref, gitErrLine(err))
	}
	if !archiveOnRemote(tips, ref, tip) {
		return nil, fmt.Errorf("archive %s could not be verified on the remote, nothing was deleted; retry", ref)
	}
	if err := g.DeleteRemoteBranchIfAt("origin", req.branch, tip); err != nil {
		return nil, fmt.Errorf("%s is archived at %s, but deleting the branch failed: %s; rerun gt polecat abandon",
			req.branch, ref, gitErrLine(err))
	}
	res := &abandonResult{Tip: tip, ArchiveRef: ref, MRID: disp.MRID}
	if fields := beads.ParseMRFields(disp.MR); fields != nil {
		res.Source = strings.TrimSpace(fields.SourceIssue)
	}
	note := fmt.Sprintf("abandoned %s at %s by %s: archived at %s. Reason: %s",
		req.branch, tip, beads.DisplayReason(req.actor), ref, reason)
	var errs []error
	for _, id := range uniqueNonEmpty(res.MRID, res.Source) {
		if !beads.IsSafeID(id) {
			continue
		}
		if err := record(id, note); err != nil {
			errs = append(errs, fmt.Errorf("recording on %s: %w", id, err))
		}
	}
	res.RecordErr = errors.Join(errs...)
	return res, nil
}

// gitErrLine is a git error safe to show: its first line, URL credentials
// masked (a remote URL may carry a token).
func gitErrLine(err error) string {
	return beads.DisplayReason(firstLine(err.Error()))
}

// archiveOnRemote reports whether ref exists on the remote at tip.
func archiveOnRemote(tips mergeproof.RemoteTips, ref, tip string) bool {
	refs, err := tips.List(ref)
	if err != nil {
		return false
	}
	for _, r := range refs {
		if r.Name == ref && strings.EqualFold(strings.TrimSpace(r.Hash), tip) {
			return true
		}
	}
	return false
}

// splitAbandonAddress splits <rig>/<rest>; rest is a polecat name or a
// polecat/... branch.
func splitAbandonAddress(addr string) (rig, rest string, err error) {
	rig, rest, ok := strings.Cut(strings.TrimSpace(addr), "/")
	if !ok || rig == "" || rest == "" {
		return "", "", fmt.Errorf("invalid address %q: want <rig>/<polecat> or <rig>/polecat/<branch>", addr)
	}
	return rig, rest, nil
}

// abandonActor names who abandons: BD_ACTOR, else the agent role, else the
// human user.
func abandonActor() string {
	c := gtCallerFrom(os.Getenv)
	switch {
	case c.actor != "":
		return c.actor
	case !c.human():
		return string(c.role)
	case strings.TrimSpace(os.Getenv("USER")) != "":
		return "human:" + strings.TrimSpace(os.Getenv("USER"))
	default:
		return "human"
	}
}

func runPolecatAbandon(cmd *cobra.Command, args []string) error {
	rigName, rest, err := splitAbandonAddress(args[0])
	if err != nil {
		return err
	}
	mgr, r, err := getPolecatManager(rigName)
	if err != nil {
		return err
	}
	branch := rest
	if !strings.HasPrefix(rest, "polecat/") {
		info, err := mgr.Get(rest)
		if err != nil || info == nil || strings.TrimSpace(info.Branch) == "" {
			return fmt.Errorf("cannot read the branch of polecat %s/%s; pass the branch: gt polecat abandon %s/polecat/<branch>",
				rigName, rest, rigName)
		}
		branch = strings.TrimSpace(info.Branch)
	}
	bd := beads.New(r.BeadsPath())
	req := abandonRequest{rig: r.Name, branch: branch, reason: polecatAbandonReason, actor: abandonActor(),
		defaultBranch: r.DefaultBranch(), now: time.Now()}
	res, err := abandonPolecatBranch(req, getRepoGitForRig(r.Path), townMergeQueueFor(r.Path), bd.AddComment)
	if err != nil {
		return err
	}
	fmt.Printf("%s Abandoned %s (MR %s): tip %s archived at %s, branch deleted\n",
		style.SuccessPrefix, branch, res.MRID, res.Tip, res.ArchiveRef)
	if res.RecordErr != nil {
		return fmt.Errorf("abandoned, but the note was not recorded on every bead (add it by hand): %w", res.RecordErr)
	}
	return nil
}
