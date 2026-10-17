package cmd

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
)

// Regression tests for gt-pd1c.43: execution blockers that are not part of
// the staged set (gate beads hidden by `bd list --parent`, beads outside the
// epic tree) must gate their consumers instead of being dropped.

// stageEpicForTest collects an epic through the stubbed bd, builds the DAG and
// computes waves, mirroring the staging pipeline.
func stageEpicForTest(t *testing.T, epicID string) (*ConvoyDAG, []Wave, []GatedTask) {
	t.Helper()
	input := &StageInput{Kind: StageInputEpic, IDs: []string{epicID}}
	beadList, deps, err := collectBeads(input)
	if err != nil {
		t.Fatalf("collectBeads: %v", err)
	}
	dag := buildConvoyDAG(beadList, deps)
	waves, gated, err := computeWaves(dag)
	if err != nil {
		t.Fatalf("computeWaves: %v", err)
	}
	return dag, waves, gated
}

func waveTaskSet(waves []Wave) map[string]int {
	set := make(map[string]int)
	for _, w := range waves {
		for _, id := range w.Tasks {
			set[id] = w.Number
		}
	}
	return set
}

func gatedBy(gated []GatedTask, taskID string) ([]string, bool) {
	for _, g := range gated {
		if g.TaskID == taskID {
			return g.GatedBy, true
		}
	}
	return nil, false
}

func findingsOf(findings []StagingFinding, category string) []StagingFinding {
	var out []StagingFinding
	for _, f := range findings {
		if f.Category == category {
			out = append(out, f)
		}
	}
	return out
}

// (a) A gate-type child is hidden by `bd list --parent` (bd 1.3.0), so it is
// never collected. Its open `blocks` edge must still gate the consumer.
func TestStageExternalBlocker_HiddenGateGatesConsumer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows — shell stubs")
	}
	dag := newTestDAG(t).
		Epic("gt-epic", "Epic").
		Task("gt-skel", "Skeleton", withRig("gastown")).ParentOf("gt-epic").
		Task("gt-cons", "Consumer", withRig("gastown")).ParentOf("gt-epic").BlockedBy("gt-skel").BlockedBy("gt-gatex").
		// The gate is a child in reality, but bd list --parent hides it: no Parent row.
		Task("gt-gatex", "Approval gate", withRig("gastown"))
	dag.beads["gt-gatex"].Type = "gate"
	dag.Setup(t)

	staged, waves, gated := stageEpicForTest(t, "gt-epic")

	inWaves := waveTaskSet(waves)
	if _, ok := inWaves["gt-cons"]; ok {
		t.Fatalf("consumer of open hidden gate must not be in a wave, waves=%+v", waves)
	}
	if inWaves["gt-skel"] != 1 {
		t.Errorf("skeleton should be wave 1, waves=%+v", waves)
	}
	by, ok := gatedBy(gated, "gt-cons")
	if !ok || len(by) != 1 || by[0] != "gt-gatex" {
		t.Fatalf("gt-cons should be gated by gt-gatex, gated=%+v", gated)
	}
	ext := staged.External["gt-gatex"]
	if ext == nil || ext.Type != "gate" || ext.Status != "open" || ext.Err != nil {
		t.Fatalf("expected resolved external gate, got %+v", ext)
	}
	if _, ok := staged.Nodes["gt-gatex"]; ok {
		t.Error("external blocker must not become a staged node (it would be tracked/dispatched)")
	}
	msgs := gatedFindings(gated, staged)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Message, "gt-gatex (outside staged set: gate, open)") {
		t.Errorf("gated finding should name the external blocker, got %+v", msgs)
	}
}

// (b) An open blocker outside the epic tree gates the consumer, and the
// blocker itself is neither staged nor placed in a wave.
func TestStageExternalBlocker_OpenOutsideEpicGatesConsumer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows — shell stubs")
	}
	dag := newTestDAG(t).
		Epic("gt-epic", "Epic").
		Task("gt-cons", "Consumer", withRig("gastown")).ParentOf("gt-epic").BlockedBy("gt-outer").
		Task("gt-free", "Independent", withRig("gastown")).ParentOf("gt-epic").
		Task("gt-outer", "Task of another epic", withRig("gastown"))
	dag.Setup(t)

	staged, waves, gated := stageEpicForTest(t, "gt-epic")

	inWaves := waveTaskSet(waves)
	if _, ok := inWaves["gt-cons"]; ok {
		t.Fatalf("consumer of open outside blocker must not be in a wave, waves=%+v", waves)
	}
	if _, ok := inWaves["gt-outer"]; ok {
		t.Fatalf("outside blocker must never be scheduled by this convoy, waves=%+v", waves)
	}
	if inWaves["gt-free"] != 1 {
		t.Errorf("independent task should be wave 1, waves=%+v", waves)
	}
	if by, ok := gatedBy(gated, "gt-cons"); !ok || len(by) != 1 || by[0] != "gt-outer" {
		t.Fatalf("gt-cons should be gated by gt-outer, gated=%+v", gated)
	}
	for _, id := range dagSlingableIDs(staged) {
		if id == "gt-outer" {
			t.Fatal("outside blocker must not be tracked by the staged convoy")
		}
	}
}

// (c) A closed blocker outside the epic tree is satisfied: the consumer is
// scheduled normally.
func TestStageExternalBlocker_ClosedOutsideEpicDoesNotGate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows — shell stubs")
	}
	dag := newTestDAG(t).
		Epic("gt-epic", "Epic").
		Task("gt-cons", "Consumer", withRig("gastown")).ParentOf("gt-epic").BlockedBy("gt-donex").
		Task("gt-donex", "Finished elsewhere", withRig("gastown")).WithStatus("closed")
	dag.Setup(t)

	_, waves, gated := stageEpicForTest(t, "gt-epic")

	if len(gated) != 0 {
		t.Fatalf("closed outside blocker must not gate, gated=%+v", gated)
	}
	if waveTaskSet(waves)["gt-cons"] != 1 {
		t.Fatalf("consumer should be wave 1, waves=%+v", waves)
	}
}

// (d) A blocker whose status cannot be read fails closed: the consumer is
// gated and staging warns.
func TestStageExternalBlocker_LookupErrorGatesAndWarns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows — shell stubs")
	}
	dag := newTestDAG(t).
		Epic("gt-epic", "Epic").
		Task("gt-cons", "Consumer", withRig("gastown")).ParentOf("gt-epic").BlockedBy("gt-ghost").
		Task("gt-free", "Independent", withRig("gastown")).ParentOf("gt-epic")
	dag.Setup(t) // gt-ghost is unknown to the stub: bd show exits 1

	staged, waves, gated := stageEpicForTest(t, "gt-epic")

	if _, ok := waveTaskSet(waves)["gt-cons"]; ok {
		t.Fatalf("consumer of unreadable blocker must not be in a wave, waves=%+v", waves)
	}
	if by, ok := gatedBy(gated, "gt-cons"); !ok || len(by) != 1 || by[0] != "gt-ghost" {
		t.Fatalf("gt-cons should be gated by gt-ghost, gated=%+v", gated)
	}
	ext := staged.External["gt-ghost"]
	if ext == nil || ext.Err == nil {
		t.Fatalf("expected unresolved external blocker with error, got %+v", ext)
	}

	warns := findingsOf(detectWarnings(staged, &StageInput{Kind: StageInputEpic, IDs: []string{"gt-epic"}}), "unresolved-blocker")
	if len(warns) != 1 {
		t.Fatalf("expected 1 unresolved-blocker warning, got %+v", warns)
	}
	if warns[0].Severity != "warning" || len(warns[0].BeadIDs) != 2 ||
		warns[0].BeadIDs[0] != "gt-ghost" || warns[0].BeadIDs[1] != "gt-cons" {
		t.Errorf("warning should name blocker then consumer, got %+v", warns[0])
	}
	if strings.Contains(warns[0].Message, "not found") {
		t.Errorf("warning must not echo raw bd output, got %q", warns[0].Message)
	}
}

// The lookup runs once per external blocker, only for execution edges of
// slingable dependents, and wrapped external:<rig>:<id> targets are normalised.
func TestBuildConvoyDAG_ExternalBlockerLookupScope(t *testing.T) {
	calls := map[string]int{}
	orig := externalBlockerLookupFn
	t.Cleanup(func() { externalBlockerLookupFn = orig })
	externalBlockerLookupFn = func(id string) (*bdShowResult, error) {
		calls[id]++
		switch id {
		case "x-open":
			return &bdShowResult{ID: id, Status: "in_progress", IssueType: "task"}, nil
		case "x-tomb":
			return &bdShowResult{ID: id, Status: "tombstone", IssueType: "task"}, nil
		}
		return nil, errors.New("boom")
	}

	beadList := []BeadInfo{
		{ID: "e", Type: "epic", Status: "open"},
		{ID: "a", Type: "task", Status: "open"},
		{ID: "b", Type: "task", Status: "open"},
		{ID: "in", Type: "task", Status: "open"},
	}
	deps := []DepInfo{
		{IssueID: "a", DependsOnID: "x-open", Type: "blocks"},
		{IssueID: "b", DependsOnID: "external:other:x-open", Type: "waits-for"},
		{IssueID: "b", DependsOnID: "x-tomb", Type: "conditional-blocks"},
		{IssueID: "a", DependsOnID: "x-rel", Type: "related"},         // not an execution edge
		{IssueID: "e", DependsOnID: "x-epicdep", Type: "blocks"},      // dependent not slingable
		{IssueID: "a", DependsOnID: "x-parent", Type: "parent-child"}, // hierarchy only
		{IssueID: "b", DependsOnID: "external:gt:in", Type: "blocks"}, // wrapped in-set target
	}
	dag := buildConvoyDAG(beadList, deps)

	if calls["x-open"] != 1 || calls["x-tomb"] != 1 || len(calls) != 2 {
		t.Fatalf("unexpected lookups: %v", calls)
	}
	if got := dag.Nodes["b"].BlockedBy; len(got) != 3 {
		t.Fatalf("b BlockedBy = %v, want x-open, x-tomb, in", got)
	}
	waves, gated, err := computeWaves(dag)
	if err != nil {
		t.Fatal(err)
	}
	if set := waveTaskSet(waves); set["in"] != 1 {
		t.Errorf("in-set task should be wave 1, waves=%+v", waves)
	}
	byA, _ := gatedBy(gated, "a")
	byB, _ := gatedBy(gated, "b")
	if len(byA) != 1 || byA[0] != "x-open" || len(byB) != 1 || byB[0] != "x-open" {
		t.Errorf("a and b should be gated only by x-open (tombstone ignored), gated=%+v", gated)
	}
}

// Launch relaunch path: a ready-looking task with an open external blocker is
// not dispatched.
func TestReadyUndispatchedTasks_ExternalBlocker(t *testing.T) {
	dag := &ConvoyDAG{
		Nodes: map[string]*ConvoyDAGNode{
			"t1": {ID: "t1", Type: "task", Status: "open", BlockedBy: []string{"x-open"}},
			"t2": {ID: "t2", Type: "task", Status: "open", BlockedBy: []string{"x-done"}},
			"t3": {ID: "t3", Type: "task", Status: "open", BlockedBy: []string{"x-err"}},
		},
		External: map[string]*ExternalBlocker{
			"x-open": {ID: "x-open", Status: "open"},
			"x-done": {ID: "x-done", Status: "closed"},
			"x-err":  {ID: "x-err", Err: errors.New("unreadable")},
		},
	}
	got := readyUndispatchedTasks(dag, func(string) bool { return false })
	if len(got) != 1 || got[0] != "t2" {
		t.Fatalf("ready = %v, want [t2]", got)
	}
}

// Output names which outside blocker holds which consumer, in text and JSON.
func TestGatedOutput_NamesExternalBlockers(t *testing.T) {
	orig := externalBlockerLookupFn
	t.Cleanup(func() { externalBlockerLookupFn = orig })
	externalBlockerLookupFn = func(id string) (*bdShowResult, error) {
		if id == "x-gate" {
			return &bdShowResult{ID: id, Status: "open", IssueType: "gate"}, nil
		}
		return nil, nil // empty result counts as unreadable
	}
	beadList := []BeadInfo{
		{ID: "dec", Type: "decision", Status: "open"},
		{ID: "a", Title: "A", Type: "task", Status: "open"},
		{ID: "b", Title: "B", Type: "task", Status: "open"},
		{ID: "c", Title: "C", Type: "task", Status: "open"},
	}
	deps := []DepInfo{
		{IssueID: "a", DependsOnID: "x-gate", Type: "blocks"},
		{IssueID: "a", DependsOnID: "x-gate", Type: "waits-for"}, // duplicate edge, recorded once
		{IssueID: "a", DependsOnID: "dec", Type: "blocks"},
		{IssueID: "b", DependsOnID: "x-void", Type: "blocks"},
		{IssueID: "c", DependsOnID: "a", Type: "blocks"},
	}
	dag := buildConvoyDAG(beadList, deps)
	if got := dag.Nodes["a"].BlockedBy; len(got) != 2 {
		t.Fatalf("a BlockedBy = %v, want [x-gate dec]", got)
	}
	if ext := dag.External["x-void"]; ext == nil || ext.Err == nil {
		t.Fatalf("empty lookup result must be unresolved, got %+v", ext)
	}
	_, gated, err := computeWaves(dag)
	if err != nil {
		t.Fatal(err)
	}

	text := renderGatedTasks(gated, dag)
	for _, want := range []string{
		"x-gate (outside staged set: gate, open)",
		"x-void (outside staged set: status unreadable)",
		"(transitively gated)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("gated section missing %q:\n%s", want, text)
		}
	}

	byID := map[string]GatedTaskJSON{}
	for _, g := range buildGatedJSON(gated, dag) {
		byID[g.ID] = g
	}
	if ext := byID["a"].ExternalBlockers; len(ext) != 1 || ext[0].ID != "x-gate" || !ext[0].Resolved || ext[0].Type != "gate" {
		t.Errorf("a external_blockers = %+v", ext)
	}
	if ext := byID["b"].ExternalBlockers; len(ext) != 1 || ext[0].Resolved {
		t.Errorf("b external_blockers = %+v", ext)
	}
	if ext := byID["c"].ExternalBlockers; ext != nil {
		t.Errorf("c is transitively gated, want no external_blockers, got %+v", ext)
	}

	findings := map[string]string{}
	for _, f := range gatedFindings(gated, dag) {
		findings[f.BeadIDs[0]] = f.Message
	}
	if !strings.Contains(findings["a"], "dec, x-gate (outside staged set: gate, open)") {
		t.Errorf("a finding = %q", findings["a"])
	}
	if !strings.Contains(findings["c"], "transitively gated") {
		t.Errorf("c finding = %q", findings["c"])
	}
}

// Dependency targets are agent-written. An ID that bd would read as a flag
// (e.g. --db=<path> pointing at a forged database) must never reach bd: it is
// unresolved, so its consumer stays gated and staging warns (fail-closed).
func TestStageExternalBlocker_FlagLikeIDNeverReachesBd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows — shell stubs")
	}
	dag := newTestDAG(t).
		Epic("gt-epic", "Epic").
		Task("gt-cons", "Consumer", withRig("gastown")).ParentOf("gt-epic").
		BlockedBy("--db=/tmp/forged").BlockedBy("external:gt:-x")
	_, logPath := dag.Setup(t)

	staged, waves, gated := stageEpicForTest(t, "gt-epic")

	if _, ok := waveTaskSet(waves)["gt-cons"]; ok {
		t.Fatalf("consumer of invalid blocker ID must not be in a wave, waves=%+v", waves)
	}
	by, _ := gatedBy(gated, "gt-cons")
	if len(by) != 2 {
		t.Fatalf("gt-cons should be gated by both invalid IDs, got %v", by)
	}
	for _, id := range []string{"--db=/tmp/forged", "-x"} {
		if ext := staged.External[id]; ext == nil || ext.Err == nil {
			t.Errorf("invalid ID %q must be unresolved, got %+v", id, ext)
		}
	}
	warns := findingsOf(detectWarnings(staged, &StageInput{Kind: StageInputEpic, IDs: []string{"gt-epic"}}), "unresolved-blocker")
	if len(warns) != 2 {
		t.Errorf("expected 2 unresolved-blocker warnings, got %+v", warns)
	}
	logBytes, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logBytes)
	if strings.Contains(log, "forged") || strings.Contains(log, " -x") {
		t.Errorf("invalid ID reached bd:\n%s", log)
	}
	if !strings.Contains(log, "CMD:show --json -- gt-epic") || !strings.Contains(log, "CMD:dep list --json -- gt-cons") {
		t.Errorf("bd show/dep list must end options with -- before the ID:\n%s", log)
	}
}

func TestBdShow_RejectsInvalidID(t *testing.T) {
	for _, id := range []string{"", "-x", "--db=/tmp/x", "gt a", "external:r:gt-a"} {
		if _, err := bdShow(id); err == nil || !strings.Contains(err.Error(), "invalid bead ID") {
			t.Errorf("bdShow(%q) err = %v, want invalid bead ID", id, err)
		}
		if _, err := bdDepList(id); err == nil || !strings.Contains(err.Error(), "invalid bead ID") {
			t.Errorf("bdDepList(%q) err = %v, want invalid bead ID", id, err)
		}
		if _, err := bdListChildren(id); err == nil || !strings.Contains(err.Error(), "invalid bead ID") {
			t.Errorf("bdListChildren(%q) err = %v, want invalid bead ID", id, err)
		}
	}
}

// merge-blocks follows the feeder rule (beads.IsBlockerResolved): closed is
// not enough, the close reason must confirm the merge. Staged and external
// blockers alike.
func TestMergeBlocks_ClosedWithoutMergeStillBlocks(t *testing.T) {
	orig := externalBlockerLookupFn
	t.Cleanup(func() { externalBlockerLookupFn = orig })
	externalBlockerLookupFn = func(id string) (*bdShowResult, error) {
		reason := "PR merged"
		if id == "x-merged" {
			reason = "Merged in abc123"
		}
		return &bdShowResult{ID: id, Status: "closed", IssueType: "task", CloseReason: reason}, nil
	}
	beadList := []BeadInfo{
		{ID: "s-unmerged", Type: "task", Status: "closed", CloseReason: "done"},
		{ID: "s-merged", Type: "task", Status: "closed", CloseReason: "Merged in def456"},
		{ID: "a", Type: "task", Status: "open"},
		{ID: "b", Type: "task", Status: "open"},
		{ID: "c", Type: "task", Status: "open"},
		{ID: "d", Type: "task", Status: "open"},
		{ID: "e", Type: "task", Status: "open"},
	}
	deps := []DepInfo{
		{IssueID: "a", DependsOnID: "s-unmerged", Type: "merge-blocks"},
		{IssueID: "b", DependsOnID: "s-merged", Type: "merge-blocks"},
		{IssueID: "c", DependsOnID: "x-unmerged", Type: "merge-blocks"},
		{IssueID: "d", DependsOnID: "x-merged", Type: "merge-blocks"},
		{IssueID: "e", DependsOnID: "s-unmerged", Type: "blocks"}, // plain blocks: closed resolves
	}
	dag := buildConvoyDAG(beadList, deps)

	ready := readyUndispatchedTasks(dag, func(string) bool { return false })
	if strings.Join(ready, ",") != "b,d,e" {
		t.Errorf("ready = %v, want [b d e]", ready)
	}
	_, gated, err := computeWaves(dag)
	if err != nil {
		t.Fatal(err)
	}
	if by, ok := gatedBy(gated, "c"); !ok || len(by) != 1 || by[0] != "x-unmerged" {
		t.Errorf("c should be gated by unmerged external blocker, gated=%+v", gated)
	}
	if _, ok := gatedBy(gated, "d"); ok {
		t.Errorf("d's external blocker is merged, must not gate: %+v", gated)
	}
}

// gt mountain status uses openBlockersOf: tombstone and closed blockers are
// not open; a closed-but-unmerged merge-blocks blocker and an external open
// blocker are, and are described.
func TestOpenBlockersOf_MountainStatus(t *testing.T) {
	dag := &ConvoyDAG{
		Nodes: map[string]*ConvoyDAGNode{
			"tomb": {ID: "tomb", Type: "task", Status: "tombstone"},
			"done": {ID: "done", Type: "task", Status: "closed"},
			"wip":  {ID: "wip", Type: "task", Status: "in_progress"},
			"mb":   {ID: "mb", Type: "task", Status: "closed", CloseReason: "done"},
			"t": {ID: "t", Type: "task", Status: "open",
				BlockedBy:      []string{"tomb", "done", "wip", "mb", "x-open", "x-gone"},
				MergeBlockedBy: map[string]bool{"mb": true}},
		},
		External: map[string]*ExternalBlocker{
			"x-open": {ID: "x-open", Type: "gate", Status: "open"},
			"x-gone": {ID: "x-gone", Type: "task", Status: "tombstone"},
		},
	}
	got := openBlockersOf(dag, dag.Nodes["t"])
	want := "wip, mb, x-open (outside staged set: gate, open)"
	if strings.Join(got, ", ") != want {
		t.Errorf("openBlockersOf = %q, want %q", strings.Join(got, ", "), want)
	}
	if openBlockersOf(dag, dag.Nodes["tomb"]) != nil {
		t.Error("node without blockers must have no open blockers")
	}
}

// A blocker reached through several edges (dup rows, wrapped and raw ID,
// different execution types) is looked up once and listed once.
func TestBuildConvoyDAG_ExternalEdgeDedup(t *testing.T) {
	calls := 0
	orig := externalBlockerLookupFn
	t.Cleanup(func() { externalBlockerLookupFn = orig })
	externalBlockerLookupFn = func(id string) (*bdShowResult, error) {
		calls++
		return &bdShowResult{ID: id, Status: "closed", IssueType: "task", CloseReason: "done"}, nil
	}
	dag := buildConvoyDAG(
		[]BeadInfo{{ID: "a", Type: "task", Status: "open"}},
		[]DepInfo{
			{IssueID: "a", DependsOnID: "x-1", Type: "blocks"},
			{IssueID: "a", DependsOnID: "x-1", Type: "blocks"},
			{IssueID: "a", DependsOnID: "external:o:x-1", Type: "merge-blocks"},
		},
	)
	if calls != 1 {
		t.Errorf("lookups = %d, want 1", calls)
	}
	if got := dag.Nodes["a"].BlockedBy; len(got) != 1 || got[0] != "x-1" {
		t.Errorf("BlockedBy = %v, want [x-1]", got)
	}
	// The strictest edge wins: merge-blocks without merge keeps blocking.
	if !dag.isOpenBlocker(dag.Nodes["a"], "x-1") {
		t.Error("merge-blocks edge must keep closed-unmerged blocker open")
	}
}
