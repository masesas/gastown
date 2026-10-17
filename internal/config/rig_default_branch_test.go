package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRigDefaultBranch(t *testing.T) {
	for content, want := range map[string]string{
		`{"default_branch":"develop"}`:      "develop",
		`{"default_branch":" release/v2 "}`: "release/v2",
		`{"default_branch":""}`:             "",
		`{"default_branch":"-x"}`:           "",
		`{"default_branch":"a/../b"}`:       "",
		`{"default_branch":"a b"}`:          "",
		`{broken`:                           "",
		`{"type":"rig"}`:                    "",
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := RigDefaultBranch(dir); got != want {
			t.Errorf("%s: got %q, want %q", content, got, want)
		}
	}
	if got := RigDefaultBranch(t.TempDir()); got != "" {
		t.Errorf("missing config: %q", got)
	}
}
