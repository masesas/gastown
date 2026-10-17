package cmd

import (
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
)

// gt-pd1c.69 (pilot k-posin): review evidence written inline, as agents do
// ("FINDINGS: head_sha: <sha>. 1) ..."), must be recognised, and every
// instruction must show the one accepted comment format.

const evidenceHead = "b207b42de41a0f8f9b6e4560cf7a5e15243ce6eb"

func TestReviewEvidenceHeadSHAInline(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{"pilot inline with trailing dot", "FINDINGS: head_sha: " + evidenceHead + ". 1) brief ok", evidenceHead},
		{"equals with trailing comma", "FINDINGS: head_sha=" + evidenceHead + ", no gaps", evidenceHead},
		{"target_head_sha inline", "REPORT: reviewed target_head_sha: " + evidenceHead + ".", evidenceHead},
		{"upper-case hex", "REPORT: head_sha=" + strings.ToUpper(evidenceHead), evidenceHead},
		{"line-start head_sha", "REPORT: ok\nhead_sha: abc123", "abc123"},
		{"line-start head", "REPORT: ok\nhead: abc123", "abc123"},
		{"no token", "FINDINGS: looked ahead: " + evidenceHead, ""},
		{"token inside a word", "FINDINGS: myhead_sha=" + evidenceHead, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reviewEvidenceHeadSHA(tt.text); got != tt.want {
				t.Fatalf("reviewEvidenceHeadSHA(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func inlineEvidenceIssue(author, text string) *beads.Issue {
	return &beads.Issue{
		ID:          "gt-review",
		Description: "review_only: true\nattached_at: 2026-07-01T12:00:00Z\n",
		Assignee:    "posin/polecats/brahmin",
		Comments: []beads.Comment{{
			Author:    author,
			CreatedAt: "2026-07-01T12:05:00Z",
			Text:      text,
		}},
	}
}

func TestReviewOnlyCloseAcceptsInlineEvidence(t *testing.T) {
	issue := inlineEvidenceIssue("posin/polecats/brahmin", "FINDINGS: head_sha: "+evidenceHead+". 1) contract ok 2) no gaps")
	if reason, fatal := doneReviewOnlyCloseSkipReasonForHead(nil, issue.ID, issue, evidenceHead); reason != "" || fatal {
		t.Fatalf("inline evidence rejected: %q (fatal=%v)", reason, fatal)
	}
}

func TestReviewOnlyCloseRejectsInlineEvidenceForWrongHeadOrAuthor(t *testing.T) {
	wrongHead := inlineEvidenceIssue("posin/polecats/brahmin", "FINDINGS: head_sha: "+strings.Repeat("a", 40)+". ok")
	if reason, _ := doneReviewOnlyCloseSkipReasonForHead(nil, wrongHead.ID, wrongHead, evidenceHead); reason == "" {
		t.Fatal("inline evidence for another head was accepted")
	}
	wrongAuthor := inlineEvidenceIssue("posin/polecats/other", "FINDINGS: head_sha: "+evidenceHead+". ok")
	if reason, _ := doneReviewOnlyCloseSkipReasonForHead(nil, wrongAuthor.ID, wrongAuthor, evidenceHead); reason == "" {
		t.Fatal("inline evidence from a non-assignee was accepted")
	}
}

func TestReviewOnlyInstructionsShowTheAcceptedCommentFormat(t *testing.T) {
	want := `bd comments add gt-review "FINDINGS: head_sha=$(git rev-parse HEAD) `
	missing := inlineEvidenceIssue("posin/polecats/brahmin", "just chatting")
	reason, _ := doneReviewOnlyCloseSkipReasonForHead(nil, missing.ID, missing, evidenceHead)
	if !strings.Contains(reason, want) {
		t.Fatalf("missing-evidence reason does not show the comment template %q:\n%s", want, reason)
	}
	if err := reviewOnlyCommitRefusal("gt-review", "origin/main"); !strings.Contains(err.Error(), want) {
		t.Fatalf("commit refusal does not show the comment template %q:\n%v", want, err)
	}
	directive := captureStdout(t, func() { outputReviewOnlyDirective("gt-review") })
	if !strings.Contains(directive, want) {
		t.Fatalf("prime directive does not show the comment template %q:\n%s", want, directive)
	}

	// The template, filled in the way the shell would, is accepted evidence.
	filled := strings.Replace(reviewEvidenceCommentBody(), "$(git rev-parse HEAD)", evidenceHead, 1) + " brief is implementable"
	ok := inlineEvidenceIssue("posin/polecats/brahmin", filled)
	if reason, _ := doneReviewOnlyCloseSkipReasonForHead(nil, ok.ID, ok, evidenceHead); reason != "" {
		t.Fatalf("a comment written from the template was rejected: %q\n%s", reason, filled)
	}
}
