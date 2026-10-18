package refinery

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	gitpkg "github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/mail"
	"github.com/steveyegge/gastown/internal/rig"
)

// fakeMailSender records protocol mail instead of delivering it.
type fakeMailSender struct {
	mu   sync.Mutex
	sent []*mail.Message
	err  error
}

func (f *fakeMailSender) Send(msg *mail.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, msg)
	return f.err
}

func (f *fakeMailSender) WaitPendingNotifications() {}

func (f *fakeMailSender) subjects() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.sent))
	for _, m := range f.sent {
		out = append(out, m.Subject)
	}
	return out
}

func (f *fakeMailSender) find(prefix string) *mail.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.sent {
		if strings.HasPrefix(m.Subject, prefix) {
			return m
		}
	}
	return nil
}

// installFakeGT puts a `gt` stub on PATH that logs its arguments, so engine
// nudges never reach a real session. Returns the log path.
func installFakeGT(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell stub for gt")
	}
	dir := t.TempDir()
	logPath := filepath.Join(dir, "gt.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + logPath + "'\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "gt"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

func readLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(data)
}

// scopedTestEngineer is newTestEngineer with a separate rig dir whose
// settings/config.json sets merge_queue.allowed_paths, and a fake mailer.
func scopedTestEngineer(t *testing.T, workDir string, allowed []string) (*Engineer, *fakeMailSender, *bytes.Buffer) {
	t.Helper()
	e := newTestEngineer(t, workDir, gitpkg.NewGit(workDir))
	rigPath := t.TempDir()
	e.rig = &rig.Rig{Name: "test-rig", Path: rigPath}
	if allowed != nil {
		writeRigSettingsMQ(t, rigPath, map[string]any{"allowed_paths": allowed})
	}
	sender := &fakeMailSender{}
	e.router = sender
	out := &bytes.Buffer{}
	e.output = out
	return e, sender, out
}

func TestDoMerge_ScopeViolationNeverMergesOrPushes(t *testing.T) {
	installFakeGT(t)
	workDir, g, _ := testGitRepo(t)
	branch := "polecat/test/out-of-scope"
	createFeatureBranch(t, workDir, branch, "outside.txt", "not allowed\n")
	commit := run(t, workDir, "git", "rev-parse", branch)
	originBefore := run(t, workDir, "git", "ls-remote", "origin", "refs/heads/main")
	localBefore := run(t, workDir, "git", "rev-parse", "main")

	e, _, _ := scopedTestEngineer(t, workDir, []string{"apps/**"})
	e.git = g
	result := e.doMerge(context.Background(), &MRInfo{ID: "mr-scope", Branch: branch, Target: "main", CommitSHA: commit})

	if result.Success || !result.ScopeViolation {
		t.Fatalf("doMerge = %+v, want ScopeViolation without success", result)
	}
	if result.Conflict || result.TestsFailed {
		t.Fatalf("doMerge = %+v: a scope violation is neither a conflict nor a test failure", result)
	}
	if !strings.Contains(result.Error, "outside.txt") {
		t.Errorf("error %q does not name the offending path", result.Error)
	}
	if got := run(t, workDir, "git", "ls-remote", "origin", "refs/heads/main"); got != originBefore {
		t.Fatalf("origin main moved after a scope violation: %q -> %q", originBefore, got)
	}
	if got := run(t, workDir, "git", "rev-parse", "main"); got != localBefore {
		t.Fatalf("local main moved after a scope violation: %s -> %s", localBefore, got)
	}
}

func TestDoMerge_InScopeMerges(t *testing.T) {
	installFakeGT(t)
	workDir, g, _ := testGitRepo(t)
	branch := "polecat/test/in-scope"
	run(t, workDir, "git", "checkout", "-b", branch, "main")
	if err := os.MkdirAll(filepath.Join(workDir, "apps"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, workDir, "apps/a.txt", "allowed\n")
	run(t, workDir, "git", "add", ".")
	run(t, workDir, "git", "commit", "-m", "feat: apps change")
	run(t, workDir, "git", "checkout", "main")
	commit := run(t, workDir, "git", "rev-parse", branch)

	e, _, _ := scopedTestEngineer(t, workDir, []string{"apps/**"})
	e.git = g
	result := e.doMerge(context.Background(), &MRInfo{ID: "mr-scope-ok", Branch: branch, Target: "main", CommitSHA: commit})
	if !result.Success {
		t.Fatalf("doMerge = %+v, want success for an in-scope MR", result)
	}
}

func TestDoMerge_ScopeLoadErrorFailsClosed(t *testing.T) {
	installFakeGT(t)
	workDir, g, _ := testGitRepo(t)
	branch := "polecat/test/scope-broken"
	createFeatureBranch(t, workDir, branch, "x.txt", "x\n")
	commit := run(t, workDir, "git", "rev-parse", branch)
	originBefore := run(t, workDir, "git", "ls-remote", "origin", "refs/heads/main")

	e, _, _ := scopedTestEngineer(t, workDir, nil)
	e.git = g
	if err := os.MkdirAll(filepath.Join(e.rig.Path, "settings"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.rig.Path, "settings", "config.json"), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := e.doMerge(context.Background(), &MRInfo{ID: "mr-scope-broken", Branch: branch, Target: "main", CommitSHA: commit})
	if result.Success || result.ScopeViolation {
		t.Fatalf("doMerge = %+v, want a plain failure (scope unknown), not success or a violation", result)
	}
	if !strings.Contains(result.Error, "merge scope") {
		t.Errorf("error %q does not say the merge scope is unknown", result.Error)
	}
	if got := run(t, workDir, "git", "ls-remote", "origin", "refs/heads/main"); got != originBefore {
		t.Fatalf("origin main moved while the scope was unknown")
	}
}

func TestBuildRebaseStack_ScopeViolationIsExcludedAndReported(t *testing.T) {
	gtLog := installFakeGT(t)
	workDir, g, _ := testGitRepo(t)
	if err := os.MkdirAll(filepath.Join(workDir, "apps"), 0o755); err != nil {
		t.Fatal(err)
	}
	createFeatureBranch(t, workDir, "polecat/a/in", "apps/in.txt", "in\n")
	createFeatureBranch(t, workDir, "polecat/b/out", "out.txt", "out\n")

	e, sender, _ := scopedTestEngineer(t, workDir, []string{"apps/**"})
	e.git = g
	in := makeMR("mr-in", "polecat/a/in", "main")
	out := makeMR("mr-out", "polecat/b/out", "main")
	out.Worker = "b"
	stacked, conflicts, err := e.BuildRebaseStack(context.Background(), []*MRInfo{in, out}, "main")
	if err != nil {
		t.Fatalf("BuildRebaseStack: %v", err)
	}
	if len(stacked) != 1 || stacked[0].ID != "mr-in" {
		t.Fatalf("stacked = %v, want [mr-in]", mrIDs(stacked))
	}
	if len(conflicts) != 0 {
		t.Fatalf("conflicts = %v, want none (a scope violation is not a conflict)", mrIDs(conflicts))
	}
	if ok, _ := g.IsAncestor(run(t, workDir, "git", "rev-parse", "polecat/b/out"), "HEAD"); ok {
		t.Fatal("out-of-scope MR is in the stack")
	}
	msg := sender.find("FIX_NEEDED")
	if msg == nil || !strings.Contains(msg.Body, "Failure-Type: scope") || !strings.Contains(msg.Body, "MR-Bead-ID: mr-out") {
		t.Fatalf("FIX_NEEDED for the scope violation not sent; sent=%v", sender.subjects())
	}
	if strings.Contains(readLog(t, gtLog), "MERGE_FAILED: branch=") {
		t.Errorf("polecat MERGE_FAILED nudge sent in addition to FIX_NEEDED mail:\n%s", readLog(t, gtLog))
	}
}

func TestBuildRebaseStack_ScopeLoadErrorFailsClosed(t *testing.T) {
	installFakeGT(t)
	workDir, g, _ := testGitRepo(t)
	createFeatureBranch(t, workDir, "polecat/a/x", "x.txt", "x\n")
	e, _, _ := scopedTestEngineer(t, workDir, nil)
	e.git = g
	if err := os.MkdirAll(filepath.Join(e.rig.Path, "settings"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.rig.Path, "settings", "config.json"), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.BuildRebaseStack(context.Background(), []*MRInfo{makeMR("mr-x", "polecat/a/x", "main")}, "main"); err == nil {
		t.Fatal("BuildRebaseStack with unknown scope = nil error, want fail closed")
	}
}

func TestHandleMRInfoFailure_ScopeSendsFixNeededWithoutConflictTask(t *testing.T) {
	gtLog := installFakeGT(t)
	workDir := t.TempDir()
	e, sender, _ := scopedTestEngineer(t, workDir, nil)
	e.workDir = workDir
	slotTouched := false
	e.mergeSlotEnsureExists = func() (string, error) { slotTouched = true; return "", errors.New("unexpected") }

	mr := &MRInfo{ID: "gt-mr-1", Branch: "polecat/nux/gt-1", Target: "main", SourceIssue: "gt-1", Worker: "polecats/nux", RetryCount: 1}
	e.HandleMRInfoFailure(mr, ProcessResult{ScopeViolation: true, Error: "merge scope violation: 1 path(s)"})

	if slotTouched {
		t.Fatal("scope failure tried to create a conflict-resolution task")
	}
	msg := sender.find("FIX_NEEDED")
	if msg == nil {
		t.Fatalf("no FIX_NEEDED mail; sent=%v", sender.subjects())
	}
	if msg.To != "test-rig/polecats/nux" {
		t.Errorf("FIX_NEEDED to %q, want test-rig/polecats/nux", msg.To)
	}
	for _, want := range []string{"Failure-Type: scope", "MR-Bead-ID: gt-mr-1", "Attempt-Number: 2", "Issue: gt-1", "Target: main"} {
		if !strings.Contains(msg.Body, want) {
			t.Errorf("FIX_NEEDED body missing %q:\n%s", want, msg.Body)
		}
	}
	log := readLog(t, gtLog)
	if strings.Contains(log, "test-rig/nux") {
		t.Errorf("polecat nudge sent in addition to FIX_NEEDED mail:\n%s", log)
	}
	if !strings.Contains(log, "nudge mayor/ MERGE_FAILED: gt-mr-1") || !strings.Contains(log, "type=scope") {
		t.Errorf("mayor not nudged about the scope failure:\n%s", log)
	}
}

func TestHandleMRInfoFailure_TestsFailedMailsWitnessWhenWorkerUnknown(t *testing.T) {
	installFakeGT(t)
	workDir := t.TempDir()
	e, sender, _ := scopedTestEngineer(t, workDir, nil)
	e.workDir = workDir
	e.HandleMRInfoFailure(&MRInfo{ID: "gt-mr-2", Branch: "polecat/x/gt-2", Target: "main"}, ProcessResult{TestsFailed: true, Error: "quality gates failed"})
	msg := sender.find("FIX_NEEDED")
	if msg == nil || msg.To != "test-rig/witness" || !strings.Contains(msg.Body, "Failure-Type: tests") {
		t.Fatalf("FIX_NEEDED = %+v, want tests failure mailed to test-rig/witness", msg)
	}
}

func TestHandleMRInfoFailure_ConflictKeepsPolecatNudgeAndSendsNoFixNeeded(t *testing.T) {
	gtLog := installFakeGT(t)
	workDir := t.TempDir()
	e, sender, _ := scopedTestEngineer(t, workDir, nil)
	e.workDir = workDir
	e.mergeSlotEnsureExists = func() (string, error) { return "", errors.New("no slot in this test") }
	e.HandleMRInfoFailure(&MRInfo{ID: "gt-mr-3", Branch: "polecat/nux/gt-3", Target: "main", Worker: "polecats/nux"}, ProcessResult{Conflict: true, Error: "merge conflicts"})
	if msg := sender.find("FIX_NEEDED"); msg != nil {
		t.Fatalf("conflict sent FIX_NEEDED %+v; conflicts go to a conflict-resolution task", msg)
	}
	if log := readLog(t, gtLog); !strings.Contains(log, "nudge test-rig/nux MERGE_FAILED") || !strings.Contains(log, "type=conflict") {
		t.Errorf("conflict did not nudge the polecat:\n%s", log)
	}
}
