package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
)

// refineryMergeFixture adds to prePushFixture the shapes rule 2(c) judges:
// merge = main --no-ff good (what gt mq merge builds for an MR at f.good),
// amended = the same merge with an extra file, octopus = main + good + other.
type refineryMergeFixture struct {
	prePushFixture
	merge, amended, octopus string
}

func newRefineryMergeFixture(t *testing.T) refineryMergeFixture {
	t.Helper()
	f := refineryMergeFixture{prePushFixture: newPrePushFixture(t)}
	g := func(args ...string) string {
		t.Helper()
		return strings.TrimSpace(runGitForMQSubmitTest(t, f.repo, args...))
	}
	g("checkout", "-q", "-b", "other", "main")
	writeMQScopeFile(t, f.repo, "docs/other.md", "other\n")
	g("add", "-A")
	g("commit", "-q", "-m", "other")
	other := g("rev-parse", "HEAD")

	g("checkout", "-q", "-b", "mr-merge", "main")
	g("merge", "-q", "--no-ff", "-m", "Merge polecat work", f.good)
	f.merge = g("rev-parse", "HEAD")
	writeMQScopeFile(t, f.repo, "docs/smuggled.md", "not in the MR\n")
	g("add", "-A")
	g("commit", "-q", "--amend", "-m", "Merge polecat work")
	f.amended = g("rev-parse", "HEAD")

	g("checkout", "-q", "-b", "mr-octopus", "main")
	g("merge", "-q", "--no-ff", "-m", "octopus", f.good, other)
	f.octopus = g("rev-parse", "HEAD")
	g("checkout", "-q", "main")
	return f
}

// Rule 2(c): a refinery push to a merge target must be the merge gt mq merge
// pushes: two parents, the first the remote tip, the second the commit_sha
// of an open MR for that target, the tree the clean merge of the parents.
func TestPrePushGuardRefineryMergeShape(t *testing.T) {
	f := newRefineryMergeFixture(t)
	plain := &prePushRig{name: "myrig", defaultBranch: "main"}
	autoLandOn := &prePushRig{name: "myrig", defaultBranch: "main", autoLand: autoLandSetting{enabled: true}}
	refinery := []string{"GT_ROLE", "myrig/refinery", "BD_ACTOR", "myrig/refinery"}
	// gt done and gt mq submit always record the rig (rig: <rig>).
	openMR := mqWith(mrIssue("mr1", "open", "polecat/nux/gt-1+mvcf8o91", f.good, "rig: myrig\n"))
	closedMR := mqWith(mrIssue("mr1", "closed", "polecat/nux/gt-1+mvcf8o91", f.good, "rig: myrig\nclose_reason: merged\n"))
	noRigMR := mqWith(mrIssue("mr1", "open", "polecat/nux/gt-1+mvcf8o91", f.good, ""))
	otherRigMR := mqWith(mrIssue("mr1", "open", "polecat/nux/gt-1+mvcf8o91", f.good, "rig: otherrig\n"))
	otherTarget := mqWith(&beads.Issue{ID: "mr1", Status: "open",
		Description: "branch: polecat/nux/gt-1\ntarget: integration/epic-a\nsource_issue: gt-1\nrig: myrig\ncommit_sha: " + f.good})
	unread := func() *beads.MergeQueue { return beads.NewMergeQueue(nil, errors.New("dolt down"), nil) }
	// gt mq merge pushes by SHA ("<sha>:refs/heads/main"), so git names the
	// local side by the object name.
	bySHA := func(local, remoteRef, remote string) prePushUpdate {
		return prePushUpdate{LocalRef: local, LocalSHA: local, RemoteRef: remoteRef, RemoteSHA: remote}
	}
	tests := []struct {
		name    string
		rig     *prePushRig
		env     []string
		queue   func() *beads.MergeQueue
		update  prePushUpdate
		wantErr string // "" = allowed
	}{
		{name: "gt mq merge's push (merge of an open MR's commit_sha, by SHA): allowed", rig: plain, env: refinery, queue: openMR,
			update: bySHA(f.merge, "refs/heads/main", f.mainSHA)},
		{name: "fast-forward to the MR commit: refused", rig: plain, env: refinery, queue: openMR,
			update: prePushUpd("refs/heads/main", f.good, f.mainSHA), wantErr: "1 parent(s)"},
		{name: "merge whose second parent is no open MR: refused", rig: plain, env: refinery, queue: mqWith(),
			update: bySHA(f.merge, "refs/heads/main", f.mainSHA), wantErr: "not the commit_sha of an open merge request"},
		{name: "merge of a closed MR's commit: refused", rig: plain, env: refinery, queue: closedMR,
			update: bySHA(f.merge, "refs/heads/main", f.mainSHA), wantErr: "not the commit_sha of an open merge request"},
		{name: "merge of an MR for another target: refused", rig: plain, env: refinery, queue: otherTarget,
			update: bySHA(f.merge, "refs/heads/main", f.mainSHA), wantErr: "not the commit_sha of an open merge request"},
		{name: "open MR that records no rig: refused", rig: plain, env: refinery, queue: noRigMR,
			update: bySHA(f.merge, "refs/heads/main", f.mainSHA), wantErr: "not the commit_sha of an open merge request"},
		{name: "open MR of another rig: refused", rig: plain, env: refinery, queue: otherRigMR,
			update: bySHA(f.merge, "refs/heads/main", f.mainSHA), wantErr: "not the commit_sha of an open merge request"},
		{name: "tree differs from the clean merge (amended merge): refused", rig: plain, env: refinery, queue: openMR,
			update: bySHA(f.amended, "refs/heads/main", f.mainSHA), wantErr: "differs from the clean merge"},
		{name: "three parents (octopus): refused", rig: plain, env: refinery, queue: openMR,
			update: bySHA(f.octopus, "refs/heads/main", f.mainSHA), wantErr: "3 parent(s)"},
		{name: "unreadable merge queue: refused", rig: plain, env: refinery, queue: unread,
			update: bySHA(f.merge, "refs/heads/main", f.mainSHA), wantErr: "fail closed"},
		{name: "no queue loader: refused", rig: plain, env: refinery,
			update: bySHA(f.merge, "refs/heads/main", f.mainSHA), wantErr: "not available"},
		{name: "refinery creating the default branch: refused", rig: &prePushRig{name: "myrig", defaultBranch: "trunk"}, env: refinery, queue: openMR,
			update: bySHA(f.merge, "refs/heads/trunk", testZeroSHA), wantErr: "does not create merge targets"},
		{name: "refinery creating integration/* with no new commit: allowed", rig: plain, env: refinery, queue: openMR,
			update: prePushUpd("refs/heads/integration/epic-b", f.mainSHA, testZeroSHA)},
		{name: "gt mq integration land (auto-land on): allowed", rig: autoLandOn, env: append([]string{"GT_INTEGRATION_LAND", "1"}, refinery...),
			queue: mqWith(), update: prePushUpd("refs/heads/main", f.land, f.mainSHA)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := coordinatorGuard(f.prePushFixture, tt.rig, tt.queue, tt.env...).check([]prePushUpdate{tt.update})
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("want allowed, got: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want rejection containing %q, got %v", tt.wantErr, err)
			}
			if !strings.Contains(err.Error(), "gt mq merge") {
				t.Fatalf("refusal must point at gt mq merge: %v", err)
			}
		})
	}

	// Callers other than the refinery of a detected rig keep their rules.
	for name, tc := range map[string]struct {
		rig *prePushRig
		env []string
	}{
		"crew":                          {plain, []string{"GT_ROLE", "myrig/crew/dom"}},
		"human":                         {plain, nil},
		"refinery in an unmanaged repo": {nil, refinery},
	} {
		g := coordinatorGuard(f.prePushFixture, tc.rig, nil, tc.env...)
		g.isManaged = func() bool { return false }
		if err := g.check([]prePushUpdate{prePushUpd("refs/heads/main", f.good, f.mainSHA)}); err != nil {
			t.Fatalf("%s: fast-forward push refused: %v", name, err)
		}
	}
}

// Security review H1: `git replace <evil> <clean>` must not make an evil merge
// look clean. The push sends the original object, so the hook reads the
// pushed tree like the proofs (replace refs ignored).
func TestPrePushGuardRefineryMergeReplaceRefCannotForgeTheTree(t *testing.T) {
	f := newRefineryMergeFixture(t)
	plain := &prePushRig{name: "myrig", defaultBranch: "main"}
	refinery := []string{"GT_ROLE", "myrig/refinery", "BD_ACTOR", "myrig/refinery"}
	openMR := mqWith(mrIssue("mr1", "open", "polecat/nux/gt-1+mvcf8o91", f.good, "rig: myrig\n"))
	// f.amended: parents (main, good) with an extra file; f.merge: the clean merge.
	runGitForMQSubmitTest(t, f.repo, "replace", f.amended, f.merge)
	err := coordinatorGuard(f.prePushFixture, plain, openMR, refinery...).check([]prePushUpdate{
		{LocalRef: f.amended, LocalSHA: f.amended, RemoteRef: "refs/heads/main", RemoteSHA: f.mainSHA}})
	if err == nil || !strings.Contains(err.Error(), "differs from the clean merge") {
		t.Fatalf("replace-forged merge: want a tree refusal, got %v", err)
	}
}
