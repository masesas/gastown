package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/polecat"
	"github.com/steveyegge/gastown/internal/rig"
	"github.com/steveyegge/gastown/internal/testutil"
)

// gt-pd1c.69 (pilot k-posin): `gt polecat nuke` must not push a review leg's
// commits to origin, even after the leg was re-slung to another polecat.

func newNukePushFixture(t *testing.T, descriptions map[string]string, failAll bool) (*rig.Rig, *polecat.Manager) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".beads"), 0o755); err != nil {
		t.Fatalf("mkdir beads: %v", err)
	}
	testutil.InstallBDShowStub(t, descriptions, failAll)
	beads.ResetBdAllowStaleCacheForTest()
	t.Cleanup(beads.ResetBdAllowStaleCacheForTest)
	r := &rig.Rig{Name: "gastown", Path: root}
	return r, polecat.NewManager(r, git.NewGit(root), nil)
}

func TestPushPolecatBranchBeforeNukeSkipsReviewOnlyLeg(t *testing.T) {
	const branch = "polecat/pipboy/gt-review+mvbqtobe"
	for _, tt := range []struct {
		name  string
		issue string // the polecat's current issue; "" after a --force re-sling moved the leg
	}{
		{"leg re-slung away (branch only)", ""},
		{"leg still hooked", "gt-review"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, mgr := newNukePushFixture(t, map[string]string{"gt-review": "review_only: true\n\nReview leg."}, false)
			clone, remote := testutil.NewUnpushedPolecatBranch(t, branch)

			pushPolecatBranchBeforeNuke(r, mgr, &polecat.Polecat{ClonePath: clone, Branch: branch, Issue: tt.issue}, branch)

			if testutil.RemoteHasBranch(t, remote, branch) {
				t.Fatal("nuke pushed the branch of a review-only leg to origin")
			}
		})
	}
}

func TestPushPolecatBranchBeforeNukeFailsClosedWhenBeadsUnavailable(t *testing.T) {
	const branch = "polecat/pipboy/gt-code+abc"
	r, mgr := newNukePushFixture(t, nil, true)
	clone, remote := testutil.NewUnpushedPolecatBranch(t, branch)

	pushPolecatBranchBeforeNuke(r, mgr, &polecat.Polecat{ClonePath: clone, Branch: branch}, branch)

	if testutil.RemoteHasBranch(t, remote, branch) {
		t.Fatal("nuke pushed although the review-only status could not be read")
	}
}

func TestPushPolecatBranchBeforeNukePreservesOrdinaryWork(t *testing.T) {
	const branch = "polecat/pipboy/gt-code+abc"
	r, mgr := newNukePushFixture(t, map[string]string{"gt-code": "Implement the thing."}, false)
	clone, remote := testutil.NewUnpushedPolecatBranch(t, branch)

	pushPolecatBranchBeforeNuke(r, mgr, &polecat.Polecat{ClonePath: clone, Branch: branch, Issue: "gt-code"}, branch)

	if !testutil.RemoteHasBranch(t, remote, branch) {
		t.Fatal("nuke no longer preserves unpushed commits of ordinary work (gt-4vr)")
	}
}
