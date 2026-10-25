package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupRigModeRig builds <town>/testrig in beads.mode=rig: a tracked-looking
// mayor/rig/.beads and a self-contained <rig>/.beads with data.
func setupRigModeRig(t *testing.T) (townRoot, rigName, rigDir string) {
	t.Helper()
	townRoot = t.TempDir()
	rigName = "testrig"
	rigDir = filepath.Join(townRoot, rigName)
	trackedBeads := filepath.Join(rigDir, "mayor", "rig", ".beads")
	if err := os.MkdirAll(trackedBeads, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(trackedBeads, "config.yaml"), []byte("issue-prefix: oto\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(trackedBeads, "metadata.json"), []byte(`{"backend":"dolt","dolt_database":"oto"}`), 0644); err != nil {
		t.Fatal(err)
	}
	localBeads := filepath.Join(rigDir, ".beads")
	if err := os.MkdirAll(localBeads, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localBeads, "config.yaml"), []byte("issue-prefix: tr\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := `{"type":"rig","version":1,"name":"testrig","beads":{"prefix":"tr","mode":"rig"}}`
	if err := os.WriteFile(filepath.Join(rigDir, "config.json"), []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
	return townRoot, rigName, rigDir
}

func writeMayorCloneRedirect(t *testing.T, rigDir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(rigDir, "mayor", "rig", ".beads", "redirect"), []byte("../../.beads\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestBeadsRedirectCheck_RigMode_PassesWithoutRigRedirect(t *testing.T) {
	townRoot, rigName, rigDir := setupRigModeRig(t)
	writeMayorCloneRedirect(t, rigDir)

	check := NewBeadsRedirectCheck()
	ctx := &CheckContext{TownRoot: townRoot, RigName: rigName}
	result := check.Run(ctx)
	if result.Status != StatusOK {
		t.Fatalf("rig mode with self-contained beads: status = %v (%s), want OK", result.Status, result.Message)
	}

	if err := check.Fix(ctx); err != nil {
		t.Fatalf("Fix: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rigDir, ".beads", "config.yaml")); err != nil {
		t.Fatalf("Fix must never remove <rig>/.beads in rig mode: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rigDir, ".beads", "redirect")); !os.IsNotExist(err) {
		t.Fatal("Fix must not write <rig>/.beads/redirect in rig mode")
	}
}

func TestBeadsRedirectCheck_RigMode_FixesMissingMayorCloneRedirect(t *testing.T) {
	townRoot, rigName, rigDir := setupRigModeRig(t)

	check := NewBeadsRedirectCheck()
	ctx := &CheckContext{TownRoot: townRoot, RigName: rigName}
	result := check.Run(ctx)
	if result.Status != StatusError || !strings.Contains(result.Message, "mayor clone") {
		t.Fatalf("missing mayor clone redirect: status = %v (%s), want error about mayor clone", result.Status, result.Message)
	}

	if err := check.Fix(ctx); err != nil {
		t.Fatalf("Fix: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(rigDir, "mayor", "rig", ".beads", "redirect"))
	if err != nil || string(data) != "../../.beads\n" {
		t.Fatalf("Fix must write mayor clone redirect ../../.beads, got %q (%v)", data, err)
	}
	if got, _ := os.ReadFile(filepath.Join(rigDir, "mayor", "rig", ".beads", "config.yaml")); string(got) != "issue-prefix: oto\n" {
		t.Fatalf("Fix must not touch tracked config.yaml, got %q", got)
	}
	if _, err := os.Stat(filepath.Join(rigDir, ".beads", "config.yaml")); err != nil {
		t.Fatalf("Fix must never remove <rig>/.beads in rig mode: %v", err)
	}
	if result := check.Run(ctx); result.Status != StatusOK {
		t.Fatalf("after Fix: status = %v (%s), want OK", result.Status, result.Message)
	}
}

func TestBeadsRedirectCheck_RigMode_FlagsRedirectToTrackedBeads(t *testing.T) {
	townRoot, rigName, rigDir := setupRigModeRig(t)
	writeMayorCloneRedirect(t, rigDir)
	if err := os.WriteFile(filepath.Join(rigDir, ".beads", "redirect"), []byte("mayor/rig/.beads\n"), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewBeadsRedirectCheck()
	ctx := &CheckContext{TownRoot: townRoot, RigName: rigName}
	result := check.Run(ctx)
	if result.Status != StatusError || !strings.Contains(result.Message, "beads.mode=rig") {
		t.Fatalf("rig redirect in rig mode: status = %v (%s), want error naming beads.mode=rig", result.Status, result.Message)
	}
	if err := check.Fix(ctx); err != nil {
		t.Fatalf("Fix: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rigDir, ".beads", "config.yaml")); err != nil {
		t.Fatalf("Fix must never remove <rig>/.beads in rig mode: %v", err)
	}
}

func TestBeadsRedirectCheck_RigMode_FixInitializesMissingRigBeads(t *testing.T) {
	installMockBdInitOnly(t)
	townRoot, rigName, rigDir := setupRigModeRig(t)
	if err := os.RemoveAll(filepath.Join(rigDir, ".beads")); err != nil {
		t.Fatal(err)
	}
	trackedBeads := filepath.Join(rigDir, "mayor", "rig", ".beads")
	before := snapshotDir(t, trackedBeads)

	check := NewBeadsRedirectCheck()
	ctx := &CheckContext{TownRoot: townRoot, RigName: rigName}
	if result := check.Run(ctx); result.Status != StatusError {
		t.Fatalf("missing rig beads in rig mode: status = %v, want error", result.Status)
	}
	if err := check.Fix(ctx); err != nil {
		t.Fatalf("Fix: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rigDir, ".beads", "metadata.json")); err != nil {
		t.Fatalf("Fix must initialize <rig>/.beads with metadata.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rigDir, ".beads", "redirect")); !os.IsNotExist(err) {
		t.Fatal("Fix must not write <rig>/.beads/redirect in rig mode")
	}
	after := snapshotDir(t, trackedBeads)
	for name, content := range before {
		if after[name] != content {
			t.Fatalf("tracked mayor/rig/.beads/%s changed", name)
		}
	}
	if len(after) != len(before)+1 || after["redirect"] != "../../.beads\n" {
		t.Fatalf("tracked dir must only gain redirect=../../.beads, got %v", after)
	}
	if result := check.Run(ctx); result.Status != StatusOK {
		t.Fatalf("after Fix: status = %v (%s), want OK", result.Status, result.Message)
	}
}

func snapshotDir(t *testing.T, dir string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		data, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		snap[e.Name()] = string(data)
	}
	return snap
}

func TestMissingDoltDBInitTarget_RigModeNeverUsesTrackedBeads(t *testing.T) {
	townRoot, rigName, rigDir := setupRigModeRig(t)
	if err := os.RemoveAll(filepath.Join(rigDir, ".beads")); err != nil {
		t.Fatal(err)
	}
	trackedBeads := filepath.Join(rigDir, "mayor", "rig", ".beads")
	before := snapshotDir(t, trackedBeads)

	beadsDir, cmdDir, err := missingDoltDBInitTarget(townRoot, rigName)
	if err != nil {
		t.Fatalf("missingDoltDBInitTarget: %v", err)
	}
	if beadsDir != filepath.Join(rigDir, ".beads") || cmdDir != rigDir {
		t.Fatalf("rig mode init target = (%q, %q), want (<rig>/.beads, <rig>)", beadsDir, cmdDir)
	}
	if info, err := os.Stat(beadsDir); err != nil || !info.IsDir() {
		t.Fatalf("rig mode must create <rig>/.beads before bd init: %v", err)
	}
	after := snapshotDir(t, trackedBeads)
	if len(after) != len(before) {
		t.Fatalf("tracked mayor/rig/.beads changed: %v -> %v", before, after)
	}
	for name, content := range before {
		if after[name] != content {
			t.Fatalf("tracked mayor/rig/.beads/%s changed", name)
		}
	}
}

func TestMissingDoltDBInitTarget_AutoModeKeepsMayorFallback(t *testing.T) {
	townRoot := t.TempDir()
	rigDir := filepath.Join(townRoot, "autorig")
	if err := os.MkdirAll(filepath.Join(rigDir, "mayor", "rig", ".beads"), 0755); err != nil {
		t.Fatal(err)
	}
	beadsDir, cmdDir, err := missingDoltDBInitTarget(townRoot, "autorig")
	if err != nil {
		t.Fatal(err)
	}
	if beadsDir != filepath.Join(rigDir, "mayor", "rig", ".beads") || cmdDir != filepath.Join(rigDir, "mayor", "rig") {
		t.Fatalf("auto mode init target = (%q, %q), want mayor/rig", beadsDir, cmdDir)
	}
}

func writeModeSyncTown(t *testing.T, configMode, registryMode string) (townRoot, rigDir string) {
	t.Helper()
	townRoot = t.TempDir()
	rigDir = filepath.Join(townRoot, "msrig")
	if err := os.MkdirAll(filepath.Join(rigDir, "mayor", "rig", ".beads"), 0755); err != nil {
		t.Fatal(err)
	}
	cfg := `{"type":"rig","version":1,"name":"msrig","git_url":"file:///x","beads":{"prefix":"","mode":"` + configMode + `"}}`
	if err := os.WriteFile(filepath.Join(rigDir, "config.json"), []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(townRoot, "mayor"), 0755); err != nil {
		t.Fatal(err)
	}
	reg := `{"version":1,"rigs":{"msrig":{"git_url":"file:///x","added_at":"2026-01-01T00:00:00Z","beads":{"repo":"","prefix":"","mode":"` + registryMode + `"}}}}`
	if err := os.WriteFile(filepath.Join(townRoot, "mayor", "rigs.json"), []byte(reg), 0644); err != nil {
		t.Fatal(err)
	}
	return townRoot, rigDir
}

func TestRigConfigSyncCheck_BeadsModeMismatch(t *testing.T) {
	tests := []struct {
		name         string
		configMode   string
		registryMode string
	}{
		{name: "registry rig, config.json empty: copy from rigs.json", configMode: "", registryMode: "rig"},
		{name: "config.json rig, registry empty: never downgrade", configMode: "rig", registryMode: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			townRoot, rigDir := writeModeSyncTown(t, tt.configMode, tt.registryMode)
			check := NewRigConfigSyncCheck()
			ctx := &CheckContext{TownRoot: townRoot}
			result := check.Run(ctx)
			if !strings.Contains(strings.Join(result.Details, "\n"), "beads.mode mismatch") {
				t.Fatalf("Run details %v must report beads.mode mismatch", result.Details)
			}
			if err := check.Fix(ctx); err != nil {
				t.Fatalf("Fix: %v", err)
			}
			cfgData, _ := os.ReadFile(filepath.Join(rigDir, "config.json"))
			regData, _ := os.ReadFile(filepath.Join(townRoot, "mayor", "rigs.json"))
			if !strings.Contains(string(cfgData), `"mode": "rig"`) {
				t.Fatalf("config.json after Fix must carry mode rig:\n%s", cfgData)
			}
			if !strings.Contains(string(regData), `"mode": "rig"`) {
				t.Fatalf("rigs.json after Fix must carry mode rig:\n%s", regData)
			}
			if result := check.Run(ctx); strings.Contains(strings.Join(result.Details, "\n"), "beads.mode mismatch") {
				t.Fatalf("mismatch must be gone after Fix: %v", result.Details)
			}
		})
	}
}

func TestStaleDoltPortCheck_RigModeSkipsTrackedMetadata(t *testing.T) {
	townRoot, rigName, _ := setupRigModeRig(t)
	if err := os.MkdirAll(filepath.Join(townRoot, "mayor"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(townRoot, "mayor", "rigs.json"), []byte(`{"version":1,"rigs":{"`+rigName+`":{}}}`), 0644); err != nil {
		t.Fatal(err)
	}
	c := NewStaleDoltPortCheck()
	for _, f := range c.findMetadataFiles(townRoot) {
		if strings.Contains(f, filepath.Join("mayor", "rig", ".beads")) {
			t.Fatalf("rig mode: stale-port check must not consider the tracked %s", f)
		}
	}
}
