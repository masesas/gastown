package rig

import "testing"

func TestSameName(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"gastown", "gastown", true},
		{"GasTown", "gastown", true},
		{" gastown ", "gastown", true},
		{"gastown", "gastown2", false},
		{"", "", false},
	} {
		if got := SameName(tc.a, tc.b); got != tc.want {
			t.Errorf("SameName(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
