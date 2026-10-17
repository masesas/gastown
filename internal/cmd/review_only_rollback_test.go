package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
)

// gt-pd1c.75 (R2-5): a failed gt sling --review-only restores the bead's
// prior review-only state; a failed restore keeps the marker (fail closed).

func stubReviewOnlySlingHookFailure(t *testing.T, townRoot string) {
	t.Helper()
	prevSpawn := spawnPolecatForSling
	prevHook := hookBeadWithRetryWithTownRootFn
	t.Cleanup(func() {
		spawnPolecatForSling = prevSpawn
		hookBeadWithRetryWithTownRootFn = prevHook
	})
	spawnPolecatForSling = func(rigName string, opts SlingSpawnOptions) (*SpawnedPolecatInfo, error) {
		return &SpawnedPolecatInfo{
			RigName:     rigName,
			PolecatName: "toast",
			ClonePath:   filepath.Join(townRoot, "gastown", "polecats", "toast"),
		}, nil
	}
	hookBeadWithRetryWithTownRootFn = func(beadID, targetAgent, hookDir, townRoot string) error {
		return errors.New("forced hook failure")
	}
}

func reviewOnlySlingParams(townRoot, rigPath string) SlingParams {
	return SlingParams{
		BeadID:      "gt-rawrollback",
		RigName:     "gastown",
		TownRoot:    townRoot,
		BeadsDir:    filepath.Join(rigPath, ".beads"),
		HookRawBead: true,
		ReviewOnly:  true,
		NoConvoy:    true,
		NoBoot:      true,
	}
}

func TestExecuteSlingReviewOnlyHookFailureKeepsPriorReviewOnly(t *testing.T) {
	townRoot, rigPath, descPath := setupMutableBDRawSlingTest(t, "review_only: true\n\nKeep this body.")
	stubReviewOnlySlingHookFailure(t, townRoot)

	if _, err := executeSling(reviewOnlySlingParams(townRoot, rigPath)); err == nil {
		t.Fatal("expected hook failure from executeSling")
	}
	assertRollbackKeptOnlyReviewOnly(t, readMutableBDDescription(t, descPath))
}

func TestExecuteSlingReviewOnlyHookFailureKeepsPriorNonCanonicalValue(t *testing.T) {
	townRoot, rigPath, descPath := setupMutableBDRawSlingTest(t, "review_only: off\n\nKeep this body.")
	stubReviewOnlySlingHookFailure(t, townRoot)

	if _, err := executeSling(reviewOnlySlingParams(townRoot, rigPath)); err == nil {
		t.Fatal("expected hook failure from executeSling")
	}
	desc := readMutableBDDescription(t, descPath)
	if !strings.Contains(desc, "review_only: off") || !beads.IsReviewOnly(&beads.Issue{Description: desc}) {
		t.Fatalf("rollback changed the prior non-canonical marker:\n%s", desc)
	}
}

func TestRestoreRollbackKeepsReviewOnlyWhenRestoreWriteFails(t *testing.T) {
	current := "attached_at: 2026-10-17T00:00:00Z\nreview_only: true\n\nKeep this body."
	townRoot, _, descPath := setupMutableBDRawSlingTest(t, current)
	if err := os.Chmod(descPath, 0o444); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(descPath, 0o644) })

	restoreRollbackRawWorkflowFieldsFromCurrent("gt-rawrollback", townRoot, filepath.Join(townRoot, "gastown", "polecats", "toast"), &beadInfo{Description: "Keep this body."})

	if desc := readMutableBDDescription(t, descPath); desc != current {
		t.Fatalf("a failed restore must leave the bead as it was (review-only):\n%s", desc)
	}
}

func TestRestoreRollbackKeepsReviewOnlyWhenOriginalUnknown(t *testing.T) {
	townRoot, _, descPath := setupMutableBDRawSlingTest(t, "attached_at: 2026-10-17T00:00:00Z\nno_merge: true\nreview_only: true\n\nKeep this body.")
	info := &beadInfo{Description: readMutableBDDescription(t, descPath)}

	if _, err := clearRollbackRawWorkflowFields("gt-rawrollback", townRoot, filepath.Join(townRoot, "gastown", "polecats", "toast"), info); err != nil {
		t.Fatalf("clearRollbackRawWorkflowFields: %v", err)
	}
	assertRollbackKeptOnlyReviewOnly(t, readMutableBDDescription(t, descPath))
}

func TestRestoreRollbackDropsOnlyMarkerAddedBySling(t *testing.T) {
	townRoot, _, descPath := setupMutableBDRawSlingTest(t, "attached_at: 2026-10-17T00:00:00Z\nreview_only: true\ndispatched_by: mayor/\n\nKeep this body.\n```\nreview_only: true\n```")
	original := &beadInfo{Description: "dispatched_by: mayor/\n\nKeep this body.\n```\nreview_only: true\n```"}

	restoreRollbackRawWorkflowFieldsFromCurrent("gt-rawrollback", townRoot, filepath.Join(townRoot, "gastown", "polecats", "toast"), original)

	desc := readMutableBDDescription(t, descPath)
	assertRollbackRestoredNonReviewBead(t, desc)
	if !strings.Contains(desc, "dispatched_by: mayor/") || !strings.Contains(desc, "```\nreview_only: true\n```") {
		t.Fatalf("rollback touched more than the sling's fields:\n%s", desc)
	}
}
