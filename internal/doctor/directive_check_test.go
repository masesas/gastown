package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/assetsync"
	"github.com/steveyegge/gastown/internal/directives"
)

func setupDirectiveTown(t *testing.T, rigs ...string) string {
	t.Helper()
	townRoot := t.TempDir()
	mayorDir := filepath.Join(townRoot, "mayor")
	if err := os.MkdirAll(mayorDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeDirectiveRigs(t, townRoot, rigs...)
	for _, r := range rigs {
		if err := os.MkdirAll(filepath.Join(townRoot, r), 0755); err != nil {
			t.Fatal(err)
		}
	}
	return townRoot
}

// writeDirectiveRigs registers rigs in mayor/rigs.json without creating their directories.
func writeDirectiveRigs(t *testing.T, townRoot string, rigs ...string) {
	t.Helper()
	var entries []string
	for _, r := range rigs {
		entries = append(entries, `"`+r+`": {"git_url": "https://example.com/x.git"}`)
	}
	content := `{"version": 1, "rigs": {` + strings.Join(entries, ",") + `}}`
	if err := os.WriteFile(filepath.Join(townRoot, "mayor", "rigs.json"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestDirectiveCheck_InvalidRigNameDoesNotBlockOthers(t *testing.T) {
	townRoot := setupDirectiveTown(t, "good")
	writeDirectiveRigs(t, townRoot, "good", "../evil")
	check := NewDirectiveCheck()

	result := check.Run(&CheckContext{TownRoot: townRoot})
	if result.Status != StatusWarning {
		t.Fatalf("status = %v, want Warning", result.Status)
	}
	joined := result.Message + "\n" + strings.Join(result.Details, "\n")
	if !strings.Contains(joined, "good/polecat.md") || !strings.Contains(joined, "invalid rig name") {
		t.Errorf("Run must report the good rig and the invalid name:\n%s", joined)
	}

	err := check.Fix(&CheckContext{TownRoot: townRoot})
	if err == nil || !strings.Contains(err.Error(), "invalid rig name") {
		t.Errorf("Fix error = %v, want the invalid rig name reported", err)
	}
	for _, p := range []string{
		filepath.Join(townRoot, "directives", "mayor.md"),
		filepath.Join(townRoot, "good", "directives", "polecat.md"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s not fixed despite one invalid rig: %v", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(townRoot), "evil")); !os.IsNotExist(err) {
		t.Error("invalid rig name created a directory outside the town")
	}
}

func TestDirectiveCheck_AbsentRigDirSkipped(t *testing.T) {
	townRoot := setupDirectiveTown(t)
	writeDirectiveRigs(t, townRoot, "ghost")
	provisionAllDirectives(t, townRoot)
	check := NewDirectiveCheck()

	if result := check.Run(&CheckContext{TownRoot: townRoot}); result.Status != StatusOK {
		t.Errorf("status = %v (%s), want OK with the absent rig skipped", result.Status, result.Message)
	}
	if err := check.Fix(&CheckContext{TownRoot: townRoot}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(townRoot, "ghost")); !os.IsNotExist(err) {
		t.Error("Fix created a phantom rig directory")
	}
}

func TestDirectiveCheck_CheckErrorsAggregated(t *testing.T) {
	townRoot := setupDirectiveTown(t, "r1", "r2")
	// r1 has a corrupt record; r2 must still be checked and fixed.
	dir := filepath.Join(townRoot, "r1", "directives")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, assetsync.RecordFile), []byte("{"), 0644); err != nil {
		t.Fatal(err)
	}
	check := NewDirectiveCheck()
	result := check.Run(&CheckContext{TownRoot: townRoot})
	joined := result.Message + "\n" + strings.Join(result.Details, "\n")
	if result.Status != StatusWarning || !strings.Contains(joined, "r1") || !strings.Contains(joined, "r2/polecat.md") {
		t.Errorf("Run did not aggregate: status=%v\n%s", result.Status, joined)
	}
	if err := check.Fix(&CheckContext{TownRoot: townRoot}); err == nil {
		t.Error("Fix hid the r1 error")
	}
	if _, err := os.Stat(filepath.Join(townRoot, "r2", "directives", "polecat.md")); err != nil {
		t.Errorf("r2 not fixed: %v", err)
	}
}

func provisionAllDirectives(t *testing.T, townRoot string, rigs ...string) {
	t.Helper()
	if _, err := assetsync.Provision(directives.TownSet(townRoot)); err != nil {
		t.Fatal(err)
	}
	for _, r := range rigs {
		s, err := directives.RigSet(townRoot, r)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := assetsync.Provision(s); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDirectiveCheck_AllOK(t *testing.T) {
	townRoot := setupDirectiveTown(t, "posin")
	provisionAllDirectives(t, townRoot, "posin")

	result := NewDirectiveCheck().Run(&CheckContext{TownRoot: townRoot})
	if result.Status != StatusOK {
		t.Fatalf("status = %v (%s), want OK", result.Status, result.Message)
	}
	if !strings.Contains(result.Message, "3 directives up-to-date") {
		t.Errorf("message = %q", result.Message)
	}
}

func TestDirectiveCheck_NewRigDirectivesFixable(t *testing.T) {
	// Town provisioned before directives shipped: nothing installed yet.
	townRoot := setupDirectiveTown(t, "posin")
	check := NewDirectiveCheck()

	result := check.Run(&CheckContext{TownRoot: townRoot})
	if result.Status != StatusWarning {
		t.Fatalf("status = %v, want Warning", result.Status)
	}
	if !strings.Contains(strings.Join(result.Details, "\n"), "posin/polecat.md") {
		t.Errorf("details do not name the rig file: %v", result.Details)
	}
	if err := check.Fix(&CheckContext{TownRoot: townRoot}); err != nil {
		t.Fatalf("Fix: %v", err)
	}
	for _, p := range []string{
		filepath.Join(townRoot, "directives", "mayor.md"),
		filepath.Join(townRoot, "posin", "directives", "crew.md"),
		filepath.Join(townRoot, "posin", "directives", "polecat.md"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s not installed by Fix: %v", p, err)
		}
	}
	if result := check.Run(&CheckContext{TownRoot: townRoot}); result.Status != StatusOK {
		t.Errorf("after Fix status = %v (%s), want OK", result.Status, result.Message)
	}
}

func TestDirectiveCheck_ModifiedReportedNotFixed(t *testing.T) {
	townRoot := setupDirectiveTown(t)
	provisionAllDirectives(t, townRoot)
	mayor := filepath.Join(townRoot, "directives", "mayor.md")
	if err := os.WriteFile(mayor, []byte("# local rules\n"), 0644); err != nil {
		t.Fatal(err)
	}

	check := NewDirectiveCheck()
	result := check.Run(&CheckContext{TownRoot: townRoot})
	if result.Status != StatusOK {
		t.Errorf("status = %v, want OK (modified is informational)", result.Status)
	}
	if !strings.Contains(result.Message, "1 modified") {
		t.Errorf("message = %q, want modified count", result.Message)
	}
	if err := check.Fix(&CheckContext{TownRoot: townRoot}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(mayor); string(got) != "# local rules\n" {
		t.Errorf("Fix overwrote a modified directive: %q", got)
	}
}

func TestDirectiveCheck_CorruptRecordWarns(t *testing.T) {
	townRoot := setupDirectiveTown(t)
	dir := filepath.Join(townRoot, "directives")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, assetsync.RecordFile), []byte("{"), 0644); err != nil {
		t.Fatal(err)
	}
	check := NewDirectiveCheck()
	if result := check.Run(&CheckContext{TownRoot: townRoot}); result.Status != StatusWarning {
		t.Errorf("status = %v, want Warning", result.Status)
	}
	if err := check.Fix(&CheckContext{TownRoot: townRoot}); err == nil {
		t.Error("Fix succeeded on a corrupt record")
	}
}
