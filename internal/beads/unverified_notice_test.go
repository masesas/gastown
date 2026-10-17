package beads

import (
	"context"
	"strings"
	"testing"
	"time"

	beadsdk "github.com/steveyegge/beads"
)

// gt-pd1c.82: unverified_notified_at is a persistent MR field.
func TestMRFields_UnverifiedNotifiedAtRoundTrip(t *testing.T) {
	in := &MRFields{Branch: "b", SourceIssue: "gt-1", UnverifiedNotifiedAt: "2026-10-16T10:00:00Z"}
	out := ParseMRFields(&Issue{Description: FormatMRFields(in)})
	if out == nil || out.UnverifiedNotifiedAt != in.UnverifiedNotifiedAt {
		t.Fatalf("round trip = %+v", out)
	}
	// SetMRFields replaces the old line instead of duplicating it.
	desc := SetMRFields(&Issue{Description: "branch: b\nunverified_notified_at: 2026-10-01T00:00:00Z\nnotes"}, in)
	if strings.Count(desc, "unverified_notified_at:") != 1 || !strings.Contains(desc, in.UnverifiedNotifiedAt) || !strings.Contains(desc, "notes") {
		t.Fatalf("SetMRFields = %q", desc)
	}
}

func TestMarkUnverifiedNotified_WritesMarkAndKeepsFields(t *testing.T) {
	store := newMockStorage()
	b := newTestBeads(store)
	_ = store.CreateIssue(context.Background(), &beadsdk.Issue{Title: "mr", Description: "branch: b\nsource_issue: gt-1\nrig: r\nprose line"}, "test")

	at := time.Date(2026, 10, 16, 10, 0, 0, 0, time.FixedZone("x", 7*3600))
	if err := b.MarkUnverifiedNotified("test-1", at); err != nil {
		t.Fatalf("MarkUnverifiedNotified: %v", err)
	}
	got, err := b.Show("test-1")
	if err != nil {
		t.Fatal(err)
	}
	f := ParseMRFields(got)
	if f == nil || f.UnverifiedNotifiedAt != "2026-10-16T03:00:00Z" || f.SourceIssue != "gt-1" || f.Rig != "r" {
		t.Fatalf("fields after mark = %+v", f)
	}
	if !strings.Contains(got.Description, "prose line") {
		t.Fatalf("other description content lost: %q", got.Description)
	}
}

func TestMarkUnverifiedNotified_MissingMRIsError(t *testing.T) {
	if err := newTestBeads(newMockStorage()).MarkUnverifiedNotified("test-404", time.Now()); err == nil {
		t.Fatal("marking a missing MR must fail")
	}
}

func TestUnverifiedExitCommands(t *testing.T) {
	notLanded, landed := UnverifiedExitCommands("gastown", "gt-wisp-1")
	if notLanded != "gt mq reject gastown gt-wisp-1 --confirm-not-landed" || landed != "gt mq post-merge gastown gt-wisp-1 --confirm-landed" {
		t.Fatalf("commands = %q, %q", notLanded, landed)
	}
	if notLanded, _ := UnverifiedExitCommands("", "gt-wisp-1"); !strings.Contains(notLanded, "reject <rig> gt-wisp-1") {
		t.Fatalf("unknown rig must show the <rig> placeholder: %q", notLanded)
	}
}
