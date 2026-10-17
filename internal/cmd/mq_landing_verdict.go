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
// verdict overrides what git could not decide, so it is a human decision:
// any autonomous agent role (polecat, refinery, witness, deacon, mayor, dog)
// is refused; a human shell (no GT_ROLE) or a crew workspace may record it.
// A polecat BD_ACTOR is refused even without GT_ROLE. This is a guard against
// agents taking the exit by mistake, not an authentication boundary.
func landingVerdictActor() (string, error) {
	actor := strings.TrimSpace(os.Getenv("BD_ACTOR"))
	if isPolecatActor(actor) {
		return "", fmt.Errorf("refusing landing verdict from polecat %s: only a human (overseer) or crew may record it", actor)
	}
	if env := strings.TrimSpace(os.Getenv(EnvGTRole)); env != "" {
		if role, _, _ := parseRoleString(env); role != RoleCrew {
			return "", fmt.Errorf("refusing landing verdict from agent role %q: only a human (overseer) or crew may record it", env)
		}
	}
	if actor == "" {
		if user := strings.TrimSpace(os.Getenv("USER")); user != "" {
			return "human:" + user, nil
		}
		return "human", nil
	}
	return actor, nil
}

// errNotInteractive refuses a landing verdict without a human at a terminal.
var errNotInteractive = errors.New("a landing verdict is a human action: run it from an interactive terminal (stdin and stdout must be a TTY)")

// landingVerdictTTY reports whether stdin and stdout are terminals; tests
// override it.
var landingVerdictTTY = func() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

// landingVerdictInput/Output are the prompt streams; tests override them.
var (
	landingVerdictInput  io.Reader = os.Stdin
	landingVerdictOutput io.Writer = os.Stdout
)

// confirmLandingInteractively is the second guard of the manual exit: a TTY
// and the MR ID typed again. There is no flag to skip it.
func confirmLandingInteractively(mrID, verdict string) error {
	if !landingVerdictTTY() {
		return errNotInteractive
	}
	_, _ = fmt.Fprintf(landingVerdictOutput, "Record landing verdict %q on MR %s? This overrides what git could not decide.\nType the MR ID to confirm: ", verdict, mrID)
	line, err := bufio.NewReader(landingVerdictInput).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("reading confirmation: %w", err)
	}
	if strings.TrimSpace(line) != mrID {
		return fmt.Errorf("confirmation did not match MR %s: nothing recorded", mrID)
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
