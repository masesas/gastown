package refname

import (
	"os/exec"
	"strings"
	"testing"
)

// Names gt really writes (internal/polecat/branch_name.go formats, the
// merge targets and integration branches). Literal here because refname is
// a leaf package; internal/polecat's tests run the generator through it.
var realBranches = []string{
	"main",
	"master",
	"release/v2",
	"integration/gt-epic",
	"beads-sync",
	"polecat/mirelurk/kp-znd+mvcf8o91",     // polecat/<name>/<issue>+<base36 ms>
	"polecat/s2test/kp-s2b+t1",             // the pilot branch
	"polecat/Nux/gt-abc.12+mvcf8o91",       // dotted child issue, capitalised name
	"polecat/nux/gt-1@mvcf8o91",            // legacy '@' issue separator (in flight)
	"polecat/nux-mvcf8o91",                 // polecat/<name>-<suffix> without an issue
	"polecat/rust/gt-1.2_x",                // unprefixed issue tail
	"polecat/nux/hq-cv-4f2+mvcf8o91",       // convoy-style ID
	"polecat/nux/kp-znd+mvcf8o91.lockstep", // ".lock" only as a whole suffix is refused
}

var badNames = []string{
	"", "-x", "+x", "a/-b", "a/+b", "a//b", "a/../b", "a..b", "/a", "a/", "a b", "a\tb",
	".hidden", "a/.b", "a.", "a/b.", "a.lock", "a/b.lock", "a/b.lock/c", "a@{1}", "@", "a/@",
	"HEAD", "a~1", "a^", "a:b", "a?", "a*", "a[b", "a\\b", "a;rm", "a$b", "a'b", "a\"b",
	"a\nb", "polecat/nux/gt-1\x00", "polecat/é", "a{b", "a}b", "a!b", "a%b", "a&b",
}

func TestValidAcceptsRealBranchNames(t *testing.T) {
	for _, s := range realBranches {
		if !Valid(s) {
			t.Errorf("Valid(%q) = false, want true", s)
		}
	}
}

func TestValidRefusesUnsafeNames(t *testing.T) {
	for _, s := range badNames {
		if Valid(s) {
			t.Errorf("Valid(%q) = true, want false", s)
		}
	}
}

// Every accepted name is a valid branch name for git itself (Valid is a strict
// subset of `git check-ref-format --branch`), so a name that passes is never
// refused or reinterpreted by git.
func TestValidIsSubsetOfGitCheckRefFormat(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	for _, s := range append(append([]string{}, realBranches...), badNames...) {
		if !Valid(s) {
			continue
		}
		if strings.ContainsAny(s, "\x00\n") {
			t.Fatalf("Valid accepted a control character: %q", s)
		}
		if out, err := exec.Command("git", "check-ref-format", "--branch", s).CombinedOutput(); err != nil {
			t.Errorf("Valid(%q) = true but git check-ref-format --branch refuses it: %s", s, out)
		}
		if out, err := exec.Command("git", "check-ref-format", "refs/heads/"+s).CombinedOutput(); err != nil {
			t.Errorf("Valid(%q) = true but git check-ref-format refs/heads/... refuses it: %s", s, out)
		}
	}
}
