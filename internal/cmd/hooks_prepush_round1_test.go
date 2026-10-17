package cmd

import (
	"errors"
	gitpkg "github.com/steveyegge/gastown/internal/git"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
)

// gt-pd1c.78 tahap 1, review round 1.

// H1: rewriting polecat/* away from an unlanded tip (force-push onto a landed
// commit, then delete) discards the old tip like a deletion.
func TestPrePushGuardPolecatRewrite(t *testing.T) {
	f := newPrePushFixture(t)
	plain := &prePushRig{name: "myrig", defaultBranch: "main"}
	const branch = "polecat/nux/gt-1"
	openMR := mqWith(mrIssue("mr1", "open", branch, f.good, ""))
	rewrite := prePushUpd("refs/heads/"+branch, f.mainSHA, f.good) // f.good is not an ancestor of main
	tests := []struct {
		name    string
		env     []string
		update  prePushUpdate
		queue   func() *beads.MergeQueue
		wantErr string
	}{
		{name: "witness force-push over unlanded tip: blocked", env: []string{"GT_ROLE", "myrig/witness"},
			update: rewrite, queue: openMR, wantErr: "rewrite"},
		{name: "other polecat force-push over unlanded tip: blocked", env: []string{"GT_ROLE", "myrig/polecats/max"},
			update: rewrite, queue: openMR, wantErr: "does not own"},
		{name: "mayor force-push, tip unknown locally: blocked", env: []string{"GT_ROLE", "mayor"},
			update: prePushUpd("refs/heads/"+branch, f.mainSHA, strings.Repeat("1", 40)), queue: openMR, wantErr: "is open"},
		{name: "witness force-push over never-submitted tip: blocked (tahap 2)", env: []string{"GT_ROLE", "myrig/witness"},
			update: rewrite, queue: mqWith(), wantErr: "no MR recorded this tip"},
		{name: "witness force-push over landed tip: allowed", env: []string{"GT_ROLE", "myrig/witness"},
			update: prePushUpd("refs/heads/"+branch, f.good, f.mainSHA), queue: openMR},
		{name: "owning polecat force-push: allowed", env: []string{"GT_ROLE", "myrig/polecats/nux", "GT_POLECAT", "nux"},
			update: rewrite, queue: openMR},
		{name: "refinery force-push: allowed", env: []string{"GT_ROLE", "myrig/refinery"}, update: rewrite, queue: openMR},
		{name: "crew force-push: allowed", env: []string{"GT_ROLE", "myrig/crew/dom"}, update: rewrite, queue: openMR},
		{name: "witness fast-forward: allowed", env: []string{"GT_ROLE", "myrig/witness"},
			update: prePushUpd("refs/heads/"+branch, f.good, f.mainSHA), queue: openMR},
		{name: "polecat with conflicting names is not the owner: blocked", env: []string{"GT_ROLE", "myrig/polecats/nux", "GT_POLECAT", "max"},
			update: rewrite, queue: openMR, wantErr: "rewrite"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := coordinatorGuard(f, plain, tt.queue, tt.env...).check([]prePushUpdate{tt.update})
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("want allowed, got: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want rejection containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

// tahap 2 S-M2: the owner's rewrite reads the merge queue; an unreadable
// queue or rig refuses (fail closed).
func TestPrePushGuardOwnerRewriteFailsClosed(t *testing.T) {
	g := &prePushGuard{
		remote: "origin",
		getenv: prePushEnv("GT_ROLE", "myrig/polecats/nux", "GT_POLECAT", "nux"),
		git:    gitpkg.NewGit(t.TempDir()),
		loadRig: func() (*prePushRig, error) {
			return nil, errors.New("rig unreadable")
		},
	}
	u := prePushUpd("refs/heads/polecat/nux/gt-1", strings.Repeat("a", 40), strings.Repeat("b", 40))
	if err := g.check([]prePushUpdate{u}); err == nil {
		t.Fatal("owner rewrite with an unreadable rig must refuse")
	}
}

// tahap 2 S-M2: the owning polecat may rewrite a tip never submitted or whose
// MR is still open (rebase for REWORK_REQUEST / MERGE_FAILED), not one whose
// MR was decided.
func TestPrePushGuardOwnerRewrite(t *testing.T) {
	f := newPrePushFixture(t)
	plain := &prePushRig{name: "myrig", defaultBranch: "main"}
	const branch = "polecat/nux/gt-1"
	rewrite := []prePushUpdate{prePushUpd("refs/heads/"+branch, f.mainSHA, f.good)}
	owner := []string{"GT_ROLE", "myrig/polecats/nux", "GT_POLECAT", "nux"}
	verdict := func(extra string) func() *beads.MergeQueue {
		return mqWithProver(gitLanded{}, mrIssue("mr1", "closed", branch, f.good, extra))
	}
	cases := []struct {
		name    string
		queue   func() *beads.MergeQueue
		wantErr string
	}{
		{"never submitted (rebase before gt done)", mqWith(), ""},
		{"MR still open (rebase for rework)", mqWith(mrIssue("mr1", "open", branch, f.good, "")), ""},
		{"later commits after a landed MR are unsubmitted work", mqWithProver(gitLanded{f.bad: true}, mrIssue("mr1", "closed", branch, f.bad, "")), ""},
		{"MR rejected", verdict("close_reason: rejected\n"), "already decided"},
		{"MR superseded", verdict("close_reason: superseded\n"), "gt polecat abandon"},
		{"MR closed for conflict", verdict("close_reason: conflict\n"), "closed for conflicts"},
		{"MR closed without verdict", verdict(""), "without an official verdict"},
		{"MR unverified", mqWith(mrIssue("mr1", "closed", branch, f.good, "")), "cannot tell"},
		{"queue unreadable", func() *beads.MergeQueue { return beads.NewMergeQueue(nil, errors.New("dolt down"), nil) }, "cannot read"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := coordinatorGuard(f, plain, c.queue, owner...).check(rewrite)
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("want allowed: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("want %q, got %v", c.wantErr, err)
			}
		})
	}
	// N2: per tip. MR1 rejected for the old tip and MR2 open for another
	// commit: the owner may not rewrite the rejected tip away; an open MR for
	// the old tip itself is the rework rebase and passes.
	otherOpen := mrIssue("mr2", "open", branch, f.bad, "")
	rejectedPlusOpen := func() *beads.MergeQueue {
		q := mqWithProver(gitLanded{}, mrIssue("mr1", "closed", branch, f.good, "close_reason: rejected\n"), otherOpen)
		return q()
	}
	if err := coordinatorGuard(f, plain, rejectedPlusOpen, owner...).check(rewrite); err == nil || !strings.Contains(err.Error(), "already decided") {
		t.Fatalf("rejected T1 with MR2 open on the branch: %v", err)
	}
	openOwn := mqWith(mrIssue("mr1", "open", branch, f.good, ""), otherOpen)
	if err := coordinatorGuard(f, plain, openOwn, owner...).check(rewrite); err != nil {
		t.Fatalf("open MR for the old tip (rework rebase): %v", err)
	}
	// Non-owners never discard a branch with an open MR, whatever the tip.
	del := []prePushUpdate{prePushUpd("refs/heads/"+branch, testZeroSHA, f.good)}
	if err := coordinatorGuard(f, plain, rejectedPlusOpen, "GT_ROLE", "myrig/witness").check(del); err == nil || !strings.Contains(err.Error(), "mr2 from this branch is open") {
		t.Fatalf("witness delete while MR2 is open: %v", err)
	}
	// An archived decided tip may be rewritten (nothing is lost).
	archiveTip(t, f, "polecat/nux/gt-1", f.good)
	if err := coordinatorGuard(f, plain, verdict("close_reason: rejected\n"), owner...).check(rewrite); err != nil {
		t.Fatalf("archived tip: %v", err)
	}
}

// M1: what a --force nuke would lose besides the branch.
func TestForceNukeWorkProblems(t *testing.T) {
	cases := []struct {
		name  string
		res   *SafetyCheckResult
		state *GitState
		err   error
		want  string // "" = nothing lost
	}{
		{name: "clean", res: &SafetyCheckResult{}, state: &GitState{Clean: true}},
		{name: "worktree gone", res: &SafetyCheckResult{}},
		{name: "stale hook is fine", res: &SafetyCheckResult{HookBead: "gt-1", HookStale: true}, state: &GitState{Clean: true}},
		{name: "active hook", res: &SafetyCheckResult{HookBead: "gt-1"}, want: "work on hook (gt-1)"},
		{name: "assigned work", res: &SafetyCheckResult{AssignedIssue: "gt-2"}, want: "gt-2"},
		{name: "agent bead unreadable", res: &SafetyCheckResult{AgentBeadUnreadable: true}, want: "agent bead unreadable"},
		{name: "uncommitted or untracked", res: &SafetyCheckResult{}, state: &GitState{UncommittedFiles: []string{"a.go"}}, want: "dirty"},
		{name: "stash", res: &SafetyCheckResult{}, state: &GitState{StashCount: 1}, want: "1 stash"},
		{name: "git state unreadable", res: &SafetyCheckResult{}, err: errors.New("boom"), want: "fail closed"},
	}
	for _, c := range cases {
		got := strings.Join(forceNukeWorkProblems(c.res, c.state, c.err), "; ")
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// M1: a dirty worktree or active hook blocks an agent's --force even when the
// branch is safe; a human confirms at a TTY.
func TestGateNukeForceWorkProblems(t *testing.T) {
	dirty := func(polecatTarget) error {
		return errors.New("worktree is dirty (1 uncommitted/untracked file(s), 0 stash(es))")
	}
	setCaller(t, "GT_ROLE", "gastown/witness")
	hp, _ := stubPrompt(true, "gastown/nux\n")
	if err := (forceDiscardGate{action: nukeForceAction, check: dirty, prompt: hp}).run(nukeTargets("nux")); err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("witness on a dirty worktree: %v", err)
	}
	setCaller(t)
	hp, _ = stubPrompt(true, "gastown/nux\n")
	if err := (forceDiscardGate{action: nukeForceAction, check: dirty, prompt: hp}).run(nukeTargets("nux")); err != nil {
		t.Fatalf("human at a TTY: %v", err)
	}
	if err := forceDiscardWorkCheck(polecatTarget{}); err == nil {
		t.Fatal("an unreadable polecat must fail closed")
	}
}

// Confirmation errors name the action and keep errNotInteractive.
func TestConfirmHumanInteractivelyNamesAction(t *testing.T) {
	hp, _ := stubPrompt(false, "")
	err := hp.confirm("gt polecat nuke --force", "p", "x")
	if !errors.Is(err, errNotInteractive) || !strings.Contains(err.Error(), "gt polecat nuke --force") {
		t.Fatalf("no TTY: %v", err)
	}
	hp, _ = stubPrompt(true, "y\n")
	if err := hp.confirm("landing verdict on MR m1", "p", "x"); err == nil ||
		!strings.Contains(err.Error(), "landing verdict on MR m1") || !strings.Contains(err.Error(), "did not match") {
		t.Fatalf("mismatch: %v", err)
	}
}

// Round 1 (convergence): runHooksPrePush clears git's repository-local
// variables first, so a stray GIT_DIR cannot point the guard elsewhere.
func TestRunHooksPrePushClearsGitLocalEnv(t *testing.T) {
	f := newPrePushFixture(t)
	t.Chdir(f.repo)
	setCaller(t)
	t.Setenv("GT_INTEGRATION_LAND", "")
	t.Setenv("GIT_DIR", filepath.Join(t.TempDir(), "not-a-repo"))
	hooksPrePushCmd.SetIn(strings.NewReader("refs/heads/main " + f.good + " refs/heads/main " + f.mainSHA + "\n"))
	defer hooksPrePushCmd.SetIn(nil)
	if err := runHooksPrePush(hooksPrePushCmd, []string{"origin", f.remote}); err != nil {
		t.Fatalf("push with a stray GIT_DIR: %v", err)
	}
	if v, ok := os.LookupEnv("GIT_DIR"); ok {
		t.Fatalf("GIT_DIR not cleared: %q", v)
	}
}

// callerMatrix is one env matrix run on the Go classification and on the
// bash fallback (.githooks/pre-push): exempt and refinery must agree.
var callerMatrix = []struct {
	env      []string
	exempt   bool
	refinery bool
}{
	{nil, true, false},
	{[]string{"GT_ROLE", "gastown/crew/max"}, true, false},
	{[]string{"GT_ROLE", "crew"}, true, false},
	{[]string{"GT_CREW", "max"}, true, false},
	{[]string{"BD_ACTOR", "gastown/crew/max"}, true, false},
	{[]string{"BD_ACTOR", "overseer"}, true, false},
	{[]string{"BD_ACTOR", "overseer", "GT_CREW", "max"}, true, false},
	{[]string{"GT_ROLE", "gastown/crew/max", "BD_ACTOR", "gastown/crew/max", "GT_CREW", "max"}, true, false},
	{[]string{"GT_ROLE", "gastown/crew/max", "BD_ACTOR", "gastown/witness"}, false, false},
	{[]string{"GT_ROLE", "gastown/crew/max", "BD_ACTOR", "overseer"}, false, false},
	{[]string{"GT_POLECAT", "nux", "BD_ACTOR", "overseer"}, false, false},
	{[]string{"GT_ROLE", "gastown/crew/max", "GT_POLECAT", "nux"}, false, false},
	{[]string{"GT_ROLE", "gastown/crew/max", "BD_ACTOR", "gastown/polecats/nux"}, false, false},
	{[]string{"GT_ROLE", "gastown/refinery"}, false, true},
	{[]string{"GT_ROLE", "gastown/refinery", "BD_ACTOR", "gastown/refinery"}, false, true},
	{[]string{"GT_ROLE", "refinery", "GT_CREW", "max"}, false, true},
	{[]string{"GT_ROLE", "gastown/refinery", "BD_ACTOR", "mayor"}, false, false},
	{[]string{"GT_ROLE", "gastown/witness"}, false, false},
	{[]string{"GT_ROLE", "mayor", "BD_ACTOR", "mayor"}, false, false},
	{[]string{"GT_ROLE", "deacon/boot", "BD_ACTOR", "deacon-boot"}, false, false},
	{[]string{"GT_ROLE", "dog", "BD_ACTOR", "deacon/dogs/alpha"}, false, false},
	{[]string{"GT_ROLE", "gastown/polecats/nux", "GT_POLECAT", "nux", "BD_ACTOR", "gastown/polecats/nux"}, false, false},
	{[]string{"BD_ACTOR", "daemon"}, false, false},
	{[]string{"GT_ROLE", "overseer"}, false, false},
}

func TestGTCallerFromMatrix(t *testing.T) {
	for _, c := range callerMatrix {
		got := gtCallerFrom(prePushEnv(c.env...))
		if got.exempt() != c.exempt || (got.role == RoleRefinery) != c.refinery {
			t.Errorf("env %v: exempt=%v role=%q, want exempt=%v refinery=%v", c.env, got.exempt(), got.role, c.exempt, c.refinery)
		}
	}
}

// Parity: the bash fallback classifies every row of callerMatrix the same
// way (deletion of polecat/* = exempt only; push to main = exempt or refinery).
func TestCallerMatrixBashParity(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	hook, err := filepath.Abs(filepath.Join("..", "..", ".githooks", "pre-push"))
	if err != nil {
		t.Fatal(err)
	}
	f := newPrePushFixture(t)
	runGitForMQSubmitTest(t, f.repo, "push", "-q", "origin", f.good+":refs/heads/polecat/nux")
	runGitForMQSubmitTest(t, f.repo, "remote", "set-head", "origin", "main")
	nogt := t.TempDir()
	for _, tool := range []string{"git", "sed", "awk", "cat"} {
		p, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("%s not available", tool)
		}
		if err := os.Symlink(p, filepath.Join(nogt, tool)); err != nil {
			t.Fatal(err)
		}
	}
	run := func(env []string, line string) bool {
		cmd := exec.Command(bash, hook, "origin")
		cmd.Dir = f.repo
		cmd.Env = []string{"PATH=" + nogt, "HOME=" + t.TempDir()}
		for i := 0; i+1 < len(env); i += 2 {
			cmd.Env = append(cmd.Env, env[i]+"="+env[i+1])
		}
		cmd.Stdin = strings.NewReader(line + "\n")
		return cmd.Run() == nil
	}
	del := "(delete) " + testZeroSHA + " refs/heads/polecat/nux " + f.good
	toMain := "HEAD " + f.good + " refs/heads/main " + f.mainSHA
	for _, c := range callerMatrix {
		if got := run(c.env, del); got != c.exempt {
			t.Errorf("bash, env %v: polecat deletion allowed=%v, want %v", c.env, got, c.exempt)
		}
		if got, want := run(c.env, toMain), c.exempt || c.refinery; got != want {
			t.Errorf("bash, env %v: push to main allowed=%v, want %v", c.env, got, want)
		}
	}
}
