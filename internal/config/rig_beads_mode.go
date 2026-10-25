package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Rig beads modes, persisted as beads.mode in <rig>/config.json and mirrored
// in mayor/rigs.json. They decide where a rig keeps its beads database.
const (
	// RigBeadsModeAuto is the historical behaviour and the default: when the
	// rig's repository tracks .beads/, the rig uses that tracked directory in
	// the mayor clone (<rig>/.beads/redirect -> mayor/rig/.beads); otherwise
	// the rig gets a self-contained <rig>/.beads/. It is never written to disk
	// (see StoredRigBeadsMode), so auto rigs keep byte-identical config files.
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

// StoredRigBeadsMode returns the value to persist as beads.mode for a parsed
// mode: RigBeadsModeRig is stored, auto is stored as "" (field omitted).
func StoredRigBeadsMode(mode string) string {
	if mode == RigBeadsModeRig {
		return RigBeadsModeRig
	}
	return ""
}

// rigConfigModeState describes what <rig>/config.json says about beads.mode.
type rigConfigModeState int

const (
	rigConfigModeAbsent  rigConfigModeState = iota // no config.json
	rigConfigModeValid                             // parsed; mode valid or empty
	rigConfigModeInvalid                           // unreadable, unparsable, or unknown mode value
)

func readRigConfigMode(rigPath string) (string, rigConfigModeState) {
	data, err := os.ReadFile(filepath.Join(rigPath, "config.json")) //nolint:gosec // G304: path is constructed internally
	if os.IsNotExist(err) {
		return "", rigConfigModeAbsent
	}
	if err != nil {
		return "", rigConfigModeInvalid
	}
	var cfg struct {
		Beads *struct {
			Mode string `json:"mode"`
		} `json:"beads"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", rigConfigModeInvalid
	}
	if cfg.Beads == nil || strings.TrimSpace(cfg.Beads.Mode) == "" {
		return "", rigConfigModeValid
	}
	mode, err := ParseRigBeadsMode(cfg.Beads.Mode)
	if err != nil {
		return "", rigConfigModeInvalid
	}
	return mode, rigConfigModeValid
}

// readRegistryRigBeadsMode returns the parsed beads.mode recorded for the rig
// in <townRoot>/mayor/rigs.json (townRoot = parent of rigPath), or "" when the
// registry, the entry, or the field is missing or unusable.
func readRegistryRigBeadsMode(rigPath string) string {
	townRoot := filepath.Dir(rigPath)
	data, err := os.ReadFile(filepath.Join(townRoot, "mayor", "rigs.json")) //nolint:gosec // G304: path is constructed internally
	if err != nil {
		return ""
	}
	var reg struct {
		Rigs map[string]struct {
			Beads *struct {
				Mode string `json:"mode"`
			} `json:"beads"`
		} `json:"rigs"`
	}
	if err := json.Unmarshal(data, &reg); err != nil {
		return ""
	}
	entry, ok := reg.Rigs[filepath.Base(rigPath)]
	if !ok || entry.Beads == nil || strings.TrimSpace(entry.Beads.Mode) == "" {
		return ""
	}
	mode, err := ParseRigBeadsMode(entry.Beads.Mode)
	if err != nil {
		return ""
	}
	return mode
}

// RigBeadsModeOf returns the effective beads mode of the rig at rigPath
// (<townRoot>/<rig>). It fails closed toward RigBeadsModeRig, because "rig"
// only stops Gas Town from writing into the repository's tracked .beads/:
//
//   - RigBeadsModeRig when <rig>/config.json or the rig's mayor/rigs.json
//     entry records beads.mode=rig;
//   - RigBeadsModeRig when config.json exists but cannot be read or parsed, or
//     holds an unknown mode, unless mayor/rigs.json explicitly records auto;
//   - RigBeadsModeAuto otherwise (including rigs created before the field
//     existed: config.json and rigs.json both without a mode).
func RigBeadsModeOf(rigPath string) string {
	if rigPath == "" {
		return RigBeadsModeAuto
	}
	cfgMode, state := readRigConfigMode(rigPath)
	regMode := readRegistryRigBeadsMode(rigPath)
	switch {
	case cfgMode == RigBeadsModeRig, regMode == RigBeadsModeRig:
		return RigBeadsModeRig
	case state == rigConfigModeInvalid && regMode != RigBeadsModeAuto:
		return RigBeadsModeRig
	default:
		return RigBeadsModeAuto
	}
}

// RigUsesOwnBeads reports whether the rig at rigPath is in RigBeadsModeRig,
// i.e. must use its self-contained <rig>/.beads/ and never the repository's
// tracked mayor/rig/.beads.
func RigUsesOwnBeads(rigPath string) bool {
	return RigBeadsModeOf(rigPath) == RigBeadsModeRig
}
