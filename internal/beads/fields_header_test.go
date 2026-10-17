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
		{"free text paragraph above the header", "Review the brief.\n\nreview_only: true", true},
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
		{"paragraph starting with an unknown key", "x1: y\nreview_only: true", false},
		{"unknown-key paragraph then header paragraph", "workflow_target: rig\n\nreview_only: true\n\nBody.", true},
		{"only the first header paragraph counts", "dispatched_by: mayor/\n\nreview_only: true", false},
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
	if got := ParseAttachmentFields(&Issue{Description: "Body only.\n```\nno_merge: true\n```\n> review_only: true"}); got != nil {
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
		{"", true, ""},
		{"no", true, "no"},
		{"off", true, "off"},
		{"0", true, "0"},
		{"yes", true, "yes"},
		{"maybe", true, "maybe"},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			status := ReviewOnlyStatusOf(&Issue{Description: "review_only: " + tt.value + "\n\nBody."})
			wantNonCanonical := tt.value != "true" && tt.value != "TRUE" && tt.value != "false" && tt.value != "False"
			if status.Marked != tt.marked || status.NonCanonical != wantNonCanonical || status.Value != tt.nonCanonical {
				t.Fatalf("ReviewOnlyStatusOf(%q) = %+v, want marked=%v nonCanonical=%q", tt.value, status, tt.marked, tt.nonCanonical)
			}
		})
	}
}

func TestReviewOnlyCanonicalTrueWinsOverNonCanonical(t *testing.T) {
	status := ReviewOnlyStatusOf(&Issue{Description: "review_only: maybe\nreview_only: true"})
	if !status.Marked || status.NonCanonical {
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
	status := ReviewOnlyStatusOf(&Issue{Description: "Body.\n> review_only: true\nother: x\n```\nreview-only: true\n```"})
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

// Round 1 (H1): attached_args / formula_vars written by older gt as raw
// multi-line values. Their continuation runs to the next known attachment key,
// whatever Markdown it contains, so the fields after it still count.
func TestLegacyMultiLineValuesKeepLaterFields(t *testing.T) {
	desc := strings.Join([]string{
		"attached_at: 2026-01-01T00:00:00Z",
		"attached_args: line one",
		"",
		"- bullet",
		"# heading",
		"> quote",
		"| table |",
		"1. item",
		"```",
		"code",
		"```",
		"no_merge: true",
		"review_only: true",
		"mode: ralph",
		"convoy_id: hq-cv-1",
		"",
		"Body.",
	}, "\n")
	for name, d := range map[string]string{"LF": desc, "CRLF": strings.ReplaceAll(desc, "\n", "\r\n")} {
		t.Run(name, func(t *testing.T) {
			fields := ParseAttachmentFields(&Issue{Description: d})
			if fields == nil || !fields.ReviewOnly || !fields.NoMerge || fields.Mode != "ralph" || fields.ConvoyID != "hq-cv-1" || fields.AttachedArgs != "line one" {
				t.Fatalf("legacy fields lost: %+v", fields)
			}
			if !IsReviewOnly(&Issue{Description: d}) {
				t.Fatal("legacy review_only after multi-line args not read")
			}
		})
	}
	formulaVars := "formula_vars: a=1\n\nb=2\n- c=3\nreview_only: true\n\nBody."
	if fields := ParseAttachmentFields(&Issue{Description: formulaVars}); fields == nil || !fields.ReviewOnly || fields.FormulaVars != "a=1" {
		t.Fatalf("legacy formula_vars continuation: %+v", fields)
	}
}

func TestLegacyContinuationRewriteKeepsText(t *testing.T) {
	issue := &Issue{Description: "attached_args: line one\n\n- bullet\nreview_only: true\n\nBody."}
	desc := SetAttachmentFields(issue, nil)
	if !IsReviewOnly(&Issue{Description: desc}) || !strings.Contains(desc, "- bullet") || !strings.Contains(desc, "Body.") || strings.Contains(desc, "attached_args") {
		t.Fatalf("rewrite of a legacy description:\n%s", desc)
	}
}

func TestSingleLineFormulaVarsDoNotSwallowBody(t *testing.T) {
	desc := FormatAttachmentFields(&AttachmentFields{FormulaVars: "a=1\nb=2"}) + "\n\nBody.\n```\nreview_only: true\n```"
	if IsReviewOnly(&Issue{Description: desc}) {
		t.Fatalf("JSON formula_vars must not continue into the body:\n%s", desc)
	}
}

// Round 1 (MEDIUM-2): free text above the header does not hide the fields.
func TestHeaderAfterFreeText(t *testing.T) {
	desc := "Intro written above the fields.\n\n```\nattached_molecule: gt-example\n```\n\nattached_molecule: gt-wisp\nno_merge: true\nconvoy_id: hq-cv-1\n\nBody."
	fields := ParseAttachmentFields(&Issue{Description: desc})
	if fields == nil || fields.AttachedMolecule != "gt-wisp" || !fields.NoMerge || fields.ConvoyID != "hq-cv-1" {
		t.Fatalf("fields below free text lost: %+v", fields)
	}
	got := SetAttachmentFields(&Issue{Description: "Intro.\n\nattached_molecule: gt-old\n\nBody."}, &AttachmentFields{AttachedMolecule: "gt-new"})
	if got != "attached_molecule: gt-new\n\nIntro.\n\nBody." {
		t.Fatalf("rewrite with free text above = %q", got)
	}
}

func TestCRLFDescriptions(t *testing.T) {
	desc := "attached_molecule: gt-1\r\nreview_only: true\r\n\r\nBody.\r\n```\r\nno_merge: true\r\n```"
	fields := ParseAttachmentFields(&Issue{Description: desc})
	if fields == nil || !fields.ReviewOnly || fields.NoMerge || fields.AttachedMolecule != "gt-1" {
		t.Fatalf("CRLF parse: %+v", fields)
	}
	cleared := WithoutReviewOnlyMarker(desc)
	if IsReviewOnly(&Issue{Description: cleared}) || !strings.Contains(cleared, "```\r\nno_merge: true\r\n```") {
		t.Fatalf("CRLF clear: %q", cleared)
	}
}

// Round 1 (L3): an empty value is not canonical: it fails closed and is kept.
func TestEmptyReviewOnlyValueFailsClosedAndIsKept(t *testing.T) {
	issue := &Issue{Description: "review_only:\n\nBody."}
	if !IsReviewOnly(issue) {
		t.Fatal("empty review_only value must fail closed")
	}
	desc := SetAttachmentFields(issue, &AttachmentFields{AttachedMolecule: "gt-new"})
	if !strings.Contains(desc, "review_only:\n") || strings.Contains(desc, "review_only: true") || !IsReviewOnly(&Issue{Description: desc}) {
		t.Fatalf("empty value was canonicalized:\n%s", desc)
	}
}

func TestStripAttachmentFields(t *testing.T) {
	got := StripAttachmentFields("attached_molecule: gt-1\nno_merge: true\nreview_only: true\n\nBody.\n```\nattached_molecule: example\n```")
	if got != "Body.\n```\nattached_molecule: example\n```" {
		t.Fatalf("StripAttachmentFields = %q", got)
	}
}
