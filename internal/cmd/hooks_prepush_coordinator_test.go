package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
)

// gt-pd1c.78 tahap 1: coordinator guards in the pre-push hook.

func mqWith(mrs ...*beads.Issue) func() *beads.MergeQueue { return mqWithProver(nil, mrs...) }

// gitLanded is a beads.MergeProver whose git says the listed commits landed
// and every other commit definitely did not.
type gitLanded map[string]bool

func (g gitLanded) VerifyPushedCommitReachableFromPushTarget(remote, branch, commit string) error {
	if g[commit] {
		return nil
	}
	return stubNotOnTarget{"commit " + commit + " not on " + branch}
}

// PatchEquivalentOnPushTarget: no rebase/squash equivalent either (a
// definite "not landed" for commits that are not listed).
func (g gitLanded) PatchEquivalentOnPushTarget(remote, branch, commit, base string) (bool, error) {
	return g[commit], nil
}

func mqWithProver(landed gitLanded, mrs ...*beads.Issue) func() *beads.MergeQueue {
	return func() *beads.MergeQueue {
		idx := beads.MergeQueueIndex{}
		for _, mr := range mrs {
			src := beads.ParseMRFields(mr).SourceIssue
			idx[src] = append(idx[src], mr)
		}
		var resolve beads.ProverResolver
		if landed != nil {
			resolve = func(string) beads.MergeProver { return landed }
		}
		return beads.NewMergeQueue(idx, nil, resolve)
	}
}

func mrIssue(id, status, branch, commit, extra string) *beads.Issue {
	return &beads.Issue{ID: id, Status: status,
		Description: "branch: " + branch + "\ntarget: main\nsource_issue: gt-src-" + id + "\ncommit_sha: " + commit + "\n" + extra}
}

func coordinatorGuard(f prePushFixture, r *prePushRig, queue func() *beads.MergeQueue, env ...string) *prePushGuard {
	g := newGuard(f, r, env...)
	g.loadQueue = queue
	return g
}

func TestPrePushGuardPolecatBranchDeletion(t *testing.T) {
	f := newPrePushFixture(t)
	f.archived = archiveTip(t, f, "polecat/old/gt-9", f.integrationOnTopBad)
	plain := &prePushRig{name: "myrig", defaultBranch: "main"}
	const branch = "polecat/nux/gt-1+mvcf8o91"
	del := func(tip string) []prePushUpdate {
		return []prePushUpdate{prePushUpd("refs/heads/"+branch, testZeroSHA, tip)}
	}
	witness := []string{"GT_ROLE", "myrig/witness"}
	unread := func() *beads.MergeQueue { return beads.NewMergeQueue(nil, errors.New("dolt down"), nil) }
	tests := []struct {
		name    string
		env     []string
		tip     string
		queue   func() *beads.MergeQueue
		wantErr string // "" = allowed
	}{
		{name: "witness deletes unlanded branch with open MR: blocked", env: witness, tip: f.good,
			queue: mqWith(mrIssue("mr1", "open", branch, f.good, "")), wantErr: "is open"},
		{name: "witness deletes branch landed on main: allowed", env: witness, tip: f.mainSHA, queue: unread},
		{name: "witness deletes branch preserved on integration/*: allowed", env: witness, tip: f.epicSHA, queue: unread},
		// tahap 2: HoldNone no longer frees a branch; each class has one exit.
		{name: "witness deletes rejected work: blocked, abandon offered", env: witness, tip: f.good,
			queue:   mqWith(mrIssue("mr1", "closed", branch, f.good, "close_reason: rejected\nlanding: not_landed\n")),
			wantErr: "gt polecat abandon myrig/" + branch},
		{name: "witness deletes superseded work: blocked, abandon offered", env: witness, tip: f.good,
			queue: mqWithProver(gitLanded{}, mrIssue("mr1", "closed", branch, f.good, "close_reason: superseded\n")), wantErr: "closed as superseded"},
		{name: "witness deletes conflicted work: blocked, rework (no abandon)", env: witness, tip: f.good,
			queue: mqWithProver(gitLanded{}, mrIssue("mr1", "closed", branch, f.good, "close_reason: conflict\n")), wantErr: "rework and resubmit"},
		{name: "witness deletes never-submitted branch: blocked, resubmit", env: witness, tip: f.good, queue: mqWith(),
			wantErr: "no MR recorded this tip"},
		{name: "witness deletes an archived tip: allowed", env: witness, tip: f.archived, queue: unread},
		{name: "witness deletes unverified branch: blocked", env: witness, tip: f.good,
			queue: mqWith(mrIssue("mr1", "closed", branch, f.good, "")), wantErr: "cannot tell"},
		{name: "beads unreadable: blocked", env: witness, tip: f.good, queue: unread, wantErr: "cannot read the merge queue"},
		{name: "no queue loader: blocked", env: witness, tip: f.good, wantErr: "not available"},
		{name: "landed only by recorded close_reason: blocked (H2)", env: witness, tip: f.good,
			queue: mqWith(mrIssue("mr1", "closed", branch, f.good, "close_reason: merged\n")), wantErr: "git has not proven"},
		{name: "landed only by recorded merge_commit: blocked (H2)", env: witness, tip: f.good,
			queue: mqWith(mrIssue("mr1", "closed", branch, f.good, "merge_commit: abc\n")), wantErr: "git has not proven"},
		{name: "recorded merged that git disproves (F7): blocked, unverified", env: witness, tip: f.good,
			queue:   mqWithProver(gitLanded{}, mrIssue("mr1", "closed", branch, f.good, "close_reason: merged\n")),
			wantErr: "recorded as merged"},
		{name: "landed only by human verdict: blocked (H2)", env: witness, tip: f.good,
			queue: mqWith(mrIssue("mr1", "closed", branch, f.good, "landing: landed\n")), wantErr: "git has not proven"},
		{name: "git-proven landed MR whose commit is the tip: allowed", env: witness, tip: f.good,
			queue: mqWithProver(gitLanded{f.good: true}, mrIssue("mr1", "closed", branch, f.good, ""))},
		{name: "git-proven landed MR with later commits on the branch: blocked", env: witness, tip: f.bad,
			queue: mqWithProver(gitLanded{f.good: true}, mrIssue("mr1", "closed", branch, f.good, "")), wantErr: "commits after the MR"},
		{name: "closed by hand, git proves not landed: blocked (no official verdict)", env: witness, tip: f.good,
			queue: mqWithProver(gitLanded{}, mrIssue("mr1", "closed", branch, f.good, "")), wantErr: "without an official verdict"},
		{name: "mayor deletes unlanded branch with open MR: blocked", env: []string{"GT_ROLE", "mayor"}, tip: f.good,
			queue: mqWith(mrIssue("mr1", "open", branch, f.good, "")), wantErr: "is open"},
		{name: "refinery deletes branch landed on main: allowed", env: []string{"GT_ROLE", "myrig/refinery"}, tip: f.mainSHA, queue: unread},
		{name: "refinery deletes unlanded branch with open MR: blocked", env: []string{"GT_ROLE", "myrig/refinery"}, tip: f.good,
			queue: mqWith(mrIssue("mr1", "open", branch, f.good, "")), wantErr: "is open"},
		{name: "crew deletes anything: allowed", env: []string{"GT_ROLE", "myrig/crew/dom"}, tip: f.good, queue: unread},
		{name: "human deletes anything: allowed", tip: f.good, queue: unread},
		{name: "tip unknown locally, no MR: blocked", env: witness,
			tip: "1234567890123456789012345678901234567890", queue: mqWith(), wantErr: "no MR recorded this tip"},
		{name: "tip unknown locally, beads unreadable: blocked", env: witness,
			tip: "1234567890123456789012345678901234567890", queue: unread, wantErr: "cannot read"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := coordinatorGuard(f, plain, tt.queue, tt.env...)
			err := g.check(del(tt.tip))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("want allowed, got: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want rejection containing %q, got %v", tt.wantErr, err)
			}
			for _, must := range []string{"BLOCKED", branch, "gt mq post-merge", "gt polecat prune"} {
				if !strings.Contains(err.Error(), must) {
					t.Fatalf("refusal must mention %q: %v", must, err)
				}
			}
			if strings.Contains(err.Error(), f.remote) {
				t.Fatalf("error leaks the push URL: %v", err)
			}
		})
	}
}

// The git proof comes first: a landed branch is deleted without reading beads.
func TestPrePushGuardPolecatDeletionGitProofSkipsBeads(t *testing.T) {
	f := newPrePushFixture(t)
	calls := 0
	g := coordinatorGuard(f, &prePushRig{name: "myrig", defaultBranch: "main"}, func() *beads.MergeQueue {
		calls++
		return beads.NewMergeQueue(nil, errors.New("must not be read"), nil)
	}, "GT_ROLE", "myrig/witness")
	if err := g.check([]prePushUpdate{prePushUpd("refs/heads/polecat/a", testZeroSHA, f.mainSHA)}); err != nil || calls != 0 {
		t.Fatalf("err=%v calls=%d; a landed tip must pass on the git proof alone", err, calls)
	}
}

func TestPrePushGuardTargetUpdateRoles(t *testing.T) {
	f := newPrePushFixture(t)
	plain := &prePushRig{name: "myrig", defaultBranch: "main"}
	direct := &prePushRig{name: "myrig", defaultBranch: "main", mergeQueueOff: true}
	main := prePushUpd("refs/heads/main", f.good, f.mainSHA)
	tests := []struct {
		name    string
		rig     *prePushRig
		env     []string
		update  prePushUpdate
		wantErr string
	}{
		{name: "mayor push to main: blocked", rig: plain, env: []string{"GT_ROLE", "mayor"}, update: main, wantErr: "agent role mayor"},
		{name: "witness push to main: blocked", rig: plain, env: []string{"GT_ROLE", "myrig/witness"}, update: main, wantErr: "agent role witness"},
		{name: "deacon push to main: blocked", rig: plain, env: []string{"GT_ROLE", "deacon"}, update: main, wantErr: "agent role deacon"},
		{name: "dog push to main: blocked", rig: plain, env: []string{"GT_ROLE", "dog"}, update: main, wantErr: "agent role dog"},
		{name: "boot push to main: blocked", rig: plain, env: []string{"GT_ROLE", "deacon/boot"}, update: main, wantErr: "agent role boot"},
		{name: "unknown agent push to main: blocked", rig: plain, env: []string{"BD_ACTOR", "daemon"}, update: main, wantErr: "agent role daemon"},
		{name: "mayor via BD_ACTOR only: blocked", rig: plain, env: []string{"BD_ACTOR", "mayor"}, update: main, wantErr: "BD_ACTOR"},
		{name: "polecat push to main with merge queue: blocked", rig: plain, env: []string{"GT_ROLE", "myrig/polecats/nux"}, update: main, wantErr: "agent role polecat"},
		{name: "polecat push to main, merge queue disabled (direct): allowed", rig: direct, env: []string{"GT_ROLE", "myrig/polecats/nux"}, update: main},
		{name: "polecat push to main outside a rig: blocked", rig: nil, env: []string{"GT_ROLE", "myrig/polecats/nux"}, update: main, wantErr: "agent role polecat"},
		{name: "mayor update of integration/*: blocked", rig: plain, env: []string{"GT_ROLE", "mayor"},
			update: prePushUpd("refs/heads/integration/epic-a", f.integrationOnTopBad, f.epicSHA), wantErr: "agent role mayor"},
		{name: "mayor creates integration/* at main (gt mq integration create): allowed", rig: plain, env: []string{"GT_ROLE", "mayor"},
			update: prePushUpd("refs/heads/integration/epic-b", f.mainSHA, testZeroSHA)},
		{name: "mayor creates integration/* on another integration tip: allowed", rig: plain, env: []string{"GT_ROLE", "mayor"},
			update: prePushUpd("refs/heads/integration/epic-b", f.epicSHA, testZeroSHA)},
		{name: "mayor creates integration/* with new commits: blocked", rig: plain, env: []string{"GT_ROLE", "mayor"},
			update: prePushUpd("refs/heads/integration/epic-b", f.good, testZeroSHA), wantErr: "agent role mayor"},
		// Rule 2(c): the refinery pushes only gt mq merge's merge shape
		// (TestPrePushGuardRefineryMergeShape); a plain commit is refused.
		{name: "refinery push of a plain commit to main: blocked (2(c))", rig: plain, env: []string{"GT_ROLE", "myrig/refinery"}, update: main, wantErr: "1 parent(s)"},
		{name: "crew push to main: unchanged (allowed)", rig: plain, env: []string{"GT_ROLE", "myrig/crew/dom"}, update: main},
		{name: "human push to main: unchanged (allowed)", rig: plain, update: main},
		{name: "overseer push to main: allowed", rig: plain, env: []string{"BD_ACTOR", "overseer"}, update: main},
		{name: "witness pushes a polecat branch: allowed", rig: plain, env: []string{"GT_ROLE", "myrig/witness"},
			update: prePushUpd("refs/heads/polecat/nux", f.good, testZeroSHA)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newGuard(f, tt.rig, tt.env...)
			if tt.rig == nil {
				g.isManaged = func() bool { return false }
			}
			err := g.check([]prePushUpdate{tt.update})
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("want allowed, got: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want rejection containing %q, got %v", tt.wantErr, err)
			}
			if !strings.Contains(err.Error(), "refinery") {
				t.Fatalf("refusal must say who lands work: %v", err)
			}
		})
	}
}

func TestResolvePrePushRigMergeQueueOff(t *testing.T) {
	town := t.TempDir()
	rigPath := town + "/myrig"
	writeMQScopeRigSettings(t, rigPath, map[string]any{"enabled": false})
	writeRigConfigForPrePush(t, rigPath)
	r, err := resolvePrePushRig(town, rigPath+"/polecats/nux")
	if err != nil || r == nil || !r.mergeQueueOff {
		t.Fatalf("merge_queue.enabled=false must be seen: r=%+v err=%v", r, err)
	}
	writeMQScopeRigSettings(t, rigPath, map[string]any{})
	if r, _ := resolvePrePushRig(town, rigPath+"/polecats/nux"); r == nil || r.mergeQueueOff {
		t.Fatalf("default is an active merge queue: %+v", r)
	}
}

func writeRigConfigForPrePush(t *testing.T, rigPath string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(rigPath, "config.json"), []byte(`{"type":"rig","default_branch":"main"}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The command path wires the town merge queue: outside a town an agent
// deletion of an unlanded polecat branch is refused; a landed one passes.
func TestRunHooksPrePushPolecatDeletionOutsideTown(t *testing.T) {
	f := newPrePushFixture(t)
	t.Chdir(f.repo)
	setCaller(t, "GT_ROLE", "myrig/witness")
	run := func(stdin string) error {
		hooksPrePushCmd.SetIn(strings.NewReader(stdin))
		defer hooksPrePushCmd.SetIn(nil)
		return runHooksPrePush(hooksPrePushCmd, []string{"origin", f.remote})
	}
	err := run("(delete) " + testZeroSHA + " refs/heads/polecat/nux " + f.good + "\n")
	if err == nil || !strings.Contains(err.Error(), "no Gas Town workspace") {
		t.Fatalf("unlanded polecat deletion outside a town must be refused, got %v", err)
	}
	if err := run("(delete) " + testZeroSHA + " refs/heads/polecat/nux " + f.mainSHA + "\n"); err != nil {
		t.Fatalf("landed polecat deletion: %v", err)
	}
	setCaller(t, "GT_ROLE", "myrig/crew/dom")
	if err := run("(delete) " + testZeroSHA + " refs/heads/polecat/nux " + f.good + "\n"); err != nil {
		t.Fatalf("crew deletion: %v", err)
	}
}

// archiveTip pushes commit to an archive ref for branch on the fixture's
// remote (what gt polecat abandon does) and returns commit.
func archiveTip(t *testing.T, f prePushFixture, branch, commit string) string {
	t.Helper()
	runGitForMQSubmitTest(t, f.repo, "push", "-q", "origin", commit+":refs/gt/archive/"+branch+"/1760000000-"+commit[:12])
	return commit
}

// gt-pd1c.78 tahap 2: the archive namespace. Agents create archives only for
// abandonable work (MR closed as rejected or superseded) and never delete or
// rewrite them; humans and crew are not checked.
func TestPrePushGuardArchiveRefs(t *testing.T) {
	f := newPrePushFixture(t)
	plain := &prePushRig{name: "myrig", defaultBranch: "main"}
	const branch = "polecat/nux/gt-1+mvcf8o91"
	ref := "refs/gt/archive/" + branch + "/1760000000-" + f.good[:12]
	create := prePushUpd(ref, f.good, testZeroSHA)
	mayor := []string{"GT_ROLE", "mayor"}
	unread := func() *beads.MergeQueue { return beads.NewMergeQueue(nil, errors.New("dolt down"), nil) }
	tests := []struct {
		name    string
		env     []string
		update  prePushUpdate
		queue   func() *beads.MergeQueue
		wantErr string
	}{
		{name: "mayor archives rejected work: allowed", env: mayor, update: create,
			queue: mqWithProver(gitLanded{}, mrIssue("mr1", "closed", branch, f.good, "close_reason: rejected\n"))},
		{name: "mayor archives superseded work: allowed", env: mayor, update: create,
			queue: mqWithProver(gitLanded{}, mrIssue("mr1", "closed", branch, f.good, "close_reason: superseded\n"))},
		{name: "mayor archives conflicted work: blocked", env: mayor, update: create,
			queue: mqWithProver(gitLanded{}, mrIssue("mr1", "closed", branch, f.good, "close_reason: conflict\n")), wantErr: "closed for conflicts"},
		{name: "mayor archives work without an MR: blocked", env: mayor, update: create, queue: mqWith(), wantErr: "no MR recorded"},
		{name: "mayor archives work closed without verdict: blocked", env: mayor, update: create,
			queue: mqWithProver(gitLanded{}, mrIssue("mr1", "closed", branch, f.good, "")), wantErr: "without an official verdict"},
		{name: "mayor archives work with an open MR: blocked", env: mayor, update: create,
			queue: mqWith(mrIssue("mr1", "open", branch, f.good, "")), wantErr: "is open"},
		{name: "mayor archives unverified work: blocked", env: mayor, update: create,
			queue: mqWith(mrIssue("mr1", "closed", branch, "", "")), wantErr: "cannot tell"},
		{name: "unreadable queue: blocked", env: mayor, update: create, queue: unread, wantErr: "cannot read the merge queue"},
		{name: "no queue loader: blocked", env: mayor, update: create, wantErr: "not available"},
		{name: "stamp names another commit: blocked", env: mayor,
			update: prePushUpd("refs/gt/archive/"+branch+"/1760000000-"+f.bad[:12], f.good, testZeroSHA),
			queue:  mqWithProver(gitLanded{}, mrIssue("mr1", "closed", branch, f.good, "close_reason: rejected\n")), wantErr: "is not an archive ref"},
		{name: "malformed archive ref: blocked", env: mayor, update: prePushUpd("refs/gt/archive/x", f.good, testZeroSHA),
			queue: mqWith(), wantErr: "is not an archive ref"},
		{name: "agent deletes an archive: blocked", env: mayor, update: prePushUpd(ref, testZeroSHA, f.good),
			queue: mqWith(), wantErr: "may not delete archive ref"},
		{name: "witness rewrites an archive: blocked", env: []string{"GT_ROLE", "myrig/witness"},
			update: prePushUpd(ref, f.bad, f.good), queue: mqWith(), wantErr: "may not rewrite archive ref"},
		{name: "refinery deletes an archive: blocked", env: []string{"GT_ROLE", "myrig/refinery"},
			update: prePushUpd(ref, testZeroSHA, f.good), queue: mqWith(), wantErr: "may not delete archive ref"},
		{name: "human prunes an archive: allowed", update: prePushUpd(ref, testZeroSHA, f.good), queue: unread},
		{name: "crew prunes an archive: allowed", env: []string{"GT_ROLE", "myrig/crew/dom"},
			update: prePushUpd(ref, testZeroSHA, f.good), queue: unread},
		{name: "human creates any archive: allowed", update: create, queue: unread},
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
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) || !strings.Contains(err.Error(), "BLOCKED") {
				t.Fatalf("want rejection containing %q, got %v", tt.wantErr, err)
			}
			if strings.Contains(err.Error(), f.remote) {
				t.Fatalf("error leaks the push URL: %v", err)
			}
		})
	}
}

// The full abandon push shape: archive created, then the branch deleted, in
// one push or two. The deletion passes on the archive alone.
func TestPrePushGuardAbandonSequence(t *testing.T) {
	f := newPrePushFixture(t)
	plain := &prePushRig{name: "myrig", defaultBranch: "main"}
	const branch = "polecat/nux/gt-1+mvcf8o91"
	rejected := mqWithProver(gitLanded{}, mrIssue("mr1", "closed", branch, f.good, "close_reason: rejected\n"))
	ref := "refs/gt/archive/" + branch + "/1760000000-" + f.good[:12]
	mayor := []string{"GT_ROLE", "mayor"}
	if err := coordinatorGuard(f, plain, rejected, mayor...).check([]prePushUpdate{prePushUpd(ref, f.good, testZeroSHA)}); err != nil {
		t.Fatalf("archive push: %v", err)
	}
	del := []prePushUpdate{prePushUpd("refs/heads/"+branch, testZeroSHA, f.good)}
	if err := coordinatorGuard(f, plain, rejected, mayor...).check(del); err == nil {
		t.Fatal("before the archive exists on the remote the delete must refuse")
	}
	runGitForMQSubmitTest(t, f.repo, "push", "-q", "origin", f.good+":"+ref)
	if err := coordinatorGuard(f, plain, rejected, mayor...).check(del); err != nil {
		t.Fatalf("delete after archive: %v", err)
	}
}

// gt-pd1c.78 S-H1: a branch an agent named "refs/heads/integration/pwn" is
// not a merge target, so a tip only there is not "covered".
func TestPrePushGuardFakeIntegrationBranchIsNotATarget(t *testing.T) {
	f := newPrePushFixture(t)
	plain := &prePushRig{name: "myrig", defaultBranch: "main"}
	runGitForMQSubmitTest(t, f.repo, "push", "-q", "origin", f.good+":refs/heads/refs/heads/integration/pwn")
	runGitForMQSubmitTest(t, f.repo, "push", "-q", "origin", f.good+":refs/heads/refs/heads/main")
	del := []prePushUpdate{prePushUpd("refs/heads/polecat/nux/gt-1+mvcf8o91", testZeroSHA, f.good)}
	err := coordinatorGuard(f, plain, mqWith(), "GT_ROLE", "myrig/witness").check(del)
	if err == nil || !strings.Contains(err.Error(), "no MR recorded this tip") {
		t.Fatalf("tip only on a fake integration branch must not count as covered: %v", err)
	}
	// Creating integration/* with new work cannot launder itself the same way.
	create := []prePushUpdate{prePushUpd("refs/heads/integration/epic-z", f.good, testZeroSHA)}
	if err := coordinatorGuard(f, plain, mqWith(), "GT_ROLE", "mayor").check(create); err == nil {
		t.Fatal("integration/* with new work covered only by a fake integration branch must be refused")
	}
}
