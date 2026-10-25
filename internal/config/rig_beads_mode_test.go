package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseRigBeadsMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "", want: RigBeadsModeAuto},
		{in: "auto", want: RigBeadsModeAuto},
		{in: "rig", want: RigBeadsModeRig},
		{in: " RIG ", want: RigBeadsModeRig},
		{in: "tracked", wantErr: true},
		{in: "mayor", wantErr: true},
	}
	for _, tt := range tests {
		got, err := ParseRigBeadsMode(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseRigBeadsMode(%q) = %q, want error", tt.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseRigBeadsMode(%q) unexpected error: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseRigBeadsMode(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func writeRigConfigJSON(t *testing.T, rigPath, content string) {
	t.Helper()
	if err := os.MkdirAll(rigPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rigPath, "config.json"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestRigBeadsModeOf(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string // "" = no config.json
		want    string
	}{
		{name: "no config.json", want: RigBeadsModeAuto},
		{name: "no beads block", content: `{"type":"rig","name":"r"}`, want: RigBeadsModeAuto},
		{name: "legacy beads without mode", content: `{"beads":{"prefix":"r"}}`, want: RigBeadsModeAuto},
		{name: "explicit auto", content: `{"beads":{"prefix":"r","mode":"auto"}}`, want: RigBeadsModeAuto},
		{name: "rig", content: `{"beads":{"prefix":"r","mode":"rig"}}`, want: RigBeadsModeRig},
		{name: "unknown value", content: `{"beads":{"prefix":"r","mode":"bogus"}}`, want: RigBeadsModeAuto},
		{name: "malformed json", content: `{"beads":`, want: RigBeadsModeAuto},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rigPath := filepath.Join(t.TempDir(), "rig")
			if tt.content != "" {
				writeRigConfigJSON(t, rigPath, tt.content)
			}
			if got := RigBeadsModeOf(rigPath); got != tt.want {
				t.Errorf("RigBeadsModeOf = %q, want %q", got, tt.want)
			}
			if got, want := RigUsesOwnBeads(rigPath), tt.want == RigBeadsModeRig; got != want {
				t.Errorf("RigUsesOwnBeads = %v, want %v", got, want)
			}
		})
	}
}

func TestBeadsConfigModeRoundTrip(t *testing.T) {
	t.Parallel()
	in := RigConfig{Type: "rig", Version: 1, Name: "r", Beads: &BeadsConfig{Prefix: "r", Mode: RigBeadsModeRig}}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out RigConfig
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Beads == nil || out.Beads.Mode != RigBeadsModeRig {
		t.Fatalf("mode lost in round trip: %s", data)
	}

	// Empty mode stays out of the JSON so legacy files are unchanged.
	legacy, err := json.Marshal(BeadsConfig{Prefix: "r"})
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(legacy, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["mode"]; ok {
		t.Fatalf("empty mode must be omitted, got %s", legacy)
	}
}
