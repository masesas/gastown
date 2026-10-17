package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
)

// Second line of defence for gt-pd1c.69: a review-only source bead may never
// produce new commits or a merge request, whatever the dispatch path did.

const reviewOnlyAssignee = "gastown/polecats/refuge"

// installReviewOnlySourceBDRecorder installs a bd stub that serves bd-source
// (and every hooked issue) from JSON in every beads dir, lists the hooked
// issues as the calling agent's assignment, and logs every call. Returns the
// log path.
func installReviewOnlySourceBDRecorder(t *testing.T, source beads.Issue, hooked ...beads.Issue) string {
	t.Helper()
	return installReviewOnlyAssignmentsBDRecorder(t, source, hooked, nil)
}

// installReviewOnlyAssignmentsBDRecorder is installReviewOnlySourceBDRecorder
// with separate hooked and in_progress assignment lists. Unknown show IDs get
// bd's real "Issue <id> not found" stderr.
func installReviewOnlyAssignmentsBDRecorder(t *testing.T, source beads.Issue, hooked, inProgress []beads.Issue) string {
	t.Helper()
	dir := t.TempDir()
	writeJSON := func(name string, v any) {
		t.Helper()
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	all := append(append([]beads.Issue{source}, hooked...), inProgress...)
	for _, issue := range all {
		writeJSON("show-"+issue.ID+".json", []beads.Issue{issue})
	}
	if hooked == nil {
		hooked = []beads.Issue{}
	}
	if inProgress == nil {
		inProgress = []beads.Issue{}
	}
	writeJSON("hooked.json", hooked)
	writeJSON("in_progress.json", inProgress)
	logPath := filepath.Join(dir, "bd.log")
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "--allow-stale" ]; then
  shift
fi
if [ "$1" = "version" ]; then
  echo "bd stub"
  exit 0
fi
printf '%%s\t%%s\n' "$BEADS_DIR" "$*" >> %q
data=%q
if [ "$1" = "show" ] && [ -f "$data/show-$2.json" ]; then
  cat "$data/show-$2.json"
  exit 0
fi
if [ "$1" = "show" ]; then
  echo "Issue $2 not found" >&2
  exit 1
fi
if [ "$1" = "comments" ] && [ "$2" != "add" ]; then
  echo '[]'
  exit 0
fi
if [ "$1" = "list" ]; then
  case "$*" in
    *--status=hooked*) cat "$data/hooked.json" ;;
    *--status=in_progress*) cat "$data/in_progress.json" ;;
    *) echo '[]' ;;
  esac
  exit 0
fi
if [ "$1" = "sql" ]; then
  echo '[]'
  exit 0
fi
if [ "$1" = "create" ]; then
  echo '{"id":"gt-mr","title":"Merge: bd-source","status":"open","priority":1,"issue_type":"task","labels":["gt:merge-request"]}'
  exit 0
fi
if [ "$1" = "comments" ] && [ "$2" = "add" ]; then
  exit 0
fi
if [ "$1" = "close" ]; then
  exit 0
fi
echo "unexpected bd command: $*" >&2
exit 1
`, logPath, dir)
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "bd"), []byte(script), 0o755); err != nil {
		t.Fatalf("write bd recorder: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	beads.ResetBdAllowStaleCacheForTest()
	t.Cleanup(beads.ResetBdAllowStaleCacheForTest)
	return logPath
}

func reviewOnlySourceIssue(attachedAt time.Time, comments ...beads.Comment) beads.Issue {
	return beads.Issue{
		ID:       "bd-source",
		Title:    "review leg",
		Status:   "hooked",
		Priority: 1,
		Type:     "task",
		Assignee: reviewOnlyAssignee,
		Description: strings.Join([]string{
			"attached_at: " + attachedAt.UTC().Format(time.RFC3339Nano),
			"review_only: true",
			"",
			"Review the contract brief and report findings.",
		}, "\n"),
		Comments: comments,
	}
}

func setupReviewOnlyDoneEnv(t *testing.T, workDir string) {
	t.Helper()
	resetDoneFlagsForTest(t)
	townRoot := routedSourceTestTownRoot(workDir)
	t.Setenv("GT_TEST_NUDGE_LOG", filepath.Join(t.TempDir(), "nudge.log"))
	t.Setenv("GT_TOWN_ROOT", townRoot)
	t.Setenv("GT_ROOT", townRoot)
	t.Setenv("GT_ROLE", reviewOnlyAssignee)
	t.Setenv("GT_RIG", "gastown")
	t.Setenv("GT_POLECAT", "refuge")
	t.Setenv("BD_ACTOR", reviewOnlyAssignee)
	t.Chdir(workDir)
	doneIssue = "bd-source"
	updateAgentStateOnDoneFn = func(cwd, townRoot, exitType, issueID string) error { return nil }
}

// setupReviewOnlyGitRepoWithoutCommits creates a pushed feature branch that has
// no commits ahead of main: what a review leg that only reports looks like.
func setupReviewOnlyGitRepoWithoutCommits(t *testing.T, workDir string) string {
	t.Helper()
	remote := t.TempDir()
	runGitForMQSubmitTest(t, remote, "init", "--bare")
	runGitForMQSubmitTest(t, workDir, "init")
	runGitForMQSubmitTest(t, workDir, "config", "user.email", "test@example.com")
	runGitForMQSubmitTest(t, workDir, "config", "user.name", "Test User")
	runGitForMQSubmitTest(t, workDir, "remote", "add", "origin", remote)
	writeMQSubmitTestFile(t, workDir, ".gitignore", ".beads/\n.runtime/\n")
	writeMQSubmitTestFile(t, workDir, "file.txt", "main\n")
	runGitForMQSubmitTest(t, workDir, "add", ".gitignore", "file.txt")
	runGitForMQSubmitTest(t, workDir, "commit", "-m", "main")
	runGitForMQSubmitTest(t, workDir, "branch", "-M", "main")
	runGitForMQSubmitTest(t, workDir, "push", "-u", "origin", "main")
	branch := "polecat/refuge/bd-source"
	runGitForMQSubmitTest(t, workDir, "checkout", "-b", branch)
	return branch
}

func assertNoMergeRequestCreated(t *testing.T, logPath string) {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read bd log: %v", err)
	}
	if strings.Contains(string(data), "gt:merge-request") {
		t.Fatalf("review-only work created a merge request bead:\n%s", data)
	}
}

func TestRunMqSubmitRefusesReviewOnlySource(t *testing.T) {
	workDir, _, _ := setupRoutedSourceTestTown(t)
	setupRoutedSubmitCommandTown(t, workDir)
	branch := setupRoutedSubmitGitRepo(t, workDir, true)
	logPath := installReviewOnlySourceBDRecorder(t, reviewOnlySourceIssue(time.Now().Add(-time.Hour)))
	resetMqSubmitFlagsForTest(t)
	t.Setenv("GT_TEST_NUDGE_LOG", filepath.Join(t.TempDir(), "nudge.log"))
	t.Setenv("GT_RIG", "")
	t.Chdir(workDir)

	mqSubmitBranch = branch
	mqSubmitIssue = "bd-source"
	mqSubmitNoCleanup = true
	err := runMqSubmit(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "review-only") {
		t.Fatalf("runMqSubmit on a review-only source = %v, want review-only refusal", err)
	}
	assertNoMergeRequestCreated(t, logPath)
}

func TestRunDoneRefusesReviewOnlySourceWithCommits(t *testing.T) {
	for _, preVerified := range []bool{false, true} {
		t.Run(fmt.Sprintf("pre-verified=%v", preVerified), func(t *testing.T) {
			workDir, _, _ := setupRoutedSourceTestTown(t)
			setupRoutedSubmitCommandTown(t, workDir)
			setupRoutedSubmitGitRepo(t, workDir, false)
			logPath := installReviewOnlySourceBDRecorder(t, reviewOnlySourceIssue(time.Now().Add(-time.Hour)))
			setupReviewOnlyDoneEnv(t, workDir)
			donePreVerified = preVerified
			doneSkipVerify = true

			err := runDone(nil, nil)
			if err == nil || !strings.Contains(err.Error(), "review-only") {
				t.Fatalf("runDone with commits on a review-only source = %v, want review-only refusal", err)
			}
			assertNoMergeRequestCreated(t, logPath)
		})
	}
}

func TestRunDoneReviewOnlyWithoutCommitsCompletes(t *testing.T) {
	workDir, _, _ := setupRoutedSourceTestTown(t)
	setupRoutedSubmitCommandTown(t, workDir)
	setupReviewOnlyGitRepoWithoutCommits(t, workDir)
	head := runGitForMQSubmitTest(t, workDir, "rev-parse", "HEAD")
	assignedAt := time.Now().Add(-time.Hour)
	evidence := beads.Comment{
		Author:    reviewOnlyAssignee,
		CreatedAt: assignedAt.Add(time.Minute).UTC().Format(time.RFC3339Nano),
		Text:      "FINDINGS: brief is implementable as written\nhead_sha: " + head,
	}
	logPath := installReviewOnlySourceBDRecorder(t, reviewOnlySourceIssue(assignedAt, evidence))
	setupReviewOnlyDoneEnv(t, workDir)

	if err := runDone(nil, nil); err != nil {
		t.Fatalf("runDone for a report-only review leg: %v", err)
	}
	assertNoMergeRequestCreated(t, logPath)
	log := readSubmitSourceBDLog(t, logPath)
	if !strings.Contains(log, "close bd-source") {
		t.Fatalf("report-only review leg was not closed:\n%s", log)
	}
}

// codeSourceIssue is an ordinary (not review-only) bead assigned to nobody.
func codeSourceIssue() beads.Issue {
	return beads.Issue{ID: "bd-source", Title: "code task", Status: "open", Priority: 1, Type: "task",
		Description: "Implement the thing."}
}

// hookedReviewLeg is the review-only leg hooked to the calling polecat.
func hookedReviewLeg() beads.Issue {
	leg := reviewOnlySourceIssue(time.Now().Add(-time.Hour))
	leg.ID = "gt-review-leg"
	return leg
}

func TestRunMqSubmitRefusesWhenHookedBeadIsReviewOnly(t *testing.T) {
	workDir, _, _ := setupRoutedSourceTestTown(t)
	setupRoutedSubmitCommandTown(t, workDir)
	branch := setupRoutedSubmitGitRepo(t, workDir, true)
	logPath := installReviewOnlySourceBDRecorder(t, codeSourceIssue(), hookedReviewLeg())
	resetMqSubmitFlagsForTest(t)
	t.Setenv("GT_TEST_NUDGE_LOG", filepath.Join(t.TempDir(), "nudge.log"))
	t.Setenv("GT_RIG", "")
	t.Setenv("GT_ROLE", reviewOnlyAssignee)
	t.Setenv("BD_ACTOR", reviewOnlyAssignee)
	t.Chdir(workDir)

	mqSubmitBranch = branch
	mqSubmitIssue = "bd-source" // another bead than the hooked review leg
	mqSubmitNoCleanup = true
	err := runMqSubmit(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "gt-review-leg") {
		t.Fatalf("runMqSubmit --issue <other bead> with a hooked review leg = %v, want refusal naming gt-review-leg", err)
	}
	assertNoMergeRequestCreated(t, logPath)
}

func TestRunDoneRefusesWhenHookedBeadIsReviewOnly(t *testing.T) {
	workDir, _, _ := setupRoutedSourceTestTown(t)
	setupRoutedSubmitCommandTown(t, workDir)
	setupRoutedSubmitGitRepo(t, workDir, false)
	logPath := installReviewOnlySourceBDRecorder(t, codeSourceIssue(), hookedReviewLeg())
	setupReviewOnlyDoneEnv(t, workDir)
	doneSkipVerify = true // doneIssue = bd-source: another bead than the hooked review leg

	err := runDone(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "gt-review-leg") {
		t.Fatalf("runDone --issue <other bead> with a hooked review leg = %v, want refusal naming gt-review-leg", err)
	}
	assertNoMergeRequestCreated(t, logPath)
}

func TestRunDoneReviewOnlyRefusalExplainsRecovery(t *testing.T) {
	workDir, _, _ := setupRoutedSourceTestTown(t)
	setupRoutedSubmitCommandTown(t, workDir)
	setupRoutedSubmitGitRepo(t, workDir, false)
	installReviewOnlySourceBDRecorder(t, reviewOnlySourceIssue(time.Now().Add(-time.Hour)))
	setupReviewOnlyDoneEnv(t, workDir)
	doneSkipVerify = true

	err := runDone(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "git reset --hard") {
		t.Fatalf("review-only refusal = %v, want recovery hint with git reset --hard", err)
	}
}

func TestRunDoneReviewOnlyWithUncommittedFilesCompletesWithoutCommit(t *testing.T) {
	workDir, _, _ := setupRoutedSourceTestTown(t)
	setupRoutedSubmitCommandTown(t, workDir)
	setupReviewOnlyGitRepoWithoutCommits(t, workDir)
	head := runGitForMQSubmitTest(t, workDir, "rev-parse", "HEAD")
	// Leftovers of a review leg: a modified tracked file and an untracked report.
	writeMQSubmitTestFile(t, workDir, "file.txt", "scratch edit\n")
	writeMQSubmitTestFile(t, workDir, "findings.md", "notes\n")
	assignedAt := time.Now().Add(-time.Hour)
	evidence := beads.Comment{
		Author:    reviewOnlyAssignee,
		CreatedAt: assignedAt.Add(time.Minute).UTC().Format(time.RFC3339Nano),
		Text:      "REPORT: no gaps found\nhead_sha: " + head,
	}
	logPath := installReviewOnlySourceBDRecorder(t, reviewOnlySourceIssue(assignedAt, evidence))
	setupReviewOnlyDoneEnv(t, workDir)

	if err := runDone(nil, nil); err != nil {
		t.Fatalf("runDone for a report-only review leg with leftover files: %v", err)
	}
	if after := runGitForMQSubmitTest(t, workDir, "rev-parse", "HEAD"); after != head {
		t.Fatalf("gt done committed on a review-only bead: HEAD %s -> %s", head, after)
	}
	assertNoMergeRequestCreated(t, logPath)
	if log := readSubmitSourceBDLog(t, logPath); !strings.Contains(log, "close bd-source") {
		t.Fatalf("report-only review leg was not closed:\n%s", log)
	}
}

func TestFindReviewOnlyWorkFailsClosedAndPrefersTarget(t *testing.T) {
	prev := findAssignedBeadsForReviewOnly
	t.Cleanup(func() { findAssignedBeadsForReviewOnly = prev })

	findAssignedBeadsForReviewOnly = func(workDir, agentID string) ([]string, error) {
		return nil, fmt.Errorf("database is locked")
	}
	if _, err := findReviewOnlyWork(t.TempDir(), reviewOnlyAssignee); err == nil {
		t.Fatal("assignment lookup failure must fail closed")
	}

	review := reviewOnlySourceIssue(time.Now())
	got, err := findReviewOnlyWork(t.TempDir(), reviewOnlyAssignee, reviewOnlyCandidate{ID: review.ID, Issue: &review})
	if err != nil || got != review.ID {
		t.Fatalf("preloaded review-only target = (%q, %v), want (%q, nil) without assignment lookup", got, err, review.ID)
	}

	findAssignedBeadsForReviewOnly = func(workDir, agentID string) ([]string, error) { return nil, nil }
	code := codeSourceIssue()
	if got, err := findReviewOnlyWork(t.TempDir(), "", reviewOnlyCandidate{ID: code.ID, Issue: &code}); err != nil || got != "" {
		t.Fatalf("code bead without agent = (%q, %v), want (\"\", nil)", got, err)
	}
}

// staleReviewLeg is an old review leg left in_progress under the same,
// reused polecat name: it is not the polecat's current hook.
func staleReviewLeg() beads.Issue {
	leg := reviewOnlySourceIssue(time.Now().Add(-48 * time.Hour))
	leg.ID = "gt-stale-leg"
	leg.Status = "in_progress"
	return leg
}

// hookedCodeBead is an ordinary bead currently hooked to the calling polecat.
func hookedCodeBead() beads.Issue {
	b := codeSourceIssue()
	b.ID = "gt-code"
	b.Status = "hooked"
	b.Assignee = reviewOnlyAssignee
	return b
}

// activeInProgressReviewLeg is the polecat's review leg after it claimed it
// with bd update --status=in_progress (hq-xa4z): still its active assignment.
func activeInProgressReviewLeg() beads.Issue {
	leg := hookedReviewLeg()
	leg.Status = "in_progress"
	return leg
}

func setupPolecatMqSubmit(t *testing.T, hooked, inProgress []beads.Issue) string {
	t.Helper()
	workDir, _, _ := setupRoutedSourceTestTown(t)
	setupRoutedSubmitCommandTown(t, workDir)
	branch := setupRoutedSubmitGitRepo(t, workDir, true)
	logPath := installReviewOnlyAssignmentsBDRecorder(t, codeSourceIssue(), hooked, inProgress)
	resetMqSubmitFlagsForTest(t)
	t.Setenv("GT_TEST_NUDGE_LOG", filepath.Join(t.TempDir(), "nudge.log"))
	t.Setenv("GT_RIG", "gastown")
	t.Setenv("GT_ROLE", reviewOnlyAssignee)
	t.Setenv("GT_POLECAT", "refuge")
	t.Setenv("BD_ACTOR", reviewOnlyAssignee)
	t.Chdir(workDir)
	mqSubmitBranch = branch
	mqSubmitIssue = "bd-source" // another bead than the review leg
	mqSubmitNoCleanup = true
	return logPath
}

func TestRunMqSubmitRefusesActiveInProgressReviewLeg(t *testing.T) {
	logPath := setupPolecatMqSubmit(t, nil, []beads.Issue{activeInProgressReviewLeg()})
	err := runMqSubmit(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "gt-review-leg") {
		t.Fatalf("mq submit --issue <other bead> with an in_progress review leg = %v, want refusal naming gt-review-leg", err)
	}
	assertNoMergeRequestCreated(t, logPath)
}

func TestRunDoneRefusesActiveInProgressReviewLeg(t *testing.T) {
	workDir, _, _ := setupRoutedSourceTestTown(t)
	setupRoutedSubmitCommandTown(t, workDir)
	setupRoutedSubmitGitRepo(t, workDir, false)
	writeMQSubmitTestFile(t, workDir, "file.txt", "uncommitted review scratch\n")
	head := runGitForMQSubmitTest(t, workDir, "rev-parse", "HEAD")
	logPath := installReviewOnlyAssignmentsBDRecorder(t, codeSourceIssue(), nil, []beads.Issue{activeInProgressReviewLeg()})
	setupReviewOnlyDoneEnv(t, workDir)
	doneSkipVerify = true // doneIssue = bd-source: another bead than the review leg

	err := runDone(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "gt-review-leg") {
		t.Fatalf("gt done --issue <other bead> with an in_progress review leg = %v, want refusal naming gt-review-leg", err)
	}
	if after := runGitForMQSubmitTest(t, workDir, "rev-parse", "HEAD"); after != head {
		t.Fatalf("gt done auto-committed for an active review leg: HEAD %s -> %s", head, after)
	}
	assertNoMergeRequestCreated(t, logPath)
}

// A hooked bead wins over in_progress ones, as in gt done's own hook lookup,
// so a stale in_progress review leg under a reused polecat name does not lock
// the polecat's current code work.
func TestStaleInProgressReviewLegDoesNotLockHookedCodeWork(t *testing.T) {
	t.Run("mq submit", func(t *testing.T) {
		setupPolecatMqSubmit(t, []beads.Issue{hookedCodeBead()}, []beads.Issue{staleReviewLeg()})
		if err := runMqSubmit(nil, nil); err != nil {
			t.Fatalf("a stale in_progress review leg locked gt mq submit: %v", err)
		}
	})
	t.Run("done", func(t *testing.T) {
		workDir, _, _ := setupRoutedSourceTestTown(t)
		setupRoutedSubmitCommandTown(t, workDir)
		setupRoutedSubmitGitRepo(t, workDir, false)
		installReviewOnlyAssignmentsBDRecorder(t, codeSourceIssue(), []beads.Issue{hookedCodeBead()}, []beads.Issue{staleReviewLeg()})
		setupReviewOnlyDoneEnv(t, workDir)
		doneSkipVerify = true
		if err := runDone(nil, nil); err != nil && strings.Contains(err.Error(), "review-only") {
			t.Fatalf("a stale in_progress review leg locked gt done: %v", err)
		}
	})
}

func TestRunMqSubmitPolecatWithoutResolvableIdentityRefuses(t *testing.T) {
	workDir, _, _ := setupRoutedSourceTestTown(t)
	setupRoutedSubmitCommandTown(t, workDir)
	branch := setupRoutedSubmitGitRepo(t, workDir, true)
	logPath := installReviewOnlySourceBDRecorder(t, codeSourceIssue(), hookedReviewLeg())
	resetMqSubmitFlagsForTest(t)
	t.Setenv("GT_TEST_NUDGE_LOG", filepath.Join(t.TempDir(), "nudge.log"))
	t.Setenv("GT_RIG", "gastown")
	t.Setenv("GT_POLECAT", "refuge")
	t.Setenv("GT_ROLE", "")
	t.Setenv("BD_ACTOR", "")
	t.Chdir(workDir)

	mqSubmitBranch = branch
	mqSubmitIssue = "bd-source"
	mqSubmitNoCleanup = true
	err := runMqSubmit(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "identify") {
		t.Fatalf("polecat gt mq submit without a resolvable identity = %v, want fail-closed refusal", err)
	}
	assertNoMergeRequestCreated(t, logPath)
}

func TestRunDoneDeferredAutoCommitsWhenReviewOnlyStatusUnknown(t *testing.T) {
	workDir, _, _ := setupRoutedSourceTestTown(t)
	setupRoutedSubmitCommandTown(t, workDir)
	setupRoutedSubmitGitRepo(t, workDir, false)
	head := runGitForMQSubmitTest(t, workDir, "rev-parse", "HEAD")
	writeMQSubmitTestFile(t, workDir, "file.txt", "unsaved implementation work\n")
	installReviewOnlySourceBDRecorder(t, codeSourceIssue())
	setupReviewOnlyDoneEnv(t, workDir)
	doneStatus = ExitDeferred
	prev := findAssignedBeadsForReviewOnly
	t.Cleanup(func() { findAssignedBeadsForReviewOnly = prev })
	findAssignedBeadsForReviewOnly = func(workDir, agentID string) ([]string, error) {
		return nil, fmt.Errorf("database is locked")
	}

	_ = runDone(nil, nil)
	if after := runGitForMQSubmitTest(t, workDir, "rev-parse", "HEAD"); after == head {
		t.Fatal("bd outage disabled the auto-commit safety net for ordinary work")
	}
}

func TestRunDonePurgedBranchTargetFallsBackToHook(t *testing.T) {
	workDir, _, _ := setupRoutedSourceTestTown(t)
	setupRoutedSubmitCommandTown(t, workDir)
	setupReviewOnlyGitRepoWithoutCommits(t, workDir)
	runGitForMQSubmitTest(t, workDir, "checkout", "-b", "polecat/refuge/gt-purged")
	head := runGitForMQSubmitTest(t, workDir, "rev-parse", "HEAD")
	assignedAt := time.Now().Add(-time.Hour)
	leg := reviewOnlySourceIssue(assignedAt, beads.Comment{
		Author:    reviewOnlyAssignee,
		CreatedAt: assignedAt.Add(time.Minute).UTC().Format(time.RFC3339Nano),
		Text:      "REPORT: ok\nhead_sha: " + head,
	})
	leg.ID = "gt-review-leg"
	logPath := installReviewOnlySourceBDRecorder(t, codeSourceIssue(), leg)
	setupReviewOnlyDoneEnv(t, workDir)
	doneIssue = "" // target comes from the (purged) branch bead

	if err := runDone(nil, nil); err != nil {
		t.Fatalf("purged branch bead blocked gt done before the stale-branch guard: %v", err)
	}
	if log := readSubmitSourceBDLog(t, logPath); !strings.Contains(log, "close gt-review-leg") {
		t.Fatalf("hooked review leg was not completed:\n%s", log)
	}
}
