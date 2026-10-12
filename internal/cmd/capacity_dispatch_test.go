package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/scheduler/capacity"
)

func TestShouldFireCrossRigEscalation_Debounces(t *testing.T) {
	resetCrossRigEscalationStateForTest()
	t.Cleanup(resetCrossRigEscalationStateForTest)

	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	if !shouldFireCrossRigEscalation("walletui", "hq", now) {
		t.Fatalf("first call must fire")
	}
	// Second call inside the debounce window must NOT fire.
	if shouldFireCrossRigEscalation("walletui", "hq", now.Add(30*time.Minute)) {
		t.Fatalf("second call inside debounce window must not fire")
	}
	// After the debounce window elapses, fire again.
	if !shouldFireCrossRigEscalation("walletui", "hq", now.Add(crossRigEscalationDebounce+time.Minute)) {
		t.Fatalf("call past debounce window must fire")
	}
}

func TestShouldFireCrossRigEscalation_KeyedByRigAndPrefix(t *testing.T) {
	resetCrossRigEscalationStateForTest()
	t.Cleanup(resetCrossRigEscalationStateForTest)

	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)

	if !shouldFireCrossRigEscalation("walletui", "hq", now) {
		t.Fatalf("walletui/hq first call must fire")
	}
	// Different rig — should fire independently.
	if !shouldFireCrossRigEscalation("furiosa", "hq", now) {
		t.Fatalf("furiosa/hq must fire (different rig)")
	}
	// Different prefix on same rig — should fire independently.
	if !shouldFireCrossRigEscalation("walletui", "wisp", now) {
		t.Fatalf("walletui/wisp must fire (different prefix)")
	}
	// Same (rig, prefix) repeats — debounced.
	if shouldFireCrossRigEscalation("walletui", "hq", now.Add(time.Minute)) {
		t.Fatalf("walletui/hq repeat must not fire")
	}
}

func TestDispatchSingleBeadRawReviewOnlyHookFailureClearsMetadata(t *testing.T) {
	townRoot, _, descPath := setupMutableBDRawSlingTest(t, "Keep this body.")

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
		assertHasRawReviewMetadata(t, readMutableBDDescription(t, descPath))
		return errors.New("forced hook failure")
	}

	_, err := dispatchSingleBead(capacity.PendingBead{
		ID:         "gt-context",
		WorkBeadID: "gt-rawrollback",
		TargetRig:  "gastown",
		Context: &capacity.SlingContextFields{
			WorkBeadID:  "gt-rawrollback",
			TargetRig:   "gastown",
			HookRawBead: true,
			NoMerge:     true,
			ReviewOnly:  true,
		},
	}, townRoot, "test")
	if err == nil {
		t.Fatal("expected scheduler dispatch hook failure")
	}
	assertNoRawReviewMetadata(t, readMutableBDDescription(t, descPath))
}

func TestListBlockedWorkBeadIDStatesPartialFailureFailsClosedPerGroup(t *testing.T) {
	townRoot := t.TempDir()
	townBeadsDir := filepath.Join(townRoot, ".beads")
	if err := os.MkdirAll(townBeadsDir, 0o755); err != nil {
		t.Fatalf("mkdir town beads: %v", err)
	}
	routes := []beads.Route{
		{Prefix: "a-", Path: "rig-a"},
		{Prefix: "b-", Path: "rig-b"},
	}
	if err := beads.WriteRoutes(townBeadsDir, routes); err != nil {
		t.Fatalf("write routes: %v", err)
	}

	blocked, unknown, err := listBlockedWorkBeadIDStatesWithRunner(townRoot, []string{"a-ready", "b-ready", "b-other"}, func(beadsDir string, groupedIDs []string) ([]byte, error) {
		switch groupedIDs[0][:1] {
		case "a":
			return []byte(`[{"id":"a-ready"}]`), nil
		case "b":
			return nil, fmt.Errorf("blocked query failed")
		default:
			return nil, fmt.Errorf("unexpected group %s", beadsDir)
		}
	})
	if err != nil {
		t.Fatalf("partial blocked query failure returned error: %v", err)
	}
	if !blocked["a-ready"] {
		t.Fatalf("a-ready should be marked blocked from successful group")
	}
	if unknown["a-ready"] {
		t.Fatalf("a-ready should not be blocked-unknown")
	}
	if !unknown["b-ready"] || !unknown["b-other"] {
		t.Fatalf("failed group IDs should be blocked-unknown, got %#v", unknown)
	}

	_, unknown, err = listBlockedWorkBeadIDStatesWithRunner(townRoot, []string{"a-ready", "b-ready"}, func(string, []string) ([]byte, error) {
		return []byte(`not-json`), nil
	})
	if err == nil {
		t.Fatalf("all blocked query JSON failures should return an error")
	}
	if !unknown["a-ready"] || !unknown["b-ready"] {
		t.Fatalf("all failed groups should mark every ID blocked-unknown, got %#v", unknown)
	}
}

func TestIsScheduledWorkBeadReadyFailsClosedForBlockedUnknown(t *testing.T) {
	info := beadStatusInfo{Status: "open"}
	if isScheduledWorkBeadReady("gt-ready", info, true, nil, map[string]bool{"gt-ready": true}) {
		t.Fatalf("blocked-unknown source must not be scheduler-ready")
	}
}

func writeReadinessRigSettings(t *testing.T, townRoot, rig, mode string) {
	t.Helper()
	dir := filepath.Join(townRoot, rig, "settings")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf(`{"type":"rig-settings","version":1,"readiness":%q}`, mode)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readinessAssessment(id, rig string, info beadStatusInfo, fields capacity.SlingContextFields) scheduledContextAssessment {
	fields.WorkBeadID = id
	fields.TargetRig = rig
	info.Status = "open"
	return scheduledContextAssessment{
		context: slingContextRecord{issue: &beads.Issue{ID: "ctx-" + id}},
		fields:  &fields,
		info:    info,
		found:   true,
		ready:   true,
	}
}

func TestReadySlingContexts_SkipsNotReady_OthersDispatched(t *testing.T) {
	townRoot := t.TempDir()
	writeReadinessRigSettings(t, townRoot, "strict", "enforce")
	writeReadinessRigSettings(t, townRoot, "lenient", "warn")

	good := beadStatusInfo{IssueType: "task", Description: "d", AcceptanceCriteria: "- [ ] a"}
	bad := beadStatusInfo{IssueType: "feature", Description: "d"}
	epic := beadStatusInfo{IssueType: "epic"}

	assessments := []scheduledContextAssessment{
		readinessAssessment("gt-bad", "strict", bad, capacity.SlingContextFields{}),
		readinessAssessment("gt-good", "strict", good, capacity.SlingContextFields{}),
		readinessAssessment("gt-epic", "strict", epic, capacity.SlingContextFields{}),
		readinessAssessment("gt-override", "strict", bad, capacity.SlingContextFields{ForceUnready: true, UnreadyReason: "hotfix"}),
		readinessAssessment("gt-warnrig", "lenient", bad, capacity.SlingContextFields{}),
	}

	got := readySlingContextsFromAssessments(townRoot, assessments)

	var ids []string
	for _, b := range got {
		ids = append(ids, b.WorkBeadID)
	}
	want := []string{"gt-good", "gt-epic", "gt-override", "gt-warnrig"}
	if fmt.Sprint(ids) != fmt.Sprint(want) {
		t.Fatalf("dispatched = %v, want %v (gt-bad must be skipped as not_ready)", ids, want)
	}
}

func TestBeadStatusInfoFromBeadInfo_CarriesContractFields(t *testing.T) {
	got := beadStatusInfoFromBeadInfo(&beadInfo{IssueType: "bug", Description: "d", AcceptanceCriteria: "- [ ] a"})
	if got.IssueType != "bug" || got.Description != "d" || got.AcceptanceCriteria != "- [ ] a" {
		t.Fatalf("contract fields not carried: %+v", got)
	}
}
