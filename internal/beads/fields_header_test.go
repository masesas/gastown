package beads

import (
	"strings"
	"testing"
)

// Tests for gt-pd1c.75: attachment fields (review_only included) live in the
// description's header block only; review_only has canonical values only.

func TestAttachmentFieldsParsedOnlyInHeaderBlock(t *testing.T) {
	tests := []struct {
		name string
		desc string
		want bool
	}{
		{"header first line", "review_only: true\n\nBody.", true},
		{"header after other fields", "attached_molecule: gt-wisp\ndispatched_by: mayor/\nreview_only: true\n\nBody.", true},
		{"leading blank lines", "\n\nreview_only: true\n\nBody.", true},
		{"legacy multi-line args before marker", "attached_args: line one\nline two of the args\nreview_only: true\n\nBody.", true},
		{"body paragraph", "Review the brief.\n\nreview_only: true", false},
		{"prose first line", "Review the brief.\nreview_only: true", false},
		{"fenced code block in body", "attached_at: 2026-10-17T00:00:00Z\n\n```\nreview_only: true\n```", false},
		{"fence directly after header", "attached_at: 2026-10-17T00:00:00Z\n```yaml\nreview_only: true\n```", false},
		{"tilde fence", "~~~\nreview_only: true\n~~~", false},
		{"block quote", "> review_only: true", false},
		{"quote after header", "dispatched_by: mayor/\n> review_only: true", false},
		{"list item", "- review_only: true", false},
		{"heading", "# review_only: true", false},
		{"indented code in body", "Body.\n\n    review_only: true", false},
		{"ordered list", "1. review_only: true", false},
		{"ordered list after header", "dispatched_by: mayor/\n2) review_only: true", false},
		{"number is continuation text", "dispatched_by: mayor/\n2026\nreview_only: true", true},
		{"identifier key with digits", "x1: y\nreview_only: true", true},
		{"key is not an identifier", "@x: y\nreview_only: true", false},
		{"key starts with a digit", "1x: y\nreview_only: true", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsReviewOnly(&Issue{Description: tt.desc}); got != tt.want {
				t.Fatalf("IsReviewOnly(%q) = %v, want %v", tt.desc, got, tt.want)
			}
		})
	}
}

func TestParseAttachmentFieldsIgnoresBodyFields(t *testing.T) {
	desc := "attached_molecule: gt-wisp-header\n\nNotes:\n```\nattached_molecule: gt-wisp-body\nno_merge: true\n```"
	fields := ParseAttachmentFields(&Issue{Description: desc})
	if fields == nil || fields.AttachedMolecule != "gt-wisp-header" || fields.NoMerge {
		t.Fatalf("body fields leaked into the header: %+v", fields)
	}
	if got := ParseAttachmentFields(&Issue{Description: "Body only.\n\nno_merge: true"}); got != nil {
		t.Fatalf("description without header parsed fields: %+v", got)
	}
}

func TestSetAttachmentFieldsKeepsBodyVerbatim(t *testing.T) {
	body := "Example:\n```\nreview_only: true\nattached_molecule: gt-example\n```"
	issue := &Issue{Description: "attached_molecule: gt-old\n\n" + body}
	desc := SetAttachmentFields(issue, &AttachmentFields{AttachedMolecule: "gt-new"})
	if desc != "attached_molecule: gt-new\n\n"+body {
		t.Fatalf("SetAttachmentFields rewrote the body:\n%s", desc)
	}
	if IsReviewOnly(&Issue{Description: desc}) {
		t.Fatalf("body example marked the bead review-only:\n%s", desc)
	}
}

func TestFormatAttachmentFieldsKeepsOneLinePerField(t *testing.T) {
	fields := &AttachmentFields{AttachedArgs: "first line\nsecond line\r\nthird", DispatchedBy: "mayor/", ReviewOnly: true}
	desc := SetAttachmentFields(&Issue{Description: "Body."}, fields)
	if !strings.HasPrefix(desc, "attached_args: first line second line third\n") {
		t.Fatalf("multi-line value broke the header:\n%s", desc)
	}
	parsed := ParseAttachmentFields(&Issue{Description: desc})
	if parsed == nil || !parsed.ReviewOnly || parsed.DispatchedBy != "mayor/" {
		t.Fatalf("fields after a multi-line value were lost: %+v\n%s", parsed, desc)
	}
}

func TestReviewOnlyCanonicalValues(t *testing.T) {
	tests := []struct {
		value        string
		marked       bool
		nonCanonical string
	}{
		{"true", true, ""},
		{"TRUE", true, ""},
		{"false", false, ""},
		{"False", false, ""},
		{"no", true, "no"},
		{"off", true, "off"},
		{"0", true, "0"},
		{"yes", true, "yes"},
		{"maybe", true, "maybe"},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			status := ReviewOnlyStatusOf(&Issue{Description: "review_only: " + tt.value + "\n\nBody."})
			if status.Marked != tt.marked || status.NonCanonical != tt.nonCanonical {
				t.Fatalf("ReviewOnlyStatusOf(%q) = %+v, want marked=%v nonCanonical=%q", tt.value, status, tt.marked, tt.nonCanonical)
			}
		})
	}
}

func TestReviewOnlyCanonicalTrueWinsOverNonCanonical(t *testing.T) {
	status := ReviewOnlyStatusOf(&Issue{Description: "review_only: maybe\nreview_only: true"})
	if !status.Marked || status.NonCanonical != "" {
		t.Fatalf("status = %+v, want canonical true", status)
	}
}

func TestNonCanonicalReviewOnlyIsNotCanonicalizedByRewrites(t *testing.T) {
	issue := &Issue{Description: "attached_molecule: gt-old\nreview_only: off\n\nBody."}
	for name, fields := range map[string]*AttachmentFields{
		"detach (nil)":   nil,
		"new attachment": {AttachedMolecule: "gt-new"},
		"from parse":     ParseAttachmentFields(issue),
		"explicit true":  {AttachedMolecule: "gt-new", ReviewOnly: true},
	} {
		t.Run(name, func(t *testing.T) {
			desc := SetAttachmentFields(issue, fields)
			if !strings.Contains(desc, "review_only: off") || strings.Contains(desc, "review_only: true") {
				t.Fatalf("non-canonical value was rewritten:\n%s", desc)
			}
			if !IsReviewOnly(&Issue{Description: desc}) {
				t.Fatalf("non-canonical value stopped failing closed:\n%s", desc)
			}
		})
	}
}

func TestReviewOnlyStatusReportsIgnoredLines(t *testing.T) {
	status := ReviewOnlyStatusOf(&Issue{Description: "Body.\n\nreview_only: true\nother: x\n```\nreview-only: true\n```"})
	if status.Marked || status.IgnoredLines != 2 {
		t.Fatalf("status = %+v, want unmarked with 2 ignored lines", status)
	}
	if status := ReviewOnlyStatusOf(&Issue{Description: "review_only: true\n\nBody."}); status.IgnoredLines != 0 {
		t.Fatalf("header marker counted as ignored: %+v", status)
	}
}

func TestWithoutReviewOnlyMarker(t *testing.T) {
	tests := []struct {
		name string
		desc string
		want string
	}{
		{"marker with fields", "attached_molecule: gt-wisp\nreview_only: true\n\nBody.", "attached_molecule: gt-wisp\n\nBody."},
		{"marker only", "review_only: true\n\nBody.", "Body."},
		{"non-canonical marker", "review-only: maybe\n\nBody.", "Body."},
		{"body example kept", "review_only: true\n\n```\nreview_only: true\n```", "```\nreview_only: true\n```"},
		{"not review-only: unchanged", "attached_molecule: gt-wisp\n\nBody.", "attached_molecule: gt-wisp\n\nBody."},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WithoutReviewOnlyMarker(tt.desc)
			if got != tt.want {
				t.Fatalf("WithoutReviewOnlyMarker(%q) = %q, want %q", tt.desc, got, tt.want)
			}
			if IsReviewOnly(&Issue{Description: got}) {
				t.Fatalf("marker survived: %q", got)
			}
		})
	}
}

func TestWithReviewOnlyMarkerKeepsExistingHeaderFields(t *testing.T) {
	got := WithReviewOnlyMarker("attached_molecule: gt-wisp\n\nBody.")
	fields := ParseAttachmentFields(&Issue{Description: got})
	if fields == nil || !fields.ReviewOnly || fields.AttachedMolecule != "gt-wisp" {
		t.Fatalf("marking dropped header fields: %q (%+v)", got, fields)
	}
	if WithoutReviewOnlyMarker(got) != "attached_molecule: gt-wisp\n\nBody." {
		t.Fatalf("set + unset is not a round trip: %q", WithoutReviewOnlyMarker(got))
	}
}
