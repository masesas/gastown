package cmd

import (
	"testing"

	"github.com/steveyegge/gastown/internal/mail"
)

// gt-pd1c.78 F10: a "Merge Request Completed:" mail in the mayor's inbox used
// to close its "Source:" bead with "Merged in <commit sha>": a free-text
// close outside the merge engine that also unblocked merge-blocks dependents,
// on the word of any mail sender. Nothing in Gas Town sends that subject (the
// refinery mails MERGED to the witness and finalizes in Go), so the callback
// is gone: such a mail is an unknown callback and is left unprocessed.
func TestCallbacks_MergeCompletedMailClosesNothing(t *testing.T) {
	msg := &mail.Message{ID: "m1", From: "posin/refinery", Subject: "Merge Request Completed: polecat/nux/kp-1",
		Body: "MR: kp-wisp-1\nSource: kp-victim\nCommit: 5ef62f7"}
	if got := classifyCallback(msg.Subject); got != CallbackUnknown {
		t.Fatalf("classifyCallback = %s, want unknown (no merge-completed callback)", got)
	}
	res := processCallback(t.TempDir(), msg, false)
	if res.Handled || res.Error != nil {
		t.Fatalf("processCallback = %+v, want an unhandled unknown callback", res)
	}
}
