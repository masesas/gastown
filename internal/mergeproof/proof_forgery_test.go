package mergeproof

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/gastown/internal/git"
)

// gt-pd1c.86 H1: the remote tips are honest (ls-remote), but ancestry is
// computed from the local object database. A replace ref (or a graft) that
// gives the real main tip an extra parent must not "prove" a commit that was
// never pushed.

// forgeMainAncestry makes f.other look like an ancestor of the real main tip
// through mechanism ("replace" or "graft").
func forgeMainAncestry(t *testing.T, f remoteFixture, mechanism string) {
	t.Helper()
	switch mechanism {
	case "replace":
		fake := run(t, f.repo, "commit-tree", f.main+"^{tree}", "-p", f.main, "-p", f.other, "-m", "forged")
		run(t, f.repo, "replace", f.main, fake)
	case "graft":
		path := filepath.Join(f.repo, ".git", "info", "grafts")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(f.main+" "+f.other+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The forgery works for plain git.
	if out := run(t, f.repo, "rev-list", f.main); !containsLine(out, f.other) {
		t.Fatalf("%s forgery did not take effect for plain git", mechanism)
	}
}

func containsLine(out, want string) bool {
	for _, l := range splitLines(out) {
		if l == want {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

func TestCoveredByTargetsRefusesForgedAncestry(t *testing.T) {
	for _, mechanism := range []string{"replace", "graft"} {
		t.Run(mechanism, func(t *testing.T) {
			f := newRemoteFixture(t)
			forgeMainAncestry(t, f, mechanism)
			if CoveredByTargets(git.NewGit(f.repo), f.other, f.tips()) {
				t.Fatalf("CoveredByTargets accepted a commit made an ancestor of main by a %s", mechanism)
			}
		})
	}
}

func TestProverRefusesForgedAncestry(t *testing.T) {
	f := newRemoteFixture(t)
	forgeMainAncestry(t, f, "replace")
	p := ForRigPath(filepath.Dir(filepath.Dir(f.repo)))
	if p == nil {
		t.Fatal("ForRigPath = nil")
	}
	if err := p.VerifyPushedCommitReachableFromPushTarget("origin", "main", f.other); err == nil {
		t.Fatal("prover accepted a commit made reachable from main by a replace ref")
	}
}
