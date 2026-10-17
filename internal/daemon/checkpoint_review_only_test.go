package daemon

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/testutil"
)

// gt-pd1c.69: the checkpoint dog must not create "WIP: checkpoint (auto)"
// commits on review-only work; such commits lock the leg in gt done.
func TestCheckpointPolecatSkipsReviewOnlyWork(t *testing.T) {
	for _, tt := range []struct {
		name           string
		branch         string
		descriptions   map[string]string
		hooked         []string
		failAll        bool
		wantCheckpoint bool
	}{
		{"review leg by branch", "polecat/nuka/gt-review+abc", map[string]string{"gt-review": "review_only: true"}, nil, false, false},
		{"review leg by hook", "polecat/nuka/gt-code+abc", map[string]string{"gt-code": "code", "gt-review": "review_only: true"}, []string{"gt-review"}, false, false},
		{"status unverifiable", "polecat/nuka/gt-code+abc", nil, nil, true, false},
		{"ordinary work", "polecat/nuka/gt-code+abc", map[string]string{"gt-code": "code"}, nil, false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			townRoot := t.TempDir()
			for _, dir := range []string{filepath.Join("rig", ".beads"), filepath.Join("rig", "polecats", "nuka")} {
				if err := os.MkdirAll(filepath.Join(townRoot, dir), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			testutil.InstallBDShowStubWithHooked(t, tt.descriptions, tt.hooked, tt.failAll)
			beads.ResetBdAllowStaleCacheForTest()
			t.Cleanup(beads.ResetBdAllowStaleCacheForTest)
			workDir, _ := testutil.NewUnpushedPolecatBranch(t, tt.branch)
			if err := os.WriteFile(filepath.Join(workDir, "scratch.md"), []byte("wip\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			before := strings.TrimSpace(mustRunGit(t, workDir, "rev-parse", "HEAD"))

			d := &Daemon{logger: log.New(io.Discard, "", 0), config: &Config{TownRoot: townRoot}}
			got := d.checkpointPolecat("rig", "nuka", workDir)

			after := strings.TrimSpace(mustRunGit(t, workDir, "rev-parse", "HEAD"))
			if got != tt.wantCheckpoint || (after != before) != tt.wantCheckpoint {
				t.Fatalf("checkpoint created = %v (HEAD %s -> %s), want %v", got, before, after, tt.wantCheckpoint)
			}
		})
	}
}
