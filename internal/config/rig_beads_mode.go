package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Rig beads modes, persisted as beads.mode in <rig>/config.json (and mirrored
// in mayor/rigs.json). They decide where a rig keeps its beads database.
const (
	// RigBeadsModeAuto is the historical behaviour and the default: when the
	// rig's repository tracks .beads/, the rig uses that tracked directory in
	// the mayor clone (<rig>/.beads/redirect -> mayor/rig/.beads); otherwise
	// the rig gets a self-contained <rig>/.beads/.
	RigBeadsModeAuto = "auto"

	// RigBeadsModeRig always gives the rig a self-contained <rig>/.beads/,
	// even when the repository tracks its own .beads/. Gas Town then never
	// writes to, initializes, or redirects into the repository's tracked
	// .beads/ files. Use it for projects that track their own issues with bd.
	RigBeadsModeRig = "rig"
)

// ParseRigBeadsMode validates a user-supplied beads mode. The empty string
// means "not specified" and maps to RigBeadsModeAuto. Matching is
// case-insensitive and ignores surrounding whitespace.
func ParseRigBeadsMode(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", RigBeadsModeAuto:
		return RigBeadsModeAuto, nil
	case RigBeadsModeRig:
		return RigBeadsModeRig, nil
	default:
		return "", fmt.Errorf("invalid beads mode %q: must be %q or %q", s, RigBeadsModeAuto, RigBeadsModeRig)
	}
}

// RigBeadsModeOf returns the beads mode recorded in <rigPath>/config.json.
//
// A missing or unreadable config.json, a missing beads block, an empty mode or
// an unrecognised value all yield RigBeadsModeAuto, so rigs created before the
// field existed keep their behaviour. Values are validated when written
// (gt rig add --beads), not here.
func RigBeadsModeOf(rigPath string) string {
	if rigPath == "" {
		return RigBeadsModeAuto
	}
	data, err := os.ReadFile(filepath.Join(rigPath, "config.json")) //nolint:gosec // G304: path is constructed internally
	if err != nil {
		return RigBeadsModeAuto
	}
	var cfg struct {
		Beads *struct {
			Mode string `json:"mode"`
		} `json:"beads"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil || cfg.Beads == nil {
		return RigBeadsModeAuto
	}
	mode, err := ParseRigBeadsMode(cfg.Beads.Mode)
	if err != nil {
		return RigBeadsModeAuto
	}
	return mode
}

// RigUsesOwnBeads reports whether the rig at rigPath is in RigBeadsModeRig,
// i.e. must use its self-contained <rig>/.beads/ and never the repository's
// tracked mayor/rig/.beads.
func RigUsesOwnBeads(rigPath string) bool {
	return RigBeadsModeOf(rigPath) == RigBeadsModeRig
}
