package beads

import (
	"os"
	"strings"
	"testing"
)

// patchProver is a MergeProver that is also a PatchProver: reach is the
// reachability answer, patch/patchErr the patch-equivalence answer.
type patchProver struct {
	reach    error
	patch    bool
	patchErr error
	calls    *int
}

func (p patchProver) VerifyPushedCommitReachableFromPushTarget(remote, branch, commit string) error {
	return p.reach
}

func (p patchProver) PatchEquivalentOnPushTarget(remote, branch, commit, base string) (bool, error) {
	if p.calls != nil {
		*p.calls++
	}
	return p.patch, p.patchErr
}

// reachOnlyProver answers reachability ("not on target") but has no patch check.
type reachOnlyProver struct{}

func (reachOnlyProver) VerifyPushedCommitReachableFromPushTarget(remote, branch, commit string) error {
	return notOnTarget{}
}

// gt-pd1c.81: "commit not on the target" is only a definite not-landed once git
// also found no equivalent patch (rebase/squash merges rewrite the commit).
func TestGitLandingVerdictPatchEquivalence(t *testing.T) {
	fields := &MRFields{Target: "main", CommitSHA: "c1c1c1c", Rig: "gastown"}
	cases := []struct {
		name   string
		prover MergeProver
		want   landingVerdict
	}{
		{"reachable", patchProver{reach: nil}, verdictLanded},
		{"squash or rebase landed", patchProver{reach: notOnTarget{}, patch: true}, verdictLanded},
		{"no equivalent patch", patchProver{reach: notOnTarget{}}, verdictNotLanded},
		{"patch check uncertain", patchProver{reach: notOnTarget{}, patchErr: os.ErrDeadlineExceeded}, verdictUnknown},
		{"prover without patch check", reachOnlyProver{}, verdictUnknown},
		{"reachability uncertain", patchProver{reach: os.ErrDeadlineExceeded, patch: true}, verdictUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, reason := gitLandingVerdict(fields, c.prover)
			if got != c.want {
				t.Fatalf("verdict = %v (%q), want %v", got, reason, c.want)
			}
			if got == verdictUnknown && reason == "" {
				t.Error("an unknown verdict must say why")
			}
		})
	}
}

// The patch check runs only after a definite "not reachable": a reachable
// commit or an uncertain reachability answer never pays for it.
func TestGitLandingVerdictPatchCheckOnlyAfterDefiniteNotOnTarget(t *testing.T) {
	fields := &MRFields{Target: "main", CommitSHA: "c1c1c1c"}
	calls := 0
	gitLandingVerdict(fields, patchProver{reach: nil, calls: &calls})
	gitLandingVerdict(fields, patchProver{reach: os.ErrDeadlineExceeded, calls: &calls})
	if calls != 0 {
		t.Fatalf("patch check ran %d times, want 0", calls)
	}
	gitLandingVerdict(fields, patchProver{reach: notOnTarget{}, calls: &calls})
	if calls != 1 {
		t.Fatalf("patch check ran %d times after not-on-target, want 1", calls)
	}
}

// A human verdict still decides when git cannot (here: patch check uncertain),
// and the git patch proof outranks a recorded verdict.
func TestMRLandingVerdictPatchProofVsHumanVerdict(t *testing.T) {
	uncertain := patchProver{reach: notOnTarget{}, patchErr: os.ErrDeadlineExceeded}
	fields := &MRFields{Target: "main", CommitSHA: "c1c1c1c", Landing: LandingNotLanded}
	if got := mrLandingVerdict(StatusClosed, fields, uncertain); got != verdictNotLanded {
		t.Fatalf("human verdict should decide when git cannot, got %v", got)
	}
	squashed := patchProver{reach: notOnTarget{}, patch: true}
	if got := mrLandingVerdict(StatusClosed, fields, squashed); got != verdictLanded {
		t.Fatalf("git patch proof must outrank a recorded not_landed, got %v", got)
	}
}

// recordingProver records what reached git.
type recordingProver struct {
	calls *int
	base  *string
}

func (p recordingProver) VerifyPushedCommitReachableFromPushTarget(remote, branch, commit string) error {
	*p.calls++
	return notOnTarget{}
}

func (p recordingProver) PatchEquivalentOnPushTarget(remote, branch, commit, base string) (bool, error) {
	*p.calls++
	*p.base = base
	return false, nil
}

// gt-pd1c.81 review: only hex object names and plain branch names reach git;
// anything else is unknown without running git.
func TestGitLandingVerdictValidatesInputs(t *testing.T) {
	cases := map[string]*MRFields{
		"option as commit":   {Target: "main", CommitSHA: "--upload-pack=x"},
		"short commit":       {Target: "main", CommitSHA: "abc12"},
		"non-hex commit":     {Target: "main", CommitSHA: "HEAD~1aaaa"},
		"option as target":   {Target: "-main", CommitSHA: "c1c1c1c1"},
		"traversal target":   {Target: "main/../x", CommitSHA: "c1c1c1c1"},
		"empty segment":      {Target: "integration//x", CommitSHA: "c1c1c1c1"},
		"option as base_sha": {Target: "main", CommitSHA: "c1c1c1c1", BaseSHA: "--all"},
	}
	for name, fields := range cases {
		calls, base := 0, ""
		got, reason := gitLandingVerdict(fields, recordingProver{&calls, &base})
		if got != verdictUnknown || reason == "" || calls != 0 {
			t.Errorf("%s: verdict %v (%q), %d git calls; want unknown, no git", name, got, reason, calls)
		}
	}

	calls, base := 0, ""
	ok := &MRFields{Target: "integration/gt-epic", CommitSHA: "C1C1C1C1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1", BaseSHA: "b1b1b1b"}
	if got, reason := gitLandingVerdict(ok, recordingProver{&calls, &base}); got != verdictNotLanded || base != "b1b1b1b" {
		t.Fatalf("valid fields: verdict %v (%q), base passed %q; want not_landed with base_sha forwarded", got, reason, base)
	}
}

func TestSafeRefAndObjectNames(t *testing.T) {
	for _, s := range []string{"main", "integration/gt-epic", "polecat/rust/gt-1.2_x"} {
		if !IsSafeRefName(s) {
			t.Errorf("IsSafeRefName(%q) = false", s)
		}
	}
	for _, s := range []string{"", "-x", "a/-b", "a//b", "a/../b", "/a", "a/", "a b", ".hidden"} {
		if IsSafeRefName(s) {
			t.Errorf("IsSafeRefName(%q) = true", s)
		}
	}
	if !IsGitObjectName("abcdef0") || IsGitObjectName("abcdef") || IsGitObjectName("abcdefg") || IsGitObjectName(strings.Repeat("a", 65)) {
		t.Error("IsGitObjectName: want hex of 7..64 characters only")
	}
}
