package beads

import (
	"context"
	"errors"
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
	// F-3: values that are not safe IDs are never inserted raw.
	for _, tc := range []struct{ rig, mr string }{
		{"gastown; rm -rf ~", "gt-wisp-1"},
		{"../etc", "gt-wisp-1"},
		{"gastown", "gt-1 && curl evil"},
		{"gastown", "$(id)"},
		{"gastown", "-x"},
	} {
		notLanded, landed := UnverifiedExitCommands(tc.rig, tc.mr)
		for _, cmd := range []string{notLanded, landed} {
			if strings.ContainsAny(cmd, ";&$()/") || strings.Contains(cmd, "rm -rf") || strings.Contains(cmd, "curl") || strings.Contains(cmd, " -x ") {
				t.Errorf("(%q, %q): raw value leaked into %q", tc.rig, tc.mr, cmd)
			}
		}
		if !strings.Contains(notLanded, "<rig>") && !strings.Contains(notLanded, "<mr-id>") {
			t.Errorf("(%q, %q): want a placeholder, got %q", tc.rig, tc.mr, notLanded)
		}
	}
}

func TestIsSafeID(t *testing.T) {
	for s, want := range map[string]bool{
		"gt-wisp-1": true, "gastown": true, "my_rig.2": true,
		"": false, ".": false, "..": false, "../x": false, "a/b": false, "a b": false, "-x": false, "a;b": false, "a\nb": false,
		// argv injection (gt-pd1c.43): flag-like and wrapped IDs are not safe.
		"--db=/tmp/x": false, "external:r:gt-a": false, "gt-a'": false, "gt-pd1c.43": true,
	} {
		if got := IsSafeID(s); got != want {
			t.Errorf("IsSafeID(%q) = %v, want %v", s, got, want)
		}
	}
}

// One computation: Detail gives hold, MR, rig and a display-safe reason.
func TestMergeQueueDetail_UnverifiedReasonIsSafe(t *testing.T) {
	mr := &Issue{ID: "gt-wisp-9", Status: "closed", Description: "source_issue: gt-1\nrig: gastown\ntarget: main\ncommit_sha: c1c1c1c\n"}
	gitErr := errors.New("fatal: unable to access 'https://x-access-token:ghp_secret@github.com/o/r/'\nline two " + strings.Repeat("x", 600))
	q := NewMergeQueue(MergeQueueIndex{"gt-1": {mr}}, nil, func(string) MergeProver { return errProver{gitErr} })
	d, err := q.Detail("gt-1")
	if err != nil || d.Hold != HoldUnverified || d.MRID != "gt-wisp-9" || d.MR != mr || d.Rig != "gastown" {
		t.Fatalf("Detail = %+v, %v", d, err)
	}
	if strings.Contains(d.Reason, "ghp_secret") || !strings.Contains(d.Reason, "://***@github.com") {
		t.Errorf("credentials not masked: %q", d.Reason)
	}
	if strings.Contains(d.Reason, "\n") || len(d.Reason) > maxHoldReasonLen+len("…") {
		t.Errorf("reason must be one bounded line, got %d bytes: %q", len(d.Reason), d.Reason)
	}
	if hold, mrID, _ := q.Hold("gt-1"); hold != d.Hold || mrID != d.MRID {
		t.Errorf("Hold and Detail disagree: %v %q", hold, mrID)
	}
	if d, err := NewMergeQueue(nil, errors.New("down"), nil).Detail("gt-1"); err == nil || d.Hold != HoldNone {
		t.Errorf("unreadable queue: %+v, %v", d, err)
	}
}

type errProver struct{ err error }

func (p errProver) VerifyPushedCommitReachableFromPushTarget(remote, branch, commit string) error {
	return p.err
}
