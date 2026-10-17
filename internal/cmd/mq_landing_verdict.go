package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/steveyegge/gastown/internal/style"
)

// Flags of the one manual exit for an unverified merge-queue hold
// (gt-pd1c.59): `gt mq reject --confirm-not-landed`, `gt mq post-merge
// --confirm-landed`.
var (
	mqRejectConfirmNotLanded bool
	mqPostMergeConfirmLanded bool
)

// landingVerdictActor returns who records a manual landing verdict. The
// verdict overrides what git could not decide, so it is a human decision
// (humanOrCrewActor).
func landingVerdictActor() (string, error) {
	return humanOrCrewActor("landing verdict")
}

// humanOrCrewActor returns the actor of a human-only action (a landing
// verdict, gt polecat nuke --force on unproven work): a human shell or a crew
// workspace (gtCaller.exempt). Any autonomous agent role (polecat, refinery,
// witness, deacon, mayor, dog, boot, unknown) is refused; a polecat BD_ACTOR
// or GT_POLECAT is refused even with a crew GT_ROLE. This is a guard against
// agents taking the exit by mistake, not an authentication boundary.
func humanOrCrewActor(action string) (string, error) {
	c := gtCallerFrom(os.Getenv)
	if !c.exempt() {
		return "", fmt.Errorf("refusing %s from %s: only a human (overseer) or crew may do this", action, c.describe())
	}
	if c.actor != "" {
		return c.actor, nil
	}
	if user := strings.TrimSpace(os.Getenv("USER")); user != "" {
		return "human:" + user, nil
	}
	return "human", nil
}

// errNotInteractive refuses a human-only action without a human at a terminal.
var errNotInteractive = errors.New("this is a human action: run it from an interactive terminal (stdin and stdout must be a TTY)")

// humanPrompt is the terminal a human-only action asks: tty reports whether
// there is a human at a terminal, in/out are the prompt streams. Commands use
// stdioHumanPrompt; tests pass their own.
type humanPrompt struct {
	tty func() bool
	in  io.Reader
	out io.Writer
}

// stdioHumanPrompt is the process's terminal: a TTY when stdin and stdout
// both are.
func stdioHumanPrompt() humanPrompt {
	return humanPrompt{
		tty: func() bool { return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) },
		in:  os.Stdin,
		out: os.Stdout,
	}
}

// confirm is the second guard of a human-only action: a TTY and the expected
// text typed again. There is no flag to skip it. Every error names the action
// (e.g. "landing verdict", "gt polecat nuke --force"); errors.Is(err,
// errNotInteractive) still holds without a TTY.
func (h humanPrompt) confirm(action, prompt, expected string) error {
	if h.tty == nil || !h.tty() || h.in == nil || h.out == nil {
		return fmt.Errorf("%s: %w", action, errNotInteractive)
	}
	_, _ = fmt.Fprint(h.out, prompt)
	line, err := bufio.NewReader(h.in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s: reading confirmation: %w", action, err)
	}
	if strings.TrimSpace(line) != expected {
		return fmt.Errorf("%s: confirmation did not match %q: nothing done", action, expected)
	}
	return nil
}

// confirmLandingInteractively asks the human to type the MR ID again before a
// landing verdict is recorded.
func confirmLandingInteractively(h humanPrompt, mrID, verdict string) error {
	prompt := fmt.Sprintf("Record landing verdict %q on MR %s? This overrides what git could not decide.\nType the MR ID to confirm: ", verdict, mrID)
	return h.confirm("landing verdict on MR "+mrID, prompt, mrID)
}

// runMQConfirmLanding records a human verdict on an MR whose landing git
// cannot decide and applies it (refinery.Manager.ConfirmLanding). Guards, in
// order: the agent-role env check, then an interactive TTY confirmation at h.
func runMQConfirmLanding(h humanPrompt, rigName, mrID string, landed bool) error {
	actor, err := landingVerdictActor()
	if err != nil {
		return err
	}
	verdict, flag := "not_landed", "gt mq reject --confirm-not-landed"
	if landed {
		verdict, flag = "landed", "gt mq post-merge --confirm-landed"
	}
	if err := confirmLandingInteractively(h, mrID, verdict); err != nil {
		return err
	}
	mgr, _, _, err := getRefineryManager(rigName)
	if err != nil {
		return err
	}
	res, err := mgr.ConfirmLanding(mrID, landed, actor+" via tty", flag)
	if err != nil {
		return err
	}
	fmt.Printf("%s Landing verdict %s recorded on %s by %s\n", style.Bold.Render("✓"), res.Verdict, res.MR.ID, actor)
	switch {
	case res.SourceErr != nil:
		fmt.Printf("  Source %s: %v\n", res.SourceIssue, res.SourceErr)
	case res.SourceClosed:
		fmt.Printf("  Source %s closed\n", res.SourceIssue)
	case res.SourceRelease != "":
		fmt.Printf("  Source %s %s\n", res.SourceIssue, res.SourceRelease)
	}
	return nil
}
