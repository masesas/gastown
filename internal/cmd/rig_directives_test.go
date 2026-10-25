package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProvisionRigDirectives(t *testing.T) {
	townRoot := t.TempDir()
	if n := provisionRigDirectives(townRoot, "posin"); n != 2 {
		t.Fatalf("provisioned %d, want 2", n)
	}
	for _, f := range []string{"crew.md", "polecat.md"} {
		if _, err := os.Stat(filepath.Join(townRoot, "posin", "directives", f)); err != nil {
			t.Errorf("%s not provisioned: %v", f, err)
		}
	}
	// Second run (e.g. re-adopt) keeps existing files.
	if n := provisionRigDirectives(townRoot, "posin"); n != 0 {
		t.Errorf("second run provisioned %d, want 0", n)
	}
	// Invalid names are rejected, nothing is written.
	if n := provisionRigDirectives(townRoot, "../x"); n != 0 {
		t.Errorf("unsafe rig name provisioned %d files", n)
	}
}
