package witness

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/mail"
)

// unverifiedMR is gt-parked1's only MR: closed by hand (no close_reason, no
// commit), so with no git prover the bead is held as beads.HoldUnverified.
// notifiedAt is its unverified_notified_at ("" = never notified).
func unverifiedMR(rig, notifiedAt string) *beads.Issue {
	desc := "source_issue: gt-parked1\nrig: " + rig + "\nbranch: polecat/alpha/gt-parked1\n"
	if notifiedAt != "" {
		desc += "unverified_notified_at: " + notifiedAt + "\n"
	}
	return &beads.Issue{ID: "gt-wisp-mr9", Status: "closed", ClosedAt: "2026-10-15T08:00:00Z", Description: desc}
}

func unverifiedQueue(notifiedAt string) *beads.MergeQueue {
	return beads.NewMergeQueue(beads.MergeQueueIndex{"gt-parked1": {unverifiedMR("otherrig", notifiedAt)}}, nil, nil)
}

// detailOf is the merge queue's verdict on gt-parked1 in q.
func detailOf(t *testing.T, q *beads.MergeQueue) beads.HoldDetail {
	t.Helper()
	d, err := q.Detail("gt-parked1")
	if err != nil {
		t.Fatalf("Detail: %v", err)
	}
	return d
}

// noticeRecorder captures the overseer mail and the MR marks of a notice.
type noticeRecorder struct {
	sent    []*mail.Message
	marks   []string
	sendErr error
	markErr error
}

var noticeNow = time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC) // 26h after the MR closed

// notice is an overseerNotice wired to the recorder.
func (r *noticeRecorder) notice() overseerNotice {
	return overseerNotice{
		rig: "testrig", interval: 24 * time.Hour, now: noticeNow,
		send: func(msg *mail.Message) error {
			if r.sendErr != nil {
				return r.sendErr
			}
			r.sent = append(r.sent, msg)
			return nil
		},
		mark: func(mrID string, at time.Time) error {
			if r.markErr != nil {
				return r.markErr
			}
			r.marks = append(r.marks, mrID+"@"+at.UTC().Format(time.RFC3339))
			return nil
		},
	}
}

// gt-pd1c.82: the first patrol that sees an unverified hold mails the
// overseer with everything needed to decide, then marks the MR.
func TestNotifyUnverifiedHold_FirstNotice(t *testing.T) {
	rec := &noticeRecorder{}
	sent, err := rec.notice().notify("gt-parked1", detailOf(t, unverifiedQueue("")))
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
		"no commit_sha", // reason from the merge queue's verdict
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

// Dedup: once the MR carries a recent mark, the next patrol stays quiet; a
// mark slightly in the future (clock skew) still counts.
func TestNotifyUnverifiedHold_DedupWithinInterval(t *testing.T) {
	for _, mark := range []time.Time{noticeNow.Add(-time.Hour), noticeNow.Add(4 * time.Minute)} {
		rec := &noticeRecorder{}
		sent, err := rec.notice().notify("gt-parked1", detailOf(t, unverifiedQueue(mark.Format(time.RFC3339))))
		if err != nil || sent || len(rec.sent) != 0 || len(rec.marks) != 0 {
			t.Fatalf("mark %v: notify = (%v, %v), sent %d, marks %v; want nothing", mark, sent, err, len(rec.sent), rec.marks)
		}
	}
}

// A hold that outlives the interval gets a reminder, and the mark moves on.
func TestNotifyUnverifiedHold_ReminderAfterInterval(t *testing.T) {
	rec := &noticeRecorder{}
	last := noticeNow.Add(-25 * time.Hour).Format(time.RFC3339)
	sent, err := rec.notice().notify("gt-parked1", detailOf(t, unverifiedQueue(last)))
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

// An invalid mark (unparsable, or beyond the skew tolerance in the future)
// never silences the overseer.
func TestNotifyUnverifiedHold_InvalidMarkNotifies(t *testing.T) {
	for _, mark := range []string{"yesterday-ish", noticeNow.Add(6 * time.Minute).Format(time.RFC3339), "2099-01-01T00:00:00Z"} {
		rec := &noticeRecorder{}
		sent, err := rec.notice().notify("gt-parked1", detailOf(t, unverifiedQueue(mark)))
		if err != nil || !sent {
			t.Fatalf("mark %q: notify = (%v, %v), want sent", mark, sent, err)
		}
		if strings.Contains(rec.sent[0].Subject, "REMINDER") {
			t.Errorf("mark %q: an invalid mark is no previous notice: %q", mark, rec.sent[0].Subject)
		}
	}
}

// Fail closed: a failed send leaves the MR unmarked so the next patrol retries.
func TestNotifyUnverifiedHold_SendFailureDoesNotMark(t *testing.T) {
	rec := &noticeRecorder{sendErr: errors.New("mail: dolt down")}
	sent, err := rec.notice().notify("gt-parked1", detailOf(t, unverifiedQueue("")))
	if err == nil || sent {
		t.Fatalf("notify = (%v, %v), want the send error", sent, err)
	}
	if len(rec.marks) != 0 {
		t.Fatalf("a failed send must not mark the MR, marks = %v", rec.marks)
	}
}

// A mark failure after a good send is reported (the next patrol resends:
// at least once, a duplicate is preferred over silence).
func TestNotifyUnverifiedHold_MarkFailureReported(t *testing.T) {
	rec := &noticeRecorder{markErr: errors.New("bd update failed")}
	sent, err := rec.notice().notify("gt-parked1", detailOf(t, unverifiedQueue("")))
	if !sent || err == nil {
		t.Fatalf("notify = (%v, %v), want sent with the mark error", sent, err)
	}
}

// Only beads.HoldUnverified is announced: pending, landed and free send nothing.
func TestNotifyUnverifiedHold_OtherHoldsSendNothing(t *testing.T) {
	rec := &noticeRecorder{}
	for name, q := range map[string]*beads.MergeQueue{
		"none":    heldQueue(beads.HoldNone, "", nil),
		"pending": heldQueue(beads.HoldPending, "gt-wisp-mr1", nil),
		"landed":  heldQueue(beads.HoldLanded, "gt-wisp-mr1", nil),
	} {
		if sent, err := rec.notice().notify("gt-parked1", detailOf(t, q)); sent || err != nil {
			t.Errorf("%s: notify = (%v, %v), want nothing", name, sent, err)
		}
	}
	if len(rec.sent) != 0 || len(rec.marks) != 0 {
		t.Fatalf("non-unverified holds must not mail or mark: sent %d, marks %v", len(rec.sent), rec.marks)
	}
}

// F-3: an MR rig that is not a safe ID is never put into the commands; the
// witness rig is used instead, or a placeholder when that is unusable too.
func TestNotifyUnverifiedHold_UnsafeRigNeverInserted(t *testing.T) {
	for _, tc := range []struct{ witnessRig, wantRig string }{{"testrig", "testrig"}, {"bad rig;x", "<rig>"}} {
		rec := &noticeRecorder{}
		n := rec.notice()
		n.rig = tc.witnessRig
		q := beads.NewMergeQueue(beads.MergeQueueIndex{"gt-parked1": {unverifiedMR("evil; rm -rf ~", "")}}, nil, nil)
		if sent, err := n.notify("gt-parked1", detailOf(t, q)); !sent || err != nil {
			t.Fatalf("notify = (%v, %v)", sent, err)
		}
		body := rec.sent[0].Body
		if strings.Contains(body, "rm -rf") || !strings.Contains(body, "gt mq reject "+tc.wantRig+" gt-wisp-mr9 --confirm-not-landed") {
			t.Errorf("witness rig %q: unsafe rig handling wrong:\n%s", tc.witnessRig, body)
		}
	}
}

// The patrol's parked-bead scan settles landed beads and announces unverified
// ones; a second patrol after the mark is quiet.
func TestScanParkedBeads_NotifiesOncePerMR(t *testing.T) {
	rec := &noticeRecorder{}
	notice := rec.notice()
	notified := ""
	notice.mark = func(mrID string, at time.Time) error {
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
	first := scanParkedBeads(bd, dir, notice)
	if len(first.Errors) != 0 || len(first.Notified) != 1 || first.Notified[0] != "gt-parked1" || first.Checked != 1 {
		t.Fatalf("first scan = %+v, want gt-parked1 notified", first)
	}
	second := scanParkedBeads(bd, dir, notice)
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

// F-7: a rig name that could escape a path segment is refused before any work.
func TestScanParkedBeads_RefusesUnsafeRigName(t *testing.T) {
	bd, mock := mockBd(
		func(args []string) (string, error) { return "[]", nil },
		func(args []string) error { return nil },
	)
	for _, rig := range []string{"", "..", "../x", "a/b"} {
		if res := ScanParkedBeads(bd, t.TempDir(), rig, nil); len(res.Errors) != 1 {
			t.Errorf("rig %q: want a refusal, got %+v", rig, res)
		}
	}
	if len(mock.calls) != 0 {
		t.Fatalf("no bd call expected for an unsafe rig: %v", mock.calls)
	}
}

// Fail closed with the real wiring: no router means no mail and no mark.
func TestScanParkedBeads_NoRouterReportsAndDoesNotMark(t *testing.T) {
	old := witnessMergeQueue
	witnessMergeQueue = func(string) *beads.MergeQueue { return unverifiedQueue("") }
	t.Cleanup(func() { witnessMergeQueue = old })
	bd, mock := mockBd(
		func(args []string) (string, error) { return `[{"id":"gt-parked1","assignee":""}]`, nil },
		func(args []string) error { return nil },
	)
	res := ScanParkedBeads(bd, t.TempDir(), "testrig", nil)
	if len(res.Notified) != 0 || len(res.Errors) != 1 || !errors.Is(res.Errors[0], errNoMailRouter) {
		t.Fatalf("scan = %+v; want the send error", res)
	}
	for _, call := range mock.calls {
		if strings.HasPrefix(call, "update") {
			t.Fatalf("no mark expected after a failed send: %s", call)
		}
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

// gt up's orphan recovery uses the same parked-bead rule: with no router the
// notice fails closed and is reported.
func TestDetectOrphanedBeads_UnverifiedParkedBeadUsesSameRule(t *testing.T) {
	installFakeTmuxNoServer(t)
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
	res := DetectOrphanedBeads(bd, t.TempDir(), "testrig", nil)
	if len(res.Errors) != 1 || !errors.Is(res.Errors[0], errNoMailRouter) {
		t.Fatalf("errors = %v, want the failed notice", res.Errors)
	}
}

func TestHoldAgeAndShortDuration(t *testing.T) {
	n := (&noticeRecorder{}).notice()
	for _, tc := range []struct {
		mr   *beads.Issue
		want string
	}{
		{nil, "unknown (MR has no closed_at)"},
		{&beads.Issue{ClosedAt: "not a time"}, "unknown (MR closed_at is not a valid time)"},
		{&beads.Issue{ClosedAt: "2026-10-16T09:30:00Z"}, "30m (MR closed 2026-10-16T09:30:00Z)"},
		{&beads.Issue{ClosedAt: "2026-10-17T09:30:00Z"}, "0m (MR closed 2026-10-17T09:30:00Z)"}, // clock skew
	} {
		if got := n.holdAge(tc.mr); got != tc.want {
			t.Errorf("holdAge(%v) = %q, want %q", tc.mr, got, tc.want)
		}
	}
	if displayID("gt-1") != "gt-1" || displayID("a b") != "<invalid id>" {
		t.Error("displayID must pass safe IDs and hide others")
	}
}
