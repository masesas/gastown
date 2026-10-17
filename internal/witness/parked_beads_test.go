package witness

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/mail"
)

// unverifiedQueue holds gt-parked1 as beads.HoldUnverified: its only MR is
// closed by hand (no close_reason, no commit) and there is no git prover.
// notifiedAt is the MR's unverified_notified_at ("" = never notified).
func unverifiedQueue(notifiedAt string) *beads.MergeQueue {
	desc := "source_issue: gt-parked1\nrig: otherrig\nbranch: polecat/alpha/gt-parked1\n"
	if notifiedAt != "" {
		desc += "unverified_notified_at: " + notifiedAt + "\n"
	}
	mr := &beads.Issue{ID: "gt-wisp-mr9", Status: "closed", ClosedAt: "2026-10-15T08:00:00Z", Description: desc}
	return beads.NewMergeQueue(beads.MergeQueueIndex{"gt-parked1": {mr}}, nil, nil)
}

// noticeRecorder stubs the overseer mail and the MR mark.
type noticeRecorder struct {
	sent    []*mail.Message
	marks   []string
	sendErr error
	markErr error
}

func (r *noticeRecorder) install(t *testing.T) {
	t.Helper()
	oldSend, oldMark := sendOverseerMail, markUnverifiedNotified
	sendOverseerMail = func(_ *mail.Router, msg *mail.Message) error {
		if r.sendErr != nil {
			return r.sendErr
		}
		r.sent = append(r.sent, msg)
		return nil
	}
	markUnverifiedNotified = func(_ string, mrID string, at time.Time) error {
		if r.markErr != nil {
			return r.markErr
		}
		r.marks = append(r.marks, mrID+"@"+at.UTC().Format(time.RFC3339))
		return nil
	}
	t.Cleanup(func() { sendOverseerMail, markUnverifiedNotified = oldSend, oldMark })
}

var noticeNow = time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC) // 26h after the MR closed

func testNotice() overseerNotice {
	return overseerNotice{workDir: "/town/testrig", rig: "testrig", interval: 24 * time.Hour, now: noticeNow}
}

// gt-pd1c.82: the first patrol that sees an unverified hold mails the
// overseer with everything needed to decide, then marks the MR.
func TestNotifyUnverifiedHold_FirstNotice(t *testing.T) {
	rec := &noticeRecorder{}
	rec.install(t)

	sent, err := testNotice().notify(unverifiedQueue(""), "gt-parked1")
	if err != nil || !sent {
		t.Fatalf("notify = (%v, %v), want sent", sent, err)
	}
	if len(rec.sent) != 1 {
		t.Fatalf("want 1 mail, got %d", len(rec.sent))
	}
	msg := rec.sent[0]
	if msg.To != "overseer" || msg.From != "testrig/witness" || msg.Priority != mail.PriorityHigh {
		t.Errorf("mail routing = to %q from %q priority %v", msg.To, msg.From, msg.Priority)
	}
	if !strings.Contains(msg.Subject, "gt-parked1") || strings.Contains(msg.Subject, "REMINDER") {
		t.Errorf("subject = %q", msg.Subject)
	}
	for _, want := range []string{
		"gt-parked1", "gt-wisp-mr9", "otherrig",
		"no commit_sha", // reason from beads.UnverifiedReason
		"26h",           // age since the MR closed
		"gt mq reject otherrig gt-wisp-mr9 --confirm-not-landed",
		"gt mq post-merge otherrig gt-wisp-mr9 --confirm-landed",
	} {
		if !strings.Contains(msg.Body, want) {
			t.Errorf("body missing %q:\n%s", want, msg.Body)
		}
	}
	if len(rec.marks) != 1 || rec.marks[0] != "gt-wisp-mr9@2026-10-16T10:00:00Z" {
		t.Errorf("marks = %v, want the MR marked at now", rec.marks)
	}
}

// Dedup: once the MR carries a recent mark, the next patrol stays quiet.
func TestNotifyUnverifiedHold_DedupWithinInterval(t *testing.T) {
	rec := &noticeRecorder{}
	rec.install(t)

	sent, err := testNotice().notify(unverifiedQueue(noticeNow.Add(-time.Hour).Format(time.RFC3339)), "gt-parked1")
	if err != nil || sent {
		t.Fatalf("notify = (%v, %v), want nothing sent", sent, err)
	}
	if len(rec.sent) != 0 || len(rec.marks) != 0 {
		t.Fatalf("second patrol must not mail or mark: sent %d, marks %v", len(rec.sent), rec.marks)
	}
}

// A hold that outlives the interval gets a reminder, and the mark moves on.
func TestNotifyUnverifiedHold_ReminderAfterInterval(t *testing.T) {
	rec := &noticeRecorder{}
	rec.install(t)

	last := noticeNow.Add(-25 * time.Hour).Format(time.RFC3339)
	sent, err := testNotice().notify(unverifiedQueue(last), "gt-parked1")
	if err != nil || !sent {
		t.Fatalf("notify = (%v, %v), want a reminder", sent, err)
	}
	if len(rec.sent) != 1 || !strings.Contains(rec.sent[0].Subject, "REMINDER") {
		t.Fatalf("want one REMINDER mail, got %v", rec.sent)
	}
	if !strings.Contains(rec.sent[0].Body, last) {
		t.Errorf("reminder should say when the last notice went out:\n%s", rec.sent[0].Body)
	}
	if len(rec.marks) != 1 {
		t.Errorf("reminder must move the mark, marks = %v", rec.marks)
	}
}

// An unreadable mark never silences the overseer.
func TestNotifyUnverifiedHold_UnparsableMarkNotifies(t *testing.T) {
	rec := &noticeRecorder{}
	rec.install(t)
	if sent, err := testNotice().notify(unverifiedQueue("yesterday-ish"), "gt-parked1"); err != nil || !sent {
		t.Fatalf("notify = (%v, %v), want sent", sent, err)
	}
}

// Fail closed: a failed send leaves the MR unmarked so the next patrol retries.
func TestNotifyUnverifiedHold_SendFailureDoesNotMark(t *testing.T) {
	rec := &noticeRecorder{sendErr: errors.New("mail: dolt down")}
	rec.install(t)

	sent, err := testNotice().notify(unverifiedQueue(""), "gt-parked1")
	if err == nil || sent {
		t.Fatalf("notify = (%v, %v), want the send error", sent, err)
	}
	if len(rec.marks) != 0 {
		t.Fatalf("a failed send must not mark the MR, marks = %v", rec.marks)
	}
}

// A mark failure after a good send is reported (the next patrol resends:
// a duplicate is preferred over silence).
func TestNotifyUnverifiedHold_MarkFailureReported(t *testing.T) {
	rec := &noticeRecorder{markErr: errors.New("bd update failed")}
	rec.install(t)
	sent, err := testNotice().notify(unverifiedQueue(""), "gt-parked1")
	if !sent || err == nil {
		t.Fatalf("notify = (%v, %v), want sent with the mark error", sent, err)
	}
}

// Only beads.HoldUnverified is announced: pending, landed, free and an
// unreadable queue send nothing.
func TestNotifyUnverifiedHold_OtherHoldsSendNothing(t *testing.T) {
	rec := &noticeRecorder{}
	rec.install(t)
	for name, q := range map[string]*beads.MergeQueue{
		"none":       heldQueue(beads.HoldNone, "", nil),
		"pending":    heldQueue(beads.HoldPending, "gt-wisp-mr1", nil),
		"landed":     heldQueue(beads.HoldLanded, "gt-wisp-mr1", nil),
		"unreadable": heldQueue(beads.HoldNone, "", errors.New("dolt down")),
	} {
		if sent, err := testNotice().notify(q, "gt-parked1"); sent || err != nil {
			t.Errorf("%s: notify = (%v, %v), want nothing", name, sent, err)
		}
	}
	if len(rec.sent) != 0 || len(rec.marks) != 0 {
		t.Fatalf("non-unverified holds must not mail or mark: sent %d, marks %v", len(rec.sent), rec.marks)
	}
}

// The patrol scan of parked beads (gt patrol scan) settles landed beads and
// announces unverified ones; a second patrol after the mark is quiet.
func TestScanParkedBeads_NotifiesOncePerMR(t *testing.T) {
	rec := &noticeRecorder{}
	rec.install(t)
	notified := ""
	markUnverifiedNotified = func(_ string, mrID string, at time.Time) error {
		notified = at.UTC().Format(time.RFC3339)
		rec.marks = append(rec.marks, mrID)
		return nil
	}
	old := witnessMergeQueue
	witnessMergeQueue = func(string) *beads.MergeQueue { return unverifiedQueue(notified) }
	t.Cleanup(func() { witnessMergeQueue = old })

	bd, mock := mockBd(
		func(args []string) (string, error) {
			if len(args) > 0 && args[0] == "list" {
				return `[{"id":"gt-parked1","assignee":""},{"id":"gt-busy1","assignee":"testrig/polecats/alpha"}]`, nil
			}
			return "[]", nil
		},
		func(args []string) error { return nil },
	)
	dir := t.TempDir()
	first := ScanParkedBeads(bd, dir, "testrig", nil)
	if len(first.Errors) != 0 || len(first.Notified) != 1 || first.Notified[0] != "gt-parked1" || first.Checked != 1 {
		t.Fatalf("first scan = %+v, want gt-parked1 notified", first)
	}
	second := ScanParkedBeads(bd, dir, "testrig", nil)
	if len(second.Notified) != 0 || len(rec.sent) != 1 {
		t.Fatalf("second scan notified %v (mails %d), want dedup", second.Notified, len(rec.sent))
	}
	for _, call := range mock.calls {
		if strings.HasPrefix(call, "close") || strings.Contains(call, "--status=open") {
			t.Errorf("unverified work must be neither closed nor reopened: %s", call)
		}
	}
}

// A failed list is an error, never a silent "nothing parked".
func TestScanParkedBeads_ListErrorReported(t *testing.T) {
	bd, _ := mockBd(
		func(args []string) (string, error) { return "", errors.New("bd: connection refused") },
		func(args []string) error { return nil },
	)
	if res := ScanParkedBeads(bd, t.TempDir(), "testrig", nil); len(res.Errors) == 0 {
		t.Fatal("a failed list must be reported")
	}
}

// gt up's orphan recovery uses the same parked-bead rule, so it notifies too.
func TestDetectOrphanedBeads_NotifiesUnverifiedParkedBead(t *testing.T) {
	installFakeTmuxNoServer(t)
	rec := &noticeRecorder{}
	rec.install(t)
	old := witnessMergeQueue
	witnessMergeQueue = func(string) *beads.MergeQueue { return unverifiedQueue("") }
	t.Cleanup(func() { witnessMergeQueue = old })
	bd, _ := mockBd(
		func(args []string) (string, error) {
			if len(args) > 0 && args[0] == "list" && strings.Contains(strings.Join(args, " "), "--status=in_progress") {
				return `[{"id":"gt-parked1","assignee":""}]`, nil
			}
			return "[]", nil
		},
		func(args []string) error { return nil },
	)
	DetectOrphanedBeads(bd, t.TempDir(), "testrig", nil)
	if len(rec.sent) != 1 || len(rec.marks) != 1 {
		t.Fatalf("want one notice and one mark, got %d mails, marks %v", len(rec.sent), rec.marks)
	}
}

// Fail closed with the real sender: no router means no mail and no mark.
func TestScanParkedBeads_NoRouterReportsAndDoesNotMark(t *testing.T) {
	oldMark := markUnverifiedNotified
	marked := false
	markUnverifiedNotified = func(string, string, time.Time) error { marked = true; return nil }
	old := witnessMergeQueue
	witnessMergeQueue = func(string) *beads.MergeQueue { return unverifiedQueue("") }
	t.Cleanup(func() { markUnverifiedNotified, witnessMergeQueue = oldMark, old })
	bd, _ := mockBd(
		func(args []string) (string, error) { return `[{"id":"gt-parked1","assignee":""}]`, nil },
		func(args []string) error { return nil },
	)
	res := ScanParkedBeads(bd, t.TempDir(), "testrig", nil)
	if marked || len(res.Notified) != 0 || len(res.Errors) != 1 || !errors.Is(res.Errors[0], errNoMailRouter) {
		t.Fatalf("scan = %+v, marked = %v; want the send error and no mark", res, marked)
	}
}

// Landed parked beads are closed by the same scan and reported.
func TestScanParkedBeads_ClosesLanded(t *testing.T) {
	stubMergeQueueHold(t, beads.HoldLanded, "gt-wisp-mr1", nil)
	bd, mock := mockBd(
		func(args []string) (string, error) { return `[{"id":"gt-parked1","assignee":""}]`, nil },
		func(args []string) error { return nil },
	)
	res := ScanParkedBeads(bd, t.TempDir(), "testrig", nil)
	if len(res.Closed) != 1 || len(res.Errors) != 0 || !strings.Contains(strings.Join(mock.calls, "\n"), "close gt-parked1") {
		t.Fatalf("scan = %+v, calls %v", res, mock.calls)
	}
}

func TestHoldAgeAndShortDuration(t *testing.T) {
	n := testNotice()
	for _, tc := range []struct {
		mr   *beads.Issue
		want string
	}{
		{nil, "unknown (MR has no closed_at)"},
		{&beads.Issue{ClosedAt: "not a time"}, `unknown (MR closed_at "not a time")`},
		{&beads.Issue{ClosedAt: "2026-10-16T09:30:00Z"}, "30m (MR closed 2026-10-16T09:30:00Z)"},
		{&beads.Issue{ClosedAt: "2026-10-17T09:30:00Z"}, "0m (MR closed 2026-10-17T09:30:00Z)"}, // clock skew
	} {
		if got := n.holdAge(tc.mr); got != tc.want {
			t.Errorf("holdAge(%v) = %q, want %q", tc.mr, got, tc.want)
		}
	}
}
