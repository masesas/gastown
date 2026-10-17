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

func mqWith(mrs ...*beads.Issue) func() *beads.MergeQueue {
	return func() *beads.MergeQueue {
		idx := beads.MergeQueueIndex{}
		for _, mr := range mrs {
			src := beads.ParseMRFields(mr).SourceIssue
			idx[src] = append(idx[src], mr)
		}
		return beads.NewMergeQueue(idx, nil, nil)
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
	plain := &prePushRig{name: "myrig", defaultBranch: "main"}
	const branch = "polecat/nux/gt-1"
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
		{name: "witness deletes branch proven not landed, no open MR: allowed", env: witness, tip: f.good,
			queue: mqWith(mrIssue("mr1", "closed", branch, f.good, "close_reason: rejected\nlanding: not_landed\n"))},
		{name: "witness deletes never-submitted branch: allowed", env: witness, tip: f.good, queue: mqWith()},
		{name: "witness deletes unverified branch: blocked", env: witness, tip: f.good,
			queue: mqWith(mrIssue("mr1", "closed", branch, f.good, "")), wantErr: "cannot tell"},
		{name: "beads unreadable: blocked", env: witness, tip: f.good, queue: unread, wantErr: "cannot read the merge queue"},
		{name: "no queue loader: blocked", env: witness, tip: f.good, wantErr: "not available"},
		{name: "landed MR whose commit is the tip: allowed", env: witness, tip: f.good,
			queue: mqWith(mrIssue("mr1", "closed", branch, f.good, "close_reason: merged\n"))},
		{name: "landed MR with later commits on the branch: blocked", env: witness, tip: f.bad,
			queue: mqWith(mrIssue("mr1", "closed", branch, f.good, "close_reason: merged\n")), wantErr: "commits after the MR"},
		{name: "mayor deletes unlanded branch with open MR: blocked", env: []string{"GT_ROLE", "mayor"}, tip: f.good,
			queue: mqWith(mrIssue("mr1", "open", branch, f.good, "")), wantErr: "is open"},
		{name: "refinery deletes branch landed on main: allowed", env: []string{"GT_ROLE", "myrig/refinery"}, tip: f.mainSHA, queue: unread},
		{name: "refinery deletes unlanded branch with open MR: blocked", env: []string{"GT_ROLE", "myrig/refinery"}, tip: f.good,
			queue: mqWith(mrIssue("mr1", "open", branch, f.good, "")), wantErr: "is open"},
		{name: "crew deletes anything: allowed", env: []string{"GT_ROLE", "myrig/crew/dom"}, tip: f.good, queue: unread},
		{name: "human deletes anything: allowed", tip: f.good, queue: unread},
		{name: "tip unknown locally, beads say not landed: allowed", env: witness,
			tip: "1234567890123456789012345678901234567890", queue: mqWith()},
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
		{name: "refinery push to main: unchanged (allowed)", rig: plain, env: []string{"GT_ROLE", "myrig/refinery"}, update: main},
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
