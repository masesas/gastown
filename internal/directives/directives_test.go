package directives

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/assetsync"
	"github.com/steveyegge/gastown/internal/config"
)

func TestEmbeddedDefaults(t *testing.T) {
	town, err := assetsync.Check(TownSet(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if got := names(town); len(got) != 1 || got[0] != "mayor.md" {
		t.Errorf("town defaults = %v, want [mayor.md]", got)
	}
	rig, err := assetsync.Check(mustRigSet(t, t.TempDir(), "r1"))
	if err != nil {
		t.Fatal(err)
	}
	if got := names(rig); len(got) != 2 || got[0] != "crew.md" || got[1] != "polecat.md" {
		t.Errorf("rig defaults = %v, want [crew.md polecat.md]", got)
	}
}

func mustRigSet(t *testing.T, townRoot, rig string) assetsync.Set {
	t.Helper()
	s, err := RigSet(townRoot, rig)
	if err != nil {
		t.Fatalf("RigSet(%q): %v", rig, err)
	}
	return s
}

func TestRigSet_RejectsUnsafeNames(t *testing.T) {
	for _, name := range []string{"", ".", "..", "a/b", `a\b`, "../escape"} {
		if _, err := RigSet(t.TempDir(), name); err == nil {
			t.Errorf("RigSet(%q) accepted an unsafe rig name", name)
		}
	}
}

func names(r *assetsync.Report) []string {
	var out []string
	for _, f := range r.Files {
		out = append(out, f.Name)
	}
	return out
}

func TestProvision_FreshTownAndRig(t *testing.T) {
	townRoot := t.TempDir()

	n, err := assetsync.Provision(TownSet(townRoot))
	if err != nil || n != 1 {
		t.Fatalf("town Provision = %d, %v; want 1, nil", n, err)
	}
	n, err = assetsync.Provision(mustRigSet(t, townRoot, "posin"))
	if err != nil || n != 2 {
		t.Fatalf("rig Provision = %d, %v; want 2, nil", n, err)
	}

	for _, p := range []string{
		filepath.Join(townRoot, "directives", "mayor.md"),
		filepath.Join(townRoot, "posin", "directives", "crew.md"),
		filepath.Join(townRoot, "posin", "directives", "polecat.md"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s not provisioned: %v", p, err)
		}
	}

	// Recorded: the hash record lives next to the directives.
	data, err := os.ReadFile(filepath.Join(townRoot, "directives", assetsync.RecordFile))
	if err != nil {
		t.Fatalf("town record: %v", err)
	}
	var rec map[string]map[string]string
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatal(err)
	}
	if rec[RecordKey]["mayor.md"] == "" {
		t.Errorf("mayor.md not recorded: %s", data)
	}

	// The provisioned files are what gt prime loads.
	if got := config.LoadRoleDirective("mayor", townRoot, ""); got == "" {
		t.Error("LoadRoleDirective(mayor) empty after provisioning")
	}
	if got := config.LoadRoleDirective("polecat", townRoot, "posin"); got == "" {
		t.Error("LoadRoleDirective(polecat) empty after provisioning")
	}
}

func TestProvision_ExistingDirectiveNotOverwritten(t *testing.T) {
	townRoot := t.TempDir()
	dir := filepath.Join(townRoot, "directives")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	custom := []byte("# my own mayor rules\n")
	if err := os.WriteFile(filepath.Join(dir, "mayor.md"), custom, 0644); err != nil {
		t.Fatal(err)
	}

	if n, err := assetsync.Provision(TownSet(townRoot)); err != nil || n != 0 {
		t.Fatalf("Provision = %d, %v; want 0, nil", n, err)
	}
	// Neither upgrade nor doctor --fix may replace a directive gt never installed.
	if _, skipped, _, err := assetsync.Update(TownSet(townRoot)); err != nil || skipped != 1 {
		t.Fatalf("Update skipped = %d, %v; want 1, nil", skipped, err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "mayor.md")); string(got) != string(custom) {
		t.Errorf("user directive overwritten: %q", got)
	}
}

func TestUpdate_RefreshesUnmodifiedKeepsModified(t *testing.T) {
	townRoot := t.TempDir()
	rs := mustRigSet(t, townRoot, "posin")
	if _, err := assetsync.Provision(rs); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(townRoot, "posin", "directives")

	// Simulate an older release: crew.md still holds the old shipped content
	// (unmodified), polecat.md was edited by the user after that install.
	oldCrew := []byte("old shipped crew\n")
	oldPolecat := []byte("old shipped polecat\n")
	if err := os.WriteFile(filepath.Join(dir, "crew.md"), oldCrew, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "polecat.md"), []byte("user edit\n"), 0644); err != nil {
		t.Fatal(err)
	}
	rec, _ := json.Marshal(map[string]map[string]string{RecordKey: {
		"crew.md":    assetsync.Hash(oldCrew),
		"polecat.md": assetsync.Hash(oldPolecat),
	}})
	if err := os.WriteFile(filepath.Join(dir, assetsync.RecordFile), rec, 0644); err != nil {
		t.Fatal(err)
	}

	updated, skipped, _, err := assetsync.Update(rs)
	if err != nil || updated != 1 || skipped != 1 {
		t.Fatalf("Update = (%d updated, %d skipped, %v); want (1, 1, nil)", updated, skipped, err)
	}
	embedded, _ := defaultsFS.ReadFile("defaults/rig/crew.md")
	if got, _ := os.ReadFile(filepath.Join(dir, "crew.md")); string(got) != string(embedded) {
		t.Error("unmodified crew.md not refreshed to the embedded version")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "polecat.md")); string(got) != "user edit\n" {
		t.Errorf("modified polecat.md overwritten: %q", got)
	}
}

func TestMayorDirective_NoRuntimeTemplatePath(t *testing.T) {
	data, err := defaultsFS.ReadFile("defaults/town/mayor.md")
	if err != nil {
		t.Fatal(err)
	}
	// Templates ship inside mol-contract-brief; no town path may be referenced.
	if strings.Contains(string(data), "templates/contract") {
		t.Error("mayor.md still points to $GT_ROOT/templates/contract")
	}
}
