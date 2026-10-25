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
