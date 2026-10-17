package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
	gitpkg "github.com/steveyegge/gastown/internal/git"
)

// gt-pd1c.78: gt polecat nuke --force (tahap 1) and gt polecat remove --force
// (tahap 2) are gated by the polecat branch deletion rule; when it does not
// hold, --force is a human/crew action at a TTY.

func nukeTargets(names ...string) []polecatTarget {
	out := make([]polecatTarget, 0, len(names))
	for _, n := range names {
		out = append(out, polecatTarget{rigName: "gastown", polecatName: n})
	}
	return out
}

func setCaller(t *testing.T, kv ...string) {
	t.Helper()
	for _, k := range agentSessionEnvVars {
		t.Setenv(k, "")
	}
	for i := 0; i+1 < len(kv); i += 2 {
		t.Setenv(kv[i], kv[i+1])
	}
}

var errUnlanded = errors.New("deleting polecat/nux: MR gt-mr1 from this branch is open")

func failFor(names ...string) func(polecatTarget) error {
	return func(p polecatTarget) error {
		for _, n := range names {
			if p.polecatName == n {
				return errUnlanded
			}
		}
		return nil
	}
}

func TestGateNukeForce(t *testing.T) {
	cases := []struct {
		name    string
		env     []string
		failing []string
		tty     bool
		input   string
		wantErr string // "" = allowed
		prompt  bool   // a TTY prompt is expected
	}{
		{name: "witness, branch not proven safe: refused", env: []string{"GT_ROLE", "gastown/witness"},
			failing: []string{"nux"}, tty: true, input: "gastown/nux\n", wantErr: "refusing gt polecat nuke --force"},
		{name: "mayor, branch not proven safe: refused", env: []string{"GT_ROLE", "mayor"},
			failing: []string{"nux"}, tty: true, input: "gastown/nux\n", wantErr: "only a human"},
		{name: "polecat claiming crew: refused", env: []string{"GT_ROLE", "gastown/crew/max", "GT_POLECAT", "nux"},
			failing: []string{"nux"}, tty: true, input: "gastown/nux\n", wantErr: "only a human"},
		{name: "witness, every branch proven safe: allowed", env: []string{"GT_ROLE", "gastown/witness"}},
		{name: "human, proven safe: allowed without prompt"},
		{name: "human without TTY, not proven safe: refused", failing: []string{"nux"}, wantErr: "interactive terminal"},
		{name: "human at TTY types the polecats: allowed", failing: []string{"nux"}, tty: true, input: "gastown/nux gastown/max\n", prompt: true},
		{name: "human at TTY mistypes: refused", failing: []string{"nux"}, tty: true, input: "gastown/nux\n", wantErr: "did not match", prompt: true},
		{name: "crew at TTY: allowed", env: []string{"GT_ROLE", "gastown/crew/max"}, failing: []string{"max"}, tty: true,
			input: "gastown/nux gastown/max\n", prompt: true},
		{name: "crew without TTY: refused", env: []string{"GT_ROLE", "gastown/crew/max"}, failing: []string{"max"}, wantErr: "interactive terminal"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setCaller(t, c.env...)
			hp, out := stubPrompt(c.tty, c.input)
			err := forceDiscardGate{action: nukeForceAction, check: failFor(c.failing...), prompt: hp}.run(nukeTargets("nux", "max"))
			if c.wantErr == "" && err != nil {
				t.Fatalf("want allowed, got %v", err)
			}
			if c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)) {
				t.Fatalf("want error containing %q, got %v", c.wantErr, err)
			}
			if c.wantErr != "" && len(c.failing) > 0 && !strings.Contains(err.Error(), "MR gt-mr1") {
				t.Fatalf("refusal must say why the branch is not safe: %v", err)
			}
			if got := out.Len() > 0; got != c.prompt {
				t.Fatalf("prompt shown=%v, want %v (%q)", got, c.prompt, out.String())
			}
			if c.prompt && !strings.Contains(out.String(), "MR gt-mr1") {
				t.Fatalf("prompt must show what would be lost: %q", out.String())
			}
		})
	}
}

// Dry runs and runs without --force never reach the gate.
func TestForceDiscardGateOnlyForForce(t *testing.T) {
	setCaller(t, "GT_ROLE", "gastown/witness")
	calls := 0
	hp, _ := stubPrompt(true, "gastown/nux\n")
	gate := forceDiscardGate{action: nukeForceAction, check: func(polecatTarget) error { calls++; return errUnlanded }, prompt: hp}
	if err := gate.runIf(nukeTargets("nux"), false, false); err != nil || calls != 0 {
		t.Fatalf("non-force nuke: err=%v calls=%d", err, calls)
	}
	if err := gate.runIf(nukeTargets("nux"), true, true); err != nil || calls != 0 {
		t.Fatalf("dry run: err=%v calls=%d", err, calls)
	}
	if err := gate.runIf(nukeTargets("nux"), true, false); err == nil || calls != 1 {
		t.Fatalf("witness --force on an unproven branch: err=%v calls=%d", err, calls)
	}
}

// tahap 2 (d): gt polecat remove --force is the same gate, named as itself.
func TestRemoveForceUsesTheSameGate(t *testing.T) {
	g := newForceDiscardGate(removeForceAction)
	if g.action != removeForceAction || g.check == nil || g.prompt.tty == nil {
		t.Fatalf("production gate: %+v", g)
	}
	setCaller(t, "GT_ROLE", "gastown/witness")
	hp, _ := stubPrompt(true, "gastown/nux\n")
	err := forceDiscardGate{action: removeForceAction, check: failFor("nux"), prompt: hp}.run(nukeTargets("nux"))
	if err == nil || !strings.Contains(err.Error(), "refusing gt polecat remove --force") || !strings.Contains(err.Error(), "MR gt-mr1") {
		t.Fatalf("witness remove --force on an unproven branch: %v", err)
	}
	// The production check fails closed on a polecat it cannot read.
	if err := g.run(nukeTargets("nux")); err == nil || !strings.Contains(err.Error(), "fail closed") {
		t.Fatalf("unreadable polecat: %v", err)
	}
}

// The rule against a real rig repository: remote tip and local tip.
func TestPolecatRepoBranchDeletable(t *testing.T) {
	f := newPrePushFixture(t)
	repo := gitpkg.NewGit(f.repo)
	push := func(sha, branch string) {
		t.Helper()
		runGitForMQSubmitTest(t, f.repo, "push", "-q", "origin", sha+":refs/heads/"+branch)
	}
	push(f.good, "polecat/nux")
	push(f.mainSHA, "polecat/landed")
	push(f.mainSHA, "polecat/split")
	runGitForMQSubmitTest(t, f.repo, "branch", "polecat/split", f.bad)
	openMR := mqWith(mrIssue("gt-mr1", "open", "polecat/nux", f.good, ""))
	openSplit := mqWith(mrIssue("gt-mr2", "open", "polecat/split", f.bad, ""))
	unread := func() *beads.MergeQueue { return beads.NewMergeQueue(nil, errors.New("dolt down"), nil) }
	deletable := func(branch string, q func() *beads.MergeQueue) error {
		return polecatRepoBranchDeletable(repo, "gastown", branch, "main", q)
	}

	if err := deletable("polecat/nux", openMR); err == nil || !strings.Contains(err.Error(), "gt-mr1") {
		t.Fatalf("unlanded branch with an open MR: %v", err)
	}
	// tahap 2: unlanded work without an MR is no longer free to discard.
	if err := deletable("polecat/nux", mqWith()); err == nil || !strings.Contains(err.Error(), "resubmit") {
		t.Fatalf("unlanded branch without any MR: %v", err)
	}
	if err := deletable("polecat/landed", unread); err != nil {
		t.Fatalf("landed branch needs no beads: %v", err)
	}
	if err := deletable("polecat/split", openSplit); err == nil {
		t.Fatal("a local tip with unlanded work and an open MR must refuse even when the remote tip landed")
	}
	// tahap 2: a branch that exists nowhere (origin read, no local ref) has
	// nothing to lose, whatever beads say.
	if err := deletable("polecat/gone", unread); err != nil {
		t.Fatalf("no branch anywhere: %v", err)
	}
	// An archived tip (gt polecat abandon) is preserved: no beads needed.
	push(f.integrationOnTopBad, "polecat/abandoned")
	archiveTip(t, f, "polecat/abandoned", f.integrationOnTopBad)
	if err := deletable("polecat/abandoned", unread); err != nil {
		t.Fatalf("archived tip: %v", err)
	}

	// End to end through the gate: a witness may not force-nuke it.
	setCaller(t, "GT_ROLE", "gastown/witness")
	hp, _ := stubPrompt(true, "gastown/nux\n")
	err := forceDiscardGate{action: nukeForceAction, prompt: hp, check: func(polecatTarget) error {
		return deletable("polecat/nux", openMR)
	}}.run(nukeTargets("nux"))
	if err == nil || !strings.Contains(err.Error(), "refusing gt polecat nuke --force") {
		t.Fatalf("witness nuke --force on an unlanded branch: %v", err)
	}
}

// An unreadable polecat fails closed.
func TestPolecatTargetBranchDeletableFailsClosed(t *testing.T) {
	if err := polecatTargetBranchDeletable(polecatTarget{rigName: "gastown", polecatName: "nux"}); err == nil ||
		!strings.Contains(err.Error(), "fail closed") {
		t.Fatalf("polecat without manager/rig: %v", err)
	}
}
