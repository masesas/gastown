package polecat

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/rig"
	"github.com/steveyegge/gastown/internal/testutil"
)

// gt-pd1c.69 (pilot k-posin, kp-aqs): commits on a review-only leg's branch
// must never be pushed to origin to "preserve" them.

const reviewLegBranch = "polecat/pipboy/gt-review+mvbqtobe"

func newReviewOnlyPushManager(t *testing.T, descriptions map[string]string, failAll bool) *Manager {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".beads"), 0o755); err != nil {
		t.Fatalf("mkdir beads: %v", err)
	}
	testutil.InstallBDShowStub(t, descriptions, failAll)
	beads.ResetBdAllowStaleCacheForTest()
	t.Cleanup(beads.ResetBdAllowStaleCacheForTest)
	return NewManager(&rig.Rig{Name: "rig", Path: root}, git.NewGit(root), nil)
}

func TestPushBranchBeforeRemovalSkipsReviewOnlyLeg(t *testing.T) {
	m := newReviewOnlyPushManager(t, map[string]string{"gt-review": "review_only: true\n\nReview leg."}, false)
	clone, remote := testutil.NewUnpushedPolecatBranch(t, reviewLegBranch)

	m.pushBranchBeforeRemoval(clone)

	if testutil.RemoteHasBranch(t, remote, reviewLegBranch) {
		t.Fatal("removal pushed the branch of a review-only leg to origin")
	}
}

func TestPushBranchBeforeRemovalSkipsWhenReviewOnlyUnknown(t *testing.T) {
	m := newReviewOnlyPushManager(t, nil, true)
	clone, remote := testutil.NewUnpushedPolecatBranch(t, reviewLegBranch)

	m.pushBranchBeforeRemoval(clone)

	if testutil.RemoteHasBranch(t, remote, reviewLegBranch) {
		t.Fatal("removal pushed although the review-only status could not be read (must fail closed)")
	}
}

func TestPushBranchBeforeRemovalPreservesOrdinaryWork(t *testing.T) {
	const branch = "polecat/pipboy/gt-code+abc"
	m := newReviewOnlyPushManager(t, map[string]string{"gt-code": "Implement the thing."}, false)
	clone, remote := testutil.NewUnpushedPolecatBranch(t, branch)

	m.pushBranchBeforeRemoval(clone)

	if !testutil.RemoteHasBranch(t, remote, branch) {
		t.Fatal("removal no longer preserves unpushed commits of ordinary work")
	}
}

func TestReviewOnlyPushSkipReason(t *testing.T) {
	show := func(issues map[string]string, err error) BeadShowFunc {
		return func(id string) (*beads.Issue, error) {
			if err != nil {
				return nil, err
			}
			desc, ok := issues[id]
			if !ok {
				return nil, beads.ErrNotFound
			}
			return &beads.Issue{ID: id, Description: desc}, nil
		}
	}
	review := map[string]string{"gt-review": "review_only: true", "gt-code": "code"}
	tests := []struct {
		name     string
		show     BeadShowFunc
		branch   string
		issues   []string
		wantSkip bool
		wantText string
	}{
		{"branch bead review-only", show(review, nil), reviewLegBranch, nil, true, "gt-review"},
		{"hooked issue review-only", show(review, nil), "polecat/pipboy/gt-code+x", []string{"gt-review"}, true, "gt-review"},
		{"ordinary work", show(review, nil), "polecat/pipboy/gt-code+x", []string{"gt-code"}, false, ""},
		{"purged branch bead", show(review, nil), "polecat/pipboy/gt-gone+x", nil, false, ""},
		{"non-polecat branch", show(review, nil), "feature/x", nil, false, ""},
		{"missing hooked issue fails closed", show(review, nil), "feature/x", []string{"gt-gone"}, true, "gt-gone"},
		{"lookup error fails closed", show(nil, errors.New("database is locked")), reviewLegBranch, nil, true, "could not verify"},
		{"nil lookup fails closed", nil, reviewLegBranch, nil, true, "could not verify"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ReviewOnlyPushSkipReason(tt.show, tt.branch, tt.issues...)
			if (got != "") != tt.wantSkip || !strings.Contains(got, tt.wantText) {
				t.Fatalf("ReviewOnlyPushSkipReason = %q, want skip=%v containing %q", got, tt.wantSkip, tt.wantText)
			}
		})
	}
}
