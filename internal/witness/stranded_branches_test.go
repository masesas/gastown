package witness

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/lock"
	"github.com/steveyegge/gastown/internal/mail"
	"github.com/steveyegge/gastown/internal/mergeproof"
)

// gt-pd1c.78 tahap 2: the witness reports polecat branches whose work is on
// no merge target and not archived to the mayor (not the overseer), with the
// options their merge-queue class allows; it never deletes them.

const (
	tipMain = "1111111111111111111111111111111111111111"
	tipRej  = "2222222222222222222222222222222222222222"
	tipSup  = "3333333333333333333333333333333333333333"
	tipCon  = "4444444444444444444444444444444444444444"
	tipRaw  = "5555555555555555555555555555555555555555"
	tipNoMR = "6666666666666666666666666666666666666666"
	tipLive = "7777777777777777777777777777777777777777"
	tipOpen = "8888888888888888888888888888888888888888"
	tipArch = "9999999999999999999999999999999999999999"
	tipUnv  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

// fakeRemote lists the refs of a fake origin.
func fakeRemote(refs map[string]string, fail error) mergeproof.RemoteTips {
	return mergeproof.RemoteTips{
		Defaults: []string{"main"},
		List: func(prefix string) ([]git.RemoteRef, error) {
			if fail != nil {
				return nil, fail
			}
			var out []git.RemoteRef
			for name, hash := range refs {
				if strings.HasPrefix(name, prefix) {
					out = append(out, git.RemoteRef{Name: name, Hash: hash})
				}
			}
			return out, nil
		},
	}
}

// coverGit says only tipMain is preserved on the main tip.
type coverGit struct{}

func (coverGit) Rev(ref string) (string, error) { return strings.TrimSuffix(ref, "^{commit}"), nil }
func (coverGit) PreservedOn(head, base string) (git.BranchPreservationStatus, error) {
	return git.BranchPreservationStatus{Preserved: head == tipMain}, nil
}

// notLandedProver: git definitely says no commit landed.
type notLandedProver struct{}

func (notLandedProver) VerifyPushedCommitReachableFromPushTarget(remote, branch, commit string) error {
	return notOnTargetErr{}
}
func (notLandedProver) PatchEquivalentOnPushTarget(remote, branch, commit, base string) (bool, error) {
	return false, nil
}

type notOnTargetErr struct{}

func (notOnTargetErr) Error() string           { return "not on target" }
func (notOnTargetErr) CommitNotOnTarget() bool { return true }

func strandedMR(id, status, branch, commit, extra string) *beads.Issue {
	return &beads.Issue{ID: id, Status: status, Description: "branch: " + branch + "\ntarget: main\nsource_issue: gt-src" + id[len(id)-1:] +
		"\nrig: testrig\ncommit_sha: " + commit + "\n" + extra}
}

func strandedQueue() *beads.MergeQueue {
	idx := beads.MergeQueueIndex{}
	for _, mr := range []*beads.Issue{
		strandedMR("mr-1", "closed", "polecat/rej/gt-1+mvcf8o91", tipRej, "close_reason: rejected\n"),
		strandedMR("mr-2", "closed", "polecat/sup/gt-2+mvcf8o91", tipSup, "close_reason: superseded\n"),
		strandedMR("mr-3", "closed", "polecat/con/gt-3+mvcf8o91", tipCon, "close_reason: conflict\n"),
		strandedMR("mr-4", "closed", "polecat/raw/gt-4+mvcf8o91", tipRaw, ""),
		strandedMR("mr-5", "open", "polecat/open/gt-5+mvcf8o91", tipOpen, ""),
	} {
		src := beads.ParseMRFields(mr).SourceIssue
		idx[src] = append(idx[src], mr)
	}
	return beads.NewMergeQueue(idx, nil, func(string) beads.MergeProver { return notLandedProver{} })
}

var strandedRefs = map[string]string{
	"refs/heads/main":                                                    tipMain,
	"refs/heads/polecat/landed":                                          tipMain, // on a merge target
	"refs/heads/polecat/rej/gt-1+mvcf8o91":                               tipRej,
	"refs/heads/polecat/sup/gt-2+mvcf8o91":                               tipSup,
	"refs/heads/polecat/con/gt-3+mvcf8o91":                               tipCon,
	"refs/heads/polecat/raw/gt-4+mvcf8o91":                               tipRaw,
	"refs/heads/polecat/ghost":                                           tipNoMR, // no MR, owner dead
	"refs/heads/polecat/alive":                                           tipLive, // no MR, owner alive
	"refs/heads/polecat/open/gt-5+mvcf8o91":                              tipOpen,
	"refs/heads/polecat/arch/gt-6+mvcf8o91":                              tipArch, // archived
	"refs/gt/archive/polecat/arch/gt-6+mvcf8o91/1760000000-999999999999": tipArch,
}

var strandedNow = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

type strandedRecorder struct {
	sent    []*mail.Message
	sendErr error
}

func (r *strandedRecorder) reconciler(state branchReportState) *branchReconciler {
	return &branchReconciler{
		rig:      "testrig",
		tips:     fakeRemote(strandedRefs, nil),
		git:      coverGit{},
		queue:    strandedQueue,
		alive:    func(name string) (bool, error) { return name == "alive", nil },
		grace:    2 * time.Hour,
		interval: 24 * time.Hour,
		now:      strandedNow,
		state:    state,
		send: func(m *mail.Message) error {
			if r.sendErr != nil {
				return r.sendErr
			}
			r.sent = append(r.sent, m)
			return nil
		},
	}
}

// seen marks every stranded branch as first seen long enough ago.
func seenLongAgo() branchReportState {
	old := strandedNow.Add(-3 * time.Hour)
	return branchReportState{
		"polecat/rej/gt-1+mvcf8o91": {Tip: tipRej, Class: string(beads.TipRejected), FirstSeen: old},
		"polecat/sup/gt-2+mvcf8o91": {Tip: tipSup, Class: string(beads.TipSuperseded), FirstSeen: old},
		"polecat/con/gt-3+mvcf8o91": {Tip: tipCon, Class: string(beads.TipConflict), FirstSeen: old},
		"polecat/raw/gt-4+mvcf8o91": {Tip: tipRaw, Class: string(beads.TipNoVerdict), FirstSeen: old},
		"polecat/ghost":             {Tip: tipNoMR, Class: string(beads.TipNoMR), FirstSeen: old},
	}
}

func TestReconcileStrandedBranchesReportsToMayor(t *testing.T) {
	rec := &strandedRecorder{}
	r := rec.reconciler(seenLongAgo())
	res := r.run()
	if len(res.Errors) != 0 {
		t.Fatalf("errors: %v", res.Errors)
	}
	wantReported := []string{"polecat/con/gt-3+mvcf8o91", "polecat/ghost", "polecat/raw/gt-4+mvcf8o91", "polecat/rej/gt-1+mvcf8o91", "polecat/sup/gt-2+mvcf8o91"}
	if strings.Join(res.Reported, ",") != strings.Join(wantReported, ",") {
		t.Fatalf("reported = %v, want %v", res.Reported, wantReported)
	}
	if len(rec.sent) != 1 {
		t.Fatalf("want one digest mail, got %d", len(rec.sent))
	}
	msg := rec.sent[0]
	if msg.To != "mayor/" || msg.From != "testrig/witness" {
		t.Fatalf("mail routing: to %q from %q (the mayor decides, not the overseer)", msg.To, msg.From)
	}
	lines := map[string]string{}
	for _, l := range strings.Split(msg.Body, "\n") {
		for _, b := range wantReported {
			if strings.HasPrefix(l, "- "+b+" ") {
				lines[b] = l
			}
		}
	}
	abandon := `gt polecat abandon testrig/`
	for b, l := range lines {
		offers := strings.Contains(l, abandon)
		wantOffer := b == "polecat/rej/gt-1+mvcf8o91" || b == "polecat/sup/gt-2+mvcf8o91"
		if offers != wantOffer {
			t.Errorf("%s: abandon offered=%v, want %v: %q", b, offers, wantOffer, l)
		}
		if !strings.Contains(l, "rework") {
			t.Errorf("%s: rework/resubmit must always be offered: %q", b, l)
		}
	}
	if len(lines) != len(wantReported) {
		t.Fatalf("one line per branch, got %v\n%s", lines, msg.Body)
	}
	if !strings.Contains(lines["polecat/rej/gt-1+mvcf8o91"], abandon+`polecat/rej/gt-1+mvcf8o91 --reason`) {
		t.Errorf("abandon must be the exact command: %q", lines["polecat/rej/gt-1+mvcf8o91"])
	}
	if !strings.Contains(lines["polecat/raw/gt-4+mvcf8o91"], "ANOMALY") {
		t.Errorf("an MR closed by hand that did not land is an anomaly: %q", lines["polecat/raw/gt-4+mvcf8o91"])
	}
	for _, skipped := range []string{"polecat/landed", "polecat/alive", "polecat/open", "polecat/arch"} {
		if strings.Contains(msg.Body, skipped) {
			t.Errorf("%s must not be reported:\n%s", skipped, msg.Body)
		}
	}
	for _, b := range wantReported {
		if got := r.state[b].NotifiedAt; !got.Equal(strandedNow) {
			t.Errorf("%s: notified_at = %v, want now", b, got)
		}
	}
}

func TestReconcileStrandedBranchesGraceAndDedup(t *testing.T) {
	// First sight: everything is in grace, nothing is mailed, first_seen set.
	rec := &strandedRecorder{}
	r := rec.reconciler(branchReportState{})
	res := r.run()
	if len(rec.sent) != 0 || len(res.Reported) != 0 || res.Pending != 5 {
		t.Fatalf("first patrol: sent=%d reported=%v pending=%d", len(rec.sent), res.Reported, res.Pending)
	}
	if st := r.state["polecat/ghost"]; !st.FirstSeen.Equal(strandedNow) || st.Tip != tipNoMR {
		t.Fatalf("state after first sight: %+v", st)
	}
	if _, ok := r.state["polecat/alive"]; ok {
		t.Fatal("a branch whose owner is alive is not tracked")
	}

	// Reported once, then quiet within the reminder interval.
	state := seenLongAgo()
	rec = &strandedRecorder{}
	r = rec.reconciler(state)
	r.run()
	r.now = strandedNow.Add(time.Hour)
	if res := r.run(); len(rec.sent) != 1 || len(res.Reported) != 0 {
		t.Fatalf("second patrol within the interval: sent=%d reported=%v", len(rec.sent), res.Reported)
	}
	// Reminder after the interval, then backoff 3x and 7x, then silence.
	r.now = strandedNow.Add(25 * time.Hour)
	if res := r.run(); len(rec.sent) != 2 || len(res.Reported) != 5 || !strings.Contains(rec.sent[1].Subject, "REMINDER") {
		t.Fatalf("reminder: sent=%d reported=%v", len(rec.sent), res.Reported)
	}
	r.now = strandedNow.Add(25*time.Hour + 48*time.Hour)
	if r.run(); len(rec.sent) != 2 {
		t.Fatalf("second reminder must wait 3x the interval, sent=%d", len(rec.sent))
	}
	r.now = strandedNow.Add(25*time.Hour + 73*time.Hour)
	if r.run(); len(rec.sent) != 3 {
		t.Fatalf("second reminder after 3x the interval, sent=%d", len(rec.sent))
	}
	r.now = r.now.Add(7 * 24 * time.Hour)
	if r.run(); len(rec.sent) != 4 || !strings.Contains(rec.sent[3].Body, "last reminder") {
		t.Fatalf("last reminder after 7x the interval, sent=%d", len(rec.sent))
	}
	r.now = r.now.Add(365 * 24 * time.Hour)
	if res := r.run(); len(rec.sent) != 4 || res.Pending != 5 {
		t.Fatalf("no report after the last reminder: sent=%d pending=%d", len(rec.sent), res.Pending)
	}

	// A new tip restarts the grace (the polecat pushed more work).
	state = seenLongAgo()
	state["polecat/rej/gt-1+mvcf8o91"] = branchReport{Tip: tipCon, Class: string(beads.TipRejected), FirstSeen: strandedNow.Add(-5 * time.Hour)}
	rec = &strandedRecorder{}
	r = rec.reconciler(state)
	if res := r.run(); containsStr(res.Reported, "polecat/rej/gt-1+mvcf8o91") {
		t.Fatalf("a changed tip must restart the grace: %v", res.Reported)
	}

	// A branch that is no longer stranded is forgotten.
	state = seenLongAgo()
	state["polecat/deleted"] = branchReport{Tip: tipRej, Class: "rejected", FirstSeen: strandedNow.Add(-5 * time.Hour)}
	rec = &strandedRecorder{}
	r = rec.reconciler(state)
	r.run()
	if _, ok := r.state["polecat/deleted"]; ok {
		t.Fatal("state of a branch that is gone must be pruned")
	}
}

func TestReconcileStrandedBranchesFailures(t *testing.T) {
	// A failed send marks nothing: the next patrol retries.
	rec := &strandedRecorder{sendErr: errors.New("mail down")}
	r := rec.reconciler(seenLongAgo())
	res := r.run()
	if len(res.Errors) == 0 || !r.state["polecat/rej/gt-1+mvcf8o91"].NotifiedAt.IsZero() {
		t.Fatalf("send failure: errors=%v state=%+v", res.Errors, r.state["polecat/rej/gt-1+mvcf8o91"])
	}
	// Unreadable merge queue: nothing reported, error surfaced.
	rec = &strandedRecorder{}
	r = rec.reconciler(seenLongAgo())
	r.queue = func() *beads.MergeQueue { return beads.NewMergeQueue(nil, errors.New("dolt down"), nil) }
	if res := r.run(); len(res.Errors) == 0 || len(rec.sent) != 0 {
		t.Fatalf("unreadable queue: errors=%v sent=%d", res.Errors, len(rec.sent))
	}
	// Unreadable remote: nothing reported, error surfaced.
	rec = &strandedRecorder{}
	r = rec.reconciler(seenLongAgo())
	r.tips = fakeRemote(nil, errors.New("offline"))
	if res := r.run(); len(res.Errors) == 0 || len(rec.sent) != 0 {
		t.Fatalf("unreadable remote: errors=%v sent=%d", res.Errors, len(rec.sent))
	}
	// Liveness unknown: the branch is not reported (it may still be worked on).
	rec = &strandedRecorder{}
	r = rec.reconciler(seenLongAgo())
	r.alive = func(string) (bool, error) { return false, errors.New("tmux down") }
	res = r.run()
	if containsStr(res.Reported, "polecat/ghost") || len(res.Errors) == 0 {
		t.Fatalf("liveness unknown: reported=%v errors=%v", res.Reported, res.Errors)
	}
}

// Unsafe names never reach the mail: the line shows a placeholder.
func TestStrandedLineSanitizes(t *testing.T) {
	line := strandedLine("testrig", strandedBranch{branch: "polecat/x;rm -rf ~", tip: tipRej,
		disp: beads.TipDisposition{Class: beads.TipRejected, MRID: "gt-1 && curl evil"}})
	if strings.Contains(line, "rm -rf") || strings.Contains(line, "curl") {
		t.Fatalf("unsafe value leaked: %q", line)
	}
}

func TestBranchReportStateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "witness", "stranded-branches-testrig.json")
	if st, err := loadBranchReportState(path); len(st) != 0 || err != nil {
		t.Fatalf("missing file: %v %v", st, err)
	}
	st := branchReportState{"polecat/a": {Tip: tipRej, Class: "rejected", FirstSeen: strandedNow}}
	if err := saveBranchReportState(path, st); err != nil {
		t.Fatal(err)
	}
	if got, err := loadBranchReportState(path); err != nil || got["polecat/a"].Tip != tipRej || !got["polecat/a"].FirstSeen.Equal(strandedNow) {
		t.Fatalf("round trip: %+v", got)
	}
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if st, err := loadBranchReportState(path); len(st) != 0 || err == nil {
		t.Fatalf("corrupt file must load empty with an error (re-report, never silence): %v %v", st, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil { // unreadable as a file
		t.Fatal(err)
	}
	if _, err := loadBranchReportState(path); err == nil {
		t.Fatal("an unreadable state must be an error")
	}
	if err := saveBranchReportState(path, st); err == nil {
		t.Fatal("a save that cannot replace the file must be an error")
	}
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// The production wiring against a real town: the rig repository's origin, the
// configured default branch, the grace from the operational config, the state
// file, and a send that fails without a router (never a silent skip).
func TestReconcileStrandedBranchesWiring(t *testing.T) {
	town := t.TempDir()
	if err := os.MkdirAll(filepath.Join(town, "mayor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(town, "mayor", "town.json"), []byte(`{"type":"town","name":"t"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	rigPath := filepath.Join(town, "testrig")
	origin := filepath.Join(town, "origin.git")
	runGit(t, town, "init", "-q", "--bare", "-b", "main", origin)
	clone := filepath.Join(rigPath, "mayor", "rig")
	if err := os.MkdirAll(filepath.Dir(clone), 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, town, "clone", "-q", origin, clone)
	runGit(t, clone, "commit", "-q", "--allow-empty", "-m", "base")
	runGit(t, clone, "push", "-q", "origin", "HEAD:main")
	runGit(t, clone, "checkout", "-q", "-b", "polecat/nux/gt-1")
	if err := os.WriteFile(filepath.Join(clone, "w.txt"), []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, clone, "add", "-A")
	runGit(t, clone, "commit", "-q", "-m", "work")
	runGit(t, clone, "push", "-q", "origin", "polecat/nux/gt-1")
	tip := runGit(t, clone, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(rigPath, "config.json"), []byte(`{"type":"rig","default_branch":"main"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	old := witnessMergeQueue
	witnessMergeQueue = func(string) *beads.MergeQueue {
		mr := strandedMR("mr-1", "closed", "polecat/nux/gt-1", tip, "close_reason: rejected\n")
		return beads.NewMergeQueue(beads.MergeQueueIndex{"gt-src1": {mr}}, nil, func(string) beads.MergeProver { return notLandedProver{} })
	}
	t.Cleanup(func() { witnessMergeQueue = old })
	bd, _ := mockBd(func(args []string) (string, error) {
		if len(args) > 1 && args[0] == "show" && args[1] == "gt-src1" {
			return `[{"status":"open"}]`, nil // the source bead waits for rework
		}
		return "[]", nil
	}, func([]string) error { return nil })

	r, statePath, err := newBranchReconciler(context.Background(), bd, rigPath, "testrig", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.grace != 2*time.Hour || r.tips.Defaults[0] != "main" || statePath != filepath.Join(town, "witness", "stranded-branches-testrig.json") {
		t.Fatalf("wiring: grace=%v defaults=%v state=%s", r.grace, r.tips.Defaults, statePath)
	}
	// First patrol: tracked, in grace, state saved.
	res := ReconcileStrandedBranches(bd, rigPath, "testrig", nil)
	if len(res.Errors) != 0 || res.Checked != 1 || res.Pending != 1 {
		t.Fatalf("first patrol: %+v", res)
	}
	st, _ := loadBranchReportState(statePath)
	if st["polecat/nux/gt-1"].Tip != tip || st["polecat/nux/gt-1"].Class != "rejected" {
		t.Fatalf("state after first patrol: %+v", st)
	}
	// Past the grace without a router: the send fails, nothing is marked.
	st["polecat/nux/gt-1"] = branchReport{Tip: tip, Class: "rejected", FirstSeen: time.Now().Add(-3 * time.Hour)}
	if err := saveBranchReportState(statePath, st); err != nil {
		t.Fatal(err)
	}
	res = ReconcileStrandedBranches(bd, rigPath, "testrig", nil)
	if len(res.Errors) != 1 || !strings.Contains(res.Errors[0].Error(), "no mail router") || len(res.Reported) != 0 {
		t.Fatalf("no router: %+v", res)
	}
	if st, _ := loadBranchReportState(statePath); !st["polecat/nux/gt-1"].NotifiedAt.IsZero() {
		t.Fatal("an unsent report must not be marked")
	}
	if res := ReconcileStrandedBranches(bd, rigPath, "bad rig", nil); len(res.Errors) != 1 {
		t.Fatalf("unsafe rig name: %+v", res)
	}
	if res := ReconcileStrandedBranches(bd, t.TempDir(), "testrig", nil); len(res.Errors) != 1 {
		t.Fatalf("outside a town: %+v", res)
	}
}

// Liveness: an agent bead that still holds hooked work keeps the branch's
// owner alive; a done or nuked bead, or none, does not.
func TestPolecatAliveFromAgentBead(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}
	for state, want := range map[string]bool{"working": true, "done": false, "nuked": false, "": false} {
		bd, _ := mockBd(func(args []string) (string, error) {
			if state == "" {
				return "", errors.New("not found")
			}
			return `[{"agent_state":"` + state + `","hook_bead":"gt-1"}]`, nil
		}, func([]string) error { return nil })
		alive, err := polecatAlive(bd, t.TempDir(), t.TempDir(), "testrig-nosession")("ghost-" + state)
		if err != nil {
			t.Skipf("tmux unusable here: %v", err)
		}
		if alive != want {
			t.Errorf("agent_state %q: alive=%v, want %v", state, alive, want)
		}
	}
	if alive, _ := polecatAlive(nil, "", "", "testrig")("bad name;x"); alive {
		t.Error("an unsafe polecat name is never alive")
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// G-H1: a decided MR whose source bead is terminal, or whose source the merge
// queue still holds through another MR (rework running, landed), is not
// stranded: "leave it" or the rework already decided it.
func TestReconcileStrandedBranchesSourceSettled(t *testing.T) {
	rec := &strandedRecorder{}
	r := rec.reconciler(seenLongAgo())
	r.terminal = func(id string) (bool, error) { return id == "gt-src1", nil } // rejected work: source closed
	base := strandedQueue
	r.queue = func() *beads.MergeQueue {
		q := base()
		// The conflict MR's source has a new open MR from another branch.
		idx := beads.MergeQueueIndex{}
		for _, src := range []string{"gt-src1", "gt-src2", "gt-src3", "gt-src4", "gt-src5"} {
			idx[src] = q.MergeRequests(src)
		}
		idx["gt-src3"] = append(idx["gt-src3"], strandedMR("mr-9", "open", "polecat/other/gt-9", tipOpen, "source_issue: gt-src3\n"))
		return beads.NewMergeQueue(idx, nil, func(string) beads.MergeProver { return notLandedProver{} })
	}
	res := r.run()
	if containsStr(res.Reported, "polecat/rej/gt-1+mvcf8o91") || containsStr(res.Reported, "polecat/con/gt-3+mvcf8o91") {
		t.Fatalf("settled sources must not be reported: %v", res.Reported)
	}
	if !containsStr(res.Reported, "polecat/sup/gt-2+mvcf8o91") || !containsStr(res.Reported, "polecat/raw/gt-4+mvcf8o91") {
		t.Fatalf("open sources are still reported: %v", res.Reported)
	}
	r = rec.reconciler(seenLongAgo())
	r.terminal = func(string) (bool, error) { return false, errors.New("bd down") }
	if res := r.run(); len(res.Errors) == 0 || containsStr(res.Reported, "polecat/rej/gt-1+mvcf8o91") {
		t.Fatalf("unreadable source: %+v", res)
	}
	if _, kept := r.state["polecat/rej/gt-1+mvcf8o91"]; !kept {
		t.Fatal("an unknown answer must keep the branch's state (its backoff)")
	}
}

// G-M1/G-M3: a failed fetch or an exhausted budget reports nothing; no
// polecat branch means no fetch; a branch with nothing due is not
// re-classified.
func TestReconcileStrandedBranchesFetchBudgetReuse(t *testing.T) {
	rec := &strandedRecorder{}
	r := rec.reconciler(seenLongAgo())
	r.fetch = func() error { return errors.New("fatal: unable to access 'https://u:tok@host/x'") }
	res := r.run()
	if len(rec.sent) != 0 || len(res.Errors) != 1 || strings.Contains(res.Errors[0].Error(), "tok") {
		t.Fatalf("fetch failure: sent=%d errors=%v", len(rec.sent), res.Errors)
	}

	fetched := false
	r = rec.reconciler(branchReportState{})
	r.tips = fakeRemote(map[string]string{"refs/heads/main": tipMain}, nil)
	r.fetch = func() error { fetched = true; return nil }
	r.run()
	if fetched {
		t.Fatal("no polecat branch: no fetch")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r = rec.reconciler(seenLongAgo())
	r.ctx = ctx
	if res := r.run(); len(rec.sent) != 0 || len(res.Errors) != 1 || !strings.Contains(res.Errors[0].Error(), "budget") {
		t.Fatalf("budget: sent=%d %+v", len(rec.sent), res)
	}

	// All stranded branches reported a minute ago: the next patrol reads
	// neither the merge queue nor git for them.
	r = rec.reconciler(seenLongAgo())
	r.run()
	queueReads := 0
	r.queue = func() *beads.MergeQueue { queueReads++; return strandedQueue() }
	r.now = r.now.Add(time.Minute)
	if res := r.run(); queueReads != 1 || res.Pending != 5 {
		// One read is for branches without state (open, alive): those are classified.
		t.Fatalf("reuse: queue reads=%d pending=%d", queueReads, res.Pending)
	}
}

// G-M4: concurrent patrols of a rig serialize on the state lock; the second
// one does nothing and says so.
func TestReconcileStrandedBranchesLockBusy(t *testing.T) {
	town := t.TempDir()
	if err := os.MkdirAll(filepath.Join(town, "mayor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(town, "mayor", "town.json"), []byte(`{"type":"town","name":"t"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	rigPath := filepath.Join(town, "testrig")
	if err := os.MkdirAll(filepath.Join(rigPath, "mayor", "rig"), 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, filepath.Join(rigPath, "mayor", "rig"), "init", "-q")
	statePath := branchReportStatePath(town, "testrig")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatal(err)
	}
	unlock, ok, err := lock.FlockTryAcquire(statePath + ".flock")
	if err != nil || !ok {
		t.Fatalf("lock: %v %v", ok, err)
	}
	defer unlock()
	bd, _ := mockBd(func([]string) (string, error) { return "[]", nil }, func([]string) error { return nil })
	if res := ReconcileStrandedBranches(bd, rigPath, "testrig", nil); !res.Busy || len(res.Errors) != 0 {
		t.Fatalf("busy: %+v", res)
	}
}
