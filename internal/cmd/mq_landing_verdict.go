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

// humanConfirmTTY reports whether stdin and stdout are terminals; tests
// override it.
var humanConfirmTTY = func() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

// humanConfirmInput/Output are the prompt streams; tests override them.
var (
	humanConfirmInput  io.Reader = os.Stdin
	humanConfirmOutput io.Writer = os.Stdout
)

// confirmHumanInteractively is the second guard of a human-only action: a TTY
// and the expected text typed again. There is no flag to skip it.
func confirmHumanInteractively(prompt, expected string) error {
	if !humanConfirmTTY() {
		return errNotInteractive
	}
	_, _ = fmt.Fprint(humanConfirmOutput, prompt)
	line, err := bufio.NewReader(humanConfirmInput).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("reading confirmation: %w", err)
	}
	if strings.TrimSpace(line) != expected {
		return fmt.Errorf("confirmation did not match %q: nothing done", expected)
	}
	return nil
}

// confirmLandingInteractively asks the human to type the MR ID again before a
// landing verdict is recorded.
func confirmLandingInteractively(mrID, verdict string) error {
	prompt := fmt.Sprintf("Record landing verdict %q on MR %s? This overrides what git could not decide.\nType the MR ID to confirm: ", verdict, mrID)
	if err := confirmHumanInteractively(prompt, mrID); err != nil {
		if errors.Is(err, errNotInteractive) {
			return err
		}
		return fmt.Errorf("MR %s: %w", mrID, err)
	}
	return nil
}

// runMQConfirmLanding records a human verdict on an MR whose landing git
// cannot decide and applies it (refinery.Manager.ConfirmLanding). Guards, in
// order: the agent-role env check, then an interactive TTY confirmation.
func runMQConfirmLanding(rigName, mrID string, landed bool) error {
	actor, err := landingVerdictActor()
	if err != nil {
		return err
	}
	verdict, flag := "not_landed", "gt mq reject --confirm-not-landed"
	if landed {
		verdict, flag = "landed", "gt mq post-merge --confirm-landed"
	}
	if err := confirmLandingInteractively(mrID, verdict); err != nil {
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
