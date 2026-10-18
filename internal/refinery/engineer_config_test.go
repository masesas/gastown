package refinery

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/rig"
)

// writeRigJSON writes v as JSON to <rigPath>/<rel>, creating parent dirs.
func writeRigJSON(t *testing.T, rigPath, rel string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(rigPath, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeRigConfigMQ(t *testing.T, rigPath string, mq map[string]any) {
	t.Helper()
	writeRigJSON(t, rigPath, "config.json", map[string]any{"type": "rig", "version": 1, "name": "test-rig", "merge_queue": mq})
}

func writeRigSettingsMQ(t *testing.T, rigPath string, mq map[string]any) {
	t.Helper()
	writeRigJSON(t, rigPath, filepath.Join("settings", "config.json"), map[string]any{"type": "rig-settings", "version": 1, "merge_queue": mq})
}

func loadTestConfig(t *testing.T, rigPath string) (*Engineer, string) {
	t.Helper()
	e := NewEngineer(&rig.Rig{Name: "test-rig", Path: rigPath})
	var out bytes.Buffer
	e.SetOutput(&out)
	if err := e.LoadConfig(); err != nil {
		t.Fatalf("LoadConfig() = %v", err)
	}
	return e, out.String()
}

// gateOrder lists gate names of a phase in run order.
func gateOrder(cfg *MergeQueueConfig, phase GatePhase) []string {
	var names []string
	for _, name := range orderedGateNames(cfg.Gates) {
		if effectiveGatePhase(cfg.Gates[name]) == phase {
			names = append(names, name)
		}
	}
	return names
}

func TestLoadConfig_RigSettingsWinOverConfigJSON(t *testing.T) {
	rigPath := t.TempDir()
	writeRigConfigMQ(t, rigPath, map[string]any{
		"merge_strategy":         "direct",
		"vcs_provider":           "bitbucket",
		"require_review":         false,
		"delete_merged_branches": true,
		"run_tests":              false,
		"test_command":           "make test",
	})
	writeRigSettingsMQ(t, rigPath, map[string]any{
		"merge_strategy":         "pr",
		"vcs_provider":           "github",
		"require_review":         true,
		"delete_merged_branches": false,
		"run_tests":              true,
	})

	e, _ := loadTestConfig(t, rigPath)
	cfg := e.Config()
	if cfg.MergeStrategy != "pr" || cfg.VCSProvider != "github" {
		t.Errorf("merge_strategy/vcs_provider = %q/%q, want pr/github from settings/config.json", cfg.MergeStrategy, cfg.VCSProvider)
	}
	if cfg.RequireReview == nil || !*cfg.RequireReview {
		t.Errorf("require_review = %v, want true from settings/config.json", cfg.RequireReview)
	}
	if cfg.DeleteMergedBranches {
		t.Error("delete_merged_branches = true, want false from settings/config.json")
	}
	if !cfg.RunTests {
		t.Error("run_tests = false, want true from settings/config.json")
	}
	if e.prProvider == nil {
		t.Error("merge_strategy=pr from settings/config.json did not initialise the PR provider")
	}
}

func TestLoadConfig_UnsetSettingsKeepConfigJSON(t *testing.T) {
	rigPath := t.TempDir()
	writeRigConfigMQ(t, rigPath, map[string]any{"run_tests": false, "delete_merged_branches": false, "test_command": "make test"})
	writeRigSettingsMQ(t, rigPath, map[string]any{"on_conflict": "assign_back"})

	e, _ := loadTestConfig(t, rigPath)
	cfg := e.Config()
	if cfg.RunTests || cfg.DeleteMergedBranches {
		t.Errorf("run_tests/delete_merged_branches = %v/%v, want config.json false/false (unset in settings)", cfg.RunTests, cfg.DeleteMergedBranches)
	}
	if cfg.TestCommand != "make test" || len(cfg.Gates) != 0 {
		t.Errorf("test_command=%q gates=%v, want legacy config.json test_command and no gates", cfg.TestCommand, cfg.Gates)
	}
}

func TestLoadConfig_GeneratesOrderedGatesFromSettingsCommands(t *testing.T) {
	rigPath := t.TempDir()
	writeRigSettingsMQ(t, rigPath, map[string]any{
		"setup_command":     "pnpm install",
		"typecheck_command": "pnpm tsc --noEmit",
		"lint_command":      "pnpm lint",
		"build_command":     "pnpm build",
		"test_command":      "pnpm test",
	})

	e, _ := loadTestConfig(t, rigPath)
	cfg := e.Config()
	if got, want := gateOrder(cfg, GatePhasePreMerge), []string{"setup"}; !reflect.DeepEqual(got, want) {
		t.Errorf("pre-merge gates = %v, want %v", got, want)
	}
	if got, want := gateOrder(cfg, GatePhasePostSquash), []string{"typecheck", "lint", "build", "test"}; !reflect.DeepEqual(got, want) {
		t.Errorf("post-squash gates = %v, want %v (typecheck, lint, build, test)", got, want)
	}
	for name, cmd := range map[string]string{"setup": "pnpm install", "typecheck": "pnpm tsc --noEmit", "lint": "pnpm lint", "build": "pnpm build", "test": "pnpm test"} {
		if g := cfg.Gates[name]; g == nil || g.Cmd != cmd {
			t.Errorf("gate %s = %+v, want cmd %q", name, g, cmd)
		}
	}
}

func TestLoadConfig_OnlyConfiguredCommandsBecomeGates(t *testing.T) {
	rigPath := t.TempDir()
	writeRigSettingsMQ(t, rigPath, map[string]any{"test_command": "go test ./...", "lint_command": "  "})

	e, _ := loadTestConfig(t, rigPath)
	if got := orderedGateNames(e.Config().Gates); !reflect.DeepEqual(got, []string{"test"}) {
		t.Fatalf("gates = %v, want [test] (blank commands are not gates)", got)
	}
}

func TestLoadConfig_RunTestsFalseGeneratesNoGates(t *testing.T) {
	rigPath := t.TempDir()
	writeRigSettingsMQ(t, rigPath, map[string]any{"run_tests": false, "test_command": "go test ./...", "build_command": "go build ./..."})

	e, _ := loadTestConfig(t, rigPath)
	if len(e.Config().Gates) != 0 || e.Config().RunTests {
		t.Fatalf("gates=%v run_tests=%v, want no gates when run_tests=false", e.Config().Gates, e.Config().RunTests)
	}
}

func TestLoadConfig_ExplicitConfigJSONGatesWinAndAreLogged(t *testing.T) {
	rigPath := t.TempDir()
	writeRigConfigMQ(t, rigPath, map[string]any{
		"gates": map[string]any{"verify": map[string]any{"cmd": "make verify", "phase": "post-squash", "order": 3}},
	})
	writeRigSettingsMQ(t, rigPath, map[string]any{"test_command": "go test ./...", "build_command": "go build ./..."})

	e, out := loadTestConfig(t, rigPath)
	if got := orderedGateNames(e.Config().Gates); !reflect.DeepEqual(got, []string{"verify"}) {
		t.Fatalf("gates = %v, want only the explicit config.json gate", got)
	}
	if g := e.Config().Gates["verify"]; g.Order != 3 || g.Phase != GatePhasePostSquash {
		t.Errorf("verify gate = %+v, want order 3 post-squash", g)
	}
	if !strings.Contains(out, "config.json") || !strings.Contains(out, "gates") || !strings.Contains(out, "settings/config.json") {
		t.Errorf("LoadConfig output %q does not log that explicit gates override the generated ones", out)
	}
}

func TestLoadConfig_UnreadableSettingsFailClosed(t *testing.T) {
	rigPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rigPath, "settings"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rigPath, "settings", "config.json"), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := NewEngineer(&rig.Rig{Name: "test-rig", Path: rigPath})
	e.SetOutput(&bytes.Buffer{})
	if err := e.LoadConfig(); err == nil {
		t.Fatal("LoadConfig() with corrupt settings/config.json = nil, want error (fail closed)")
	}
}

func TestLoadConfig_InvalidGateOrderRejected(t *testing.T) {
	rigPath := t.TempDir()
	writeRigConfigMQ(t, rigPath, map[string]any{"gates": map[string]any{"x": map[string]any{"cmd": "true", "order": -1}}})
	e := NewEngineer(&rig.Rig{Name: "test-rig", Path: rigPath})
	e.SetOutput(&bytes.Buffer{})
	if err := e.LoadConfig(); err == nil {
		t.Fatal("LoadConfig() with a negative gate order = nil, want error")
	}
}

func TestRunGatesForPhase_RunsByOrderThenName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("gate commands run via sh -c")
	}
	e := NewEngineer(&rig.Rig{Name: "test-rig", Path: t.TempDir()})
	e.workDir = t.TempDir()
	e.SetOutput(&bytes.Buffer{})
	logFile := filepath.Join(t.TempDir(), "order.log")
	gate := func(name string, order int) *GateConfig {
		return &GateConfig{Cmd: "echo " + name + " >> " + logFile, Phase: GatePhasePostSquash, Order: order}
	}
	e.config.Gates = map[string]*GateConfig{
		"a-test":      gate("a-test", 4),
		"typecheck":   gate("typecheck", 1),
		"lint":        gate("lint", 2),
		"build":       gate("build", 3),
		"b-unordered": gate("b-unordered", 0),
		"a-unordered": gate("a-unordered", 0),
	}
	if res := e.runGatesForPhase(context.Background(), GatePhasePostSquash); !res.Success {
		t.Fatalf("gates failed: %s", res.Error)
	}
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Fields(string(data))
	want := []string{"a-unordered", "b-unordered", "typecheck", "lint", "build", "a-test"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("run order = %v, want %v (by order, then name)", got, want)
	}
}
