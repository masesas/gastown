package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProvisionRigDirectives(t *testing.T) {
	townRoot := t.TempDir()
	provisionRigDirectives(townRoot, "posin")
	dir := filepath.Join(townRoot, "posin", "directives")
	for _, f := range []string{"crew.md", "polecat.md"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("%s not provisioned: %v", f, err)
		}
	}

	// Second run (e.g. re-adopt) keeps an edited file.
	edited := filepath.Join(dir, "crew.md")
	if err := os.WriteFile(edited, []byte("mine\n"), 0644); err != nil {
		t.Fatal(err)
	}
	provisionRigDirectives(townRoot, "posin")
	if got, _ := os.ReadFile(edited); string(got) != "mine\n" {
		t.Errorf("second run overwrote crew.md: %q", got)
	}

	// Invalid names are rejected, nothing is written outside the town.
	provisionRigDirectives(townRoot, "../x")
	if _, err := os.Stat(filepath.Join(filepath.Dir(townRoot), "x")); !os.IsNotExist(err) {
		t.Errorf("unsafe rig name wrote outside the town")
	}
}
