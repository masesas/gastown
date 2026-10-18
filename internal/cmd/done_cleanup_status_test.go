package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/gastown/internal/git"
)

// gt-pd1c.96: cleanup_status records a fact gt computes, not an agent claim.

func TestDoneHasNoAgentSuppliedCleanupStatusFlag(t *testing.T) {
	if f := doneCmd.Flags().Lookup("cleanup-status"); f != nil {
		t.Fatalf("gt done still accepts --cleanup-status (agent-supplied cleanup_status): %+v", f)
	}
}

func TestDetectDoneCleanupStatus(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, clone string)
		want   string
	}{
		{name: "unpushed commit", mutate: func(*testing.T, string) {}, want: "unpushed"},
		{name: "uncommitted file", mutate: func(t *testing.T, clone string) {
			safetyProofGit(t, clone, "push", "origin", "HEAD")
			if err := os.WriteFile(filepath.Join(clone, "draft.go"), []byte("package draft\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, want: "uncommitted"},
		{name: "clean and pushed", mutate: func(t *testing.T, clone string) {
			safetyProofGit(t, clone, "push", "origin", "HEAD")
		}, want: "clean"},
		{name: "git status error", mutate: func(t *testing.T, clone string) {
			if err := os.WriteFile(filepath.Join(clone, ".git", "index"), []byte("not a git index\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, want: "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, clone := newSafetyProofTarget(t)
			tt.mutate(t, clone)
			if got := detectDoneCleanupStatus(git.NewGit(clone), "polecat/toast/gt-work"); got != tt.want {
				t.Fatalf("detectDoneCleanupStatus = %q, want %q", got, tt.want)
			}
		})
	}
}

// An undeterminable status is recorded as "unknown" so the bead does not keep
// a stale "clean" from an earlier run.
func TestRecordedCleanupStatus(t *testing.T) {
	for in, want := range map[string]string{
		"":            "",
		"clean":       "clean",
		"uncommitted": "has_uncommitted",
		"unpushed":    "has_unpushed",
		"stash":       "has_stash",
		"unknown":     "unknown",
	} {
		if got := recordedCleanupStatus(in); got != want {
			t.Errorf("recordedCleanupStatus(%q) = %q, want %q", in, got, want)
		}
	}
}
