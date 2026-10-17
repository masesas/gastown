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

func newReviewOnlyPushManager(t *testing.T, descriptions map[string]string, failAll bool, hookedIDs ...string) *Manager {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{".beads", filepath.Join("polecats", "pipboy")} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	testutil.InstallBDShowStubWithHooked(t, descriptions, hookedIDs, failAll)
	beads.ResetBdAllowStaleCacheForTest()
	t.Cleanup(beads.ResetBdAllowStaleCacheForTest)
	return NewManager(&rig.Rig{Name: "rig", Path: root}, git.NewGit(root), nil)
}

func TestPushBranchBeforeRemovalSkipsReviewOnlyLeg(t *testing.T) {
	m := newReviewOnlyPushManager(t, map[string]string{"gt-review": "review_only: true\n\nReview leg."}, false)
	clone, remote := testutil.NewUnpushedPolecatBranch(t, reviewLegBranch)

	m.preserveBranchBeforeRemoval("pipboy", clone)

	if testutil.RemoteHasBranch(t, remote, reviewLegBranch) {
		t.Fatal("removal pushed the branch of a review-only leg to origin")
	}
}

func TestPushBranchBeforeRemovalSkipsWhenReviewOnlyUnknown(t *testing.T) {
	m := newReviewOnlyPushManager(t, nil, true)
	clone, remote := testutil.NewUnpushedPolecatBranch(t, reviewLegBranch)

	m.preserveBranchBeforeRemoval("pipboy", clone)

	if testutil.RemoteHasBranch(t, remote, reviewLegBranch) {
		t.Fatal("removal pushed although the review-only status could not be read (must fail closed)")
	}
}

func TestPushBranchBeforeRemovalPreservesOrdinaryWork(t *testing.T) {
	const branch = "polecat/pipboy/gt-code+abc"
	m := newReviewOnlyPushManager(t, map[string]string{"gt-code": "Implement the thing."}, false)
	clone, remote := testutil.NewUnpushedPolecatBranch(t, branch)

	m.preserveBranchBeforeRemoval("pipboy", clone)

	if !testutil.RemoteHasBranch(t, remote, branch) {
		t.Fatal("removal no longer preserves unpushed commits of ordinary work")
	}
}

func TestPreserveBranchBeforeRemovalHonoursHookedReviewLeg(t *testing.T) {
	// Branch names ordinary bead gt-code, but the polecat's hook is review leg gt-review.
	const branch = "polecat/pipboy/gt-code+abc"
	m := newReviewOnlyPushManager(t, map[string]string{
		"gt-code":   "Implement the thing.",
		"gt-review": "review_only: true",
	}, false, "gt-review")
	clone, remote := testutil.NewUnpushedPolecatBranch(t, branch)

	m.preserveBranchBeforeRemoval("pipboy", clone)

	if testutil.RemoteHasBranch(t, remote, branch) {
		t.Fatal("removal pushed although the polecat's hooked bead is review-only")
	}
}

func TestCleanupStaleBranchesKeepsUnverifiedBranches(t *testing.T) {
	for _, tt := range []struct {
		name     string
		failAll  bool
		wantKept bool
	}{
		{"beads unavailable keeps the branch", true, true},
		{"ordinary orphan is deleted", false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const branch = "polecat/ghost/gt-code+abc"
			m := newReviewOnlyPushManager(t, map[string]string{"gt-code": "Implement the thing."}, tt.failAll)
			if err := os.RemoveAll(filepath.Join(m.rig.Path, "polecats", "pipboy")); err != nil {
				t.Fatal(err)
			}
			clone, _ := testutil.NewUnpushedPolecatBranch(t, branch)
			cloneGit := git.NewGit(clone)
			if err := cloneGit.Checkout("main"); err != nil {
				t.Fatalf("checkout main: %v", err)
			}
			if err := os.MkdirAll(filepath.Join(m.rig.Path, "mayor"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(clone, filepath.Join(m.rig.Path, "mayor", "rig")); err != nil {
				t.Fatal(err)
			}

			if _, err := m.CleanupStaleBranches(); err != nil {
				t.Fatalf("CleanupStaleBranches: %v", err)
			}
			exists, err := cloneGit.BranchExists(branch)
			if err != nil {
				t.Fatalf("BranchExists: %v", err)
			}
			if exists != tt.wantKept {
				t.Fatalf("branch kept = %v, want %v", exists, tt.wantKept)
			}
		})
	}
}

func TestReviewOnlyWorkDecision(t *testing.T) {
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
		want     ReviewOnlyVerdict
		wantText string
	}{
		{"branch bead review-only", show(review, nil), reviewLegBranch, nil, ReviewOnlyConfirmed, "gt-review"},
		{"hooked issue review-only", show(review, nil), "polecat/pipboy/gt-code+x", []string{"gt-review"}, ReviewOnlyConfirmed, "gt-review"},
		{"ordinary work", show(review, nil), "polecat/pipboy/gt-code+x", []string{"gt-code"}, ReviewOnlyNotReview, ""},
		{"purged branch bead", show(review, nil), "polecat/pipboy/gt-gone+x", nil, ReviewOnlyNotReview, ""},
		{"non-polecat branch", show(review, nil), "feature/x", nil, ReviewOnlyNotReview, ""},
		{"missing hooked issue", show(review, nil), "feature/x", []string{"gt-gone"}, ReviewOnlyUnverified, "gt-gone"},
		{"lookup error", show(nil, errors.New("database is locked")), reviewLegBranch, nil, ReviewOnlyUnverified, "could not verify"},
		{"nil lookup", nil, reviewLegBranch, nil, ReviewOnlyUnverified, "could not verify"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ReviewOnlyWorkDecision(tt.show, tt.branch, tt.issues...)
			if got.Verdict != tt.want || !strings.Contains(got.Reason, tt.wantText) {
				t.Fatalf("ReviewOnlyWorkDecision = %+v, want verdict %v with reason containing %q", got, tt.want, tt.wantText)
			}
			if got.PreserveAllowed() != (tt.want == ReviewOnlyNotReview) || got.KeepLocalBranch() != (tt.want == ReviewOnlyUnverified) {
				t.Fatalf("decision helpers disagree with verdict: %+v", got)
			}
		})
	}
}
