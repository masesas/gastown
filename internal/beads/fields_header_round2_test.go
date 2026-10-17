package beads

import (
	"strings"
	"testing"
)

// Round 2 of gt-pd1c.75: attached_args is written as a JSON string; legacy
// continuation only for raw values, only to a later key of the old writer
// order, only onto a contiguous run of keys; strong keys start the header;
// review_only fails closed in any field paragraph.

func TestAttachedArgsJSONRoundTrip(t *testing.T) {
	args := "line one\n\n- bullet\nreview_only: true\n\"quoted\" <b>&"
	desc := FormatAttachmentFields(&AttachmentFields{AttachedArgs: args, DispatchedBy: "mayor/"})
	if strings.Count(desc, "\n") != 1 {
		t.Fatalf("args must stay on one line:\n%s", desc)
	}
	if !strings.Contains(desc, `<b>&`) {
		t.Fatalf("args JSON should not HTML-escape: %s", desc)
	}
	fields := ParseAttachmentFields(&Issue{Description: desc + "\n\nBody."})
	if fields == nil || fields.AttachedArgs != args || fields.ReviewOnly || fields.DispatchedBy != "mayor/" {
		t.Fatalf("round trip: %+v", fields)
	}
	if got := ParseAttachmentFields(&Issue{Description: "attached_args: plain legacy text"}); got == nil || got.AttachedArgs != "plain legacy text" {
		t.Fatalf("raw legacy value: %+v", got)
	}
}

func TestLegacyContinuationProbes(t *testing.T) {
	tests := []struct {
		name       string
		desc       string
		reviewOnly bool
		molecule   string
	}{
		// go M-A / sec M-A1: a key before attached_args in the old order is
		// not a continuation target; the body key is not read.
		{"earlier key in body is not a target", "attached_args: z\nattached_molecule: m1\n\nbody\nattached_molecule: m2", false, "m1"},
		// a run must be keys up to the blank line / end
		{"key followed by prose is not a target", "attached_args: z\n\nbody\nreview_only: true\nmore prose", false, ""},
		{"fenced example is not a target", "attached_args: z\n\n```\nreview_only: true\n```", false, ""},
		// sec M-A3: the real field after raw args text that itself holds
		// "mode: x" and a blank line
		{"review_only after a key-like args line", "attached_args: a\nmode: x\n\nmore\nreview_only: true", true, ""},
		{"JSON args never continue", "attached_args: \"z\"\n\nbody\nreview_only: true", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fields := ParseAttachmentFields(&Issue{Description: tt.desc})
			if got := IsReviewOnly(&Issue{Description: tt.desc}); got != tt.reviewOnly {
				t.Fatalf("IsReviewOnly = %v, want %v (%+v)", got, tt.reviewOnly, fields)
			}
			if tt.molecule != "" && (fields == nil || fields.AttachedMolecule != tt.molecule) {
				t.Fatalf("AttachedMolecule = %+v, want %q", fields, tt.molecule)
			}
		})
	}
}

// sec M-A2: an opening prose line on a weak key (mode, convoy) does not take
// the header; review_only fails closed in any field paragraph.
func TestWeakKeysDoNotStartTheHeader(t *testing.T) {
	tests := []struct {
		name string
		desc string
		want bool
	}{
		{"Mode prose above header", "Mode: fast please\nconvoy: the usual\n\nattached_molecule: gt-1\nreview_only: true\n\nBody.", true},
		{"convoy prose above header", "convoy: hq-cv-1 is related\n\nreview_only: true", true},
		{"review_only inside a weak-key paragraph", "Mode: fast please\nreview_only: true", true},
		{"quote stays ignored", "Mode: fast\n\n> review_only: true", false},
		{"list stays ignored", "dispatched_by: x\n\n- review_only: true", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsReviewOnly(&Issue{Description: tt.desc}); got != tt.want {
				t.Fatalf("IsReviewOnly(%q) = %v, want %v", tt.desc, got, tt.want)
			}
		})
	}
	fields := ParseAttachmentFields(&Issue{Description: "Mode: fast please\n\nattached_molecule: gt-1\nmode: ralph\nconvoy: hq-cv-1"})
	if fields == nil || fields.AttachedMolecule != "gt-1" || fields.Mode != "ralph" || fields.ConvoyID != "hq-cv-1" {
		t.Fatalf("weak keys inside the header must still be read: %+v", fields)
	}
}

func TestDuplicateHeaderKeysFirstWins(t *testing.T) {
	fields := ParseAttachmentFields(&Issue{Description: "attached_molecule: m1\nattached_molecule: m2\nno_merge: true\nno_merge: false\nreview_only: false\nreview_only: true"})
	if fields == nil || fields.AttachedMolecule != "m1" || !fields.NoMerge || !fields.ReviewOnly {
		t.Fatalf("duplicates: %+v", fields)
	}
}

func TestClearRemovesReviewOnlyFromEveryFieldParagraph(t *testing.T) {
	desc := "dispatched_by: mayor/\n\nBODY one\n\nreview_only: true\n\nBODY two"
	cleared := WithoutReviewOnlyMarker(desc)
	if IsReviewOnly(&Issue{Description: cleared}) || !strings.Contains(cleared, "BODY one") || !strings.Contains(cleared, "BODY two") {
		t.Fatalf("clear left the marker or dropped text:\n%s", cleared)
	}
}

// Set(Parse(D)) is stable after one pass and never drops a body line (lines
// marked BODY in the fixtures).
func TestSetParseIdempotentAndKeepsBody(t *testing.T) {
	fixtures := map[string]string{
		"new":               "attached_molecule: gt-1\nattached_args: \"do x\"\ndispatched_by: mayor/\nreview_only: true\n\nBODY a\n```\nBODY review_only: true\nattached_molecule: example\n```",
		"legacy multi-line": "attached_at: 2026-01-01T00:00:00Z\nattached_args: line one\n\n- BODY-ish args bullet\n# args heading\ndispatched_by: mayor/\nno_merge: true\nreview_only: true\n\nBODY a\nBODY b",
		"free text above":   "BODY intro\n\nattached_molecule: gt-1\nno_merge: true\n\nBODY after",
		"crlf":              "attached_molecule: gt-1\r\nreview_only: true\r\n\r\nBODY a\r\n```\r\nno_merge: true\r\n```",
		"args last":         "attached_at: 2026-01-01T00:00:00Z\nattached_args: z\n\nBODY a\nattached_molecule: m2\n\nBODY b",
		"reorder":           "review_only: true\nconvoy_id: hq-cv-1\nattached_molecule: gt-1\n\nBODY a",
		"duplicates":        "attached_molecule: m1\nattached_molecule: m2\n\nBODY a",
		"body field para":   "dispatched_by: mayor/\n\nBODY a\n\nreview_only: maybe\n\nBODY b",
		"probe M-A3":        "attached_args: a\nmode: x\n\nBODY more\nreview_only: true",
		"weak prose":        "BODY Mode: fast please\n\nattached_molecule: gt-1\n\nBODY z",
		"mode only":         "mode: ralph\n\nBODY a",
		"mode + convoy_id":  "mode: ralph\nconvoy_id: hq-cv-1\n\nBODY a",
	}
	for name, d := range fixtures {
		t.Run(name, func(t *testing.T) {
			setParse := func(desc string) string {
				issue := &Issue{Description: desc}
				return SetAttachmentFields(issue, ParseAttachmentFields(issue))
			}
			once := setParse(d)
			twice := setParse(once)
			if once != twice {
				t.Fatalf("not idempotent:\nonce:\n%s\ntwice:\n%s", once, twice)
			}
			if IsReviewOnly(&Issue{Description: d}) != IsReviewOnly(&Issue{Description: once}) {
				t.Fatalf("review_only changed:\n%s\n->\n%s", d, once)
			}
			for _, line := range strings.Split(d, "\n") {
				if strings.Contains(line, "BODY") && !strings.Contains(once, strings.TrimRight(line, "\r")) {
					t.Fatalf("body line %q dropped:\n%s", line, once)
				}
			}
		})
	}
}

// L-1: readiness's machine keys come from the shared attachment key list.
func TestMachineFieldKeysDerivedFromAttachmentKeys(t *testing.T) {
	for spelling := range attachmentFieldKeys {
		want := spelling != "mode" && spelling != "convoy"
		if got := isMachineFieldKey(spelling); got != want {
			t.Errorf("isMachineFieldKey(%q) = %v, want %v", spelling, got, want)
		}
	}
	if !isMachineFieldKey("workflow_target") || isMachineFieldKey("note") {
		t.Fatal("workflow_target must count, other keys must not")
	}
}

// Round 3 (L2): tooling may write only weak keys (mode, convoy). Without a
// strong-key paragraph, a first paragraph made only of field lines is the
// header.
func TestWeakKeyOnlyHeaderFallback(t *testing.T) {
	for name, fields := range map[string]*AttachmentFields{
		"mode only":        {Mode: "ralph"},
		"mode + convoy_id": {Mode: "ralph", ConvoyID: "hq-cv-1"},
	} {
		t.Run(name, func(t *testing.T) {
			desc := SetAttachmentFields(&Issue{Description: "Body."}, fields)
			parsed := ParseAttachmentFields(&Issue{Description: desc})
			if parsed == nil || parsed.Mode != "ralph" || parsed.ConvoyID != fields.ConvoyID {
				t.Fatalf("weak-key header lost: %q -> %+v", desc, parsed)
			}
		})
	}
	if got := ParseAttachmentFields(&Issue{Description: "convoy: hq-cv-1\n\nBody."}); got == nil || got.ConvoyID != "hq-cv-1" {
		t.Fatalf("convoy-only header: %+v", got)
	}
	// A strong header still wins over opening weak-key prose (M-A2).
	fields := ParseAttachmentFields(&Issue{Description: "Mode: fast please\n\nattached_molecule: gt-1\nreview_only: true"})
	if fields == nil || fields.Mode != "" || fields.AttachedMolecule != "gt-1" || !fields.ReviewOnly {
		t.Fatalf("strong header must win: %+v", fields)
	}
	// A first paragraph that is not only field lines is prose.
	if got := ParseAttachmentFields(&Issue{Description: "Mode: fast please\nand keep it short\n\nBody."}); got != nil {
		t.Fatalf("prose paragraph read as header: %+v", got)
	}
}

// Round 3 (L3): clearing repeats until no marker counts, keeping text.
func TestWithoutReviewOnlyMarkerIsComplete(t *testing.T) {
	desc := "Review-Only: TRUE\nfoo: bar\n\nattached_args: raw\n# H\nreview_only: true"
	cleared := WithoutReviewOnlyMarker(desc)
	if IsReviewOnly(&Issue{Description: cleared}) {
		t.Fatalf("marker survived the clear:\n%s", cleared)
	}
	for _, keep := range []string{"foo: bar", "# H"} {
		if !strings.Contains(cleared, keep) {
			t.Fatalf("clear dropped %q:\n%s", keep, cleared)
		}
	}
	if fields := ParseAttachmentFields(&Issue{Description: cleared}); fields == nil || fields.AttachedArgs != "raw" {
		t.Fatalf("clear lost attached_args: %+v\n%s", fields, cleared)
	}
}

// Round 3 (L1 b): a legacy raw attached_args that happens to be a valid JSON
// string is decoded (documented).
func TestLegacyArgsThatLookLikeJSONAreDecoded(t *testing.T) {
	fields := ParseAttachmentFields(&Issue{Description: "attached_args: \"hello\"\n\nBody.\nreview_only: true"})
	if fields == nil || fields.AttachedArgs != "hello" || fields.ReviewOnly {
		t.Fatalf("JSON-looking legacy args: %+v", fields)
	}
}
