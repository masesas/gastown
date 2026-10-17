package cmd

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
)

// gt-pd1c.75: gt bead clear-review-only is the one explicit way to drop the
// sticky review_only marker; it is audited, needs a human at a terminal and is
// refused to every agent, the mayor included (round 1, M1).

func setReviewOnlyClearCaller(t *testing.T, kv ...string) {
	t.Helper()
	for _, k := range agentSessionEnvVars {
		t.Setenv(k, "")
	}
	for i := 0; i+1 < len(kv); i += 2 {
		t.Setenv(kv[i], kv[i+1])
	}
}

func TestReviewOnlyClearActor(t *testing.T) {
	cases := []struct {
		name      string
		env       []string
		ok        bool
		wantActor string
	}{
		{"human shell", nil, true, ""},
		{"overseer", []string{"BD_ACTOR", "overseer"}, true, "overseer"},
		{"crew", []string{EnvGTRole, "gastown/crew/max", "BD_ACTOR", "gastown/crew/max"}, true, "gastown/crew/max"},
		{"mayor role", []string{EnvGTRole, "mayor"}, false, ""},
		{"mayor actor", []string{EnvGTRole, "mayor", "BD_ACTOR", "mayor"}, false, ""},
		{"actor with control characters", []string{"BD_ACTOR", "gastown/crew/max\x1b[31m", "GT_CREW", "max"}, false, ""},
		{"polecat", []string{EnvGTRole, "gastown/polecats/nux", "BD_ACTOR", "gastown/polecats/nux"}, false, ""},
		{"polecat actor only", []string{"BD_ACTOR", "gastown/polecats/nux"}, false, ""},
		{"polecat env", []string{"GT_POLECAT", "nux"}, false, ""},
		{"crew role with polecat actor", []string{EnvGTRole, "gastown/crew/max", "BD_ACTOR", "gastown/polecats/nux"}, false, ""},
		{"mayor role inside a polecat session", []string{EnvGTRole, "mayor", "GT_POLECAT", "nux"}, false, ""},
		{"witness", []string{EnvGTRole, "gastown/witness"}, false, ""},
		{"refinery", []string{EnvGTRole, "gastown/refinery"}, false, ""},
		{"deacon", []string{EnvGTRole, "deacon"}, false, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setReviewOnlyClearCaller(t, c.env...)
			actor, err := reviewOnlyClearActor()
			if (err == nil) != c.ok {
				t.Fatalf("err = %v, want ok=%v", err, c.ok)
			}
			if err != nil {
				if !strings.Contains(err.Error(), "refusing to clear review_only") || !strings.Contains(err.Error(), "only a human") {
					t.Fatalf("refusal must name the action and who may do it: %v", err)
				}
				return
			}
			if actor == "" || (c.wantActor != "" && actor != c.wantActor) {
				t.Fatalf("actor = %q, want %q", actor, c.wantActor)
			}
		})
	}
}

// fakeReviewOnlyStore records what clearReviewOnlyMarker does to a bead.
type fakeReviewOnlyStore struct {
	issue      *beads.Issue
	showErr    error
	commentErr error
	// failCommentN fails only the Nth AddComment call (1-based) when > 0.
	failCommentN int
	commentCalls int
	updateErr    error
	// updateNoop accepts the update but leaves the bead unchanged.
	updateNoop bool
	comments   []string
	updates    []string
}

func (s *fakeReviewOnlyStore) Show(id string) (*beads.Issue, error) {
	if s.showErr != nil {
		return nil, s.showErr
	}
	copied := *s.issue
	return &copied, nil
}

func (s *fakeReviewOnlyStore) AddComment(id, comment string) error {
	s.commentCalls++
	if s.commentErr != nil {
		return s.commentErr
	}
	if s.failCommentN == s.commentCalls {
		return errors.New("comment write failed")
	}
	s.comments = append(s.comments, comment)
	return nil
}

func (s *fakeReviewOnlyStore) Update(id string, opts beads.UpdateOptions) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	if opts.Description == nil {
		return errors.New("update without description")
	}
	s.updates = append(s.updates, *opts.Description)
	if !s.updateNoop {
		s.issue.Description = *opts.Description
	}
	return nil
}

var reviewOnlyClearNow = time.Date(2026, 10, 18, 9, 30, 0, 0, time.UTC)

func TestClearReviewOnlyMarkerRecordsAuditAndIsIdempotent(t *testing.T) {
	store := &fakeReviewOnlyStore{issue: &beads.Issue{ID: "gt-leg1", Description: "attached_at: 2026-10-17T00:00:00Z\nreview_only: true\n\nBody.\n```\nreview_only: true\n```"}}

	outcome, err := clearReviewOnlyMarker(store, "gt-leg1", "gastown/crew/max", "leg was mislabeled", reviewOnlyClearNow)
	if err != nil || !outcome.Cleared || outcome.ConfirmErr != nil {
		t.Fatalf("clear: outcome=%+v err=%v", outcome, err)
	}
	if len(store.comments) != 2 {
		t.Fatalf("comments = %q, want request + confirmation", store.comments)
	}
	for i, prefix := range []string{"review_only clear requested by ", "review_only cleared by "} {
		if !strings.HasPrefix(store.comments[i], prefix) {
			t.Fatalf("comment %d = %q, want prefix %q", i, store.comments[i], prefix)
		}
		for _, want := range []string{"gastown/crew/max", "2026-10-18T09:30:00Z", "leg was mislabeled"} {
			if !strings.Contains(store.comments[i], want) {
				t.Fatalf("audit comment %q lacks %q", store.comments[i], want)
			}
		}
	}
	if len(store.updates) != 1 || beads.IsReviewOnly(&beads.Issue{Description: store.updates[0]}) {
		t.Fatalf("description still review-only: %q", store.updates)
	}
	if !strings.Contains(store.updates[0], "attached_at: 2026-10-17T00:00:00Z") || !strings.Contains(store.updates[0], "```\nreview_only: true\n```") {
		t.Fatalf("clear touched more than the marker: %q", store.updates[0])
	}

	outcome, err = clearReviewOnlyMarker(store, "gt-leg1", "gastown/crew/max", "again", reviewOnlyClearNow)
	if err != nil || outcome.Cleared {
		t.Fatalf("second clear: outcome=%+v err=%v, want a no-op", outcome, err)
	}
	if len(store.comments) != 2 || len(store.updates) != 1 {
		t.Fatalf("second clear wrote again: comments=%d updates=%d", len(store.comments), len(store.updates))
	}
}

func TestClearReviewOnlyMarkerNonCanonicalValue(t *testing.T) {
	store := &fakeReviewOnlyStore{issue: &beads.Issue{ID: "gt-leg1", Description: "review_only: maybe\n\nBody."}}
	outcome, err := clearReviewOnlyMarker(store, "gt-leg1", "human:me", "value was a typo", reviewOnlyClearNow)
	if err != nil || !outcome.Cleared || len(store.updates) != 1 || store.updates[0] != "Body." {
		t.Fatalf("outcome=%+v err=%v updates=%q", outcome, err, store.updates)
	}
}

func TestClearReviewOnlyMarkerFailsClosed(t *testing.T) {
	desc := "review_only: true\n\nBody."
	t.Run("show fails", func(t *testing.T) {
		store := &fakeReviewOnlyStore{issue: &beads.Issue{Description: desc}, showErr: errors.New("dolt down")}
		if outcome, err := clearReviewOnlyMarker(store, "gt-leg1", "human:me", "why", reviewOnlyClearNow); err == nil || outcome.Cleared {
			t.Fatalf("outcome=%+v err=%v, want an error", outcome, err)
		}
	})
	t.Run("audit comment fails: marker kept", func(t *testing.T) {
		store := &fakeReviewOnlyStore{issue: &beads.Issue{Description: desc}, commentErr: errors.New("comments down")}
		outcome, err := clearReviewOnlyMarker(store, "gt-leg1", "human:me", "why", reviewOnlyClearNow)
		if err == nil || outcome.Cleared || len(store.updates) != 0 {
			t.Fatalf("outcome=%+v err=%v updates=%q, want no write without audit", outcome, err, store.updates)
		}
		if !strings.Contains(err.Error(), "review_only was kept") {
			t.Fatalf("error must say the marker was kept: %v", err)
		}
	})
	t.Run("update fails", func(t *testing.T) {
		store := &fakeReviewOnlyStore{issue: &beads.Issue{Description: desc}, updateErr: errors.New("write failed")}
		outcome, err := clearReviewOnlyMarker(store, "gt-leg1", "human:me", "why", reviewOnlyClearNow)
		if err == nil || outcome.Cleared {
			t.Fatalf("outcome=%+v err=%v, want an error", outcome, err)
		}
		if !beads.IsReviewOnly(store.issue) {
			t.Fatal("a failed update must leave the bead review-only")
		}
		if len(store.comments) != 1 {
			t.Fatalf("comments = %q, want only the request", store.comments)
		}
	})
	t.Run("update not visible on re-read", func(t *testing.T) {
		store := &fakeReviewOnlyStore{issue: &beads.Issue{Description: desc}, updateNoop: true}
		outcome, err := clearReviewOnlyMarker(store, "gt-leg1", "human:me", "why", reviewOnlyClearNow)
		if err == nil || outcome.Cleared || len(store.comments) != 1 {
			t.Fatalf("outcome=%+v err=%v comments=%q, want an error and no confirmation", outcome, err, store.comments)
		}
	})
	t.Run("confirmation comment fails", func(t *testing.T) {
		store := &fakeReviewOnlyStore{issue: &beads.Issue{Description: desc}, failCommentN: 2}
		outcome, err := clearReviewOnlyMarker(store, "gt-leg1", "human:me", "why", reviewOnlyClearNow)
		if err != nil || !outcome.Cleared || outcome.ConfirmErr == nil {
			t.Fatalf("outcome=%+v err=%v, want cleared with a confirmation error", outcome, err)
		}
	})
}

// testClearDeps returns clear-review-only deps with a scripted confirmation:
// confirmErr (nil = the human typed the ID) and the expected value it got.
func testClearDeps(confirmErr error, gotExpected *string) clearReviewOnlyDeps {
	deps := defaultClearReviewOnlyDeps()
	deps.confirm = func(action, prompt, expected string) error {
		if gotExpected != nil {
			*gotExpected = expected
		}
		return confirmErr
	}
	deps.now = func() time.Time { return reviewOnlyClearNow }
	return deps
}

func TestNormalizeClearReason(t *testing.T) {
	if got, err := normalizeClearReason("  leg was\nmislabeled  "); err != nil || got != "leg was mislabeled" {
		t.Fatalf("got %q, %v", got, err)
	}
	if got, err := normalizeClearReason("see https://u:secret@host/x"); err != nil || strings.Contains(got, "secret") {
		t.Fatalf("credentials kept: %q, %v", got, err)
	}
	for name, raw := range map[string]string{
		"empty":   "  \n ",
		"control": "why\x1b[31m",
		"nul":     "why\x00",
		"long":    strings.Repeat("x", maxClearReasonLen+1),
	} {
		if _, err := normalizeClearReason(raw); err == nil {
			t.Errorf("%s: accepted %q", name, raw)
		}
	}
}

func TestRunBeadClearReviewOnlyRefusesBeforeAnyLookup(t *testing.T) {
	failIfUsed := testClearDeps(nil, nil)
	failIfUsed.storeFor = func(string, string) reviewOnlyClearStore {
		t.Fatal("store used before the checks passed")
		return nil
	}

	setReviewOnlyClearCaller(t, EnvGTRole, "gastown/polecats/nux", "BD_ACTOR", "gastown/polecats/nux")
	if err := clearReviewOnlyCommand("gt-leg1", "my own leg", failIfUsed); err == nil || !strings.Contains(err.Error(), "refusing to clear review_only") {
		t.Fatalf("polecat: err = %v, want a refusal", err)
	}

	setReviewOnlyClearCaller(t)
	if err := clearReviewOnlyCommand("gt-leg1", "  ", failIfUsed); err == nil || !strings.Contains(err.Error(), "--reason") {
		t.Fatalf("no reason: err = %v, want --reason required", err)
	}
	for _, id := range []string{"--force", "gt-a/b", "gt a", "../gt-a"} {
		if err := clearReviewOnlyCommand(id, "why", failIfUsed); err == nil || !strings.Contains(err.Error(), "invalid bead ID") {
			t.Fatalf("ID %q: err = %v, want invalid bead ID", id, err)
		}
	}

	setReviewOnlyClearCaller(t, EnvGTRole, "mayor")
	if err := clearReviewOnlyCommand("gt-leg1", "why", failIfUsed); err == nil || !strings.Contains(err.Error(), "refusing to clear review_only") {
		t.Fatalf("mayor: err = %v, want a refusal", err)
	}
}

func TestDefaultClearDepsUseHumanConfirmation(t *testing.T) {
	deps := defaultClearReviewOnlyDeps()
	if deps.confirm == nil || deps.storeFor == nil || deps.now == nil {
		t.Fatalf("default deps incomplete: %+v", deps)
	}
	// Without a TTY the default confirmation refuses (no bypass).
	prevTTY := humanConfirmTTY
	humanConfirmTTY = func() bool { return false }
	t.Cleanup(func() { humanConfirmTTY = prevTTY })
	if err := deps.confirm("clearing review_only on gt-leg1", "prompt", "gt-leg1"); !errors.Is(err, errNotInteractive) {
		t.Fatalf("default confirm without TTY: %v, want errNotInteractive", err)
	}
}

func TestRunBeadClearReviewOnlyNeedsConfirmation(t *testing.T) {
	_, _, descPath := setupMutableBDRawSlingTest(t, "review_only: true\n\nKeep this body.")
	setReviewOnlyClearCaller(t)

	var expected string
	if err := clearReviewOnlyCommand("gt-rawrollback", "why", testClearDeps(errNotInteractive, &expected)); !errors.Is(err, errNotInteractive) {
		t.Fatalf("refused confirmation: err = %v", err)
	}
	if expected != "gt-rawrollback" {
		t.Fatalf("confirmation must ask for the bead ID, got %q", expected)
	}
	if !beads.IsReviewOnly(&beads.Issue{Description: readMutableBDDescription(t, descPath)}) {
		t.Fatal("a refused confirmation changed the bead")
	}
}

func TestRunBeadClearReviewOnlyClearsBead(t *testing.T) {
	_, _, descPath := setupMutableBDRawSlingTest(t, "attached_at: 2026-10-17T00:00:00Z\nreview_only: true\n\nKeep this body.")
	setReviewOnlyClearCaller(t, EnvGTRole, "gastown/crew/max", "BD_ACTOR", "gastown/crew/max")

	if err := clearReviewOnlyCommand("gt-rawrollback", "rollback left the marker", testClearDeps(nil, nil)); err != nil {
		t.Fatalf("clearReviewOnlyCommand: %v", err)
	}
	desc := readMutableBDDescription(t, descPath)
	if beads.IsReviewOnly(&beads.Issue{Description: desc}) || !strings.Contains(desc, "Keep this body.") || !strings.Contains(desc, "attached_at: 2026-10-17T00:00:00Z") {
		t.Fatalf("description after clear:\n%s", desc)
	}
	// Idempotent: a second run is a no-op, not an error, and asks nothing.
	if err := clearReviewOnlyCommand("gt-rawrollback", "again", testClearDeps(errors.New("must not prompt"), nil)); err != nil {
		t.Fatalf("second run: %v", err)
	}
}

func TestReviewOnlyMarkerWarnings(t *testing.T) {
	if got := reviewOnlyMarkerWarnings("gt-leg1", &beadInfo{Description: "review_only: true\n\nBody."}); len(got) != 0 {
		t.Fatalf("canonical marker warned: %q", got)
	}
	got := reviewOnlyMarkerWarnings("gt-leg1", &beadInfo{Description: "review_only: off\n\nBody."})
	if len(got) != 1 || !strings.Contains(got[0], `"off"`) || !strings.Contains(got[0], "fail closed") || !strings.Contains(got[0], "clear-review-only") {
		t.Fatalf("non-canonical warning = %q", got)
	}
	got = reviewOnlyMarkerWarnings("gt-leg1", &beadInfo{Description: "review_only:\n\nBody."})
	if len(got) != 1 || !strings.Contains(got[0], `""`) {
		t.Fatalf("non-canonical warning = %q", got)
	}
	got = reviewOnlyMarkerWarnings("gt-leg1", &beadInfo{Description: "Body.\n```\nreview_only: true\n```"})
	if len(got) != 1 || !strings.Contains(got[0], "outside the description header") || !strings.Contains(got[0], "--review-only") {
		t.Fatalf("ignored-line warning = %q", got)
	}
	if got := reviewOnlyMarkerWarnings("gt-leg1", nil); len(got) != 0 {
		t.Fatalf("nil info warned: %q", got)
	}
}

// Round 1 (H1 safety net): gt done and gt mq submit decide through
// findReviewOnlyWork, which warns about an ignored marker like gt sling.
func TestFindReviewOnlyWorkWarnsAboutIgnoredMarker(t *testing.T) {
	out := captureStderr(t, func() {
		id, err := findReviewOnlyWork(t.TempDir(), "", reviewOnlyCandidate{ID: "gt-leg1", Issue: &beads.Issue{ID: "gt-leg1", Description: "Body.\n```\nreview_only: true\n```"}})
		if err != nil || id != "" {
			t.Fatalf("id=%q err=%v, want not review-only", id, err)
		}
	})
	if !strings.Contains(out, "outside the description header") {
		t.Fatalf("no warning about the ignored marker: %q", out)
	}
}
