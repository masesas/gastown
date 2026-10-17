package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
)

// Regression tests for gt-pd1c.69: review-only mode must stick to the bead and
// survive every re-dispatch path, not only the first --review-only sling.

const staleReviewOnlyAttachedAt = "2026-01-01T00:00:00Z"

func reviewOnlyBeadDescription(extra ...string) string {
	lines := append([]string{
		"attached_at: " + staleReviewOnlyAttachedAt,
		"review_only: true",
	}, extra...)
	return strings.Join(append(lines, "", "Keep this body."), "\n")
}

// assertFreshReviewOnly checks that desc carries review_only and an
// assignment timestamp written after start (gt done needs a fresh one).
func assertFreshReviewOnly(t *testing.T, desc string, start time.Time) {
	t.Helper()
	fields := beads.ParseAttachmentFields(&beads.Issue{Description: desc})
	if fields == nil || !fields.ReviewOnly {
		t.Fatalf("review_only lost on re-dispatch:\n%s", desc)
	}
	at, err := time.Parse(time.RFC3339Nano, fields.AttachedAt)
	if err != nil {
		t.Fatalf("attached_at %q is not RFC3339Nano: %v\n%s", fields.AttachedAt, err, desc)
	}
	if at.Before(start.Add(-time.Second)) {
		t.Fatalf("attached_at %s was not refreshed by the re-dispatch (start %s):\n%s", fields.AttachedAt, start.Format(time.RFC3339Nano), desc)
	}
	if !strings.Contains(desc, "Keep this body.") {
		t.Fatalf("description body lost:\n%s", desc)
	}
}

func TestBurnExistingMoleculesKeepsReviewOnly(t *testing.T) {
	initial := reviewOnlyBeadDescription("attached_molecule: gt-wisp-old", "attached_formula: mol-polecat-work")
	townRoot, _, descPath := setupMutableBDRawSlingTest(t, initial)

	if err := burnExistingMolecules([]string{"gt-wisp-old"}, "gt-rawrollback", townRoot); err != nil {
		t.Fatalf("burnExistingMolecules: %v", err)
	}

	desc := readMutableBDDescription(t, descPath)
	fields := beads.ParseAttachmentFields(&beads.Issue{Description: desc})
	if fields == nil || !fields.ReviewOnly {
		t.Fatalf("burning the previous molecule dropped review_only:\n%s", desc)
	}
	if fields.AttachedMolecule != "" || fields.AttachedFormula != "" {
		t.Fatalf("burn left the stale molecule attachment behind: %+v", fields)
	}
}

func TestRunSlingWithoutFlagInheritsReviewOnly(t *testing.T) {
	townRoot, _, descPath := setupMutableBDRawSlingTest(t, reviewOnlyBeadDescription())
	workDir := filepath.Join(townRoot, "gastown", "crew", "toast")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workDir: %v", err)
	}

	prevHookRaw, prevNoMerge, prevReviewOnly := slingHookRawBead, slingNoMerge, slingReviewOnly
	prevNoConvoy, prevDryRun := slingNoConvoy, slingDryRun
	prevResolve, prevHook := resolveTargetAgentFn, hookBeadWithRetryFn
	t.Cleanup(func() {
		slingHookRawBead, slingNoMerge, slingReviewOnly = prevHookRaw, prevNoMerge, prevReviewOnly
		slingNoConvoy, slingDryRun = prevNoConvoy, prevDryRun
		resolveTargetAgentFn, hookBeadWithRetryFn = prevResolve, prevHook
	})
	slingHookRawBead = true
	slingNoMerge = false
	slingReviewOnly = false // plain re-sling: no --review-only flag
	slingNoConvoy = true
	slingDryRun = false
	resolveTargetAgentFn = func(target string) (string, string, string, error) {
		return "gastown/crew/toast", "", workDir, nil
	}
	start := time.Now()
	hooked := false
	hookBeadWithRetryFn = func(beadID, targetAgent, hookDir string) error {
		hooked = true
		// The fresh review-only assignment must be on the bead before the
		// assignee can see the hook.
		assertFreshReviewOnly(t, readMutableBDDescription(t, descPath), start)
		return nil
	}

	if err := runSling(nil, []string{"gt-rawrollback", "gastown/crew/toast"}); err != nil {
		t.Fatalf("runSling: %v", err)
	}
	if !hooked {
		t.Fatal("bead was never hooked")
	}
	assertFreshReviewOnly(t, readMutableBDDescription(t, descPath), start)
}

func TestExecuteSlingWithoutFlagInheritsReviewOnly(t *testing.T) {
	townRoot, rigPath, descPath := setupMutableBDRawSlingTest(t, reviewOnlyBeadDescription())

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
			Pane:        "%1",
		}, nil
	}
	start := time.Now()
	hookBeadWithRetryWithTownRootFn = func(beadID, targetAgent, hookDir, townRoot string) error {
		assertFreshReviewOnly(t, readMutableBDDescription(t, descPath), start)
		return nil
	}

	// Capacity dispatch, batch sling and convoy relaunch all reach executeSling
	// with ReviewOnly=false when the dispatcher did not pass --review-only.
	result, err := executeSling(SlingParams{
		BeadID:      "gt-rawrollback",
		RigName:     "gastown",
		TownRoot:    townRoot,
		BeadsDir:    filepath.Join(rigPath, ".beads"),
		HookRawBead: true,
		NoConvoy:    true,
		NoBoot:      true,
	})
	if err != nil {
		t.Fatalf("executeSling: %v", err)
	}
	if result == nil || !result.Success {
		t.Fatalf("executeSling result not successful: %+v", result)
	}
	assertFreshReviewOnly(t, readMutableBDDescription(t, descPath), start)
}

func TestOutputMoleculeWorkflowReviewOnlyReplacesMergeQueueReminder(t *testing.T) {
	townRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(townRoot, "myrig"), 0o755); err != nil {
		t.Fatalf("mkdir rig: %v", err)
	}
	attachment := &beads.AttachmentFields{AttachedFormula: "mol-polecat-work", ReviewOnly: true}

	output := captureStdout(t, func() {
		if err := outputMoleculeWorkflow(RoleContext{Role: RolePolecat, Rig: "myrig", TownRoot: townRoot}, attachment); err != nil {
			t.Fatalf("outputMoleculeWorkflow: %v", err)
		}
	})
	if !strings.Contains(output, "REVIEW-ONLY") {
		t.Fatalf("review-only bead got no review-only directive:\n%s", output)
	}
	if strings.Contains(output, "to submit to the merge queue") {
		t.Fatalf("review-only workflow still demands a merge-queue submit:\n%s", output)
	}
}

func TestCheckSlungWorkShowsReviewOnlyDirectiveForRawBead(t *testing.T) {
	prevShow := primeShowBead
	t.Cleanup(func() { primeShowBead = prevShow })
	bead := &beads.Issue{ID: "gt-leg", Title: "review leg", Description: "review_only: true\n\nReview the brief."}
	primeShowBead = func(ctx RoleContext, id string) (*beads.Issue, error) { return bead, nil }

	output := captureStdout(t, func() {
		if _, err := checkSlungWork(RoleContext{Role: RolePolecat, Rig: "myrig", TownRoot: t.TempDir()}, bead); err != nil {
			t.Fatalf("checkSlungWork: %v", err)
		}
	})
	if !strings.Contains(output, "REVIEW-ONLY") {
		t.Fatalf("raw review-only bead got no review-only directive:\n%s", output)
	}
}
