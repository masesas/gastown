package cmd

import (
	"fmt"
	"os"
	"strings"

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

// runMQConfirmLanding records a human verdict on an MR whose landing git
// cannot decide and applies it (refinery.Manager.ConfirmLanding).
func runMQConfirmLanding(rigName, mrID string, landed bool) error {
	actor, err := landingVerdictActor()
	if err != nil {
		return err
	}
	mgr, _, _, err := getRefineryManager(rigName)
	if err != nil {
		return err
	}
	res, err := mgr.ConfirmLanding(mrID, landed, actor)
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
