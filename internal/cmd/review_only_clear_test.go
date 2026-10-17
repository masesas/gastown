package cmd

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
)

// gt-pd1c.75: gt bead clear-review-only is the one explicit way to drop the
// sticky review_only marker; it is audited and refused to agents other than
// the mayor.

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
		{"mayor role", []string{EnvGTRole, "mayor"}, true, "mayor"},
		{"mayor actor", []string{EnvGTRole, "mayor", "BD_ACTOR", "mayor"}, true, "mayor"},
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
				if !strings.Contains(err.Error(), "refusing to clear review_only") || !strings.Contains(err.Error(), "mayor") {
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
	updateErr  error
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
	if s.commentErr != nil {
		return s.commentErr
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
	s.issue.Description = *opts.Description
	return nil
}

var reviewOnlyClearNow = time.Date(2026, 10, 18, 9, 30, 0, 0, time.UTC)

func TestClearReviewOnlyMarkerRecordsAuditAndIsIdempotent(t *testing.T) {
	store := &fakeReviewOnlyStore{issue: &beads.Issue{ID: "gt-leg1", Description: "attached_at: 2026-10-17T00:00:00Z\nreview_only: true\n\nBody.\n```\nreview_only: true\n```"}}

	cleared, err := clearReviewOnlyMarker(store, "gt-leg1", "gastown/crew/max", "leg was mislabeled", reviewOnlyClearNow)
	if err != nil || !cleared {
		t.Fatalf("clear: cleared=%v err=%v", cleared, err)
	}
	if len(store.comments) != 1 {
		t.Fatalf("comments = %q, want one audit comment", store.comments)
	}
	for _, want := range []string{"review_only cleared", "gastown/crew/max", "2026-10-18T09:30:00Z", "leg was mislabeled"} {
		if !strings.Contains(store.comments[0], want) {
			t.Fatalf("audit comment %q lacks %q", store.comments[0], want)
		}
	}
	if len(store.updates) != 1 || beads.IsReviewOnly(&beads.Issue{Description: store.updates[0]}) {
		t.Fatalf("description still review-only: %q", store.updates)
	}
	if !strings.Contains(store.updates[0], "attached_at: 2026-10-17T00:00:00Z") || !strings.Contains(store.updates[0], "```\nreview_only: true\n```") {
		t.Fatalf("clear touched more than the marker: %q", store.updates[0])
	}

	cleared, err = clearReviewOnlyMarker(store, "gt-leg1", "gastown/crew/max", "again", reviewOnlyClearNow)
	if err != nil || cleared {
		t.Fatalf("second clear: cleared=%v err=%v, want a no-op", cleared, err)
	}
	if len(store.comments) != 1 || len(store.updates) != 1 {
		t.Fatalf("second clear wrote again: comments=%d updates=%d", len(store.comments), len(store.updates))
	}
}

func TestClearReviewOnlyMarkerNonCanonicalValue(t *testing.T) {
	store := &fakeReviewOnlyStore{issue: &beads.Issue{ID: "gt-leg1", Description: "review_only: maybe\n\nBody."}}
	cleared, err := clearReviewOnlyMarker(store, "gt-leg1", "mayor", "value was a typo", reviewOnlyClearNow)
	if err != nil || !cleared || len(store.updates) != 1 || store.updates[0] != "Body." {
		t.Fatalf("cleared=%v err=%v updates=%q", cleared, err, store.updates)
	}
}

func TestClearReviewOnlyMarkerFailsClosed(t *testing.T) {
	desc := "review_only: true\n\nBody."
	t.Run("show fails", func(t *testing.T) {
		store := &fakeReviewOnlyStore{issue: &beads.Issue{Description: desc}, showErr: errors.New("dolt down")}
		if cleared, err := clearReviewOnlyMarker(store, "gt-leg1", "mayor", "why", reviewOnlyClearNow); err == nil || cleared {
			t.Fatalf("cleared=%v err=%v, want an error", cleared, err)
		}
	})
	t.Run("audit comment fails: marker kept", func(t *testing.T) {
		store := &fakeReviewOnlyStore{issue: &beads.Issue{Description: desc}, commentErr: errors.New("comments down")}
		cleared, err := clearReviewOnlyMarker(store, "gt-leg1", "mayor", "why", reviewOnlyClearNow)
		if err == nil || cleared || len(store.updates) != 0 {
			t.Fatalf("cleared=%v err=%v updates=%q, want no write without audit", cleared, err, store.updates)
		}
		if !strings.Contains(err.Error(), "review_only was kept") {
			t.Fatalf("error must say the marker was kept: %v", err)
		}
	})
	t.Run("update fails", func(t *testing.T) {
		store := &fakeReviewOnlyStore{issue: &beads.Issue{Description: desc}, updateErr: errors.New("write failed")}
		cleared, err := clearReviewOnlyMarker(store, "gt-leg1", "mayor", "why", reviewOnlyClearNow)
		if err == nil || cleared {
			t.Fatalf("cleared=%v err=%v, want an error", cleared, err)
		}
		if !beads.IsReviewOnly(store.issue) {
			t.Fatal("a failed update must leave the bead review-only")
		}
	})
}

func TestRunBeadClearReviewOnlyRefusesBeforeAnyLookup(t *testing.T) {
	prev := beadClearReviewOnlyReason
	t.Cleanup(func() { beadClearReviewOnlyReason = prev })

	setReviewOnlyClearCaller(t, EnvGTRole, "gastown/polecats/nux", "BD_ACTOR", "gastown/polecats/nux")
	beadClearReviewOnlyReason = "my own leg"
	if err := runBeadClearReviewOnly(nil, []string{"gt-leg1"}); err == nil || !strings.Contains(err.Error(), "refusing to clear review_only") {
		t.Fatalf("polecat: err = %v, want a refusal", err)
	}

	setReviewOnlyClearCaller(t)
	beadClearReviewOnlyReason = "  "
	if err := runBeadClearReviewOnly(nil, []string{"gt-leg1"}); err == nil || !strings.Contains(err.Error(), "--reason") {
		t.Fatalf("no reason: err = %v, want --reason required", err)
	}

	beadClearReviewOnlyReason = "why"
	if err := runBeadClearReviewOnly(nil, []string{"--force"}); err == nil || !strings.Contains(err.Error(), "invalid bead ID") {
		t.Fatalf("flag-like ID: err = %v, want invalid bead ID", err)
	}
}

func TestRunBeadClearReviewOnlyClearsBead(t *testing.T) {
	_, _, descPath := setupMutableBDRawSlingTest(t, "attached_at: 2026-10-17T00:00:00Z\nreview_only: true\n\nKeep this body.")
	prev := beadClearReviewOnlyReason
	t.Cleanup(func() { beadClearReviewOnlyReason = prev })
	setReviewOnlyClearCaller(t, EnvGTRole, "mayor")
	beadClearReviewOnlyReason = "rollback left the marker"

	if err := runBeadClearReviewOnly(nil, []string{"gt-rawrollback"}); err != nil {
		t.Fatalf("runBeadClearReviewOnly: %v", err)
	}
	desc := readMutableBDDescription(t, descPath)
	if beads.IsReviewOnly(&beads.Issue{Description: desc}) || !strings.Contains(desc, "Keep this body.") || !strings.Contains(desc, "attached_at: 2026-10-17T00:00:00Z") {
		t.Fatalf("description after clear:\n%s", desc)
	}
	// Idempotent: a second run is a no-op, not an error.
	if err := runBeadClearReviewOnly(nil, []string{"gt-rawrollback"}); err != nil {
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
	got = reviewOnlyMarkerWarnings("gt-leg1", &beadInfo{Description: "Body.\n\nreview_only: true"})
	if len(got) != 1 || !strings.Contains(got[0], "outside the description header") || !strings.Contains(got[0], "--review-only") {
		t.Fatalf("ignored-line warning = %q", got)
	}
	if got := reviewOnlyMarkerWarnings("gt-leg1", nil); len(got) != 0 {
		t.Fatalf("nil info warned: %q", got)
	}
}
