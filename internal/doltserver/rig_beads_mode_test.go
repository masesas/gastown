package doltserver

import (
	"os"
	"path/filepath"
	"testing"
)

func setupTrackedRig(t *testing.T, mode string) (townRoot string, rigName string) {
	t.Helper()
	townRoot = t.TempDir()
	rigName = "myrig"
	rigPath := filepath.Join(townRoot, rigName)
	if err := os.MkdirAll(filepath.Join(rigPath, "mayor", "rig", ".beads"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(rigPath, ".beads"), 0755); err != nil {
		t.Fatal(err)
	}
	if mode != "" {
		cfg := `{"type":"rig","version":1,"name":"myrig","beads":{"prefix":"mr","mode":"` + mode + `"}}`
		if err := os.WriteFile(filepath.Join(rigPath, "config.json"), []byte(cfg), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return townRoot, rigName
}

func TestFindRigBeadsDir_RigModeIgnoresTrackedBeads(t *testing.T) {
	t.Parallel()
	townRoot, rigName := setupTrackedRig(t, "rig")
	want := filepath.Join(townRoot, rigName, ".beads")

	if got := FindRigBeadsDir(townRoot, rigName); got != want {
		t.Errorf("FindRigBeadsDir = %q, want %q", got, want)
	}
	got, err := FindOrCreateRigBeadsDir(townRoot, rigName)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("FindOrCreateRigBeadsDir = %q, want %q", got, want)
	}
}

func TestFindRigBeadsDir_AutoModeKeepsTrackedBeads(t *testing.T) {
	t.Parallel()
	townRoot, rigName := setupTrackedRig(t, "")
	want := filepath.Join(townRoot, rigName, "mayor", "rig", ".beads")

	if got := FindRigBeadsDir(townRoot, rigName); got != want {
		t.Errorf("FindRigBeadsDir = %q, want %q", got, want)
	}
	got, err := FindOrCreateRigBeadsDir(townRoot, rigName)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("FindOrCreateRigBeadsDir = %q, want %q", got, want)
	}
}

func TestEnsureMetadata_RigModeDoesNotTouchTrackedBeads(t *testing.T) {
	t.Parallel()
	townRoot, rigName := setupTrackedRig(t, "rig")
	if err := EnsureMetadata(townRoot, rigName); err != nil {
		t.Fatalf("EnsureMetadata: %v", err)
	}
	if _, err := os.Stat(filepath.Join(townRoot, rigName, "mayor", "rig", ".beads", "metadata.json")); !os.IsNotExist(err) {
		t.Fatal("EnsureMetadata wrote metadata.json into the tracked mayor/rig/.beads in rig mode")
	}
	if _, err := os.Stat(filepath.Join(townRoot, rigName, ".beads", "metadata.json")); err != nil {
		t.Fatalf("EnsureMetadata did not write rig/.beads/metadata.json: %v", err)
	}
}
