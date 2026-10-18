package refinery

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	beadsdk "github.com/steveyegge/beads"
	"github.com/steveyegge/gastown/internal/beads"
	gitpkg "github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/rig"
)

// memStore is an in-memory beads store for the MergeOne tests: every bead
// read and write of the engine goes through it, and every close is recorded
// (closes), so a test sees each close reason the Go refinery path writes.
// Unimplemented Storage methods panic (nil embedded interface): an
// unexpected bead operation fails the test loudly.
type memStore struct {
	beadsdk.Storage
	mu     sync.Mutex
	issues map[string]*beadsdk.Issue
	deps   []*beadsdk.Dependency
	closes []closeCall
	nextID int
}

type closeCall struct{ ID, Reason string }

func newMemStore(issues ...*beadsdk.Issue) *memStore {
	s := &memStore{issues: map[string]*beadsdk.Issue{}}
	for _, is := range issues {
		s.issues[is.ID] = is
	}
	return s
}

func (s *memStore) GetIssue(_ context.Context, id string) (*beadsdk.Issue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	is, ok := s.issues[id]
	if !ok {
		return nil, fmt.Errorf("%w: issue %s", beadsdk.ErrNotFound, id)
	}
	cp := *is
	cp.Labels = append([]string(nil), is.Labels...)
	return &cp, nil
}

func (s *memStore) GetIssuesByIDs(ctx context.Context, ids []string) ([]*beadsdk.Issue, error) {
	var out []*beadsdk.Issue
	for _, id := range ids {
		if is, err := s.GetIssue(ctx, id); err == nil {
			out = append(out, is)
		}
	}
	return out, nil
}

func (s *memStore) GetLabels(_ context.Context, id string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if is, ok := s.issues[id]; ok {
		return append([]string(nil), is.Labels...), nil
	}
	return nil, fmt.Errorf("issue %s not found", id)
}

func (s *memStore) GetDependenciesWithMetadata(_ context.Context, id string) ([]*beadsdk.IssueWithDependencyMetadata, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*beadsdk.IssueWithDependencyMetadata
	for _, d := range s.deps {
		if d.IssueID != id {
			continue
		}
		if target, ok := s.issues[d.DependsOnID]; ok {
			out = append(out, &beadsdk.IssueWithDependencyMetadata{Issue: *target, DependencyType: d.Type})
		}
	}
	return out, nil
}

func (s *memStore) UpdateIssue(_ context.Context, id string, updates map[string]interface{}, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	is, ok := s.issues[id]
	if !ok {
		return fmt.Errorf("issue %s not found", id)
	}
	for k, v := range updates {
		str, _ := v.(string)
		switch k {
		case "description":
			is.Description = str
		case "status":
			is.Status = beadsdk.Status(str)
		case "assignee":
			is.Assignee = str
		case "title":
			is.Title = str
		}
	}
	is.UpdatedAt = time.Now()
	return nil
}

func (s *memStore) CloseIssue(_ context.Context, id, reason, _, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	is, ok := s.issues[id]
	if !ok {
		return fmt.Errorf("issue %s not found", id)
	}
	now := time.Now()
	is.Status = beadsdk.StatusClosed
	is.CloseReason = reason
	is.ClosedAt = &now
	s.closes = append(s.closes, closeCall{ID: id, Reason: reason})
	return nil
}

func (s *memStore) SearchIssues(_ context.Context, _ string, filter beadsdk.IssueFilter) ([]*beadsdk.Issue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*beadsdk.Issue
	for _, is := range s.issues {
		if filter.Status != nil && is.Status != *filter.Status {
			continue
		}
		if !hasAllLabels(is.Labels, filter.Labels) {
			continue
		}
		cp := *is
		out = append(out, &cp)
	}
	return out, nil
}

func (s *memStore) CreateIssue(_ context.Context, is *beadsdk.Issue, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if is.ID == "" {
		s.nextID++
		is.ID = fmt.Sprintf("gt-task%d", s.nextID)
	}
	if is.Status == "" {
		is.Status = beadsdk.StatusOpen
	}
	if is.IssueType == "" {
		is.IssueType = beadsdk.IssueType("task")
	}
	cp := *is
	s.issues[is.ID] = &cp
	return nil
}

func (s *memStore) AddDependency(_ context.Context, dep *beadsdk.Dependency, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deps = append(s.deps, dep)
	return nil
}

func (s *memStore) issue(t *testing.T, id string) *beadsdk.Issue {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	is, ok := s.issues[id]
	if !ok {
		t.Fatalf("issue %s missing", id)
	}
	cp := *is
	return &cp
}

func (s *memStore) closeCalls() []closeCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]closeCall(nil), s.closes...)
}

func hasAllLabels(have, want []string) bool {
	for _, w := range want {
		found := false
		for _, h := range have {
			if h == w {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func memIssue(id, desc string, labels ...string) *beadsdk.Issue {
	now := time.Now()
	return &beadsdk.Issue{ID: id, Title: id, Description: desc, Status: beadsdk.StatusOpen,
		IssueType: beadsdk.IssueType("task"), Priority: 2, CreatedAt: now, UpdatedAt: now, Labels: labels}
}

func memMRIssue(id, branch, target, source, commit string) *beadsdk.Issue {
	desc := beads.FormatMRFields(&beads.MRFields{
		Branch: branch, Target: target, SourceIssue: source, Worker: "polecats/nux", Rig: "testrig", CommitSHA: commit,
	})
	return memIssue(id, desc, "gt:merge-request")
}

// installFailingBD puts a `bd` stub on PATH that always fails, so the few
// bead reads that still shell out (the wisp side of ListMergeRequests) never
// reach a real database.
func installFailingBD(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bd"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// mergeOneFixture is a minimal town (mayor/town.json, routes gt- -> testrig)
// with rig testrig whose refinery worktree (<rig>/refinery/rig) clones a bare
// origin, and one MR (gt-mr) for polecat/nux/gt-src submitted at commit.
type mergeOneFixture struct {
	workDir  string
	rigPath  string
	rigBeads string
	git      *gitpkg.Git
	store    *memStore
	sender   *fakeMailSender
	gtLog    string
	out      *bytes.Buffer
	branch   string
	commit   string
}

const (
	fixtureMR     = "gt-mr"
	fixtureSource = "gt-src"
)

// newMergeOneFixture builds the fixture. The feature branch changes
// apps/feature.txt, is pushed to origin and deleted locally, as when only the
// polecat's worktree had it.
func newMergeOneFixture(t *testing.T) *mergeOneFixture {
	t.Helper()
	f := &mergeOneFixture{branch: "polecat/nux/gt-src"}
	f.gtLog = installFakeGT(t)
	installNoPRGH(t)
	installFailingBD(t)
	town := t.TempDir()
	f.rigPath = filepath.Join(town, "testrig")
	rigBeads := filepath.Join(f.rigPath, ".beads")
	for _, dir := range []string{filepath.Join(town, "mayor"), filepath.Join(town, ".beads"), rigBeads} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(town, "mayor"), "town.json", "{}")
	if err := beads.WriteRoutes(filepath.Join(town, ".beads"), []beads.Route{{Prefix: "gt-", Path: "testrig"}}); err != nil {
		t.Fatal(err)
	}
	f.rigBeads = rigBeads
	f.workDir, f.git = githubShapedRepo(t, town, filepath.Join(f.rigPath, "refinery", "rig"))
	if err := os.MkdirAll(filepath.Join(f.workDir, "apps"), 0o755); err != nil {
		t.Fatal(err)
	}
	createFeatureBranch(t, f.workDir, f.branch, "apps/feature.txt", "feature\n")
	f.commit = run(t, f.workDir, "git", "rev-parse", f.branch)
	run(t, f.workDir, "git", "push", "origin", f.branch)
	run(t, f.workDir, "git", "branch", "-D", f.branch)

	f.store = newMemStore(
		memIssue(fixtureSource, "Implement the feature"),
		memMRIssue(fixtureMR, f.branch, "main", fixtureSource, f.commit),
	)
	f.sender = &fakeMailSender{}
	f.out = &bytes.Buffer{}
	f.settings(t, nil)
	return f
}

// settings writes the rig's settings/config.json merge_queue: a passing test
// gate (the engine refuses to merge with no gates) plus extra keys.
func (f *mergeOneFixture) settings(t *testing.T, extra map[string]any) {
	t.Helper()
	mq := map[string]any{"test_command": "true"}
	for k, v := range extra {
		mq[k] = v
	}
	writeRigSettingsMQ(t, f.rigPath, mq)
}

// githubShapedRepo is testGitRepo with a bare origin whose local path ends in
// "x@github.com/example/repo.git": git treats it as a local path, while the
// PR lookup reads it as the GitHub repo example/repo (the fake gh answers "no
// PR"), so the remote branch delete runs without an upstream remote (which
// would make the rig fork-backed and refuse the direct push).
func githubShapedRepo(t *testing.T, tmp, workDir string) (string, *gitpkg.Git) {
	t.Helper()
	bare := filepath.Join(tmp, "x@github.com", "example", "repo.git")
	for _, dir := range []string{filepath.Dir(bare), filepath.Dir(workDir)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	run(t, tmp, "git", "init", "--bare", "--initial-branch=main", bare)
	run(t, tmp, "git", "clone", bare, workDir)
	run(t, workDir, "git", "config", "user.email", "test@test.com")
	run(t, workDir, "git", "config", "user.name", "Test")
	run(t, workDir, "git", "checkout", "-b", "main")
	writeFile(t, workDir, "README.md", "# Test\n")
	run(t, workDir, "git", "add", ".")
	run(t, workDir, "git", "commit", "-m", "initial commit")
	run(t, workDir, "git", "push", "-u", "origin", "main")
	return workDir, gitpkg.NewGit(workDir)
}

func (f *mergeOneFixture) engineer(t *testing.T) *Engineer {
	t.Helper()
	e := NewEngineer(&rig.Rig{Name: "testrig", Path: f.rigPath})
	e.git = f.git
	e.workDir = f.workDir
	e.beads = beads.NewWithBeadsDirAndStore(f.workDir, f.rigBeads, f.store)
	e.router = f.sender
	e.output = f.out
	e.mergeSlotEnsureExists = func() (string, error) { return "test-slot", nil }
	e.mergeSlotAcquire = func(holder string, _ bool) (*beads.MergeSlotStatus, error) {
		return &beads.MergeSlotStatus{Available: true, Holder: holder}, nil
	}
	e.mergeSlotRelease = func(string) error { return nil }
	return e
}

func (f *mergeOneFixture) originMain(t *testing.T) string {
	t.Helper()
	return strings.Fields(run(t, f.workDir, "git", "ls-remote", "origin", "refs/heads/main"))[0]
}

func (f *mergeOneFixture) mergeOne(t *testing.T) MergeOutcome {
	t.Helper()
	out := f.engineer(t).MergeOne(context.Background(), fixtureMR)
	t.Logf("outcome: %+v\nengine output:\n%s", out, f.out.String())
	return out
}

func (f *mergeOneFixture) assertOpenAndUnclaimed(t *testing.T) {
	t.Helper()
	mr := f.store.issue(t, fixtureMR)
	if mr.Status != beadsdk.StatusOpen {
		t.Fatalf("MR status = %s, want open", mr.Status)
	}
	if mr.Assignee != "" {
		t.Fatalf("MR assignee = %q, want the claim released", mr.Assignee)
	}
	if src := f.store.issue(t, fixtureSource); src.Status != beadsdk.StatusOpen {
		t.Fatalf("source status = %s, want open", src.Status)
	}
}

func TestMergeOne_HappyPathLandsClosesCleansAndMails(t *testing.T) {
	f := newMergeOneFixture(t)
	out := f.mergeOne(t)

	if out.Kind != OutcomeMerged || !out.Merged() {
		t.Fatalf("Kind = %s (%s), want merged", out.Kind, out.Message)
	}
	tip := f.originMain(t)
	if out.MergeCommit != tip {
		t.Fatalf("MergeCommit = %s, origin main = %s", out.MergeCommit, tip)
	}
	if second := run(t, f.workDir, "git", "rev-parse", tip+"^2"); second != f.commit {
		t.Fatalf("origin main %s second parent = %s, want submitted commit_sha %s", tip, second, f.commit)
	}
	mr := f.store.issue(t, fixtureMR)
	if mr.Status != beadsdk.StatusClosed || mr.CloseReason != string(CloseReasonMerged) {
		t.Fatalf("MR status/close reason = %s/%q, want closed/merged", mr.Status, mr.CloseReason)
	}
	if fields := beads.ParseMRFields(&beads.Issue{Description: mr.Description}); fields == nil || fields.MergeCommit != tip || fields.CloseReason != string(CloseReasonMerged) {
		t.Fatalf("MR fields = %+v, want merge_commit %s and close_reason merged", fields, tip)
	}
	src := f.store.issue(t, fixtureSource)
	if src.Status != beadsdk.StatusClosed || validateMergedCloseReason(src.CloseReason, fixtureMR) != nil {
		t.Fatalf("source status/close reason = %s/%q, want closed with \"Merged in %s\"", src.Status, src.CloseReason, fixtureMR)
	}
	if !beads.IsBlockerResolved("merge-blocks", "closed", src.CloseReason) {
		t.Fatalf("source close reason %q does not resolve merge-blocks dependents", src.CloseReason)
	}
	if out := run(t, f.workDir, "git", "ls-remote", "--heads", "origin", f.branch); out != "" {
		t.Fatalf("remote branch still exists: %q", out)
	}
	if ok, _ := f.git.BranchExists(f.branch); ok {
		t.Fatal("local branch still exists")
	}
	if out.BranchCleanup == nil || !out.BranchCleanup.RemoteDeleted || !out.BranchCleanup.LocalDeleted {
		t.Fatalf("BranchCleanup = %+v, want remote and local deleted", out.BranchCleanup)
	}
	msg := f.sender.find("MERGED")
	if msg == nil || msg.To != "testrig/witness" || !strings.Contains(msg.Body, "Merge-Commit: "+tip) {
		t.Fatalf("MERGED mail = %+v, want one to testrig/witness naming %s", msg, tip)
	}
	if !strings.Contains(readLog(t, f.gtLog), "nudge mayor/ MERGED: "+fixtureMR) {
		t.Errorf("mayor not nudged:\n%s", readLog(t, f.gtLog))
	}
}

func TestMergeOne_RefusesBlockedMR(t *testing.T) {
	f := newMergeOneFixture(t)
	f.store.issues["gt-blocker"] = memIssue("gt-blocker", "still open")
	f.store.deps = append(f.store.deps, &beadsdk.Dependency{IssueID: fixtureMR, DependsOnID: "gt-blocker", Type: beadsdk.DepBlocks})
	before := f.originMain(t)

	out := f.mergeOne(t)
	if out.Kind != OutcomeNotEligible || !strings.Contains(out.Message, "gt-blocker") {
		t.Fatalf("outcome = %s %q, want not_eligible naming gt-blocker", out.Kind, out.Message)
	}
	if f.originMain(t) != before {
		t.Fatal("origin main moved for a blocked MR")
	}
	f.assertOpenAndUnclaimed(t)
}

func TestMergeOne_RefusesMRClaimedByAnother(t *testing.T) {
	f := newMergeOneFixture(t)
	f.store.issues[fixtureMR].Assignee = "otherrig/refinery"
	out := f.mergeOne(t)
	if out.Kind != OutcomeNotEligible || !strings.Contains(out.Message, "otherrig/refinery") {
		t.Fatalf("outcome = %s %q, want not_eligible naming the other claimant", out.Kind, out.Message)
	}
	if got := f.store.issue(t, fixtureMR).Assignee; got != "otherrig/refinery" {
		t.Fatalf("assignee = %q: another claim was overwritten or released", got)
	}
}

func TestMergeOne_RefusesUntrustedTarget(t *testing.T) {
	f := newMergeOneFixture(t)
	f.store.issues[fixtureMR] = memMRIssue(fixtureMR, f.branch, "polecat/nux/other", fixtureSource, f.commit)
	out := f.mergeOne(t)
	if out.Kind != OutcomeNotEligible || !strings.Contains(out.Message, "not a merge target") {
		t.Fatalf("outcome = %s %q, want not_eligible (untrusted target)", out.Kind, out.Message)
	}
}

func TestMergeOne_RefusesStrayLocalTargetCommits(t *testing.T) {
	f := newMergeOneFixture(t)
	writeFile(t, f.workDir, "stray.txt", "never pushed\n")
	run(t, f.workDir, "git", "add", ".")
	run(t, f.workDir, "git", "commit", "-m", "stray local commit")
	before := f.originMain(t)

	out := f.mergeOne(t)
	if out.Kind != OutcomePrecondition || !strings.Contains(out.Message, "not on origin/main") {
		t.Fatalf("outcome = %s %q, want precondition (stray local commits)", out.Kind, out.Message)
	}
	if f.originMain(t) != before {
		t.Fatal("stray local commit was pushed")
	}
	f.assertOpenAndUnclaimed(t)
}

func TestMergeOne_RefusesDirtyTree(t *testing.T) {
	for name, dirty := range map[string]func(t *testing.T, f *mergeOneFixture){
		"modified file":  func(t *testing.T, f *mergeOneFixture) { writeFile(t, f.workDir, "README.md", "edited\n") },
		"untracked file": func(t *testing.T, f *mergeOneFixture) { writeFile(t, f.workDir, "planted.sh", "#!/bin/sh\n") },
	} {
		t.Run(name, func(t *testing.T) {
			f := newMergeOneFixture(t)
			dirty(t, f)
			before := f.originMain(t)
			out := f.mergeOne(t)
			if out.Kind != OutcomePrecondition || !strings.Contains(out.Message, "clean the tree") {
				t.Fatalf("outcome = %s %q, want precondition (dirty tree)", out.Kind, out.Message)
			}
			if f.originMain(t) != before {
				t.Fatal("origin main moved with a dirty tree")
			}
			f.assertOpenAndUnclaimed(t)
		})
	}
}

func TestMergeOne_RefusesUnfinishedMerge(t *testing.T) {
	f := newMergeOneFixture(t)
	gitDir := run(t, f.workDir, "git", "rev-parse", "--absolute-git-dir")
	writeFile(t, gitDir, "MERGE_HEAD", f.commit+"\n")
	out := f.mergeOne(t)
	if out.Kind != OutcomePrecondition || !strings.Contains(out.Message, "merge is in progress") {
		t.Fatalf("outcome = %s %q, want precondition (merge in progress)", out.Kind, out.Message)
	}
	f.assertOpenAndUnclaimed(t)
}

func TestMergeOne_RefusesAutoPushFalse(t *testing.T) {
	f := newMergeOneFixture(t)
	writeRigConfigMQ(t, f.rigPath, map[string]any{"auto_push": false})
	before := f.originMain(t)
	out := f.mergeOne(t)
	if out.Kind != OutcomePrecondition || !strings.Contains(out.Message, "auto_push") {
		t.Fatalf("outcome = %s %q, want precondition (auto_push=false)", out.Kind, out.Message)
	}
	if f.originMain(t) != before {
		t.Fatal("origin main moved with auto_push=false")
	}
	f.assertOpenAndUnclaimed(t)
}

func TestMergeOne_ConflictCreatesTaskAndReleasesClaim(t *testing.T) {
	f := newMergeOneFixture(t)
	// origin main changes the same file differently after the branch was cut.
	if err := os.MkdirAll(filepath.Join(f.workDir, "apps"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, f.workDir, "apps/feature.txt", "main's version\n")
	run(t, f.workDir, "git", "add", ".")
	run(t, f.workDir, "git", "commit", "-m", "conflicting main change")
	run(t, f.workDir, "git", "push", "origin", "main")
	before := f.originMain(t)

	out := f.mergeOne(t)
	if out.Kind != OutcomeConflict {
		t.Fatalf("outcome = %s %q, want conflict", out.Kind, out.Message)
	}
	if f.originMain(t) != before {
		t.Fatal("origin main moved on a conflict")
	}
	if out.ConflictTask == "" {
		t.Fatal("no conflict-resolution task reported")
	}
	task := f.store.issue(t, out.ConflictTask)
	if !strings.Contains(task.Description, "Original MR: "+fixtureMR) {
		t.Fatalf("conflict task %s description lacks the MR:\n%s", task.ID, task.Description)
	}
	blocked := false
	for _, d := range f.store.deps {
		if d.IssueID == fixtureMR && d.DependsOnID == out.ConflictTask {
			blocked = true
		}
	}
	if !blocked {
		t.Fatal("MR not blocked on its conflict task")
	}
	f.assertOpenAndUnclaimed(t)
	if f.sender.find("FIX_NEEDED") != nil {
		t.Fatal("a conflict sent FIX_NEEDED")
	}
}

func TestMergeOne_ScopeViolationLeavesOriginUnchanged(t *testing.T) {
	f := newMergeOneFixture(t)
	f.settings(t, map[string]any{"allowed_paths": []string{"docs/**"}})
	before := f.originMain(t)

	out := f.mergeOne(t)
	if out.Kind != OutcomeScopeViolation || !strings.Contains(out.Message, "apps/feature.txt") {
		t.Fatalf("outcome = %s %q, want scope_violation naming apps/feature.txt", out.Kind, out.Message)
	}
	if f.originMain(t) != before {
		t.Fatal("origin main moved on a scope violation")
	}
	if remote := run(t, f.workDir, "git", "ls-remote", "--heads", "origin", f.branch); !strings.Contains(remote, f.commit) {
		t.Fatalf("MR branch touched on a scope violation: %q", remote)
	}
	f.assertOpenAndUnclaimed(t)
	if msg := f.sender.find("FIX_NEEDED"); msg == nil || !strings.Contains(msg.Body, "Failure-Type: scope") || msg.To != "testrig/polecats/nux" {
		t.Fatalf("FIX_NEEDED = %+v, want a scope failure mailed to testrig/polecats/nux", msg)
	}
}

func TestMergeOne_FailedGateBlocksWithoutBaselineRerun(t *testing.T) {
	f := newMergeOneFixture(t)
	marker := filepath.Join(t.TempDir(), "runs")
	f.settings(t, map[string]any{"test_command": "echo run >> " + marker + "; exit 1"})
	before := f.originMain(t)

	out := f.mergeOne(t)
	if out.Kind != OutcomeGatesFailed {
		t.Fatalf("outcome = %s %q, want gates_failed", out.Kind, out.Message)
	}
	if f.originMain(t) != before {
		t.Fatal("origin main moved after a failed gate")
	}
	if runs := strings.Count(readLog(t, marker), "run"); runs != 1 {
		t.Fatalf("test gate ran %d times, want exactly once (no baseline re-run)", runs)
	}
	f.assertOpenAndUnclaimed(t)
	if msg := f.sender.find("FIX_NEEDED"); msg == nil || !strings.Contains(msg.Body, "Failure-Type: tests") {
		t.Fatalf("FIX_NEEDED = %+v, want a tests failure", msg)
	}
}

func TestMergeOne_BranchMissingOnOrigin(t *testing.T) {
	f := newMergeOneFixture(t)
	run(t, f.workDir, "git", "push", "origin", "--delete", f.branch)
	run(t, f.workDir, "git", "fetch", "--prune", "origin")
	out := f.mergeOne(t)
	if out.Kind != OutcomeBranchMissing {
		t.Fatalf("outcome = %s %q, want branch_missing", out.Kind, out.Message)
	}
	f.assertOpenAndUnclaimed(t)
	if !strings.Contains(readLog(t, f.gtLog), "BRANCH_MISSING: MR "+fixtureMR) {
		t.Errorf("mayor not told about the missing branch:\n%s", readLog(t, f.gtLog))
	}
}

func TestMergeOne_OriginBranchMovedAfterSubmit(t *testing.T) {
	f := newMergeOneFixture(t)
	run(t, f.workDir, "git", "checkout", "-b", "tmp", f.commit)
	writeFile(t, f.workDir, "apps/later.txt", "pushed after submit\n")
	run(t, f.workDir, "git", "add", ".")
	run(t, f.workDir, "git", "commit", "-m", "later")
	run(t, f.workDir, "git", "push", "origin", "tmp:"+f.branch)
	run(t, f.workDir, "git", "checkout", "main")
	run(t, f.workDir, "git", "branch", "-D", "tmp")
	before := f.originMain(t)

	out := f.mergeOne(t)
	if out.Kind != OutcomePrecondition || !strings.Contains(out.Message, "not the submitted commit_sha") {
		t.Fatalf("outcome = %s %q, want precondition (origin branch moved)", out.Kind, out.Message)
	}
	if f.originMain(t) != before {
		t.Fatal("origin main moved for a re-pushed branch")
	}
	f.assertOpenAndUnclaimed(t)
}

func TestMergeOne_RefusesMRWithoutCommitSHA(t *testing.T) {
	f := newMergeOneFixture(t)
	f.store.issues[fixtureMR] = memMRIssue(fixtureMR, f.branch, "main", fixtureSource, "")
	out := f.mergeOne(t)
	if out.Kind != OutcomeNotEligible || !strings.Contains(out.Message, "commit_sha") {
		t.Fatalf("outcome = %s %q, want not_eligible (no commit_sha)", out.Kind, out.Message)
	}
}

func TestMergeOne_RefusesNonMRAndClosedMR(t *testing.T) {
	f := newMergeOneFixture(t)
	f.store.issues["gt-plain"] = memIssue("gt-plain", "not an MR")
	e := f.engineer(t)
	if out := e.MergeOne(context.Background(), "gt-plain"); out.Kind != OutcomeNotEligible {
		t.Fatalf("plain issue: outcome = %s %q, want not_eligible", out.Kind, out.Message)
	}
	if out := e.MergeOne(context.Background(), "gt-missing"); out.Kind != OutcomeNotEligible {
		t.Fatalf("missing MR: outcome = %s %q, want not_eligible", out.Kind, out.Message)
	}
	if out := e.MergeOne(context.Background(), "--force"); out.Kind != OutcomeNotEligible {
		t.Fatalf("option-looking id: outcome = %s %q, want not_eligible", out.Kind, out.Message)
	}
	f.store.issues[fixtureMR].Status = beadsdk.StatusClosed
	if out := e.MergeOne(context.Background(), fixtureMR); out.Kind != OutcomeNotEligible {
		t.Fatalf("closed MR: outcome = %s %q, want not_eligible", out.Kind, out.Message)
	}
}

func TestMergeOne_CanceledContextNeverPushes(t *testing.T) {
	f := newMergeOneFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := f.originMain(t)
	out := f.engineer(t).MergeOne(ctx, fixtureMR)
	if out.Kind != OutcomeError || !strings.Contains(out.Message, "canceled") {
		t.Fatalf("outcome = %s %q, want error (canceled)", out.Kind, out.Message)
	}
	if f.originMain(t) != before {
		t.Fatal("origin main moved after cancel")
	}
	f.assertOpenAndUnclaimed(t)
}

func TestMergeOne_LocalTargetBehindOriginIsFastForwarded(t *testing.T) {
	f := newMergeOneFixture(t)
	// Another clone lands work on origin main; the refinery's local main is
	// now behind, and its `git pull` is broken (bad pull.ff config).
	other := filepath.Join(t.TempDir(), "other")
	run(t, f.workDir, "git", "clone", run(t, f.workDir, "git", "remote", "get-url", "origin"), other)
	run(t, other, "git", "config", "user.email", "o@test.com")
	run(t, other, "git", "config", "user.name", "Other")
	writeFile(t, other, "other.txt", "landed elsewhere\n")
	run(t, other, "git", "add", ".")
	run(t, other, "git", "commit", "-m", "other work")
	run(t, other, "git", "push", "origin", "main")
	landed := run(t, other, "git", "rev-parse", "HEAD")
	run(t, f.workDir, "git", "config", "pull.ff", "bogus")

	out := f.mergeOne(t)
	if out.Kind != OutcomeMerged {
		t.Fatalf("outcome = %s %q, want merged after fast-forwarding local main", out.Kind, out.Message)
	}
	if first := run(t, f.workDir, "git", "rev-parse", f.originMain(t)+"^1"); first != landed {
		t.Fatalf("merge first parent = %s, want the origin tip %s", first, landed)
	}
}
