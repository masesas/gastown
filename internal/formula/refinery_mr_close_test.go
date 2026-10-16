package formula

import (
	"strings"
	"testing"
)

// gt-pd1c.70: the refinery agent once pushed to the target and ran a bare
// `bd close` on the MR, leaving the source bead open so the convoy feeder
// re-dispatched finished work. merge-push must name gt mq post-merge as the
// only way to close the MR and forbid manual bd close.
func TestRefineryPatrolMergePushClosesMROnlyViaPostMerge(t *testing.T) {
	f := loadRefineryPatrolFormula(t)
	d := requireFormulaStep(t, f, "merge-push").Description
	for _, want := range []string{
		"**MR close is `gt mq post-merge` ONLY.**",
		"MUST be closed through `gt mq post-merge <rig> <mr-bead-id>`",
		"FORBIDDEN: `bd close`",
		"on the MR bead or\non the source issue",
		"run\n`gt mq post-merge <rig> <mr-bead-id>` now",
	} {
		if !strings.Contains(d, want) {
			t.Errorf("merge-push missing %q", want)
		}
	}
}
