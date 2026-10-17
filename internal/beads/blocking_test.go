package beads

import "testing"

func TestIsBlockingDependencyType(t *testing.T) {
	for _, typ := range []string{"blocks", "conditional-blocks", "waits-for", "merge-blocks", " Blocks "} {
		if !IsBlockingDependencyType(typ) {
			t.Errorf("IsBlockingDependencyType(%q) = false, want true", typ)
		}
	}
	for _, typ := range []string{"", "parent-child", "tracks", "related", "discovered-from", "thread", "custom"} {
		if IsBlockingDependencyType(typ) {
			t.Errorf("IsBlockingDependencyType(%q) = true, want false", typ)
		}
	}
}

func TestIsBlockerResolved(t *testing.T) {
	cases := []struct {
		depType, status, reason string
		want                    bool
	}{
		{"blocks", "closed", "", true},
		{"blocks", "tombstone", "", true},
		{"blocks", "open", "", false},
		{"blocks", "in_progress", "", false},
		{"blocks", "pinned", "", false},
		{"blocks", "", "", false},
		{"waits-for", "closed", "done", true},
		{"merge-blocks", "closed", "Merged in abc123", true},
		{"merge-blocks", "closed", "PR merged", false},
		{"merge-blocks", "closed", "", false},
		{"merge-blocks", "tombstone", "", true},
		{"merge-blocks", "open", "Merged in x", false},
	}
	for _, c := range cases {
		if got := IsBlockerResolved(c.depType, c.status, c.reason); got != c.want {
			t.Errorf("IsBlockerResolved(%q, %q, %q) = %v, want %v", c.depType, c.status, c.reason, got, c.want)
		}
	}
}
