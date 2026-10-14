package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/config"
)

// writeRigSettingsJSON writes <townRoot>/<rig>/settings/config.json verbatim.
func writeRigSettingsJSON(t *testing.T, townRoot, rigName, body string) {
	t.Helper()
	dir := filepath.Join(townRoot, rigName, "settings")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir settings: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
}

const (
	settingsMQEnabled  = `{"type":"rig-settings","version":1,"merge_queue":{"enabled":true,"run_tests":true}}`
	settingsMQDisabled = `{"type":"rig-settings","version":1,"merge_queue":{"enabled":false}}`
)

func TestRigMergeQueueActive(t *testing.T) {
	tests := []struct {
		name     string
		rigName  string
		settings string // "" = no settings file
		want     bool
	}{
		{"no rig", "", "", false},
		{"no settings file: refinery defaults apply", "gastown", "", true},
		{"merge queue enabled", "gastown", settingsMQEnabled, true},
		{"merge queue explicitly disabled", "gastown", settingsMQDisabled, false},
		{"merge_queue without enabled key", "gastown", `{"merge_queue":{"run_tests":true}}`, true},
		{"no merge_queue section", "gastown", `{"type":"rig-settings","version":1}`, true},
		{"unreadable settings fail closed", "gastown", `{not json`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			townRoot := t.TempDir()
			if tt.settings != "" {
				writeRigSettingsJSON(t, townRoot, tt.rigName, tt.settings)
			}
			if got := rigMergeQueueActive(townRoot, tt.rigName); got != tt.want {
				t.Fatalf("rigMergeQueueActive = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateConvoyMergeForRig(t *testing.T) {
	townRoot := t.TempDir()
	writeRigSettingsJSON(t, townRoot, "mqrig", settingsMQEnabled)
	writeRigSettingsJSON(t, townRoot, "plainrig", settingsMQDisabled)

	err := validateConvoyMergeForRig(townRoot, "mqrig", convoyMergeDirect)
	if err == nil {
		t.Fatal("direct on a merge-queue rig accepted, want rejection")
	}
	for _, want := range []string{`rig "mqrig" uses a merge queue`, "skipping the refinery and its test gate", "Drop --merge", "--merge=mr"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}

	for _, tc := range []struct{ rig, merge string }{
		{"plainrig", convoyMergeDirect},
		{"", convoyMergeDirect}, // no rig (town-level work): no merge queue to bypass
		{"mqrig", convoyMergeMR},
		{"mqrig", convoyMergeLocal},
		{"mqrig", ""},
	} {
		if err := validateConvoyMergeForRig(townRoot, tc.rig, tc.merge); err != nil {
			t.Fatalf("validateConvoyMergeForRig(%s, %q) = %v, want nil", tc.rig, tc.merge, err)
		}
	}
}

func TestValidateConvoyMergeForBeads(t *testing.T) {
	townRoot := t.TempDir()
	if err := beads.WriteRoutes(filepath.Join(townRoot, ".beads"), []beads.Route{
		{Prefix: "hq-", Path: "."},
		{Prefix: "pl-", Path: "plainrig/mayor/rig"},
		{Prefix: "mq-", Path: "mqrig/mayor/rig"},
	}); err != nil {
		t.Fatalf("write routes: %v", err)
	}
	writeRigSettingsJSON(t, townRoot, "mqrig", settingsMQEnabled)
	writeRigSettingsJSON(t, townRoot, "plainrig", settingsMQDisabled)

	if err := validateConvoyMergeForBeads(townRoot, convoyMergeDirect, []string{"hq-1", "pl-1"}); err != nil {
		t.Fatalf("town-level and no-merge-queue beads rejected: %v", err)
	}
	err := validateConvoyMergeForBeads(townRoot, convoyMergeDirect, []string{"pl-1", "mq-2"})
	if err == nil || !strings.Contains(err.Error(), "mq-2") || !strings.Contains(err.Error(), "uses a merge queue") {
		t.Fatalf("validateConvoyMergeForBeads = %v, want rejection naming mq-2", err)
	}
	if err := validateConvoyMergeForBeads(townRoot, convoyMergeMR, []string{"mq-2"}); err != nil {
		t.Fatalf("mr strategy rejected: %v", err)
	}
}

func TestEffectiveDoneMergeStrategy(t *testing.T) {
	localSource := &beads.Issue{ID: "gt-1", Description: "merge_strategy: local\n"}
	tests := []struct {
		name           string
		info           *ConvoyInfo
		source         *beads.Issue
		mqActive       bool
		wantStrategy   string
		wantRedirected bool
	}{
		{"no convoy", nil, nil, true, convoyMergeMR, false},
		{"mr convoy", &ConvoyInfo{MergeStrategy: "mr"}, nil, true, convoyMergeMR, false},
		{"empty convoy strategy", &ConvoyInfo{}, nil, false, convoyMergeMR, false},
		{"local convoy", &ConvoyInfo{MergeStrategy: "local"}, nil, true, convoyMergeLocal, false},
		{"local on bead without convoy", nil, localSource, true, convoyMergeLocal, false},
		{"local convoy wins over no_merge", &ConvoyInfo{MergeStrategy: "local"}, &beads.Issue{ID: "gt-3", Description: "no_merge: true\n"}, true, convoyMergeLocal, false},
		{"no_merge bead never pushes direct", &ConvoyInfo{MergeStrategy: "direct"}, &beads.Issue{ID: "gt-2", Description: "no_merge: true\n"}, false, convoyMergeMR, false},
		{"direct without merge queue stays direct", &ConvoyInfo{MergeStrategy: "direct"}, nil, false, convoyMergeDirect, false},
		{"direct with merge queue goes through the gate (kp-iwm.3)", &ConvoyInfo{MergeStrategy: "direct", Owned: true}, nil, true, convoyMergeMR, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, redirected := effectiveDoneMergeStrategy(tt.info, tt.source, tt.mqActive)
			if got != tt.wantStrategy || redirected != tt.wantRedirected {
				t.Fatalf("effectiveDoneMergeStrategy = (%q, %v), want (%q, %v)", got, redirected, tt.wantStrategy, tt.wantRedirected)
			}
		})
	}
}

// setupMergeGateSlingTown builds a town whose rig "gastown" (bead prefix zz-)
// has the given settings. bd show succeeds everywhere; any bd mutation fails
// the command and is logged, so tests can assert "no side effects".
func setupMergeGateSlingTown(t *testing.T, settings string) (townRoot, logPath string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell bd stub")
	}
	townRoot = t.TempDir()
	for _, dir := range []string{
		filepath.Join(townRoot, "mayor", "rig"),
		filepath.Join(townRoot, "gastown", "mayor", "rig", ".beads"),
		filepath.Join(townRoot, ".beads"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	if err := config.SaveRigsConfig(filepath.Join(townRoot, "mayor", "rigs.json"), &config.RigsConfig{
		Version: 1,
		Rigs: map[string]config.RigEntry{
			"gastown": {
				GitURL:      "git@github.com:test/gastown.git",
				AddedAt:     time.Now().Truncate(time.Second),
				BeadsConfig: &config.BeadsConfig{Repo: "local", Prefix: "zz-"},
			},
		},
	}); err != nil {
		t.Fatalf("SaveRigsConfig: %v", err)
	}
	if err := beads.WriteRoutes(filepath.Join(townRoot, ".beads"), []beads.Route{
		{Prefix: "gt-", Path: "."},
		{Prefix: "zz-", Path: "gastown/mayor/rig"},
	}); err != nil {
		t.Fatalf("write routes: %v", err)
	}
	writeRigSettingsJSON(t, townRoot, "gastown", settings)

	binDir := filepath.Join(townRoot, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	logPath = filepath.Join(townRoot, "bd.log")
	script := `#!/bin/sh
echo "$*" >> "${BD_LOG}"
cmd="$1"
shift || true
if [ "$cmd" = "--allow-stale" ]; then
  cmd="$1"
  shift || true
fi
case "$cmd" in
  show)
    echo '[{"id":"zz-work","title":"Gated work","status":"open","assignee":"","issue_type":"task","description":"Do the work.","acceptance_criteria":"- [ ] tests pass"}]'
    ;;
  list|sql)
    echo '[]'
    ;;
  create|update|cook|mol|close|dep|comments)
    echo "unexpected side effect: $cmd" >&2
    exit 2
    ;;
esac
exit 0
`
	_ = writeBDStub(t, binDir, script, "")

	t.Setenv("BD_LOG", logPath)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(EnvGTRole, "mayor")
	t.Setenv("GT_POLECAT", "")
	t.Setenv("GT_CREW", "")
	t.Setenv("TMUX_PANE", "")
	t.Setenv("GT_TEST_NO_NUDGE", "1")
	t.Setenv("GT_TEST_SKIP_HOOK_VERIFY", "1")
	t.Chdir(filepath.Join(townRoot, "mayor", "rig"))
	return townRoot, logPath
}

// stubSlingSpawn replaces spawnPolecatForSling and reports whether it ran.
func stubSlingSpawn(t *testing.T, townRoot string) *bool {
	t.Helper()
	prev := spawnPolecatForSling
	t.Cleanup(func() { spawnPolecatForSling = prev })
	called := false
	spawnPolecatForSling = func(rigName string, opts SlingSpawnOptions) (*SpawnedPolecatInfo, error) {
		called = true
		return &SpawnedPolecatInfo{RigName: rigName, PolecatName: "toast", ClonePath: filepath.Join(townRoot, "fake-polecat")}, nil
	}
	return &called
}

// setSlingMergeForTest sets the --merge flag (and resets flags that would
// change routing) for one test.
func setSlingMergeForTest(t *testing.T, merge string) {
	t.Helper()
	prevMerge, prevDryRun, prevForce, prevNoConvoy, prevNoBoot := slingMerge, slingDryRun, slingForce, slingNoConvoy, slingNoBoot
	t.Cleanup(func() {
		slingMerge, slingDryRun, slingForce, slingNoConvoy, slingNoBoot = prevMerge, prevDryRun, prevForce, prevNoConvoy, prevNoBoot
	})
	slingMerge, slingDryRun, slingForce, slingNoConvoy, slingNoBoot = merge, false, false, false, true
}

func assertMergeGateRejected(t *testing.T, err error, spawned bool, logPath string) {
	t.Helper()
	if err == nil {
		t.Fatal("--merge=direct to a merge-queue rig was accepted")
	}
	if !strings.Contains(err.Error(), `rig "gastown" uses a merge queue`) {
		t.Fatalf("error = %v, want merge-queue rejection", err)
	}
	if spawned {
		t.Fatal("polecat spawned before the merge-strategy rejection")
	}
	logBytes, _ := os.ReadFile(logPath)
	for _, sideEffect := range []string{"create", "update", "cook", "mol ", "close", "dep ", "comments"} {
		if strings.Contains(string(logBytes), sideEffect) {
			t.Fatalf("bd side effect %q ran before the merge-strategy rejection; log:\n%s", sideEffect, logBytes)
		}
	}
}

func TestRunSlingRejectsDirectMergeOnMergeQueueRig(t *testing.T) {
	townRoot, logPath := setupMergeGateSlingTown(t, settingsMQEnabled)
	setSlingMergeForTest(t, convoyMergeDirect)
	spawned := stubSlingSpawn(t, townRoot)

	err := runSling(nil, []string{"zz-work", "gastown"})
	assertMergeGateRejected(t, err, *spawned, logPath)
}

func TestBatchSlingRejectsDirectMergeOnMergeQueueRig(t *testing.T) {
	townRoot, logPath := setupMergeGateSlingTown(t, settingsMQEnabled)
	setSlingMergeForTest(t, convoyMergeDirect)
	spawned := stubSlingSpawn(t, townRoot)

	err := runBatchSling([]string{"zz-work", "zz-more"}, "gastown", filepath.Join(townRoot, ".beads"))
	assertMergeGateRejected(t, err, *spawned, logPath)
}

func TestScheduleBeadRejectsDirectMergeOnMergeQueueRig(t *testing.T) {
	townRoot, logPath := setupMergeGateSlingTown(t, settingsMQEnabled)
	spawned := stubSlingSpawn(t, townRoot)

	err := scheduleBead("zz-work", "gastown", ScheduleOptions{Merge: convoyMergeDirect})
	assertMergeGateRejected(t, err, *spawned, logPath)
}

func TestExecuteSlingRejectsDirectMergeOnMergeQueueRig(t *testing.T) {
	townRoot, logPath := setupMergeGateSlingTown(t, settingsMQEnabled)
	spawned := stubSlingSpawn(t, townRoot)

	result, err := executeSling(SlingParams{
		BeadID:   "zz-work",
		RigName:  "gastown",
		Merge:    convoyMergeDirect,
		TownRoot: townRoot,
		BeadsDir: filepath.Join(townRoot, ".beads"),
	})
	assertMergeGateRejected(t, err, *spawned, logPath)
	if result == nil || result.ErrMsg != "merge_strategy_rejected" {
		t.Fatalf("result = %+v, want ErrMsg merge_strategy_rejected", result)
	}
}

// Scheduler dispatch passes NoConvoy, so Merge has no effect there; a context
// queued before the guard existed must not be stranded by it (gt done
// redirects such work into the merge queue instead).
func TestExecuteSlingSchedulerDispatchDoesNotRejectQueuedDirect(t *testing.T) {
	townRoot, _ := setupMergeGateSlingTown(t, settingsMQEnabled)
	prev := spawnPolecatForSling
	t.Cleanup(func() { spawnPolecatForSling = prev })
	// Stop right at spawn: reaching it proves the merge gate let the dispatch through.
	spawnPolecatForSling = func(string, SlingSpawnOptions) (*SpawnedPolecatInfo, error) {
		return nil, errors.New("spawn reached")
	}

	_, err := executeSling(SlingParams{
		BeadID:   "zz-work",
		RigName:  "gastown",
		Merge:    convoyMergeDirect,
		NoConvoy: true,
		TownRoot: townRoot,
		BeadsDir: filepath.Join(townRoot, ".beads"),
	})
	if err == nil || !strings.Contains(err.Error(), "spawn reached") {
		t.Fatalf("scheduler dispatch did not reach spawn: %v", err)
	}
}

func TestBatchSlingAllowsDirectMergeWithoutMergeQueue(t *testing.T) {
	townRoot, _ := setupMergeGateSlingTown(t, settingsMQDisabled)
	setSlingMergeForTest(t, convoyMergeDirect)
	slingDryRun = true // stop after validation: only the gate decision matters here
	spawned := stubSlingSpawn(t, townRoot)

	if err := runBatchSling([]string{"zz-work"}, "gastown", filepath.Join(townRoot, ".beads")); err != nil {
		t.Fatalf("direct on a rig without merge queue rejected: %v", err)
	}
	if *spawned {
		t.Fatal("dry run spawned a polecat")
	}
}

func TestConvoyCreateRejectsDirectMergeForMergeQueueRigBeads(t *testing.T) {
	_, logPath := setupMergeGateSlingTown(t, settingsMQEnabled)
	prev := convoyMerge
	t.Cleanup(func() { convoyMerge = prev })
	convoyMerge = convoyMergeDirect

	err := runConvoyCreate(nil, []string{"Gated convoy", "zz-work"})
	if err == nil || !strings.Contains(err.Error(), "zz-work") || !strings.Contains(err.Error(), `rig "gastown" uses a merge queue`) {
		t.Fatalf("runConvoyCreate = %v, want merge-queue rejection naming zz-work", err)
	}
	logBytes, _ := os.ReadFile(logPath)
	if strings.Contains(string(logBytes), "create") || strings.Contains(string(logBytes), "dep ") {
		t.Fatalf("convoy written before rejection; bd log:\n%s", logBytes)
	}
}

// A settings file without merge_queue.enabled must stay "active" after any
// gt command loads and saves it (e.g. gt rig settings set); with a plain bool
// the round-trip used to write "enabled": false and unlock --merge=direct.
func TestRigMergeQueueActiveSurvivesSettingsRoundTrip(t *testing.T) {
	townRoot := t.TempDir()
	writeRigSettingsJSON(t, townRoot, "gastown", `{"type":"rig-settings","version":1,"merge_queue":{"run_tests":true}}`)
	path := config.RigSettingsPath(filepath.Join(townRoot, "gastown"))
	settings, err := config.LoadRigSettings(path)
	if err != nil {
		t.Fatalf("LoadRigSettings: %v", err)
	}
	if err := config.SaveRigSettings(path, settings); err != nil {
		t.Fatalf("SaveRigSettings: %v", err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), `"enabled"`) {
		t.Fatalf("round-trip invented an enabled key:\n%s", data)
	}
	if !rigMergeQueueActive(townRoot, "gastown") {
		t.Fatal("merge queue reported inactive after a settings round-trip")
	}
}
