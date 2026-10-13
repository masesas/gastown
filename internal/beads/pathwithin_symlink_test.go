package beads

import (
	"os"
	"path/filepath"
	"testing"
)

// A town root under a symlinked directory (e.g. macOS TMPDIR under /var ->
// /private/var) must still contain paths that do not exist yet.
func TestPathWithin_SymlinkedRootAndMissingPath(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "missing child", path: filepath.Join(link, "gantry"), want: true},
		{name: "missing nested child", path: filepath.Join(link, "algoanki", "mayor", "rig"), want: true},
		{name: "root itself", path: link, want: true},
		{name: "escape via dotdot", path: filepath.Join(link, "..", "outside"), want: false},
		{name: "unrelated missing path", path: filepath.Join(t.TempDir(), "elsewhere"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pathWithin(link, tt.path); got != tt.want {
				t.Errorf("pathWithin(%q, %q) = %v, want %v", link, tt.path, got, tt.want)
			}
		})
	}
}
