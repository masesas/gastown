package beads

import (
	"reflect"
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
		{"marker below body", &Issue{Description: "body\nreview_only: true"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsReviewOnly(tt.issue); got != tt.want {
				t.Fatalf("IsReviewOnly = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDetachedAttachmentFieldsKeepsOnlyReviewOnly(t *testing.T) {
	if got := DetachedAttachmentFields(nil); got != nil {
		t.Fatalf("nil fields: got %+v, want nil", got)
	}
	plain := &AttachmentFields{AttachedMolecule: "gt-wisp", NoMerge: true, ConvoyID: "hq-cv-1"}
	if got := DetachedAttachmentFields(plain); got != nil {
		t.Fatalf("non-review fields survived detach: %+v", got)
	}
	review := &AttachmentFields{
		AttachedMolecule: "gt-wisp",
		AttachedFormula:  "mol-polecat-work",
		AttachedAt:       "2026-01-01T00:00:00Z",
		DispatchedBy:     "mayor/",
		ReviewOnly:       true,
	}
	got := DetachedAttachmentFields(review)
	if got == nil || !reflect.DeepEqual(*got, AttachmentFields{ReviewOnly: true}) {
		t.Fatalf("DetachedAttachmentFields = %+v, want only ReviewOnly", got)
	}
	if review.AttachedMolecule != "gt-wisp" {
		t.Fatal("DetachedAttachmentFields mutated its input")
	}
}

func TestDetachDescriptionKeepsReviewOnlyMarker(t *testing.T) {
	issue := &Issue{Description: "attached_molecule: gt-wisp\nattached_at: 2026-01-01T00:00:00Z\nreview_only: true\n\nBody text."}
	desc := SetAttachmentFields(issue, DetachedAttachmentFields(ParseAttachmentFields(issue)))
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
