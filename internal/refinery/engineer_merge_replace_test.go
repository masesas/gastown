package refinery

import (
	"strings"
	"testing"
)

// gt-pd1c.86 R2-1: merge-shape decisions read a commit's parents from the
// commit itself. A replace ref (git replace M X) that gives M the parents an
// attacker wants must not make M "a two-parent merge of commit_sha".

func TestMergeOne_RecordedMergeForgedByReplaceRefIsRefused(t *testing.T) {
	f := newMergeOneFixture(t)
	landed := f.landByHand(t)
	commitSHA := run(t, f.workDir, "git", "rev-parse", "origin/"+f.branch)
	// M: a pushed merge of something else (its second parent is "side").
	run(t, f.workDir, "git", "checkout", "-q", "-b", "side", landed+"~1")
	writeFile(t, f.workDir, "side.txt", "side\n")
	run(t, f.workDir, "git", "add", ".")
	run(t, f.workDir, "git", "commit", "-q", "-m", "side")
	run(t, f.workDir, "git", "checkout", "-q", "main")
	run(t, f.workDir, "git", "merge", "--no-ff", "-q", "-m", "merge side", "side")
	run(t, f.workDir, "git", "push", "-q", "origin", "main")
	run(t, f.workDir, "git", "branch", "-D", "side")
	m := f.originMain(t)
	// Forge: M now "has" parents (landed, commit_sha) for plain git.
	fake := run(t, f.workDir, "git", "commit-tree", m+"^{tree}", "-p", landed, "-p", commitSHA, "-m", "forged")
	run(t, f.workDir, "git", "replace", m, fake)
	if got := run(t, f.workDir, "git", "rev-parse", m+"^2"); got != commitSHA {
		t.Fatalf("forgery not in effect for plain git: %s^2 = %s", m, got)
	}
	f.store.issues[fixtureMR].Description += "\nclose_reason: merged\nmerge_commit: " + m

	out := f.mergeOne(t)
	if out.Kind != OutcomeNotEligible || out.AlreadyLanded || !strings.Contains(out.Message, "second parent") {
		t.Fatalf("outcome = %s %q (already_landed=%v), want not_eligible: second parent", out.Kind, out.Message, out.AlreadyLanded)
	}
	if closes := f.store.closeCalls(); len(closes) != 0 {
		t.Fatalf("closed %v on a merge forged by a replace ref", closes)
	}
}

func TestVerifyMergeShape_RefusesOctopusForgedByReplaceRef(t *testing.T) {
	workDir, g, _ := testGitRepo(t)
	e, _ := finalizeTestEngineer(t, workDir, g)
	base := run(t, workDir, "git", "rev-parse", "main")
	createFeatureBranch(t, workDir, "polecat/a/1", "a1.txt", "1\n")
	createFeatureBranch(t, workDir, "polecat/a/2", "a2.txt", "2\n")
	h1 := run(t, workDir, "git", "rev-parse", "polecat/a/1")
	run(t, workDir, "git", "merge", "--no-ff", "-q", "-m", "octopus", "polecat/a/1", "polecat/a/2")
	tip := run(t, workDir, "git", "rev-parse", "HEAD")
	fake := run(t, workDir, "git", "commit-tree", tip+"^{tree}", "-p", base, "-p", h1, "-m", "forged two-parent")
	run(t, workDir, "git", "replace", tip, fake)
	if err := e.verifyMergeShape(tip, base, []string{h1}); err == nil {
		t.Fatal("an octopus merge disguised by a replace ref was accepted")
	}
}
