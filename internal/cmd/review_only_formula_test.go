package cmd

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/formula"
)

// gt-pd1c.69: convoy review legs carry review_only as a structured field on
// the leg bead itself, not only in prompt text or the first sling's flag.
func TestConvoyReviewLegBeadsCarryReviewOnlyField(t *testing.T) {
	tests := []struct {
		path       string
		reviewOnly bool
	}{
		{"../../docs/enh-masesas/assets/formulas/mol-contract-brief-review.formula.toml", true},
		{"../formula/formulas/mol-plan-review.formula.toml", true},
		{"../formula/formulas/mol-prd-review.formula.toml", true},
		{"../formula/formulas/code-review.formula.toml", false},
	}
	for _, tt := range tests {
		t.Run(filepath.Base(tt.path), func(t *testing.T) {
			f, err := formula.ParseFile(tt.path)
			if err != nil {
				t.Fatalf("ParseFile: %v", err)
			}
			if len(f.Legs) == 0 {
				t.Fatal("formula has no legs")
			}
			for _, leg := range f.Legs {
				desc := convoyLegBeadDescription(f, leg, leg.Description)
				if got := beads.IsReviewOnly(&beads.Issue{Description: desc}); got != tt.reviewOnly {
					t.Fatalf("leg %s bead review_only = %v, want %v:\n%s", leg.ID, got, tt.reviewOnly, desc)
				}
				if !strings.Contains(desc, strings.TrimSpace(leg.Description)) {
					t.Fatalf("leg %s description body lost:\n%s", leg.ID, desc)
				}
				args := buildConvoyLegSlingArgs("bead-1", "rig", desc, leg.Title, "", convoyLegReviewOnly(f, leg))
				if got := slices.Contains(args, "--review-only"); got != tt.reviewOnly {
					t.Fatalf("leg %s sling args --review-only = %v, want %v: %v", leg.ID, got, tt.reviewOnly, args)
				}
			}
		})
	}
}
