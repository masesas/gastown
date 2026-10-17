package refinery

import (
	"os"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/rig"
)

func noProver(t *testing.T) {
	t.Helper()
	old := rigProverFor
	rigProverFor = func(string) beads.MergeProver { return nil }
	t.Cleanup(func() { rigProverFor = old })
}

func verdictManager(t *testing.T) *Manager {
	t.Helper()
	return NewManager(&rig.Rig{Name: "testrig", Path: t.TempDir()})
}

// gt-pd1c.59 round 4 (B1): the manual exit records the verdict only on a
// closed MR whose landing git cannot decide.
func TestConfirmLanding_RecordsVerdictOnUnverifiedMR(t *testing.T) {
	noProver(t)
	logPath := installTerminalMRBDStub(t, parkedSourceIssues("closed", "commit_sha: c1\\n", ""))
	stubTownMergeQueue(t, beads.MergeQueueIndex{}, nil)
	res, err := verdictManager(t).ConfirmLanding("gt-wisp-mr1", false, "overseer")
	if err != nil {
		t.Fatalf("ConfirmLanding: %v", err)
	}
	if res.Verdict != beads.LandingNotLanded {
		t.Fatalf("verdict = %q, want not_landed", res.Verdict)
	}
	data, _ := os.ReadFile(logPath)
	if !strings.Contains(string(data), "update gt-wisp-mr1") {
		t.Fatalf("verdict not written to the MR:\n%s", data)
	}
}

func TestConfirmLanding_RefusesWhenAlreadyDecidable(t *testing.T) {
	noProver(t)
	for name, issues := range map[string]map[string]string{
		"open MR":         parkedSourceIssues("open", "commit_sha: c1\\n", ""),
		"merged record":   parkedSourceIssues("closed", "close_reason: merged\\n", ""),
		"earlier verdict": parkedSourceIssues("closed", "landing: not_landed\\n", ""),
	} {
		t.Run(name, func(t *testing.T) {
			logPath := installTerminalMRBDStub(t, issues)
			if _, err := verdictManager(t).ConfirmLanding("gt-wisp-mr1", true, "overseer"); err == nil {
				t.Fatal("a decidable MR must not take a manual verdict")
			}
			data, _ := os.ReadFile(logPath)
			if strings.Contains(string(data), "update gt-wisp-mr1") {
				t.Fatalf("refused verdict must not write the MR:\n%s", data)
			}
		})
	}
}

// Once recorded, the verdict is ordinary proof for the one rule: not_landed
// reopens the parked source, landed closes it.
func TestCloseTerminalMR_HumanVerdictDecidesTheSource(t *testing.T) {
	logPath := installTerminalMRBDStub(t, parkedSourceIssues("closed", "commit_sha: c1\\nlanding: not_landed\\n", ""))
	stubTownMergeQueue(t, beads.MergeQueueIndex{"gt-work1": {{ID: "gt-wisp-mr1", Status: "closed", Description: "source_issue: gt-work1\ntarget: main\ncommit_sha: c1\nlanding: not_landed\n"}}}, nil)
	res, err := closeTerminalMR(beads.New(t.TempDir()), "gt-wisp-mr1", terminalMRCloseOptions{Reason: "rejected: not landed", TownRoot: t.TempDir()})
	if err != nil || res.SourceRelease != beads.ReleaseReopened {
		t.Fatalf("not_landed: (%+v, %v), want the source reopened", res, err)
	}
	if sourceWasClosed(t, logPath) {
		t.Fatal("not_landed must not close the source")
	}

	logPath = installTerminalMRBDStub(t, parkedSourceIssues("closed", "commit_sha: c1\\nlanding: landed\\n", ""))
	res, err = closeTerminalMR(beads.New(t.TempDir()), "gt-wisp-mr1", terminalMRCloseOptions{Reason: string(CloseReasonMerged), TownRoot: t.TempDir()})
	if err != nil || !res.SourceClose.Closed || !sourceWasClosed(t, logPath) {
		t.Fatalf("landed: (%+v, %v), want the source closed", res, err)
	}
}
