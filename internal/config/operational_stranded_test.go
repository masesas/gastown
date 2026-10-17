package config

import (
	"testing"
	"time"
)

// gt-pd1c.78 tahap 2: the stranded-branch grace has a default and a floor.
func TestStrandedBranchGraceD(t *testing.T) {
	var nilWT *WitnessThresholds
	cases := []struct {
		wt   *WitnessThresholds
		want time.Duration
	}{
		{nilWT, DefaultWitnessStrandedBranchGrace},
		{&WitnessThresholds{}, DefaultWitnessStrandedBranchGrace},
		{&WitnessThresholds{StrandedBranchGrace: "garbage"}, DefaultWitnessStrandedBranchGrace},
		{&WitnessThresholds{StrandedBranchGrace: "-1h"}, DefaultWitnessStrandedBranchGrace},
		{&WitnessThresholds{StrandedBranchGrace: "1m"}, MinWitnessStrandedBranchGrace},
		{&WitnessThresholds{StrandedBranchGrace: "6h"}, 6 * time.Hour},
	}
	for _, c := range cases {
		if got := c.wt.StrandedBranchGraceD(); got != c.want {
			t.Errorf("%+v: got %v, want %v", c.wt, got, c.want)
		}
	}
	if DefaultWitnessStrandedBranchGrace != 2*time.Hour {
		t.Fatalf("default grace = %v, want 2h", DefaultWitnessStrandedBranchGrace)
	}
}
