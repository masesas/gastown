package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/config"
)

// Convoy merge strategies, set with `gt sling --merge` / `gt convoy create --merge`
// and stored on the bead (merge_strategy) and the convoy (Merge:).
//
// They are NOT the rig's merge_queue.merge_strategy setting: that one controls
// how the refinery lands work that already passed its gates ("direct" merge vs
// "pr" through the VCS API). The convoy strategy controls who lands the work:
//
//	mr     (default) the polecat submits an MR; the refinery runs the gates and lands it
//	direct the polecat pushes to the target branch itself, skipping the refinery
//	local  nothing is pushed; the work stays on the feature branch
//
// The stored values are part of existing bead/convoy data and must not be renamed.
const (
	convoyMergeDirect = "direct"
	convoyMergeMR     = "mr"
	convoyMergeLocal  = "local"
)

// rigMergeQueueActive reports whether completed work on the rig lands through
// its merge queue (refinery + gates). The refinery itself has no off switch
// (it always runs with an enabled queue), so the queue counts as active unless
// <rig>/settings/config.json explicitly sets merge_queue.enabled=false.
// Missing, unreadable or invalid settings count as active: when in doubt,
// work goes through the gate (fail closed).
//
// Only the rig-local settings file counts, not the repo's
// .gastown/settings.json: polecats can commit to the repo, so they must not be
// able to switch their own gate off.
//
// An empty rigName means the work has no rig (town-level bead, non-rig
// target): there is no merge queue to bypass, so this reports false.
func rigMergeQueueActive(townRoot, rigName string) bool {
	if rigName == "" {
		return false
	}
	settings, err := config.LoadRigSettings(config.RigSettingsPath(filepath.Join(townRoot, rigName)))
	if err != nil || settings == nil {
		return true
	}
	return settings.MergeQueue.IsEnabled()
}

// validateConvoyMergeForRig rejects the direct convoy strategy for a rig whose
// merge queue is active. Every dispatch path that accepts --merge calls this
// before any side effect (polecat, convoy, hook, sling context, bead update).
func validateConvoyMergeForRig(townRoot, rigName, merge string) error {
	if merge != convoyMergeDirect || !rigMergeQueueActive(townRoot, rigName) {
		return nil
	}
	return fmt.Errorf("--merge=direct rejected: rig %q uses a merge queue; direct makes the polecat push "+
		"to the target branch itself, skipping the refinery and its test gate\n"+
		"Drop --merge (the merge queue is the default) or pass --merge=mr", rigName)
}

// validateConvoyMergeForBeads applies validateConvoyMergeForRig to the rig
// that owns each bead (resolved from its ID prefix). Beads without a rig
// (town-level) are skipped: they have no merge queue of their own.
func validateConvoyMergeForBeads(townRoot, merge string, beadIDs []string) error {
	if merge != convoyMergeDirect {
		return nil
	}
	for _, beadID := range beadIDs {
		if err := validateConvoyMergeForRig(townRoot, resolveRigForBead(townRoot, beadID), merge); err != nil {
			return fmt.Errorf("%s: %w", beadID, err)
		}
	}
	return nil
}

// effectiveDoneMergeStrategy decides how gt done lands completed work, from the
// convoy strategy stored at dispatch time and the rig's merge queue state.
// A direct strategy on a rig with an active merge queue (data written before
// gt sling rejected that combination) is redirected to the merge queue instead
// of failing, so the polecat's work is not stranded; redirected reports that.
// no_merge work always takes the mr path, whose no-merge step pushes the
// branch for review and closes the bead without an MR.
func effectiveDoneMergeStrategy(info *ConvoyInfo, source *beads.Issue, mergeQueueActive bool) (strategy string, redirected bool) {
	attachment := beads.ParseAttachmentFields(source)
	if (attachment != nil && strings.EqualFold(strings.TrimSpace(attachment.MergeStrategy), convoyMergeLocal)) ||
		(info != nil && info.MergeStrategy == convoyMergeLocal) {
		return convoyMergeLocal, false
	}
	if info == nil || (attachment != nil && attachment.NoMerge) {
		return convoyMergeMR, false
	}
	if info.MergeStrategy != convoyMergeDirect {
		return convoyMergeMR, false
	}
	if mergeQueueActive {
		return convoyMergeMR, true
	}
	return convoyMergeDirect, false
}
