package doctor

import (
	"os"
	"path/filepath"
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
