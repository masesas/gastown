package beads

import (
	"strings"
	"testing"
)

func TestIsReviewOnly(t *testing.T) {
	tests := []struct {
		name  string
		issue *Issue
		want  bool
	}{
		{"nil issue", nil, false},
		{"empty description", &Issue{}, false},
		{"no marker", &Issue{Description: "attached_molecule: gt-wisp\n\nbody"}, false},
		{"marker false", &Issue{Description: "review_only: false"}, false},
		{"marker true", &Issue{Description: "review_only: true\n\nbody"}, true},
		{"dash alias", &Issue{Description: "review-only: TRUE"}, true},
		{"marker below body is ignored", &Issue{Description: "body\nreview_only: true"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsReviewOnly(tt.issue); got != tt.want {
				t.Fatalf("IsReviewOnly = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDetachDescriptionKeepsReviewOnlyMarker(t *testing.T) {
	issue := &Issue{Description: "attached_molecule: gt-wisp\nattached_at: 2026-01-01T00:00:00Z\nreview_only: true\n\nBody text."}
	desc := SetAttachmentFields(issue, nil) // what DetachMolecule writes
	fields := ParseAttachmentFields(&Issue{Description: desc})
	if fields == nil || !fields.ReviewOnly || fields.AttachedMolecule != "" || fields.AttachedAt != "" {
		t.Fatalf("detached description = %q, fields %+v", desc, fields)
	}
	if !strings.Contains(desc, "Body text.") {
		t.Fatalf("detach lost the body: %q", desc)
	}
}

func TestWithReviewOnlyMarker(t *testing.T) {
	if got := WithReviewOnlyMarker(""); got != "review_only: true" {
		t.Fatalf("empty description: got %q", got)
	}
	body := "Check the brief.\nmode: strict review\nconvoy: keep this prompt line"
	got := WithReviewOnlyMarker(body)
	if !IsReviewOnly(&Issue{Description: got}) {
		t.Fatalf("marker missing: %q", got)
	}
	if !strings.HasSuffix(got, body) {
		t.Fatalf("body was not kept verbatim: %q", got)
	}
	if again := WithReviewOnlyMarker(got); again != got {
		t.Fatalf("marking twice changed the description: %q", again)
	}
}

func TestReviewOnlyParsingFailsClosed(t *testing.T) {
	tests := []struct {
		name string
		desc string
		want bool
	}{
		{"true then false", "review_only: true\nreview_only: false", true},
		{"false then true", "review_only: false\nreview_only: true", true},
		{"true below body is ignored", "review_only: false\n\nbody\nreview_only: true", false},
		{"yes", "review_only: yes", true},
		{"one", "review_only: 1", true},
		{"garbage", "review_only: maybe", true},
		{"false", "review_only: false", false},
		{"FALSE", "review_only: FALSE", false},
		{"zero is not canonical: fails closed", "review_only: 0", true},
		{"no is not canonical: fails closed", "review_only: no", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsReviewOnly(&Issue{Description: tt.desc}); got != tt.want {
				t.Fatalf("IsReviewOnly(%q) = %v, want %v", tt.desc, got, tt.want)
			}
		})
	}
}

func TestWithReviewOnlyMarkerOverridesExplicitFalse(t *testing.T) {
	got := WithReviewOnlyMarker("Leg body.\nreview_only: false")
	if !IsReviewOnly(&Issue{Description: got}) {
		t.Fatalf("a later review_only: false line disabled the marker: %q", got)
	}
}

func TestSetAttachmentFieldsKeepsReviewOnlySticky(t *testing.T) {
	issue := &Issue{Description: "attached_molecule: gt-old\nreview_only: true\n\nBody."}
	for name, fields := range map[string]*AttachmentFields{
		"detach (nil)":          nil,
		"new attachment":        {AttachedMolecule: "gt-new", AttachedAt: "2026-10-17T00:00:00Z"},
		"explicit false marker": {ReviewOnly: false, NoMerge: true},
	} {
		t.Run(name, func(t *testing.T) {
			desc := SetAttachmentFields(issue, fields)
			if !IsReviewOnly(&Issue{Description: desc}) {
				t.Fatalf("SetAttachmentFields dropped review_only: %q", desc)
			}
			if strings.Contains(desc, "gt-old") {
				t.Fatalf("stale attachment kept: %q", desc)
			}
		})
	}
	if fields := (&AttachmentFields{AttachedMolecule: "gt-new"}); SetAttachmentFields(&Issue{Description: "Body."}, fields) != "attached_molecule: gt-new\n\nBody." || fields.ReviewOnly {
		t.Fatal("non-review bead gained review_only or input was mutated")
	}
}
