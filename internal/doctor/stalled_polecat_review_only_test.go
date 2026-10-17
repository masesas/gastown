package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/testutil"
)

// gt-pd1c.69: `gt doctor --fix` must not push a stalled review leg's branch.
func TestStalledPolecatFixSkipsReviewOnlyLeg(t *testing.T) {
	const branch = "polecat/pipboy/gt-review+mvbqtobe"
	townRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(townRoot, "gastown", ".beads"), 0o755); err != nil {
		t.Fatalf("mkdir beads: %v", err)
	}
	testutil.InstallBDShowStub(t, map[string]string{"gt-review": "review_only: true"}, false)
	beads.ResetBdAllowStaleCacheForTest()
	t.Cleanup(beads.ResetBdAllowStaleCacheForTest)
	clone, remote := testutil.NewUnpushedPolecatBranch(t, branch)

	c := NewStalledPolecatCheck()
	c.stalledPolecats = []stalledPolecatInfo{{name: "pipboy", rigName: "gastown", branch: branch, unpushedCount: 1, clonePath: clone}}
	_ = c.Fix(&CheckContext{TownRoot: townRoot})

	if testutil.RemoteHasBranch(t, remote, branch) {
		t.Fatal("doctor --fix pushed the branch of a review-only leg to origin")
	}
}

func newStalledFixFixture(t *testing.T, descriptions map[string]string, hookedIDs []string, failAll bool, branch string) (*StalledPolecatCheck, *CheckContext, string) {
	t.Helper()
	townRoot := t.TempDir()
	for _, dir := range []string{filepath.Join("gastown", ".beads"), filepath.Join("gastown", "polecats", "pipboy")} {
		if err := os.MkdirAll(filepath.Join(townRoot, dir), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	testutil.InstallBDShowStubWithHooked(t, descriptions, hookedIDs, failAll)
	beads.ResetBdAllowStaleCacheForTest()
	t.Cleanup(beads.ResetBdAllowStaleCacheForTest)
	clone, remote := testutil.NewUnpushedPolecatBranch(t, branch)
	c := NewStalledPolecatCheck()
	c.stalledPolecats = []stalledPolecatInfo{{name: "pipboy", rigName: "gastown", branch: branch, unpushedCount: 1, clonePath: clone}}
	return c, &CheckContext{TownRoot: townRoot}, remote
}

func TestStalledPolecatFixHonoursHookedReviewLeg(t *testing.T) {
	const branch = "polecat/pipboy/gt-code+abc"
	c, ctx, remote := newStalledFixFixture(t, map[string]string{
		"gt-code":   "Implement the thing.",
		"gt-review": "review_only: true",
	}, []string{"gt-review"}, false, branch)
	_ = c.Fix(ctx)
	if testutil.RemoteHasBranch(t, remote, branch) {
		t.Fatal("doctor --fix pushed although the polecat's hooked bead is review-only")
	}
}

func TestStalledPolecatFixReportsUnverifiedDeferral(t *testing.T) {
	const branch = "polecat/pipboy/gt-code+abc"
	c, ctx, remote := newStalledFixFixture(t, nil, nil, true, branch)
	err := c.Fix(ctx)
	if testutil.RemoteHasBranch(t, remote, branch) {
		t.Fatal("doctor --fix pushed although the review-only status could not be read")
	}
	if err == nil || !strings.Contains(err.Error(), "pipboy") || !strings.Contains(err.Error(), "deferred") {
		t.Fatalf("Fix error = %v, want a reported deferral naming the polecat", err)
	}
}
